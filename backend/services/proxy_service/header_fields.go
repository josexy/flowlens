package proxyservice

import (
	"slices"
	"strconv"
	"strings"

	"github.com/josexy/mitmproxy-go/v2"
	http "github.com/josexy/xhttp"
)

func headerFieldsForKind(
	blocks []http.HeaderBlock,
	kind http.HeaderBlockKind,
) ([]HTTPHeaderField, bool) {
	for _, block := range blocks {
		if block.Kind != kind {
			continue
		}
		fields := make([]HTTPHeaderField, len(block.Fields))
		for index, field := range block.Fields {
			fields[index] = HTTPHeaderField{Name: field.Name, Value: field.Value}
		}
		return fields, block.Truncated
	}
	return nil, false
}

func initialHeaderFields(blocks []http.HeaderBlock) ([]HTTPHeaderField, bool) {
	return headerFieldsForKind(blocks, http.HeaderBlockInitial)
}

func trailerHeaderFields(blocks []http.HeaderBlock) ([]HTTPHeaderField, bool) {
	return headerFieldsForKind(blocks, http.HeaderBlockTrailer)
}

func headerFieldsFromMap(headers map[string][]string) []HTTPHeaderField {
	keys := make([]string, 0, len(headers))
	for name := range headers {
		if strings.TrimSpace(name) != "" {
			keys = append(keys, name)
		}
	}
	slices.SortFunc(keys, func(left, right string) int {
		if compared := strings.Compare(strings.ToLower(left), strings.ToLower(right)); compared != 0 {
			return compared
		}
		return strings.Compare(left, right)
	})

	fields := make([]HTTPHeaderField, 0, len(keys))
	for _, name := range keys {
		values := headers[name]
		if len(values) == 0 {
			fields = append(fields, HTTPHeaderField{Name: name})
			continue
		}
		for _, value := range values {
			fields = append(fields, HTTPHeaderField{Name: name, Value: value})
		}
	}
	return fields
}

func firstHeaderFieldValue(fields []HTTPHeaderField, name string) string {
	for _, field := range fields {
		if strings.EqualFold(field.Name, name) {
			return field.Value
		}
	}
	return ""
}

func mergeMissingHeaderFields(fields, fallback []HTTPHeaderField) []HTTPHeaderField {
	result := append([]HTTPHeaderField(nil), fields...)
	remaining := make(map[string]int, len(fields))
	for _, field := range fields {
		remaining[headerFieldIdentity(field)]++
	}
	for _, field := range fallback {
		identity := headerFieldIdentity(field)
		if remaining[identity] > 0 {
			remaining[identity]--
			continue
		}
		result = append(result, field)
	}
	return result
}

func headerFieldIdentity(field HTTPHeaderField) string {
	return strings.ToLower(field.Name) + "\x00" + field.Value
}

func completeInitialHeaderFields(
	blocks []http.HeaderBlock,
	fallback []HTTPHeaderField,
) (fields []HTTPHeaderField, truncated bool, orderUnavailable bool) {
	fields, truncated = initialHeaderFields(blocks)
	if fields == nil {
		return append([]HTTPHeaderField{}, fallback...), false, true
	}
	if truncated {
		fields = mergeMissingHeaderFields(fields, fallback)
	}
	return fields, truncated, false
}

func responseTrailerFallbackFields(trailers http.Header) []HTTPHeaderField {
	if len(trailers) == 0 {
		return nil
	}
	filtered := make(map[string][]string, len(trailers))
	for name, values := range trailers {
		switch {
		case strings.EqualFold(name, "Transfer-Encoding"),
			strings.EqualFold(name, "Trailer"),
			strings.EqualFold(name, "Content-Length"):
			continue
		}
		filtered[name] = values
	}
	if len(filtered) == 0 {
		return nil
	}
	return headerFieldsFromMap(filtered)
}

func completeResponseTrailerFields(
	resp *http.Response,
	blocks []http.HeaderBlock,
) (fields []HTTPHeaderField, truncated bool, orderUnavailable bool) {
	if resp == nil {
		return nil, false, false
	}
	fallback := responseTrailerFallbackFields(resp.Trailer)
	fields, truncated = trailerHeaderFields(blocks)
	if fields == nil {
		if fallback == nil {
			return nil, false, false
		}
		return append([]HTTPHeaderField{}, fallback...), false, true
	}
	if truncated {
		fields = mergeMissingHeaderFields(fields, fallback)
	}
	return fields, truncated, false
}

func completeRequestHeaderFields(
	req *http.Request,
	blocks []http.HeaderBlock,
) (fields []HTTPHeaderField, truncated bool, orderUnavailable bool) {
	fallback := requestHeaderFallbackFields(req)
	return completeInitialHeaderFields(blocks, fallback)
}

func requestHeaderFallbackFields(req *http.Request) []HTTPHeaderField {
	if req == nil {
		return nil
	}
	fallback := make([]HTTPHeaderField, 0, len(req.Header)+6)
	host := req.Host
	if host == "" && req.URL != nil {
		host = req.URL.Host
	}
	if req.ProtoMajor >= 2 {
		fallback = append(fallback,
			HTTPHeaderField{Name: ":method", Value: req.Method},
			HTTPHeaderField{Name: ":authority", Value: host},
		)
		if req.Method != http.MethodConnect {
			scheme := "http"
			if req.TLS != nil {
				scheme = "https"
			}
			path := "/"
			if req.URL != nil {
				if req.URL.Scheme != "" {
					scheme = req.URL.Scheme
				}
				path = req.URL.RequestURI()
				if path == "" {
					path = "/"
				}
			}
			fallback = append(fallback,
				HTTPHeaderField{Name: ":scheme", Value: scheme},
				HTTPHeaderField{Name: ":path", Value: path},
			)
		}
	} else if host != "" {
		fallback = append(fallback, HTTPHeaderField{Name: "Host", Value: host})
	}
	fallback = mergeMissingHeaderFields(fallback, headerFieldsFromMap(req.Header))
	if req.ContentLength > 0 && len(req.TransferEncoding) == 0 {
		fallback = mergeMissingHeaderFields(fallback, []HTTPHeaderField{{
			Name:  "Content-Length",
			Value: strconv.FormatInt(req.ContentLength, 10),
		}})
	}
	for _, value := range req.TransferEncoding {
		fallback = mergeMissingHeaderFields(fallback, []HTTPHeaderField{{
			Name:  "Transfer-Encoding",
			Value: value,
		}})
	}
	return fallback
}

func completeResponseHeaderFields(
	resp *http.Response,
	blocks []http.HeaderBlock,
) (fields []HTTPHeaderField, truncated bool, orderUnavailable bool) {
	if resp == nil {
		return []HTTPHeaderField{}, false, true
	}
	fallback := make([]HTTPHeaderField, 0, len(resp.Header)+4)
	if resp.ProtoMajor >= 2 {
		fallback = append(fallback, HTTPHeaderField{
			Name:  ":status",
			Value: strconv.Itoa(resp.StatusCode),
		})
	}
	fallback = mergeMissingHeaderFields(fallback, headerFieldsFromMap(resp.Header))
	if resp.ContentLength > 0 && len(resp.TransferEncoding) == 0 {
		fallback = mergeMissingHeaderFields(fallback, []HTTPHeaderField{{
			Name:  "Content-Length",
			Value: strconv.FormatInt(resp.ContentLength, 10),
		}})
	}
	for _, value := range resp.TransferEncoding {
		fallback = mergeMissingHeaderFields(fallback, []HTTPHeaderField{{
			Name:  "Transfer-Encoding",
			Value: value,
		}})
	}
	if resp.Close && resp.ProtoMajor < 2 {
		fallback = mergeMissingHeaderFields(fallback, []HTTPHeaderField{{
			Name:  "Connection",
			Value: "close",
		}})
	}
	if len(resp.Trailer) > 0 {
		keys := make([]string, 0, len(resp.Trailer))
		for name := range resp.Trailer {
			name = http.CanonicalHeaderKey(name)
			switch name {
			case "", "Transfer-Encoding", "Trailer", "Content-Length":
			default:
				keys = append(keys, name)
			}
		}
		slices.Sort(keys)
		for _, name := range keys {
			fallback = mergeMissingHeaderFields(fallback, []HTTPHeaderField{{
				Name:  "Trailer",
				Value: name,
			}})
		}
	}
	return completeInitialHeaderFields(blocks, fallback)
}

func syntheticResponseHeaderBlocks(resp *http.Response) []http.HeaderBlock {
	return http.ResponseHeaderBlocks(resp)
}

type preparedLiveRequest struct {
	request          *http.Request
	headerFields     []HTTPHeaderField
	headersTruncated bool
	orderUnavailable bool
	optionsApplied   bool
}

func (s *ProxyService) prepareLiveRequest(req *http.Request) preparedLiveRequest {
	prepared := preparedLiveRequest{request: req}
	if req == nil {
		return prepared
	}
	antiCache := s.antiCache.Load()
	antiComp := s.antiComp.Load()
	if !antiCache && !antiComp {
		return prepared
	}
	prepared.optionsApplied = true

	blocks := mitmproxy.RequestWireHeaderBlocks(req)
	initialFields, truncated := initialHeaderFields(blocks)
	prepared.headersTruncated = truncated

	if antiCache {
		deleteLiveHeader(req.Header, "If-None-Match")
		deleteLiveHeader(req.Header, "If-Modified-Since")
	}
	if antiComp {
		deleteLiveHeader(req.Header, "Accept-Encoding")
		req.Header.Set("Accept-Encoding", "identity")
	}

	if len(initialFields) > 0 || (len(blocks) > 0 && initialFields != nil) {
		prepared.headerFields = transformLiveHeaderFields(initialFields, antiCache, antiComp)
		if truncated {
			prepared.headerFields = mergeMissingHeaderFields(
				prepared.headerFields,
				requestHeaderFallbackFields(req),
			)
		}
		prepared.orderUnavailable = false
	} else {
		prepared.headerFields, _, prepared.orderUnavailable = completeRequestHeaderFields(req, nil)
	}

	order := mitmproxy.RequestWireHeaderOrder(req)
	if len(order.Headers) > 0 || len(order.Trailers) > 0 {
		order.Headers = transformLiveHeaderOrder(order.Headers, antiCache, antiComp, req.Header)
		if next, err := mitmproxy.WithRequestHeaderOrder(req, order); err == nil {
			prepared.request = next
		}
	}
	return prepared
}

func transformLiveHeaderFields(fields []HTTPHeaderField, antiCache, antiComp bool) []HTTPHeaderField {
	result := make([]HTTPHeaderField, 0, len(fields)+1)
	acceptEncodingAdded := false
	for _, field := range fields {
		name := strings.ToLower(field.Name)
		if antiCache && (name == "if-none-match" || name == "if-modified-since") {
			continue
		}
		if antiComp && name == "accept-encoding" {
			if !acceptEncodingAdded {
				result = append(result, HTTPHeaderField{Name: field.Name, Value: "identity"})
				acceptEncodingAdded = true
			}
			continue
		}
		result = append(result, field)
	}
	if antiComp && !acceptEncodingAdded {
		result = append(result, HTTPHeaderField{Name: "Accept-Encoding", Value: "identity"})
	}
	return result
}

func transformLiveHeaderOrder(
	names []string,
	antiCache bool,
	antiComp bool,
	headers http.Header,
) []string {
	result := make([]string, 0, len(names)+1)
	acceptEncodingAdded := false
	for _, name := range names {
		lower := strings.ToLower(name)
		if antiCache && (lower == "if-none-match" || lower == "if-modified-since") {
			continue
		}
		if antiComp && lower == "accept-encoding" {
			if !acceptEncodingAdded {
				result = append(result, name)
				acceptEncodingAdded = true
			}
			continue
		}
		result = append(result, name)
	}
	if antiComp && !acceptEncodingAdded && hasLiveHeader(headers, "Accept-Encoding") {
		result = append(result, "Accept-Encoding")
	}
	return result
}

func deleteLiveHeader(headers http.Header, name string) {
	for key := range headers {
		if strings.EqualFold(key, name) {
			delete(headers, key)
		}
	}
}

func hasLiveHeader(headers http.Header, name string) bool {
	for key := range headers {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}
