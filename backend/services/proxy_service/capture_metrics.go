package proxyservice

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/josexy/mitmproxy-go/v2"
	http "github.com/josexy/xhttp"
)

// captureExchange serializes the independently delivered transport trace and
// body lifecycle callbacks for one logical traffic entry. entry is a private
// working copy; every publish stores a deep-cloned immutable snapshot.
type captureExchange struct {
	service         *ProxyService
	entry           *TrafficEntry
	ctx             context.Context
	mu              sync.Mutex
	requestBodyless bool
	published       bool
}

func newCaptureExchange(service *ProxyService, ctx context.Context, entry *TrafficEntry) *captureExchange {
	return &captureExchange{
		service: service,
		entry:   entry,
		ctx:     ctx,
	}
}

func newPendingHTTPMessageMetrics(fields []HTTPHeaderField, truncated bool) *HTTPMessageMetrics {
	headerSize := logicalHARHeaderSize(fields)
	if truncated {
		headerSize = -1
	}
	return &HTTPMessageMetrics{
		StartedAtMicros: -1,
		EndedAtMicros:   -1,
		HeaderSize:      headerSize,
		BodySize:        -1,
		State:           HTTPMessageStatePending,
	}
}

func ensureHTTPMessageMetrics(message *HTTPMessage) *HTTPMessageMetrics {
	if message.Metrics == nil {
		message.Metrics = newPendingHTTPMessageMetrics(message.HeaderFields, message.HeadersTruncated)
	}
	return message.Metrics
}

func completedHandshakeMetrics(
	message *HTTPMessage,
	startedAt time.Time,
	endedAt time.Time,
	bodySize int64,
) *HTTPMessageMetrics {
	metrics := newPendingHTTPMessageMetrics(message.HeaderFields, message.HeadersTruncated)
	if !startedAt.IsZero() {
		metrics.StartedAtMicros = startedAt.UnixMicro()
	}
	if !endedAt.IsZero() {
		metrics.EndedAtMicros = endedAt.UnixMicro()
	}
	metrics.BodySize = bodySize
	metrics.State = HTTPMessageStateCompleted
	return metrics
}

func (x *captureExchange) setRequestBodyless(bodyless bool) {
	x.mu.Lock()
	x.requestBodyless = bodyless
	x.mu.Unlock()
}

func (x *captureExchange) observeHTTPExchangeTiming(event mitmproxy.HTTPExchangeTimingEvent) {
	switch event.Phase {
	case mitmproxy.HTTPExchangeRequestStarted:
		x.requestStarted(event.Timestamp, event.Attempt)
	case mitmproxy.HTTPExchangeRequestEnded:
		x.requestEnded(event.Timestamp, event.Complete, event.Error)
	case mitmproxy.HTTPExchangeResponseStarted:
		x.responseStarted(event.Timestamp)
	case mitmproxy.HTTPExchangeResponseEnded:
		x.responseEnded(event.Timestamp, event.Complete, event.Error)
	}
}

func (x *captureExchange) requestStarted(started time.Time, attempt int) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.entry.Request == nil {
		x.entry.Request = &HTTPMessage{}
	}
	metrics := ensureHTTPMessageMetrics(x.entry.Request)
	if attempt <= 1 || metrics.StartedAtMicros < 0 {
		metrics.StartedAtMicros = started.UnixMicro()
		x.entry.StartedAt = started
	}
	metrics.EndedAtMicros = -1
	metrics.BodySize = -1
	if x.requestBodyless {
		metrics.BodySize = 0
	}
	metrics.State = HTTPMessageStatePending
	if attempt > 1 && x.entry.Response != nil && x.entry.Response.Metrics != nil {
		x.entry.Response.Metrics.StartedAtMicros = -1
		x.entry.Response.Metrics.EndedAtMicros = -1
		x.entry.Response.Metrics.BodySize = -1
		x.entry.Response.Metrics.State = HTTPMessageStatePending
	}
	x.publishMetricsLocked(true)
}

// observeRetryRequestBodies preserves raw entity-payload accounting when the
// transport rewinds a replayable request through GetBody. The callback stores
// an attempt-local total, so a retry replaces the prior attempt's size instead
// of accumulating duplicate bytes.
func (x *captureExchange) observeRetryRequestBodies(request *http.Request) {
	if request == nil || request.GetBody == nil {
		return
	}
	getBody := request.GetBody
	expected := request.ContentLength
	request.GetBody = func() (io.ReadCloser, error) {
		body, err := getBody()
		if err != nil {
			return nil, err
		}
		return observeTransferBody(body, expected, x.requestBodyFinished), nil
	}
}

func (x *captureExchange) requestEnded(ended time.Time, complete bool, writeErr error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.entry.Request == nil {
		x.entry.Request = &HTTPMessage{}
	}
	metrics := ensureHTTPMessageMetrics(x.entry.Request)
	metrics.EndedAtMicros = timestampAtOrAfter(ended.UnixMicro(), metrics.StartedAtMicros)
	if writeErr != nil {
		metrics.State = stateForCaptureError(x.ctx, writeErr)
	} else if !complete {
		metrics.State = HTTPMessageStateCanceled
	} else {
		metrics.State = HTTPMessageStateCompleted
	}

	x.publishMetricsLocked(false)
}

func (x *captureExchange) responseStarted(started time.Time) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.entry.Response == nil {
		x.entry.Response = &HTTPMessage{}
	}
	metrics := ensureHTTPMessageMetrics(x.entry.Response)
	// Header bytes are not known until the complete response header block is
	// parsed. An empty field list at first-byte time must not be presented as a
	// known zero-byte header.
	metrics.HeaderSize = -1
	metrics.StartedAtMicros = started.UnixMicro()
	metrics.EndedAtMicros = -1
	metrics.State = HTTPMessageStatePending
	x.publishMetricsLocked(false)
}

func (x *captureExchange) responseHeaders(response *http.Response) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.entry.StatusCode = response.StatusCode
	x.entry.Status = response.Status
	x.service.fillResponseHTTPMessage(response, x.entry)
	metrics := ensureHTTPMessageMetrics(x.entry.Response)
	metrics.HeaderSize = logicalHARHeaderSize(x.entry.Response.HeaderFields)
	if x.entry.Response.HeadersTruncated {
		metrics.HeaderSize = -1
	}
	x.publishResponseHeadersLocked()
}

func (x *captureExchange) responseEnded(ended time.Time, complete bool, responseErr error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	created := false
	if x.entry.Response == nil {
		x.entry.Response = &HTTPMessage{}
		created = true
	}
	metrics := ensureHTTPMessageMetrics(x.entry.Response)
	if created {
		metrics.HeaderSize = -1
	}
	metrics.EndedAtMicros = timestampAtOrAfter(ended.UnixMicro(), metrics.StartedAtMicros)
	if responseErr != nil {
		metrics.State = stateForCaptureError(x.ctx, responseErr)
	} else if !complete {
		metrics.State = HTTPMessageStateCanceled
	} else {
		metrics.State = HTTPMessageStateCompleted
	}
	x.publishMetricsLocked(true)
}

func (x *captureExchange) markBodylessResponseSize() {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.entry.Response == nil {
		return
	}
	metrics := ensureHTTPMessageMetrics(x.entry.Response)
	metrics.BodySize = 0
	x.publishMetricsLocked(true)
}

func (x *captureExchange) requestBodyFinished(size int64, complete bool, readErr error) {
	x.bodyFinished(true, size, complete, readErr)
}

func (x *captureExchange) responseBodyFinished(size int64, complete bool, readErr error) {
	x.bodyFinished(false, size, complete, readErr)
}

func (x *captureExchange) bodyFinished(isRequest bool, size int64, _ bool, _ error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	message := x.entry.Response
	if isRequest {
		message = x.entry.Request
	}
	if message == nil {
		return
	}
	metrics := ensureHTTPMessageMetrics(message)
	metrics.BodySize = size
	x.publishMetricsLocked(!isRequest)
}

func timestampAtOrAfter(value, lowerBound int64) int64 {
	if lowerBound >= 0 && value < lowerBound {
		return lowerBound
	}
	return value
}

func (x *captureExchange) fail(err error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	now := time.Now()
	state := stateForCaptureError(x.ctx, err)
	if x.entry.Request != nil {
		metrics := ensureHTTPMessageMetrics(x.entry.Request)
		if metrics.State != HTTPMessageStateCompleted {
			metrics.State = state
		}
	}
	if x.entry.Response == nil {
		x.entry.Response = &HTTPMessage{}
	}
	metrics := ensureHTTPMessageMetrics(x.entry.Response)
	metrics.HeaderSize = -1
	metrics.BodySize = -1
	metrics.State = state
	x.entry.Error = &TrafficError{Timestamp: now, Error: err.Error()}
	x.publishFailureLocked()
}

func (x *captureExchange) fillResponseTrailers(response *http.Response) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if response == nil || x.entry.Response == nil {
		return
	}
	fields, truncated, orderUnavailable := completeResponseTrailerFields(
		response,
		mitmproxy.ResponseWireHeaderBlocks(response),
	)
	if fields == nil {
		return
	}
	x.entry.Response.TrailerFields = fields
	x.entry.Response.TrailersTruncated = truncated
	x.entry.Response.TrailerOrderUnavailable = orderUnavailable
	x.publishResponseTrailersLocked()
}

func (x *captureExchange) publishMetricsLocked(emit bool) {
	x.service.trafficPublishMu.Lock()
	defer x.service.trafficPublishMu.Unlock()
	stored := x.service.storeTrafficMetrics(x.entry)
	if stored == nil {
		return
	}
	if !x.published {
		x.service.emitTraffic(stored)
		x.published = true
		return
	}
	if emit && x.shouldEmitResponseMetricsLocked() {
		x.service.emitTrafficPatch(stored, newTrafficMetricsPatch(stored))
	}
}

func (x *captureExchange) shouldEmitResponseMetricsLocked() bool {
	if x.entry.Response == nil || x.entry.Response.Metrics == nil {
		return false
	}
	metrics := x.entry.Response.Metrics
	if metrics.State == HTTPMessageStateCompleted {
		return metrics.EndedAtMicros >= 0 && metrics.BodySize >= 0
	}
	return metrics.State == HTTPMessageStateFailed || metrics.State == HTTPMessageStateCanceled
}

func (x *captureExchange) publishResponseHeadersLocked() {
	x.service.trafficPublishMu.Lock()
	defer x.service.trafficPublishMu.Unlock()
	stored := x.service.storeTrafficResponseHeaders(x.entry)
	if stored == nil {
		return
	}
	if !x.published {
		x.service.emitTraffic(stored)
		x.published = true
		return
	}
	x.service.emitTrafficPatch(stored, newTrafficResponseHeadersPatch(stored))
}

func (x *captureExchange) publishResponseTrailersLocked() {
	x.service.trafficPublishMu.Lock()
	defer x.service.trafficPublishMu.Unlock()
	stored := x.service.storeTrafficResponseTrailers(x.entry)
	if stored == nil {
		return
	}
	if !x.published {
		x.service.emitTraffic(stored)
		x.published = true
		return
	}
	x.service.emitTrafficPatch(stored, newTrafficResponseTrailersPatch(stored))
}

func (x *captureExchange) publishFailureLocked() {
	x.service.trafficPublishMu.Lock()
	defer x.service.trafficPublishMu.Unlock()
	stored := x.service.storeTrafficFailure(x.entry)
	if stored == nil {
		return
	}
	if !x.published {
		x.service.emitTraffic(stored)
		x.published = true
		return
	}
	x.service.emitTrafficPatch(stored, newTrafficFailurePatch(stored))
}

func newTrafficPatch(entry *TrafficEntry) TrafficEntryPatch {
	return TrafficEntryPatch{TrafficID: entry.ID, Revision: entry.Revision}
}

func newTrafficMetricsSection(entry *TrafficEntry) *TrafficMetricsPatch {
	metrics := &TrafficMetricsPatch{}
	if entry.Request != nil && entry.Request.Metrics != nil {
		metrics.Request = entry.Request.Metrics
	}
	if entry.Response != nil && entry.Response.Metrics != nil {
		metrics.Response = entry.Response.Metrics
	}
	if metrics.Request == nil && metrics.Response == nil {
		return nil
	}
	return metrics
}

func newTrafficMetricsPatch(entry *TrafficEntry) TrafficEntryPatch {
	patch := newTrafficPatch(entry)
	patch.Metrics = newTrafficMetricsSection(entry)
	return patch
}

func newTrafficResponseHeadersPatch(entry *TrafficEntry) TrafficEntryPatch {
	patch := newTrafficPatch(entry)
	if entry.Response != nil {
		patch.ResponseHeaders = &TrafficResponseHeadersPatch{
			StatusCode:             entry.StatusCode,
			Status:                 entry.Status,
			Proto:                  entry.Response.Proto,
			HeaderFields:           entry.Response.HeaderFields,
			HeadersTruncated:       entry.Response.HeadersTruncated,
			HeaderOrderUnavailable: entry.Response.HeaderOrderUnavailable,
		}
	}
	patch.Metrics = newTrafficMetricsSection(entry)
	return patch
}

func newTrafficResponseTrailersPatch(entry *TrafficEntry) TrafficEntryPatch {
	patch := newTrafficPatch(entry)
	if entry.Response != nil {
		patch.ResponseTrailers = &TrafficResponseTrailersPatch{
			TrailerFields:           entry.Response.TrailerFields,
			TrailersTruncated:       entry.Response.TrailersTruncated,
			TrailerOrderUnavailable: entry.Response.TrailerOrderUnavailable,
		}
	}
	return patch
}

func newTrafficFailurePatch(entry *TrafficEntry) TrafficEntryPatch {
	patch := newTrafficMetricsPatch(entry)
	patch.Error = entry.Error
	return patch
}

func newTrafficProcessPatch(entry *TrafficEntry) TrafficEntryPatch {
	patch := newTrafficPatch(entry)
	if entry.Metadata != nil {
		patch.Process = entry.Metadata.Process
	}
	return patch
}

func stateForCaptureError(ctx context.Context, err error) HTTPMessageState {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		(ctx != nil && ctx.Err() != nil) {
		return HTTPMessageStateCanceled
	}
	return HTTPMessageStateFailed
}

// transferCountingBody observes entity payload bytes after transfer framing
// has been removed by xhttp but before FlowLens decodes Content-Encoding.
type transferCountingBody struct {
	source   io.ReadCloser
	expected int64
	onDone   func(size int64, complete bool, err error)
	mu       sync.Mutex
	size     int64
	done     bool
	closed   bool
	closeErr error
}

func observeTransferBody(
	source io.ReadCloser,
	expected int64,
	onDone func(size int64, complete bool, err error),
) io.ReadCloser {
	if source == nil || source == http.NoBody {
		return source
	}
	return &transferCountingBody{source: source, expected: expected, onDone: onDone}
}

func (b *transferCountingBody) Read(p []byte) (int, error) {
	n, err := b.source.Read(p)
	b.mu.Lock()
	b.size += int64(n)
	if err != nil {
		b.finishLocked(err == io.EOF, err)
	}
	b.mu.Unlock()
	return n, err
}

func (b *transferCountingBody) Close() error {
	b.mu.Lock()
	if !b.closed {
		b.closeErr = b.source.Close()
		b.closed = true
	}
	if !b.done {
		complete := b.closeErr == nil && b.expected >= 0 && b.size == b.expected
		b.finishLocked(complete, b.closeErr)
	}
	err := b.closeErr
	b.mu.Unlock()
	return err
}

func (b *transferCountingBody) finishLocked(complete bool, err error) {
	if b.done {
		return
	}
	b.done = true
	if err == io.EOF {
		err = nil
	}
	if b.onDone != nil {
		b.onDone(b.size, complete, err)
	}
}
