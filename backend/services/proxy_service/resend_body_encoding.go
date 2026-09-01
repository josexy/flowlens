package proxyservice

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/andybalholm/brotli"
	"github.com/josexy/flowlens/backend/pkg/compresspool"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/snappy"
	"github.com/klauspost/compress/zlib"
	"github.com/klauspost/compress/zstd"
)

func encodeDecodedRequestBodyForResend(entry *TrafficEntry, body []byte) ([]byte, error) {
	if len(body) == 0 || entry == nil || entry.Request == nil {
		return body, nil
	}
	contentEncoding := firstHeaderFieldValue(entry.Request.HeaderFields, "Content-Encoding")
	return encodeDecodedBodyForContentEncoding(body, contentEncoding)
}

func encodeDecodedBodyForContentEncoding(body []byte, contentEncoding string) ([]byte, error) {
	encodings := normalizedContentEncodingTokens(contentEncoding)
	if len(encodings) == 0 || hasUnsupportedContentEncoding(contentEncoding) {
		return body, nil
	}

	encoded := body
	for _, encoding := range encodings {
		next, err := encodeBodyForContentEncoding(encoded, encoding)
		if err != nil {
			return nil, fmt.Errorf("encode %s request body: %w", encoding, err)
		}
		encoded = next
	}
	return encoded, nil
}

func encodeSendRequestBodyForContentEncoding(fields []HTTPHeaderField, body *sendRequestBodyResult) error {
	if body == nil || body.reader == nil {
		return nil
	}
	contentEncoding := firstHeaderFieldValue(fields, "Content-Encoding")
	encodings := normalizedContentEncodingTokens(contentEncoding)
	if len(encodings) == 0 || hasUnsupportedContentEncoding(contentEncoding) {
		return nil
	}

	encodedReader, closeEncodedReader := newContentEncodingReader(body.reader, encodings, body.close)
	body.reader = encodedReader
	body.contentLength = -1
	body.close = closeEncodedReader
	if body.getBody != nil {
		originalGetBody := body.getBody
		body.getBody = func() (io.ReadCloser, error) {
			source, err := originalGetBody()
			if err != nil {
				return nil, err
			}
			reader, closeReader := newContentEncodingReader(source, encodings, source.Close)
			return &callbackReadCloser{Reader: reader, close: closeReader}, nil
		}
	}
	return nil
}

type callbackReadCloser struct {
	io.Reader
	close func() error
}

func (r *callbackReadCloser) Close() error {
	if r == nil || r.close == nil {
		return nil
	}
	return r.close()
}

func newContentEncodingReader(source io.Reader, encodings []string, closeSource func() error) (io.Reader, func() error) {
	pipeReader, pipeWriter := io.Pipe()
	closeOnce := sync.Once{}
	var closeErr error

	go func() {
		var copyErr error
		writer := io.Writer(pipeWriter)
		closers := make([]io.Closer, 0, len(encodings))
		for _, encoding := range slices.Backward(encodings) {
			encodedWriter, err := newBodyEncoderWriter(writer, encoding)
			if err != nil {
				copyErr = err
				break
			}
			writer = encodedWriter
			closers = append(closers, encodedWriter)
		}

		if copyErr == nil {
			_, copyErr = io.Copy(writer, source)
		}
		if closeEncErr := closeBodyEncoderWriters(closers); copyErr == nil {
			copyErr = closeEncErr
		}
		if copyErr != nil {
			_ = pipeWriter.CloseWithError(copyErr)
			return
		}
		_ = pipeWriter.Close()
	}()

	closeReader := func() error {
		closeOnce.Do(func() {
			closeErr = pipeReader.Close()
			if closeSource != nil {
				if err := closeSource(); closeErr == nil {
					closeErr = err
				}
			}
		})
		return closeErr
	}

	return pipeReader, closeReader
}

func closeBodyEncoderWriters(closers []io.Closer) error {
	var closeErr error
	for _, closer := range slices.Backward(closers) {
		if err := closer.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func newBodyEncoderWriter(w io.Writer, encoding string) (io.WriteCloser, error) {
	switch normalizeContentEncodingToken(encoding) {
	case "gzip":
		return gzip.NewWriter(w), nil
	case "br":
		return brotli.NewWriter(w), nil
	case "snappy":
		return snappy.NewBufferedWriter(w), nil
	case "deflate":
		return zlib.NewWriter(w), nil
	case "zstd":
		encoder, err := compresspool.AcquireZstdEncoder(w)
		if err != nil {
			return nil, err
		}
		return &pooledZstdBodyWriter{encoder: encoder}, nil
	default:
		return noopWriteCloser{Writer: w}, nil
	}
}

type noopWriteCloser struct {
	io.Writer
}

func (w noopWriteCloser) Close() error {
	return nil
}

type pooledZstdBodyWriter struct {
	encoder *zstd.Encoder
}

func (w *pooledZstdBodyWriter) Write(p []byte) (int, error) {
	return w.encoder.Write(p)
}

func (w *pooledZstdBodyWriter) Close() error {
	err := w.encoder.Close()
	compresspool.ReleaseZstdEncoder(w.encoder)
	return err
}

func encodeBodyForContentEncoding(body []byte, encoding string) ([]byte, error) {
	switch normalizeContentEncodingToken(encoding) {
	case "gzip":
		return encodeGzipBody(body)
	case "br":
		return encodeBrotliBody(body)
	case "snappy":
		return encodeSnappyBody(body)
	case "deflate":
		return encodeZlibBody(body)
	case "zstd":
		return encodeZstdBody(body)
	default:
		return body, nil
	}
}

func encodeGzipBody(body []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(body); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeBrotliBody(body []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := brotli.NewWriter(&buf)
	if _, err := writer.Write(body); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeSnappyBody(body []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := snappy.NewBufferedWriter(&buf)
	if _, err := writer.Write(body); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeZlibBody(body []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)
	if _, err := writer.Write(body); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeZstdBody(body []byte) ([]byte, error) {
	var buf bytes.Buffer
	encoder, err := compresspool.AcquireZstdEncoder(&buf)
	if err != nil {
		return nil, err
	}
	defer compresspool.ReleaseZstdEncoder(encoder)
	if _, err := encoder.Write(body); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
