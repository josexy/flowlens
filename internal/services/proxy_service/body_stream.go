package proxyservice

import (
	"io"
	"slices"
	"sync"
	"sync/atomic"

	bodycache "github.com/josexy/flowlens/internal/pkg/body_cache"
	"github.com/josexy/flowlens/internal/pkg/compresspool"
	"github.com/josexy/flowlens/internal/pkg/logger"
	http "github.com/josexy/xhttp"
)

const CHUNK_SIZE = 32 * 1024

type StreamBodyReader struct {
	io.ReadCloser
	// source is the original request/response body. mitmproxy-go and
	// The HTTP transport closes the replacement wrapper, so Close must cascade here.
	source      io.Closer
	pw          *io.PipeWriter
	decodedDone <-chan struct{}
	closeOnce   sync.Once
	closeErr    error
}

func (s *ProxyService) newStreamBodyReader(body io.ReadCloser, flowID uint64, encoding string, isRequest bool, extraOnDone ...func()) io.ReadCloser {
	return s.newObservedStreamBodyReader(body, flowID, encoding, isRequest, nil, extraOnDone...)
}

func (s *ProxyService) newCaptureStreamBodyReader(
	body io.ReadCloser,
	entry *TrafficEntry,
	encoding string,
	isRequest bool,
	extraOnDone ...func(),
) io.ReadCloser {
	return s.newCaptureObservedStreamBodyReader(body, entry, encoding, isRequest, nil, extraOnDone...)
}

func (s *ProxyService) newCaptureObservedStreamBodyReader(
	body io.ReadCloser,
	entry *TrafficEntry,
	encoding string,
	isRequest bool,
	onChunk func(offset int64, data []byte),
	extraOnDone ...func(),
) io.ReadCloser {
	if body == http.NoBody || body == nil {
		return body
	}

	s.captureLifecycleMu.RLock()
	defer s.captureLifecycleMu.RUnlock()
	if !s.isCurrentTrafficEntryLocked(entry) {
		return body
	}
	if s.captureAttachPrecheckHook != nil {
		s.captureAttachPrecheckHook()
	}
	release := entry.lifecycle.hold()
	bodies := s.attachTrafficBodies(entry.ID, isRequest)
	// Deletion sets the tombstone before it removes the entry's bodies, and
	// this side attaches its bodies before rechecking the tombstone, so one of
	// the two always sees the other's state.
	if entry.lifecycle != nil && entry.lifecycle.deleted.Load() {
		s.releaseOrphanedTrafficBodies(entry.ID, bodies)
		release()
		return body
	}
	captured := s.wrapObservedStreamBody(body, entry.ID, bodies, encoding, isRequest, onChunk, extraOnDone...)
	return &settlingBody{ReadCloser: captured, release: release}
}

// settlingBody ends a captured body's hold on its entry once the body reaches
// its end, fails, or is closed, whether or not the entry is still captured.
type settlingBody struct {
	io.ReadCloser
	release func()
}

func (b *settlingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.release()
	}
	return n, err
}

func (b *settlingBody) Close() error {
	err := b.ReadCloser.Close()
	b.release()
	return err
}

func (s *ProxyService) newObservedStreamBodyReader(
	body io.ReadCloser,
	flowID uint64,
	encoding string,
	isRequest bool,
	onChunk func(offset int64, data []byte),
	extraOnDone ...func(),
) io.ReadCloser {
	if body == http.NoBody || body == nil {
		return body
	}
	bodies := s.attachTrafficBodies(flowID, isRequest)
	return s.wrapObservedStreamBody(body, flowID, bodies, encoding, isRequest, onChunk, extraOnDone...)
}

func (s *ProxyService) attachTrafficBodies(flowID uint64, isRequest bool) *TrafficBodies {
	bodiesValue, _ := s.trafficBodies.LoadOrStore(flowID, &TrafficBodies{
		liveState: 1,
	})
	bodies := bodiesValue.(*TrafficBodies)
	s.ensureBodyCapture(flowID, bodies, isRequest)
	return bodies
}

// releaseOrphanedTrafficBodies drops capture state attached after deletion had
// already removed the entry's bodies and cache files.
func (s *ProxyService) releaseOrphanedTrafficBodies(flowID uint64, bodies *TrafficBodies) {
	s.trafficBodies.CompareAndDelete(flowID, bodies)
	abortTrafficBodies(bodies)
	s.deleteTrafficBodyCacheFiles(flowID)
}

func abortTrafficBodies(bodies *TrafficBodies) {
	atomic.StoreInt32(&bodies.liveState, 0)
	bodies.lockReqBody.Lock()
	if bodies.requestBody != nil {
		bodies.requestBody.Abort()
		bodies.requestBody = nil
	}
	bodies.lockReqBody.Unlock()
	bodies.lockRespBody.Lock()
	if bodies.responseBody != nil {
		bodies.responseBody.Abort()
		bodies.responseBody = nil
	}
	bodies.lockRespBody.Unlock()
}

func (s *ProxyService) deleteTrafficBodyCacheFiles(flowID uint64) {
	s.bodyCacheMu.RLock()
	if s.bodyCache != nil {
		s.bodyCache.Delete(flowID)
	}
	s.bodyCacheMu.RUnlock()
}

func (s *ProxyService) wrapObservedStreamBody(
	body io.ReadCloser,
	flowID uint64,
	bodies *TrafficBodies,
	encoding string,
	isRequest bool,
	onChunk func(offset int64, data []byte),
	extraOnDone ...func(),
) io.ReadCloser {
	onDone := s.makeCaptureOnDoneFunc(bodies, isRequest, extraOnDone...)
	if encoding == "" {
		return newChunkBodyReader(body, flowID, bodies, isRequest, CHUNK_SIZE, onDone, onChunk)
	}
	pr, pw := io.Pipe()
	teeReader := io.TeeReader(body, pw)
	decodedDone := make(chan struct{})
	go func() {
		defer close(decodedDone)
		// Decoding can stop before the transport has finished forwarding the
		// encoded payload: a truncated gzip member, a bad checksum, or bytes
		// after the end of a deflate stream. Always consume the rest of the
		// pipe so TeeReader writes never block and the untouched encoded
		// bytes still reach the peer.
		defer func() { _, _ = io.Copy(io.Discard, pr) }()
		decodedReader, err := getDecodedReader(pr, encoding)
		if err != nil {
			logger.G().Errorf("[%d] error getting decoded [%s] reader: %v", flowID, encoding, err)
			onDone()
			return
		}
		decodedReader = newChunkBodyReader(decodedReader, flowID, bodies, isRequest, CHUNK_SIZE, onDone, onChunk)
		if _, copyErr := io.Copy(io.Discard, decodedReader); copyErr != nil {
			logger.G().Errorf("[%d] error decoding [%s] body: %v", flowID, encoding, copyErr)
		}
		_ = decodedReader.Close()
	}()

	return &StreamBodyReader{
		ReadCloser:  io.NopCloser(teeReader),
		source:      body,
		pw:          pw,
		decodedDone: decodedDone,
	}
}

func (b *StreamBodyReader) Read(p []byte) (n int, err error) {
	n, err = b.ReadCloser.Read(p)
	if err != nil {
		// Close the pipe writer to signal end of data to the goroutine
		b.pw.CloseWithError(err)
		if b.decodedDone != nil {
			<-b.decodedDone
		}
	}
	return
}

func (b *StreamBodyReader) Close() error {
	b.closeOnce.Do(func() {
		err := b.ReadCloser.Close()
		// Close the pipe writer so the decompression goroutine is not stuck on pr.Read()
		if b.pw != nil {
			_ = b.pw.Close()
		}
		if b.source != nil {
			if closeErr := b.source.Close(); err == nil {
				err = closeErr
			}
		}
		if b.decodedDone != nil {
			<-b.decodedDone
		}
		b.closeErr = err
	})
	return b.closeErr
}

func (s *ProxyService) ensureBodyCapture(flowID uint64, bodies *TrafficBodies, isRequest bool) {
	threshold := s.getCacheThreshold()
	s.bodyCacheMu.RLock()
	cache := s.bodyCache
	defer s.bodyCacheMu.RUnlock()
	if isRequest {
		bodies.lockReqBody.Lock()
		if bodies.requestBody == nil {
			bodies.requestBody = newCapturedBody(flowID, bodycache.KindRequest, threshold, cache)
		}
		bodies.lockReqBody.Unlock()
		return
	}

	bodies.lockRespBody.Lock()
	if bodies.responseBody == nil {
		bodies.responseBody = newCapturedBody(flowID, bodycache.KindResponse, threshold, cache)
	}
	bodies.lockRespBody.Unlock()
}

func (s *ProxyService) makeCaptureOnDoneFunc(bodies *TrafficBodies, isRequest bool, extraOnDone ...func()) func() {
	return func() {
		if isRequest {
			bodies.lockReqBody.Lock()
			if bodies.requestBody != nil {
				bodies.requestBody.Close()
			}
			bodies.lockReqBody.Unlock()
		} else {
			bodies.lockRespBody.Lock()
			if bodies.responseBody != nil {
				bodies.responseBody.Close()
			}
			bodies.lockRespBody.Unlock()
		}
		for _, callback := range extraOnDone {
			if callback != nil {
				callback()
			}
		}
	}
}

type chunkBodyReader struct {
	io.ReadCloser
	flowID    uint64
	chunkSize int
	isRequest bool
	bodies    *TrafficBodies
	onDone    func()
	onChunk   func(offset int64, data []byte)
	offset    int64
	doneOnce  sync.Once
}

func newChunkBodyReader(
	body io.ReadCloser,
	flowID uint64,
	bodies *TrafficBodies,
	isRequest bool,
	chunkSize int,
	onDone func(),
	onChunk func(offset int64, data []byte),
) io.ReadCloser {
	return &chunkBodyReader{
		ReadCloser: body,
		flowID:     flowID,
		chunkSize:  chunkSize,
		isRequest:  isRequest,
		bodies:     bodies,
		onDone:     onDone,
		onChunk:    onChunk,
	}
}

func (r *chunkBodyReader) finishCapture() {
	r.doneOnce.Do(func() {
		if r.onDone != nil {
			r.onDone()
		}
	})
	r.bodies = nil
}

func (r *chunkBodyReader) writeBodyChunk(data []byte) {
	if r.isRequest {
		r.bodies.lockReqBody.Lock()
		if r.bodies.requestBody != nil {
			r.bodies.requestBody.Write(data)
		}
		r.bodies.lockReqBody.Unlock()
	} else {
		r.bodies.lockRespBody.Lock()
		if r.bodies.responseBody != nil {
			r.bodies.responseBody.Write(data)
		}
		r.bodies.lockRespBody.Unlock()
	}
}

func (r *chunkBodyReader) Read(p []byte) (n int, err error) {
	if r.chunkSize <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > int64(r.chunkSize) {
		p = p[0:r.chunkSize]
	}

	n, err = r.ReadCloser.Read(p)

	if r.bodies == nil {
		return n, err
	}
	if atomic.LoadInt32(&r.bodies.liveState) == 0 {
		r.bodies = nil
		return n, err
	}

	var sentUpdate bool
	if n > 0 {
		r.writeBodyChunk(p[:n])
		if r.onChunk != nil {
			r.onChunk(r.offset, p[:n])
		}
		r.offset += int64(n)
		sentUpdate = true
	}

	if err != nil && err != io.EOF {
		logger.G().Errorf("[%d] error reading streaming body: %v", r.flowID, err)
	}

	if err == io.EOF && !sentUpdate {
		r.writeBodyChunk(p[:n])
	}

	if err != nil {
		r.finishCapture()
	}

	return n, err
}

func (r *chunkBodyReader) Close() error {
	err := r.ReadCloser.Close()
	if r.bodies != nil {
		r.finishCapture()
	}
	return err
}

func getDecodedReader(r io.Reader, encoding string) (io.ReadCloser, error) {
	encodings := normalizedContentEncodingTokens(encoding)
	if len(encodings) == 0 || hasUnsupportedContentEncoding(encoding) {
		return io.NopCloser(r), nil
	}

	reader := r
	closers := make([]io.Closer, 0, len(encodings))
	for _, encoding := range slices.Backward(encodings) {
		decodedReader, err := newDecodedReaderForEncoding(reader, encoding)
		if err != nil {
			closeDecodedReaderChain(closers)
			return nil, err
		}
		reader = decodedReader
		closers = append(closers, decodedReader)
	}

	return &decodedReaderChain{
		reader:  reader,
		closers: closers,
	}, nil
}

type decodedReaderChain struct {
	reader    io.Reader
	closers   []io.Closer
	closeOnce sync.Once
	closeErr  error
}

func (r *decodedReaderChain) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *decodedReaderChain) Close() error {
	r.closeOnce.Do(func() {
		r.closeErr = closeDecodedReaderChain(r.closers)
	})
	return r.closeErr
}

func closeDecodedReaderChain(closers []io.Closer) error {
	var closeErr error
	for _, closer := range slices.Backward(closers) {
		if err := closer.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func newDecodedReaderForEncoding(r io.Reader, encoding string) (io.ReadCloser, error) {
	switch normalizeContentEncodingToken(encoding) {
	case "gzip":
		return compresspool.NewPooledGzipReadCloser(r)
	case "br":
		return compresspool.NewPooledBrotliReadCloser(r)
	case "snappy":
		return compresspool.NewPooledSnappyReadCloser(r), nil
	case "deflate":
		return compresspool.NewPooledDeflateReadCloser(r)
	case "zstd":
		return compresspool.NewPooledZstdReadCloser(r)
	default:
		return io.NopCloser(r), nil
	}
}
