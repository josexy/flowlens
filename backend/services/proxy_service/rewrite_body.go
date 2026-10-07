package proxyservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	bodyspool "github.com/josexy/flowlens/backend/pkg/body_spool"
	rewriteservice "github.com/josexy/flowlens/backend/services/rewrite_service"
	"github.com/josexy/mitmproxy-go/v2"
	http "github.com/josexy/xhttp"
)

var rewriteBodySlots = make(chan struct{}, 4)

const rewriteBodyTimeout = 10 * time.Second

type rewriteBodyPhase struct {
	parent              context.Context
	ctx                 context.Context
	cancel              context.CancelFunc
	request             *http.Request
	acquired            bool
	originalContentType string
	abort               context.CancelFunc
}

func newRewriteBodyPhase(ctx context.Context, req *http.Request, abort context.CancelFunc) *rewriteBodyPhase {
	return &rewriteBodyPhase{parent: ctx, request: req, abort: abort}
}
func (p *rewriteBodyPhase) close() {
	if p.cancel != nil {
		p.cancel()
	}
	if p.acquired {
		<-rewriteBodySlots
	}
}
func (p *rewriteBodyPhase) acquire() error {
	if p.acquired {
		return p.ctx.Err()
	}
	p.ctx, p.cancel = context.WithTimeout(p.parent, rewriteBodyTimeout)
	select {
	case rewriteBodySlots <- struct{}{}:
		p.acquired = true
		return p.ctx.Err()
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}

type rewriteSpoolBody struct {
	io.ReadCloser
	store *bodyspool.Store
	once  sync.Once
}

func (b *rewriteSpoolBody) Close() error {
	var err error
	b.once.Do(func() {
		err = b.ReadCloser.Close()
		if e := b.store.Close(); err == nil {
			err = e
		}
	})
	return err
}
func boundedRewritePayload(ctx context.Context, store *bodyspool.Store, reader io.Reader) (bodyspool.Payload, error) {
	payload, err := store.Read(ctx, io.LimitReader(reader, rewriteservice.MaxBodyBytes+1), bodyspool.DefaultInlineLimit)
	if err != nil {
		return bodyspool.Payload{}, err
	}
	if payload.Size() > rewriteservice.MaxBodyBytes {
		return bodyspool.Payload{}, errors.New("rewrite body exceeds 8 MiB")
	}
	return payload, nil
}
func rewritePayloadBytes(payload bodyspool.Payload) ([]byte, error) {
	reader, err := payload.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
func validateRewriteTextType(contentType string) error {
	if contentType == "" {
		return nil
	}
	typ, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return errors.New("invalid body content type")
	}
	typ = strings.ToLower(typ)
	if charset := strings.ToLower(params["charset"]); charset != "" && charset != "utf-8" && charset != "utf8" && charset != "us-ascii" {
		return errors.New("body charset is not UTF-8")
	}
	if typ == "text/event-stream" || strings.Contains(typ, "ndjson") || strings.Contains(typ, "json-seq") {
		return errors.New("continuous streaming bodies cannot be rewritten")
	}
	if strings.HasPrefix(typ, "text/") || strings.HasSuffix(typ, "+json") || strings.HasSuffix(typ, "+xml") || typ == "application/json" || typ == "application/xml" || typ == "application/javascript" || typ == "application/x-www-form-urlencoded" || typ == "application/graphql" {
		return nil
	}
	return errors.New("binary bodies cannot be rewritten")
}
func (p *rewriteBodyPhase) rewrite(body io.ReadCloser, length int64, headers http.Header, match rewriteservice.Match) (result io.ReadCloser, size int64, changed bool, err error) {
	// Always retain a closeable reader on errors, allowing the interceptor's
	// terminal path to clean up earlier transformations and original streams.
	result = body
	size = length
	if err = validateRewriteTextType(p.originalContentType); err != nil {
		return
	}
	if err = validateRewriteTextType(headers.Get("Content-Type")); err != nil {
		return
	}
	encoding := headers.Get("Content-Encoding")
	if hasUnsupportedContentEncoding(encoding) {
		err = errors.New("unsupported body content encoding")
		return
	}
	if length > rewriteservice.MaxBodyBytes {
		err = errors.New("rewrite body exceeds 8 MiB")
		return
	}
	if err = p.acquire(); err != nil {
		return
	}
	if body == nil {
		body = http.NoBody
	}
	store, storeErr := bodyspool.New("flowlens-rewrite-body-")
	if storeErr != nil {
		err = storeErr
		return
	}
	handoff := false
	defer func() {
		if !handoff {
			_ = store.Close()
		}
	}()
	// Timeout cancellation interrupts the owned stream. Incoming HTTP/1 reads
	// additionally use the proxy's connection-aware abort helper at integration.
	stop := context.AfterFunc(p.ctx, func() {
		if p.abort != nil {
			p.abort()
		}
		if p.request != nil {
			_ = mitmproxy.AbortHTTPRequestRead(p.request, p.ctx.Err())
		}
		closeRewriteBody(body)
	})
	defer stop()
	var encoded bodyspool.Payload
	encoded, err = boundedRewritePayload(p.ctx, store, body)
	if err != nil {
		return
	}
	if closeErr := body.Close(); closeErr != nil {
		err = closeErr
		return
	}
	var encodedReader io.ReadCloser
	encodedReader, err = encoded.Open()
	if err != nil {
		return
	}
	defer encodedReader.Close()
	var decoded io.ReadCloser
	decoded, err = getDecodedReader(encodedReader, encoding)
	if err != nil {
		return
	}
	defer decoded.Close()
	var decodedPayload bodyspool.Payload
	decodedPayload, err = boundedRewritePayload(p.ctx, store, decoded)
	if err != nil {
		return
	}
	var textBytes []byte
	textBytes, err = rewritePayloadBytes(decodedPayload)
	if err != nil {
		return
	}
	if !utf8.Valid(textBytes) || bytes.IndexByte(textBytes, 0) >= 0 {
		err = errors.New("body is binary or not valid UTF-8")
		return
	}
	var output string
	output, changed, err = match.ReplaceBody(p.ctx, string(textBytes))
	if err != nil {
		return
	}
	payload := encoded
	if changed {
		var input io.Reader = strings.NewReader(output)
		if encodings := normalizedContentEncodingTokens(encoding); len(encodings) > 0 {
			var closeEncoder func() error
			input, closeEncoder = newContentEncodingReader(input, encodings, nil)
			defer closeEncoder()
		}
		payload, err = boundedRewritePayload(p.ctx, store, input)
		if err != nil {
			return
		}
	}
	if err = p.ctx.Err(); err != nil {
		return
	}
	var reader io.ReadCloser
	reader, err = payload.Open()
	if err != nil {
		return
	}
	result = &rewriteSpoolBody{ReadCloser: reader, store: store}
	size = payload.Size()
	handoff = true
	return
}

type rewriteCanceledBody struct {
	io.ReadCloser
	cancel         context.CancelFunc
	expected, read int64
	complete       bool
}

func (b *rewriteCanceledBody) Read(p []byte) (int, error) {
	if b.ReadCloser == nil {
		b.complete = true
		return 0, io.EOF
	}
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	if err == io.EOF || b.expected >= 0 && b.read >= b.expected {
		b.complete = true
	}
	return n, err
}
func (b *rewriteCanceledBody) Close() error {
	// The send observer releases successful requests after recording the send.
	// Canceling here on normal EOF would taint that terminal observation.
	if !b.complete {
		b.cancel()
	}
	if b.ReadCloser != nil {
		return b.ReadCloser.Close()
	}
	return nil
}
