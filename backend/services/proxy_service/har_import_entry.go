package proxyservice

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Pointer overrides preserve the distinction between absent values, zero and
// explicitly empty payloads. The export types supply the remaining fields.
type harImportEntry struct {
	harEntry
	Time              *float64                    `json:"time"`
	Request           *harImportRequest           `json:"request"`
	Response          *harImportResponse          `json:"response"`
	Timings           harImportTimings            `json:"timings"`
	ConnectionTimings *harImportConnectionTimings `json:"_connectionTimings"`
}
type harImportConnectionTimings struct {
	DNSStartedAtMicros     *int64 `json:"dnsStartedAtMicros"`
	DNSEndedAtMicros       *int64 `json:"dnsEndedAtMicros"`
	ConnectStartedAtMicros *int64 `json:"connectStartedAtMicros"`
	ConnectEndedAtMicros   *int64 `json:"connectEndedAtMicros"`
	TLSStartedAtMicros     *int64 `json:"tlsStartedAtMicros"`
	TLSEndedAtMicros       *int64 `json:"tlsEndedAtMicros"`
}
type harImportRequest struct {
	harRequest
	BodySize       *int64             `json:"bodySize"`
	StartTimestamp *int64             `json:"_startTimestamp"`
	EndTimestamp   *int64             `json:"_endTimestamp"`
	PostData       *harImportPostData `json:"postData"`
}
type harImportResponse struct {
	harResponse
	BodySize       *int64           `json:"bodySize"`
	StartTimestamp *int64           `json:"_startTimestamp"`
	EndTimestamp   *int64           `json:"_endTimestamp"`
	Content        harImportContent `json:"content"`
}
type harImportContent struct {
	harContent
	Size *int64 `json:"size"`
}
type harImportPostData struct {
	harPostData
	Text *string `json:"text"`
}
type harImportTimings struct {
	Blocked *float64 `json:"blocked"`
	DNS     *float64 `json:"dns"`
	Connect *float64 `json:"connect"`
	SSL     *float64 `json:"ssl"`
	Send    *float64 `json:"send"`
	Wait    *float64 `json:"wait"`
	Receive *float64 `json:"receive"`
}

func convertHARImportEntry(raw []byte, id uint64) (*TrafficEntry, trafficBodyViewInner, []string, error) {
	var input harImportEntry
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, trafficBodyViewInner{}, nil, err
	}
	if input.Request == nil || strings.TrimSpace(input.Request.Method) == "" {
		return nil, trafficBodyViewInner{}, nil, errors.New("har: request is required")
	}
	u, err := url.Parse(input.Request.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "ws" && u.Scheme != "wss") {
		return nil, trafficBodyViewInner{}, nil, errors.New("har: invalid request URL")
	}
	started, err := time.Parse(time.RFC3339Nano, input.StartedDateTime)
	if err != nil || started.UnixMicro() < 0 || !started.Equal(time.Unix(0, started.UnixNano())) {
		return nil, trafficBodyViewInner{}, nil, errors.New("har: invalid start time")
	}
	entry := &TrafficEntry{ID: id, Type: u.Scheme, StartedAt: started, Method: input.Request.Method, URL: input.Request.URL, Host: u.Host, Path: u.RequestURI()}
	entry.Request = importHTTPMessage(input.Request.HTTPVersion, input.Request.Headers, input.Request.BodySize)
	if input.Response != nil {
		if input.Response.Status < 0 || input.Response.Status > 599 || (input.Response.Status > 0 && input.Response.Status < 100) {
			return nil, trafficBodyViewInner{}, nil, errors.New("har: invalid response status")
		}
		entry.StatusCode = input.Response.Status
		if entry.StatusCode > 0 {
			entry.Status = strings.TrimSpace(fmt.Sprintf("%d %s", entry.StatusCode, input.Response.StatusText))
		}
		entry.Response = importHTTPMessage(input.Response.HTTPVersion, input.Response.Headers, input.Response.BodySize)
		if entry.StatusCode == 101 && strings.EqualFold(firstHeaderFieldValue(entry.Response.HeaderFields, "Upgrade"), "websocket") {
			if u.Scheme == "https" {
				entry.Type = "wss"
			} else if u.Scheme == "http" {
				entry.Type = "ws"
			}
		}
	} else {
		entry.Response = importHTTPMessage("", nil, nil)
	}
	entry.Request.Metrics.HeaderSize = logicalHTTPRequestHeaderSize(entry)
	entry.Response.Metrics.HeaderSize = logicalHTTPResponseHeaderSize(entry)
	diagnostics := []string{}
	importHARTimes(entry, &input, &diagnostics)
	importHARMetadata(entry, &input, u)
	if input.Error != "" {
		entry.Error = &TrafficError{Timestamp: started, Error: input.Error}
	}
	requestBody := importHARRequestBody(input.Request)
	responseBody := importHARResponseBody(entry, input.Response)
	body := trafficBodyViewInner{
		RequestBodyReader:  io.NopCloser(bytes.NewReader(requestBody.Data)),
		ResponseBodyReader: io.NopCloser(bytes.NewReader(responseBody.Data)),
		RequestBodySize:    int64(len(requestBody.Data)), ResponseBodySize: int64(len(responseBody.Data)),
		RequestBodyEncoding: requestBody.Encoding, ResponseBodyEncoding: responseBody.Encoding,
		RequestBodyUnavailable: !requestBody.Available, ResponseBodyUnavailable: !responseBody.Available,
	}
	if !requestBody.Available {
		diagnostics = append(diagnostics, "request_body_unavailable")
	}
	if !responseBody.Available {
		diagnostics = append(diagnostics, "response_body_unavailable")
	}
	return entry, body, diagnostics, nil
}

func importHTTPMessage(proto string, fields []harNameValue, size *int64) *HTTPMessage {
	message := &HTTPMessage{Proto: harHTTPVersion(&HTTPMessage{Proto: proto}, nil), HeaderOrderUnavailable: true, TrailerOrderUnavailable: true,
		Metrics: &HTTPMessageMetrics{StartedAtMicros: -1, EndedAtMicros: -1, HeaderSize: -1, BodySize: -1, State: HTTPMessageStateCompleted}}
	if size != nil && *size >= 0 && *size <= 1<<53-1 {
		message.Metrics.BodySize = *size
	}
	if fields != nil {
		message.HeaderFields = make([]HTTPHeaderField, len(fields))
		for i, field := range fields {
			message.HeaderFields[i] = HTTPHeaderField{Name: field.Name, Value: field.Value}
		}
	}
	return message
}

func importHARPayload(text *string, encoding string) HARBody {
	if text == nil {
		return HARBody{}
	}
	if encoding == "" {
		return HARBody{Data: []byte(*text), Available: true}
	}
	if encoding != "base64" {
		return HARBody{}
	}
	data, err := base64.StdEncoding.DecodeString(*text)
	if err != nil {
		return HARBody{}
	}
	return HARBody{Data: data, Available: true, Encoding: "base64"}
}

func importHARRequestBody(request *harImportRequest) HARBody {
	if post := request.PostData; post != nil {
		if post.Text != nil {
			return importHARPayload(post.Text, post.Encoding)
		}
		mediaType, _, _ := mime.ParseMediaType(post.MimeType)
		if mediaType == "application/x-www-form-urlencoded" && post.Params != nil {
			pairs := make([]string, 0, len(post.Params))
			for _, param := range post.Params {
				if param.FileName != nil || param.Encoding != "" {
					return HARBody{}
				}
				pairs = append(pairs, url.QueryEscape(param.Name)+"="+url.QueryEscape(param.Value))
			}
			return HARBody{Data: []byte(strings.Join(pairs, "&")), Available: true}
		}
		return HARBody{}
	}
	return HARBody{Available: request.BodySize != nil && *request.BodySize == 0}
}

func importHARResponseBody(entry *TrafficEntry, response *harImportResponse) HARBody {
	if response == nil {
		return HARBody{}
	}
	if response.Content.Text != nil {
		return importHARPayload(response.Content.Text, response.Content.Encoding)
	}
	empty := strings.EqualFold(entry.Method, "HEAD") || entry.StatusCode == 204 || entry.StatusCode == 304 || (entry.StatusCode >= 100 && entry.StatusCode < 200)
	if response.Content.Size != nil {
		empty = empty || *response.Content.Size == 0
	} else if response.BodySize != nil {
		empty = empty || *response.BodySize == 0
	}
	return HARBody{Available: empty}
}

func importMillis(value *float64) int64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > float64(1<<53-1)/1000 {
		return -1
	}
	return int64(math.Round(*value * 1000))
}

func importTimeAdd(start, duration int64) int64 {
	if start < 0 || duration < 0 || duration > (1<<53-1)-start {
		return -1
	}
	return start + duration
}

func importMessageExtension(metrics *HTTPMessageMetrics, start, end *int64, state string) bool {
	if start == nil || *start < -1 || *start > 1<<53-1 {
		return false
	}
	switch HTTPMessageState(state) {
	case HTTPMessageStatePending, HTTPMessageStateCompleted, HTTPMessageStateFailed, HTTPMessageStateCanceled:
	default:
		return false
	}
	metrics.StartedAtMicros, metrics.EndedAtMicros, metrics.State = *start, -1, HTTPMessageState(state)
	if metrics.State == HTTPMessageStateCompleted && *start >= 0 && end != nil && *end >= *start && *end <= 1<<53-1 {
		metrics.EndedAtMicros = *end
	}
	return true
}

func importHARTimes(entry *TrafficEntry, input *harImportEntry, diagnostics *[]string) {
	request, response := entry.Request.Metrics, entry.Response.Metrics
	start := entry.StartedAt.UnixMicro()
	request.StartedAtMicros = start
	failed := input.Error != "" || entry.StatusCode == 0
	if failed {
		response.State = HTTPMessageStateFailed
	}
	total, receive, wait := importMillis(input.Time), importMillis(input.Timings.Receive), importMillis(input.Timings.Wait)
	if inconsistentHARImportTimings(input) {
		request.State = HTTPMessageStateCompleted
		if failed {
			request.State = HTTPMessageStateFailed
		}
		importMessageExtension(request, input.Request.StartTimestamp, input.Request.EndTimestamp, input.Request.Status)
		if input.Response != nil {
			importMessageExtension(response, input.Response.StartTimestamp, input.Response.EndTimestamp, input.Response.StatusValue)
		}
		*diagnostics = append(*diagnostics, "timing_unavailable")
		return
	}
	if !failed && total >= 0 {
		response.EndedAtMicros = importTimeAdd(start, total)
		if receive >= 0 && receive <= total && response.EndedAtMicros >= start {
			response.StartedAtMicros = response.EndedAtMicros - receive
			if wait >= 0 && wait <= total-receive {
				request.EndedAtMicros = response.StartedAtMicros - wait
			}
		}
	}
	// A known completed send may survive a later failed response. Optional HAR
	// phases with -1 do not apply; unknown mandatory phases remain unknown.
	send := importMillis(input.Timings.Send)
	if request.EndedAtMicros < 0 && send >= 0 {
		end := importTimeAdd(start, send)
		for _, phase := range []*float64{input.Timings.Blocked, input.Timings.DNS, input.Timings.Connect} {
			end = importTimeAdd(end, max(0, importMillis(phase)))
		}
		if total < 0 || (end >= start && end-start <= total) {
			request.EndedAtMicros = end
		}
	}
	if response.StartedAtMicros < 0 && request.EndedAtMicros >= 0 && wait >= 0 {
		candidate := importTimeAdd(request.EndedAtMicros, wait)
		if response.EndedAtMicros < 0 || candidate <= response.EndedAtMicros {
			response.StartedAtMicros = candidate
		}
	}
	if !failed && total < 0 && response.StartedAtMicros >= 0 && receive >= 0 {
		response.EndedAtMicros = importTimeAdd(response.StartedAtMicros, receive)
	}
	if failed && request.EndedAtMicros < 0 {
		request.State = HTTPMessageStateFailed
	}
	importMessageExtension(request, input.Request.StartTimestamp, input.Request.EndTimestamp, input.Request.Status)
	if input.Response != nil {
		importMessageExtension(response, input.Response.StartTimestamp, input.Response.EndTimestamp, input.Response.StatusValue)
	}
	if request.EndedAtMicros < 0 || response.EndedAtMicros < 0 {
		*diagnostics = append(*diagnostics, "timing_unavailable")
	}
}

func importHARMetadata(entry *TrafficEntry, input *harImportEntry, u *url.URL) {
	md := &Metadata{}
	address := input.ServerAddress
	if address == "" {
		address = input.ServerIPAddress
	}
	if net.ParseIP(address) != nil {
		port := u.Port()
		if port == "" {
			if u.Scheme == "https" || u.Scheme == "wss" {
				port = "443"
			} else {
				port = "80"
			}
		}
		if input.ServerPort != nil && *input.ServerPort > 0 && *input.ServerPort <= 65535 {
			port = strconv.Itoa(*input.ServerPort)
		}
		md.RemoteDestinationAddr = net.JoinHostPort(address, port)
	}
	if net.ParseIP(input.ClientAddress) != nil && input.ClientPort != nil && *input.ClientPort > 0 && *input.ClientPort <= 65535 {
		md.LocalSourceAddr = net.JoinHostPort(input.ClientAddress, strconv.Itoa(*input.ClientPort))
	}
	if input.CTime != nil && *input.CTime >= 0 && *input.CTime <= math.MaxInt64/int64(time.Millisecond) {
		md.LocalConnectionEstablishedAt = time.UnixMilli(*input.CTime)
	}
	if input.STime != nil && *input.STime >= 0 && *input.STime <= math.MaxInt64/int64(time.Millisecond) {
		md.RemoteConnectionEstablishedAt = time.UnixMilli(*input.STime)
	}
	if input.App != nil {
		md.Process = &ProcessInfo{Status: ProcessStatusResolved, PID: input.App.PID, DisplayName: input.App.Name, AppID: input.App.ID, ExecutablePath: input.App.Path}
	}
	md.ConnectionTimings = importHARConnectionTimings(entry, input)
	entry.Metadata = md
}

func importHARConnectionTimings(entry *TrafficEntry, input *harImportEntry) *HTTPConnectionTimings {
	unknown := func() *HTTPConnectionTimings {
		return &HTTPConnectionTimings{DNSStartedAtMicros: -1, DNSEndedAtMicros: -1, ConnectStartedAtMicros: -1, ConnectEndedAtMicros: -1, TLSStartedAtMicros: -1, TLSEndedAtMicros: -1}
	}
	result := unknown()
	if input.ConnectionTimings != nil {
		c := input.ConnectionTimings
		pairs := [][2]*int64{{c.DNSStartedAtMicros, c.DNSEndedAtMicros}, {c.ConnectStartedAtMicros, c.ConnectEndedAtMicros}, {c.TLSStartedAtMicros, c.TLSEndedAtMicros}}
		dest := [][2]*int64{{&result.DNSStartedAtMicros, &result.DNSEndedAtMicros}, {&result.ConnectStartedAtMicros, &result.ConnectEndedAtMicros}, {&result.TLSStartedAtMicros, &result.TLSEndedAtMicros}}
		for i, p := range pairs {
			if p[0] != nil && p[1] != nil && *p[0] >= 0 && *p[1] >= *p[0] && *p[1] <= 1<<53-1 {
				*dest[i][0], *dest[i][1] = *p[0], *p[1]
			}
		}
		return result
	}
	if inconsistentHARImportTimings(input) {
		return result
	}
	current := importTimeAdd(entry.StartedAt.UnixMicro(), max(0, importMillis(input.Timings.Blocked)))
	if dns := importMillis(input.Timings.DNS); dns >= 0 {
		end := importTimeAdd(current, dns)
		if current >= 0 && end >= 0 {
			result.DNSStartedAtMicros, result.DNSEndedAtMicros = current, end
		}
		current = end
	}
	connect, ssl := importMillis(input.Timings.Connect), importMillis(input.Timings.SSL)
	if connect >= 0 && current >= 0 {
		end := importTimeAdd(current, connect)
		if end >= 0 {
			result.ConnectStartedAtMicros, result.ConnectEndedAtMicros = current, end
			if ssl >= 0 && ssl <= connect {
				result.ConnectEndedAtMicros = end - ssl
				result.TLSStartedAtMicros, result.TLSEndedAtMicros = end-ssl, end
			}
		}
	}
	// Reject serial reconstructions that extend past the completed upload.
	if end := entry.Request.Metrics.EndedAtMicros; end >= 0 {
		if result.DNSEndedAtMicros > end || result.ConnectEndedAtMicros > end || result.TLSEndedAtMicros > end {
			return unknown()
		}
	}
	return result
}

func inconsistentHARImportTimings(input *harImportEntry) bool {
	total := importMillis(input.Time)
	if total < 0 {
		return false
	}
	sum := int64(0)
	complete := true
	for _, phase := range []*float64{input.Timings.Send, input.Timings.Wait, input.Timings.Receive} {
		value := importMillis(phase)
		if value < 0 {
			complete = false
			continue
		}
		sum = importTimeAdd(sum, value)
		if value > total {
			return true
		}
	}
	for _, phase := range []*float64{input.Timings.Blocked, input.Timings.DNS, input.Timings.Connect} {
		sum = importTimeAdd(sum, max(0, importMillis(phase)))
	}
	// Independent millisecond-to-microsecond rounding can differ by a few us.
	return complete && (sum < 0 || sum-total > 10 || total-sum > 10)
}
