package proxyservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	rewriteservice "github.com/josexy/flowlens/backend/services/rewrite_service"
	"github.com/josexy/mitmproxy-go/v2"
	http "github.com/josexy/xhttp"
)

// SetRewriteService wires startup ownership without exposing configuration
// mutation through the proxy service's Wails API.
func SetRewriteService(s *ProxyService, rules *rewriteservice.RewriteService) {
	s.rewriteService = rules
}

const maxRewriteExecutions = 64

type RewriteExecution struct {
	RuleID   string `json:"ruleId"`
	RuleName string `json:"ruleName"`
	Action   string `json:"action"`
	Outcome  string `json:"outcome"`
	Reason   string `json:"reason"`
}
type rewriteResult struct {
	fields      []HTTPHeaderField
	truncated   bool
	unavailable bool
	changed     bool
	bodyChanged bool
	executions  []RewriteExecution
}

func rewriteError(match rewriteservice.Match, err error) error {
	return fmt.Errorf("rewrite %q (%s): %v: %w", match.Rule.Name, match.Rule.Action.Type, err, mitmproxy.ErrDropHTTP)
}
func rewriteSummary(match rewriteservice.Match, err error) RewriteExecution {
	result := RewriteExecution{RuleID: match.Rule.ID, RuleName: match.Rule.Name, Action: match.Rule.Action.Type, Outcome: "success"}
	if err != nil {
		result.Outcome = "failed"
		result.Reason = string([]rune(err.Error())[:min(512, len([]rune(err.Error())))])
	}
	return result
}

// Keep the latest actions so a terminal failure is always visible.
func appendRewriteExecutions(current []RewriteExecution, added ...RewriteExecution) []RewriteExecution {
	result := append(slices.Clone(current), added...)
	if len(result) > maxRewriteExecutions {
		result = slices.Clone(result[len(result)-maxRewriteExecutions:])
	}
	return result
}

func isRewriteRequestAction(action string) bool {
	return action == rewriteservice.ActionRedirect || action == rewriteservice.ActionRequest
}
func hasResponseRewrite(matches []rewriteservice.Match) bool {
	for _, m := range matches {
		if m.Rule.Action.Type == rewriteservice.ActionResponse {
			return true
		}
	}
	return false
}
func (s *ProxyService) matchRewriteRequest(req *http.Request) []rewriteservice.Match {
	if s.rewriteService == nil || req == nil || req.URL == nil || req.Method == http.MethodConnect || strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		return nil
	}
	return s.rewriteService.Snapshot().Match(req.Method, req.URL.String())
}
func applyRewriteFields(fields []HTTPHeaderField, ops []rewriteservice.FieldOperation) []HTTPHeaderField {
	result := slices.Clone(fields)
	for _, op := range ops {
		if op.Operation == "add" {
			result = append(result, HTTPHeaderField{Name: op.Name, Value: op.Value})
			continue
		}
		next := make([]HTTPHeaderField, 0, len(result)+1)
		found := false
		for _, field := range result {
			if strings.EqualFold(field.Name, op.Name) {
				if !found && op.Operation == "set" {
					next = append(next, HTTPHeaderField{Name: op.Name, Value: op.Value})
				}
				found = true
				continue
			}
			next = append(next, field)
		}
		if !found && op.Operation == "set" {
			next = append(next, HTTPHeaderField{Name: op.Name, Value: op.Value})
		}
		result = next
	}
	return result
}
func applyRewriteQuery(raw string, ops []rewriteservice.FieldOperation) string {
	var parts []string
	if raw != "" {
		parts = strings.Split(raw, "&")
	}
	for _, op := range ops {
		encoded := url.QueryEscape(op.Name) + "=" + url.QueryEscape(op.Value)
		if op.Operation == "add" {
			parts = append(parts, encoded)
			continue
		}
		next := make([]string, 0, len(parts)+1)
		found := false
		for _, part := range parts {
			name, _, _ := strings.Cut(part, "=")
			decoded, err := url.QueryUnescape(name)
			if err == nil && decoded == op.Name {
				if !found && op.Operation == "set" {
					next = append(next, encoded)
				}
				found = true
				continue
			}
			next = append(next, part)
		}
		if !found && op.Operation == "set" {
			next = append(next, encoded)
		}
		parts = next
	}
	return strings.Join(parts, "&")
}
func rewriteHeaderMap(fields []HTTPHeaderField) http.Header {
	header := make(http.Header)
	for _, f := range fields {
		if !strings.HasPrefix(f.Name, ":") && !strings.EqualFold(f.Name, "Host") {
			header.Add(f.Name, f.Value)
		}
	}
	return header
}

// Keep fields in place while updating backend-owned pseudo headers. Exact
// outgoing blocks preserve interleaved duplicate fields, unlike HeaderOrder.
func rewriteRequestFields(req *http.Request, fields []HTTPHeaderField) []HTTPHeaderField {
	for i := range fields {
		switch strings.ToLower(fields[i].Name) {
		case "host", ":authority":
			fields[i].Value = req.Host
		case ":path":
			fields[i].Value = req.URL.RequestURI()
		case ":scheme":
			fields[i].Value = req.URL.Scheme
		case ":method":
			fields[i].Value = req.Method
		}
	}
	if req.ProtoMajor < 2 && !slices.ContainsFunc(fields, func(f HTTPHeaderField) bool { return strings.EqualFold(f.Name, "host") }) {
		fields = append([]HTTPHeaderField{{Name: "Host", Value: req.Host}}, fields...)
	}
	return fields
}
func rewriteBlock(fields []HTTPHeaderField, _ string) http.HeaderBlock {
	block := http.HeaderBlock{Kind: http.HeaderBlockInitial, Fields: make([]http.HeaderField, 0, len(fields))}
	for _, f := range fields {
		block.Fields = append(block.Fields, http.HeaderField{Name: f.Name, Value: f.Value})
	}
	return block
}
func (s *ProxyService) rewriteRequest(ctx context.Context, req *http.Request, prepared preparedLiveRequest, matches []rewriteservice.Match, abort context.CancelFunc) (*http.Request, rewriteResult, error) {
	result := rewriteResult{}
	if prepared.optionsApplied {
		result.fields = slices.Clone(prepared.headerFields)
		result.truncated = prepared.headersTruncated
		result.unavailable = prepared.orderUnavailable
	} else {
		result.fields, result.truncated, result.unavailable = completeRequestHeaderFields(req, mitmproxy.RequestWireHeaderBlocks(req))
	}
	original := slices.Clone(result.fields)
	originalURL := req.URL.String()
	originalHost := req.Host
	phase := newRewriteBodyPhase(ctx, req, abort)
	phase.originalContentType = req.Header.Get("Content-Type")
	defer phase.close()
	for _, match := range matches {
		action := match.Rule.Action
		if !isRewriteRequestAction(action.Type) {
			continue
		}
		var err error
		switch action.Type {
		case rewriteservice.ActionRedirect:
			var target string
			target, err = match.TargetURL()
			if err == nil {
				req.URL, err = url.Parse(target)
				if err == nil && action.HostPolicy == "target" {
					req.Host = req.URL.Host
				}
			}
		case rewriteservice.ActionRequest:
			req.URL.RawQuery = applyRewriteQuery(req.URL.RawQuery, action.Query)
			result.fields = applyRewriteFields(result.fields, action.Headers)
			for _, op := range action.Headers {
				if strings.EqualFold(op.Name, "Host") {
					req.Host = op.Value
				}
			}
			req.Header = rewriteHeaderMap(result.fields)
			if action.Body.Mode != "none" {
				var changed bool
				req.Body, req.ContentLength, changed, err = phase.rewrite(req.Body, req.ContentLength, req.Header, match)
				if err == nil && changed {
					result.changed = true
					result.bodyChanged = true
					result.fields = rewrittenBodyFields(result.fields, req.ContentLength)
					req.TransferEncoding = nil
					req.Trailer = nil
					req.GetBody = nil
				}
			}
		}
		result.executions = appendRewriteExecutions(result.executions, rewriteSummary(match, err))
		if err != nil {
			return req, result, rewriteError(match, err)
		}
	}
	result.fields = rewriteRequestFields(req, result.fields)
	result.changed = result.changed || !slices.Equal(original, result.fields) || originalURL != req.URL.String() || originalHost != req.Host
	if result.changed {
		req.Header = rewriteHeaderMap(result.fields)
		next, err := mitmproxy.WithRequestHeaderBlock(req, rewriteBlock(result.fields, req.Proto))
		if err != nil {
			return req, result, fmt.Errorf("final request headers: %v: %w", err, mitmproxy.ErrDropHTTP)
		}
		req = next
		// A URL rewrite, including a same-origin path change, explicitly authorizes
		// transport routing after the original authority has already been checked.
		if originalURL != req.URL.String() {
			next, err = mitmproxy.WithHTTPUpstreamTarget(req, req.URL)
			if err != nil {
				return req, result, fmt.Errorf("rewrite target: %v: %w", err, mitmproxy.ErrDropHTTP)
			}
			req = next
		}
	}
	return req, result, nil
}
func (s *ProxyService) rewriteResponse(ctx context.Context, req *http.Request, resp *http.Response, matches []rewriteservice.Match, abort context.CancelFunc) (rewriteResult, error) {
	result := rewriteResult{}
	result.fields, result.truncated, result.unavailable = completeResponseHeaderFields(resp, mitmproxy.ResponseWireHeaderBlocks(resp))
	original := slices.Clone(result.fields)
	phase := newRewriteBodyPhase(ctx, req, abort)
	phase.originalContentType = resp.Header.Get("Content-Type")
	defer phase.close()
	for _, match := range matches {
		action := match.Rule.Action
		if action.Type != rewriteservice.ActionResponse {
			continue
		}
		var err error
		result.fields = applyRewriteFields(result.fields, action.Headers)
		resp.Header = rewriteHeaderMap(result.fields)
		if action.Body.Mode != "none" {
			if req.Method == http.MethodHead || resp.StatusCode < 200 || resp.StatusCode == 204 || resp.StatusCode == 304 {
				err = errors.New("response does not allow an entity body")
			} else {
				var changed bool
				resp.Body, resp.ContentLength, changed, err = phase.rewrite(resp.Body, resp.ContentLength, resp.Header, match)
				if err == nil && changed {
					result.changed = true
					result.bodyChanged = true
					result.fields = rewrittenBodyFields(result.fields, resp.ContentLength)
					resp.TransferEncoding = nil
					resp.Trailer = nil
				}
			}
		}
		result.executions = appendRewriteExecutions(result.executions, rewriteSummary(match, err))
		if err != nil {
			return result, rewriteError(match, err)
		}
	}
	result.changed = result.changed || !slices.Equal(original, result.fields)
	if result.changed {
		resp.Header = rewriteHeaderMap(result.fields)
		if err := mitmproxy.SetResponseHeaderBlock(resp, rewriteBlock(result.fields, resp.Proto)); err != nil {
			return result, fmt.Errorf("final response headers: %v: %w", err, mitmproxy.ErrDropHTTP)
		}
	}
	return result, nil
}
func rewrittenBodyFields(fields []HTTPHeaderField, size int64) []HTTPHeaderField {
	for _, name := range []string{"Content-Length", "Transfer-Encoding", "Trailer", "Content-MD5", "Digest", "Content-Digest", "Repr-Digest", "ETag", "Content-Range", "Accept-Ranges"} {
		fields = applyRewriteFields(fields, []rewriteservice.FieldOperation{{Operation: "delete", Name: name}})
	}
	return append(fields, HTTPHeaderField{Name: "Content-Length", Value: strconv.FormatInt(size, 10)})
}

func (x *captureExchange) installRewrittenRequest(req *http.Request, result rewriteResult) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.entry.URL = req.URL.String()
	x.entry.Host = req.Host
	x.entry.Path = req.URL.Path
	x.entry.Type = req.URL.Scheme
	x.entry.Request.HeaderFields = result.fields
	x.entry.Request.HeadersTruncated = result.truncated
	x.entry.Request.HeaderOrderUnavailable = result.unavailable
	x.entry.RewriteExecutions = append([]RewriteExecution(nil), result.executions...)
	x.service.trafficPublishMu.Lock()
	defer x.service.trafficPublishMu.Unlock()
	if x.service.storeTrafficEntry(x.entry) {
		x.service.emitTraffic(x.entry)
	}
}
func (x *captureExchange) installRewrittenResponse(resp *http.Response, result rewriteResult) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.entry.RewriteExecutions = appendRewriteExecutions(x.entry.RewriteExecutions, result.executions...)
	x.responseBodyRewritten = result.bodyChanged
	x.entry.StatusCode = resp.StatusCode
	x.entry.Status = resp.Status
	if x.entry.Response == nil {
		x.entry.Response = &HTTPMessage{}
	}
	x.entry.Response.Proto = resp.Proto
	x.entry.Response.HeaderFields = result.fields
	x.entry.Response.HeadersTruncated = result.truncated
	x.entry.Response.HeaderOrderUnavailable = result.unavailable
	ensureHTTPMessageMetrics(x.entry.Response).HeaderSize = logicalHTTPResponseHeaderSize(x.entry)
	if result.changed {
		x.entry.ResponseMetricsSource = "downstream"
		x.downstreamResponse = true
	}
	x.publishResponseHeadersLocked()
}
func (x *captureExchange) observeRewrittenResponseSent(result mitmproxy.HTTPResponseSendResult) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if result.HeaderBlock.Kind == http.HeaderBlockInitial {
		x.entry.Response.HeaderFields = fieldsFromRewriteBlock(result.HeaderBlock)
		x.entry.Response.Proto = rewriteProto(result.HeaderBlock.ProtoMajor)
		ensureHTTPMessageMetrics(x.entry.Response).HeaderSize = logicalHTTPResponseHeaderSize(x.entry)
		x.publishResponseHeadersLocked()
	}
	metrics := ensureHTTPMessageMetrics(x.entry.Response)
	if !result.StartedAt.IsZero() {
		metrics.StartedAtMicros = result.StartedAt.UnixMicro()
	}
	if !result.EndedAt.IsZero() {
		metrics.EndedAtMicros = result.EndedAt.UnixMicro()
	}
	if result.Err != nil || result.Canceled {
		metrics.State = stateForCaptureError(x.ctx, result.Err)
		if result.Canceled {
			metrics.State = HTTPMessageStateCanceled
		}
		reason := "downstream response canceled"
		if result.Err != nil {
			reason = result.Err.Error()
		}
		x.entry.Error = &TrafficError{Timestamp: time.Now(), Error: reason}
		x.publishFailureLocked()
	} else {
		metrics.State = HTTPMessageStateCompleted
		metrics.BodySize = result.BodyBytes
		x.publishMetricsLocked(true)
	}
}

// response rules may pre-read the upstream body. Defer its timing until the
// final action result determines whether upstream or downstream is authoritative.
func (x *captureExchange) resolveRewriteResponseTiming(changed bool) {
	x.mu.Lock()
	x.deferResponseTiming = false
	x.downstreamResponse = changed
	started, ended := x.deferredResponseStart, x.deferredResponseEnd
	x.mu.Unlock()
	if !changed {
		if started != nil {
			x.observeHTTPExchangeTiming(*started)
		}
		if ended != nil {
			x.observeHTTPExchangeTiming(*ended)
		}
	}
}

// retain imported io references near the owned close boundary.
func closeRewriteBody(body io.ReadCloser) {
	if body != nil {
		_ = body.Close()
	}
}

func rewriteProto(major int) string {
	if major == 2 {
		return "HTTP/2.0"
	}
	return "HTTP/1.1"
}
func fieldsFromRewriteBlock(block http.HeaderBlock) []HTTPHeaderField {
	fields := make([]HTTPHeaderField, len(block.Fields))
	for i, f := range block.Fields {
		fields[i] = HTTPHeaderField{Name: f.Name, Value: f.Value}
	}
	return fields
}
func (x *captureExchange) observeRewrittenRequestHeaders(block http.HeaderBlock) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.entry.Request.Proto = rewriteProto(block.ProtoMajor)
	x.entry.Request.HeaderFields = fieldsFromRewriteBlock(block)
	ensureHTTPMessageMetrics(x.entry.Request).HeaderSize = logicalHTTPRequestHeaderSize(x.entry)
	x.service.trafficPublishMu.Lock()
	defer x.service.trafficPublishMu.Unlock()
	if x.service.storeTrafficEntry(x.entry) {
		x.service.emitTraffic(x.entry)
	}
}
