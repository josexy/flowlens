package logic

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	apicollectionservice "github.com/josexy/flowlens/backend/services/api_collection_service"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

// RequestDraftSource says where a request draft came from.
type RequestDraftSource string

// Draft sources.
const (
	DraftNew         RequestDraftSource = "new"
	DraftCaptureEdit RequestDraftSource = "capture-edit"
	DraftHistoryEdit RequestDraftSource = "history-edit"
)

// Body types of an HTTP request.
const (
	BodyTypeNone       = "none"
	BodyTypeJSON       = "json"
	BodyTypeText       = "text"
	BodyTypeXML        = "xml"
	BodyTypeFile       = "file"
	BodyTypeFormData   = "form-data"
	BodyTypeURLEncoded = "urlencoded"
)

// Proxy modes of a sent request.
const (
	ProxyModeNone    = "none"
	ProxyModeSystem  = "system"
	ProxyModeMITM    = "mitm"
	FormDataItemText = "text"
	FormDataItemFile = "file"
	ProxyModeCustom  = "custom"
)

// DefaultTLSClientHello is the TLS profile of a new request.
const DefaultTLSClientHello = "golang"

// TLSClientHelloIDs lists the TLS profiles.
var TLSClientHelloIDs = []string{"golang", "chrome_auto", "firefox_auto", "safari_auto", "edge_auto", "ios_auto", "android_11_okhttp", "randomized_alpn"}

// DefaultHTTPRequestPythonScript is the script of a new request.
const DefaultHTTPRequestPythonScript = `from flowlens import *

def onRequest(context, request):
    print(f"request {request.method} {request.url}")
    return request

def onResponse(context, response):
    print(f"response {response.code}")
    return response
`

// Workspace tab types.
const (
	TabTypeCapture         = "capture"
	TabTypeHistory         = "history"
	TabTypeHTTPRequest     = "http-request"
	TabTypeWebSocketClient = "websocket-client"

	// CaptureTabKey is the capture tab's key.
	CaptureTabKey = "capture"
	// HistoryTabKey is the history tab's key.
	HistoryTabKey = "history"
	// HTTPRequestTabPrefix starts the key of a request tab.
	HTTPRequestTabPrefix = "http-request:"
	// WebSocketClientTabPrefix starts the key of a WebSocket tab.
	WebSocketClientTabPrefix = "websocket-client:"
)

// RequestFileValue is a file chosen for a request.
type RequestFileValue struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// FormDataItem is one row of a multipart body.
type FormDataItem struct {
	ID       string
	Enabled  bool
	Name     string
	ItemType string
	Value    string
	File     *RequestFileValue
}

// HTTPSendSettings are how a request is sent.
type HTTPSendSettings struct {
	ProxyMode        string `json:"proxyMode"`
	Protocol         string `json:"protocol"`
	CustomProxy      string `json:"customProxy"`
	TimeoutMs        int64  `json:"timeoutMs"`
	MaxRedirects     int    `json:"maxRedirects"`
	SkipVerifyTLS    bool   `json:"skipVerifyTls"`
	TLSClientHelloID string `json:"tlsClientHelloId"`
	HTTP2Fingerprint string `json:"http2Fingerprint"`
}

// HTTPResponseView is the response shown for a request.
type HTTPResponseView struct {
	Kind, Outcome, StreamState, StatusText, Protocol, Body, BodyEncoding, ErrorMessage string
	PluginExecution                                                                    *proxyservice.HTTPRequestPluginExecution
	StatusCode                                                                         int
	ElapsedMicros                                                                      int64
	Headers, Trailers                                                                  []EditableRow
	HeadersHaveWireOrder, HeadersTruncated, TrailersHaveWireOrder, TrailersTruncated   bool
}

// HTTPEditorState is one HTTP Request Editor tab.
type HTTPEditorState struct {
	Source                 RequestDraftSource
	SourceEntryID          uint64
	SourceHistoryKey, Name string
	IsSending, IsStreaming bool
	StreamSessionID        string
	PluginsEnabled         bool
	InlineScriptEnabled    bool
	InlineScriptSource     string
	ScriptConsoleEntries   []PluginLogEntry
	ScriptExecutionID      string
	SentAt                 time.Time
	ActiveRequestTab       string
	ActiveResponseTab      string
	Method, URL            string
	Params, Headers        []EditableRow
	RequestBodyType        string
	RequestBodyText        string
	RequestBodyFile        *RequestFileValue
	RequestBodyFormData    []FormDataItem
	RequestBodyURLEncoded  []EditableRow
	Settings               HTTPSendSettings
	Response               *HTTPResponseView
}

// WebSocketSettings are how a WebSocket connects.
type WebSocketSettings struct {
	ProxyMode        string `json:"proxyMode"`
	CustomProxy      string `json:"customProxy"`
	TimeoutMs        int64  `json:"timeoutMs"`
	SkipVerifyTLS    bool   `json:"skipVerifyTls"`
	TLSClientHelloID string `json:"tlsClientHelloId"`
}

// WebSocketClientState is one WebSocket Client tab.
type WebSocketClientState struct {
	Source                 RequestDraftSource
	SourceEntryID          uint64
	SourceHistoryKey, Name string
	IsSendingMessage       bool
	ActiveLeftTab          string
	ActiveRightTab         string
	URL                    string
	Params, Headers        []EditableRow
	ResponseHeaders        []EditableRow
	ResponseError          string
	Settings               WebSocketSettings
	SessionID              string
	DraftType, DraftText   string
	DraftFile              *RequestFileValue
	Messages               []WebSocketDisplayMessage
	ConnectionStatus       string
}

// WorkspaceTab is one tab of the traffic workspace.
type WorkspaceTab struct {
	Key, Type, Title string
	Closable         bool
	HistoryKey       string
	APIID            string
	APIUpdatedAt     int64
	SavedSnapshot    string
	HTTP             *HTTPEditorState
	WebSocket        *WebSocketClientState
}

var rowSequence atomic.Uint64

func newRowID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixMilli(), rowSequence.Add(1))
}

func emptyRow() EditableRow { return EditableRow{Enabled: true} }

// NewFormDataRow returns an empty form row.
func NewFormDataRow() FormDataItem {
	return FormDataItem{ID: newRowID(), Enabled: true, ItemType: "text"}
}

func hasURLQuery(rawURL string) bool {
	fragment := strings.IndexByte(rawURL, '#')
	query := strings.IndexByte(rawURL, '?')
	return query >= 0 && (fragment < 0 || query < fragment)
}

func rawURLQuery(rawURL string) string {
	if !hasURLQuery(rawURL) {
		return ""
	}
	end := len(rawURL)
	if fragment := strings.IndexByte(rawURL, '#'); fragment >= 0 {
		end = fragment
	}
	return rawURL[strings.IndexByte(rawURL, '?')+1 : end]
}

// ParseRequestQueryRows returns the editable rows of a URL's query.
func ParseRequestQueryRows(rawURL string) []EditableRow {
	fields := DecodeQuery(rawURLQuery(rawURL))
	if len(fields) == 0 {
		return []EditableRow{emptyRow()}
	}
	rows := make([]EditableRow, len(fields))
	for index, field := range fields {
		rows[index] = EditableRow{Key: field.Name, Value: field.Value, Enabled: true}
	}
	return rows
}

// encodeFormComponent escapes as URLSearchParams does.
func encodeFormComponent(value string) string {
	var out strings.Builder
	for _, b := range []byte(value) {
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '*', b == '-', b == '.', b == '_':
			out.WriteByte(b)
		case b == ' ':
			out.WriteByte('+')
		default:
			fmt.Fprintf(&out, "%%%02X", b)
		}
	}
	return out.String()
}

// SerializeRequestQueryRows writes enabled, named rows as a query string.
func SerializeRequestQueryRows(rows []EditableRow) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		key := strings.TrimSpace(row.Key)
		if row.Enabled && key != "" {
			parts = append(parts, encodeFormComponent(key)+"="+encodeFormComponent(row.Value))
		}
	}
	return strings.Join(parts, "&")
}

// CountRequestQueryRows counts rows that take effect.
func CountRequestQueryRows(rows []EditableRow) int {
	count := 0
	for _, row := range rows {
		if row.Enabled && strings.TrimSpace(row.Key) != "" {
			count++
		}
	}
	return count
}

// ReplaceRequestURLQuery replaces a URL's query with rows.
func ReplaceRequestURLQuery(rawURL string, rows []EditableRow) string {
	base, fragment := rawURL, ""
	if index := strings.IndexByte(rawURL, '#'); index >= 0 {
		base, fragment = rawURL[:index], rawURL[index:]
	}
	if index := strings.IndexByte(base, '?'); index >= 0 {
		base = base[:index]
	}
	if query := SerializeRequestQueryRows(rows); query != "" {
		return base + "?" + query + fragment
	}
	return base + fragment
}

func restoreSavedURL(rawURL string, params []EditableRow) string {
	if hasURLQuery(rawURL) {
		return rawURL
	}
	return ReplaceRequestURLQuery(rawURL, params)
}

// NormalizeHTTPMaxRedirects keeps nonnegative whole numbers.
func NormalizeHTTPMaxRedirects(value float64) int {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	return int(math.Trunc(value))
}

func defaultHTTPSettings() HTTPSendSettings {
	return HTTPSendSettings{ProxyMode: ProxyModeNone, Protocol: string(ProtocolAuto), TLSClientHelloID: DefaultTLSClientHello}
}

func defaultWebSocketSettings() WebSocketSettings {
	return WebSocketSettings{ProxyMode: ProxyModeNone, TLSClientHelloID: DefaultTLSClientHello}
}

func normalizeTLSClientHello(value string) string {
	if slices.Contains(TLSClientHelloIDs, value) {
		return value
	}
	return DefaultTLSClientHello
}

// BuildEmptyHTTPEditorState returns a new request.
func BuildEmptyHTTPEditorState(source RequestDraftSource) *HTTPEditorState {
	return &HTTPEditorState{
		Source: source, InlineScriptSource: DefaultHTTPRequestPythonScript, ActiveRequestTab: "headers", ActiveResponseTab: "response-headers",
		Method: "GET", Params: []EditableRow{emptyRow()}, Headers: []EditableRow{emptyRow()}, RequestBodyType: BodyTypeNone,
		RequestBodyFormData: []FormDataItem{NewFormDataRow()}, RequestBodyURLEncoded: []EditableRow{emptyRow()}, Settings: defaultHTTPSettings(),
	}
}

// BuildEmptyWebSocketClientState returns a new WebSocket client.
func BuildEmptyWebSocketClientState(source RequestDraftSource) *WebSocketClientState {
	return &WebSocketClientState{
		Source: source, ActiveLeftTab: "message", ActiveRightTab: "response-headers", Params: []EditableRow{emptyRow()}, Headers: []EditableRow{emptyRow()},
		Settings: defaultWebSocketSettings(), DraftType: "text", ConnectionStatus: "idle",
	}
}

func savedRows(items []EditableRow) []*apicollectionservice.SavedKeyValue {
	var saved []*apicollectionservice.SavedKeyValue
	for _, item := range items {
		if strings.TrimSpace(item.Key) != "" || strings.TrimSpace(item.Value) != "" {
			saved = append(saved, &apicollectionservice.SavedKeyValue{Key: item.Key, Value: item.Value, Enabled: item.Enabled})
		}
	}
	return saved
}

func withoutPseudoRows(rows []EditableRow) []EditableRow {
	return slices.DeleteFunc(slices.Clone(rows), func(item EditableRow) bool { return strings.HasPrefix(strings.TrimSpace(item.Key), ":") })
}

func rowsFromSaved(items []*apicollectionservice.SavedKeyValue) []EditableRow {
	var rows []EditableRow
	for _, item := range items {
		if item != nil {
			rows = append(rows, EditableRow{Key: item.Key, Value: item.Value, Enabled: item.Enabled})
		}
	}
	if len(rows) == 0 {
		return []EditableRow{emptyRow()}
	}
	return rows
}

func headerRowsFromSaved(items []*apicollectionservice.SavedKeyValue) []EditableRow {
	rows := withoutPseudoRows(rowsFromSaved(items))
	if len(rows) == 0 {
		return []EditableRow{emptyRow()}
	}
	return rows
}

func savedFile(file *RequestFileValue) *apicollectionservice.SavedFile {
	if file == nil || file.Path == "" {
		return nil
	}
	return &apicollectionservice.SavedFile{Path: file.Path, Name: file.Name, Size: file.Size}
}

func fileFromSaved(file *apicollectionservice.SavedFile) *RequestFileValue {
	if file == nil || file.Path == "" {
		return nil
	}
	return &RequestFileValue{Path: file.Path, Name: file.Name, Size: file.Size}
}

func savedFormData(items []FormDataItem) []*apicollectionservice.SavedFormDataItem {
	var saved []*apicollectionservice.SavedFormDataItem
	for _, item := range items {
		if strings.TrimSpace(item.Name) == "" && strings.TrimSpace(item.Value) == "" && (item.File == nil || item.File.Path == "") {
			continue
		}
		saved = append(saved, &apicollectionservice.SavedFormDataItem{ID: item.ID, Enabled: item.Enabled, Name: item.Name, ItemType: item.ItemType, Value: item.Value, File: savedFile(item.File)})
	}
	return saved
}

func formDataFromSaved(items []*apicollectionservice.SavedFormDataItem) []FormDataItem {
	var rows []FormDataItem
	for _, item := range items {
		if item == nil {
			continue
		}
		id, kind := item.ID, "text"
		if id == "" {
			id = newRowID()
		}
		if item.ItemType == "file" {
			kind = "file"
		}
		rows = append(rows, FormDataItem{ID: id, Enabled: item.Enabled, Name: item.Name, ItemType: kind, Value: item.Value, File: fileFromSaved(item.File)})
	}
	if len(rows) == 0 {
		return []FormDataItem{NewFormDataRow()}
	}
	return rows
}

// BuildSavedHTTPRequest returns what a collection stores for a request.
func BuildSavedHTTPRequest(state *HTTPEditorState) *apicollectionservice.SavedHTTPRequest {
	source := state.InlineScriptSource
	return &apicollectionservice.SavedHTTPRequest{
		Method: state.Method, URL: state.URL, Params: savedRows(state.Params), Headers: savedRows(withoutPseudoRows(state.Headers)),
		BodyType: state.RequestBodyType, BodyText: state.RequestBodyText, BodyFile: savedFile(state.RequestBodyFile),
		BodyFormData: savedFormData(state.RequestBodyFormData), BodyURLEncoded: savedRows(state.RequestBodyURLEncoded), InlineScriptSource: &source,
		ProxyMode: proxyservice.SendRequestProxyMode(state.Settings.ProxyMode), Protocol: proxyservice.SendRequestProtocol(state.Settings.Protocol),
		CustomProxy: state.Settings.CustomProxy, TimeoutMs: state.Settings.TimeoutMs, SkipVerifyTLS: state.Settings.SkipVerifyTLS,
		TLSClientHelloID: proxyservice.TLSClientHelloID(state.Settings.TLSClientHelloID), HTTP2Fingerprint: state.Settings.HTTP2Fingerprint, MaxRedirects: state.Settings.MaxRedirects,
	}
}

// BuildSavedWebSocketRequest returns what a collection stores for a client.
func BuildSavedWebSocketRequest(state *WebSocketClientState) *apicollectionservice.SavedWebSocketRequest {
	return &apicollectionservice.SavedWebSocketRequest{
		URL: state.URL, Params: savedRows(state.Params), Headers: savedRows(withoutPseudoRows(state.Headers)), DraftType: state.DraftType, DraftText: state.DraftText,
		DraftFile: savedFile(state.DraftFile), ProxyMode: proxyservice.SendRequestProxyMode(state.Settings.ProxyMode), CustomProxy: state.Settings.CustomProxy,
		TimeoutMs: state.Settings.TimeoutMs, SkipVerifyTLS: state.Settings.SkipVerifyTLS, TLSClientHelloID: proxyservice.TLSClientHelloID(state.Settings.TLSClientHelloID),
	}
}

func savedHTTPSettings(request *apicollectionservice.SavedHTTPRequest, protocol RequestProtocol) HTTPSendSettings {
	mode := string(request.ProxyMode)
	if mode == "" {
		mode = ProxyModeNone
	}
	return HTTPSendSettings{
		ProxyMode: mode, Protocol: string(protocol), CustomProxy: request.CustomProxy, TimeoutMs: request.TimeoutMs, SkipVerifyTLS: request.SkipVerifyTLS,
		TLSClientHelloID: normalizeTLSClientHello(string(request.TLSClientHelloID)), HTTP2Fingerprint: request.HTTP2Fingerprint, MaxRedirects: NormalizeHTTPMaxRedirects(float64(request.MaxRedirects)),
	}
}

func savedInlineScript(request *apicollectionservice.SavedHTTPRequest) string {
	if request.InlineScriptSource == nil {
		return DefaultHTTPRequestPythonScript
	}
	return *request.InlineScriptSource
}

// ApplySavedHTTPRequest replaces a tab's request with a saved one.
func ApplySavedHTTPRequest(state *HTTPEditorState, request *apicollectionservice.SavedHTTPRequest, name string) {
	state.Name = name
	state.Method = request.Method
	if state.Method == "" {
		state.Method = "GET"
	}
	state.Params = rowsFromSaved(request.Params)
	state.URL = restoreSavedURL(request.URL, state.Params)
	protocol := NormalizeRequestProtocol(string(request.Protocol))
	state.Headers = ConvertRequestRouteHeaders(headerRowsFromSaved(request.Headers), protocol, state.URL)
	state.RequestBodyType = request.BodyType
	if state.RequestBodyType == "" {
		state.RequestBodyType = BodyTypeNone
	}
	state.RequestBodyText, state.RequestBodyFile = request.BodyText, fileFromSaved(request.BodyFile)
	state.RequestBodyFormData, state.RequestBodyURLEncoded = formDataFromSaved(request.BodyFormData), rowsFromSaved(request.BodyURLEncoded)
	state.InlineScriptSource = savedInlineScript(request)
	state.Settings = savedHTTPSettings(request, protocol)
}

// BuildHTTPEditorStateFromSaved opens a saved request.
func BuildHTTPEditorStateFromSaved(request *apicollectionservice.SavedHTTPRequest, name string) *HTTPEditorState {
	state := BuildEmptyHTTPEditorState(DraftNew)
	ApplySavedHTTPRequest(state, request, name)
	return state
}

// ApplySavedWebSocketRequest replaces a tab's client with a saved one.
func ApplySavedWebSocketRequest(state *WebSocketClientState, request *apicollectionservice.SavedWebSocketRequest, name string) {
	state.Name = name
	state.Params = rowsFromSaved(request.Params)
	state.URL = restoreSavedURL(request.URL, state.Params)
	state.Headers = headerRowsFromSaved(request.Headers)
	mode := string(request.ProxyMode)
	if mode == "" {
		mode = ProxyModeNone
	}
	state.Settings = WebSocketSettings{ProxyMode: mode, CustomProxy: request.CustomProxy, TimeoutMs: request.TimeoutMs, SkipVerifyTLS: request.SkipVerifyTLS, TLSClientHelloID: normalizeTLSClientHello(string(request.TLSClientHelloID))}
	state.DraftType = request.DraftType
	if state.DraftType == "" {
		state.DraftType = "text"
	}
	state.DraftText, state.DraftFile = request.DraftText, fileFromSaved(request.DraftFile)
}

// BuildWebSocketClientStateFromSaved opens a saved client.
func BuildWebSocketClientStateFromSaved(request *apicollectionservice.SavedWebSocketRequest, name string) *WebSocketClientState {
	state := BuildEmptyWebSocketClientState(DraftNew)
	ApplySavedWebSocketRequest(state, request, name)
	return state
}

type snapshotFormItem struct {
	Enabled  bool              `json:"enabled"`
	Name     string            `json:"name"`
	ItemType string            `json:"itemType"`
	Value    string            `json:"value"`
	File     *RequestFileValue `json:"file"`
}

type httpSnapshot struct {
	Type         string             `json:"type"`
	Method       string             `json:"method"`
	URL          string             `json:"url"`
	Params       []EditableRow      `json:"params"`
	Headers      []EditableRow      `json:"headers"`
	BodyType     string             `json:"requestBodyType"`
	BodyText     string             `json:"requestBodyText"`
	BodyFile     *RequestFileValue  `json:"requestBodyFile"`
	FormData     []snapshotFormItem `json:"requestBodyFormData"`
	URLEncoded   []EditableRow      `json:"requestBodyUrlEncoded"`
	InlineScript string             `json:"inlineScriptSource"`
	Settings     HTTPSendSettings   `json:"settings"`
}

type webSocketSnapshot struct {
	Type      string            `json:"type"`
	URL       string            `json:"url"`
	Params    []EditableRow     `json:"params"`
	Headers   []EditableRow     `json:"headers"`
	DraftType string            `json:"draftType"`
	DraftText string            `json:"draftText"`
	DraftFile *RequestFileValue `json:"draftFile"`
	Settings  WebSocketSettings `json:"settings"`
}

func meaningfulRows(rows []EditableRow) []EditableRow {
	result := []EditableRow{}
	for _, row := range rows {
		if row.Enabled && (strings.TrimSpace(row.Key) != "" || strings.TrimSpace(row.Value) != "") {
			result = append(result, row)
		}
	}
	return result
}

func meaningfulHeaders(rows []EditableRow) []EditableRow {
	result := []EditableRow{}
	for _, row := range rows {
		if !strings.HasPrefix(strings.TrimSpace(row.Key), ":") && (strings.TrimSpace(row.Key) != "" || strings.TrimSpace(row.Value) != "") {
			result = append(result, row)
		}
	}
	return result
}

func meaningfulFormData(rows []FormDataItem) []snapshotFormItem {
	result := []snapshotFormItem{}
	for _, row := range rows {
		if row.Enabled && (strings.TrimSpace(row.Name) != "" || strings.TrimSpace(row.Value) != "" || (row.File != nil && row.File.Path != "")) {
			result = append(result, snapshotFormItem{row.Enabled, row.Name, row.ItemType, row.Value, row.File})
		}
	}
	return result
}

func httpSnapshotOf(state *HTTPEditorState) httpSnapshot {
	return httpSnapshot{
		Type: "http", Method: strings.TrimSpace(state.Method), URL: strings.TrimSpace(state.URL), Params: meaningfulRows(state.Params), Headers: meaningfulHeaders(state.Headers),
		BodyType: state.RequestBodyType, BodyText: state.RequestBodyText, BodyFile: state.RequestBodyFile, FormData: meaningfulFormData(state.RequestBodyFormData),
		URLEncoded: meaningfulRows(state.RequestBodyURLEncoded), InlineScript: state.InlineScriptSource, Settings: state.Settings,
	}
}

func webSocketSnapshotOf(state *WebSocketClientState) webSocketSnapshot {
	return webSocketSnapshot{
		Type: "websocket", URL: strings.TrimSpace(state.URL), Params: meaningfulRows(state.Params), Headers: meaningfulHeaders(state.Headers),
		DraftType: state.DraftType, DraftText: state.DraftText, DraftFile: state.DraftFile, Settings: state.Settings,
	}
}

// CreateSnapshotForTab returns a value that changes when a tab's saved content
// would.
func CreateSnapshotForTab(tab *WorkspaceTab) string {
	var payload any
	switch {
	case tab.Type == TabTypeHTTPRequest && tab.HTTP != nil:
		payload = httpSnapshotOf(tab.HTTP)
	case tab.Type == TabTypeWebSocketClient && tab.WebSocket != nil:
		payload = webSocketSnapshotOf(tab.WebSocket)
	default:
		return ""
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

// HasMeaningfulRequestDraft reports whether a tab holds anything worth keeping.
func HasMeaningfulRequestDraft(tab *WorkspaceTab) bool {
	switch {
	case tab.Type == TabTypeHTTPRequest && tab.HTTP != nil:
		s := httpSnapshotOf(tab.HTTP)
		return s.URL != "" || s.Method != "GET" || len(s.Params) > 0 || len(s.Headers) > 0 || s.BodyType != BodyTypeNone || s.BodyText != "" ||
			s.BodyFile != nil || len(s.FormData) > 0 || len(s.URLEncoded) > 0 || s.InlineScript != DefaultHTTPRequestPythonScript ||
			s.Settings.ProxyMode != ProxyModeNone || s.Settings.Protocol != string(ProtocolAuto) || s.Settings.CustomProxy != "" || s.Settings.TimeoutMs > 0 ||
			s.Settings.MaxRedirects > 0 || s.Settings.SkipVerifyTLS || s.Settings.TLSClientHelloID != DefaultTLSClientHello || strings.TrimSpace(s.Settings.HTTP2Fingerprint) != ""
	case tab.Type == TabTypeWebSocketClient && tab.WebSocket != nil:
		s := webSocketSnapshotOf(tab.WebSocket)
		return s.URL != "" || len(s.Params) > 0 || len(s.Headers) > 0 || s.DraftType != "text" || s.DraftText != "" || s.DraftFile != nil ||
			s.Settings.ProxyMode != ProxyModeNone || s.Settings.CustomProxy != "" || s.Settings.TimeoutMs > 0 || s.Settings.SkipVerifyTLS || s.Settings.TLSClientHelloID != DefaultTLSClientHello
	default:
		return false
	}
}

// ApplySavedMetadata links a tab to its saved request.
func ApplySavedMetadata(tab *WorkspaceTab, request *apicollectionservice.APICollectionRequest) {
	tab.APIID, tab.APIUpdatedAt = request.ID, request.UpdatedAt
	tab.SavedSnapshot = CreateSnapshotForTab(tab)
}

// ErrRequestBodyUnavailable reports a captured body that is missing.
var ErrRequestBodyUnavailable = fmt.Errorf("request_body_unavailable")

func inferBodyType(fields []HeaderField, body string) string {
	value, _ := FirstHeaderValue(fields, "Content-Type")
	contentType := strings.ToLower(value)
	switch {
	case contentType == "":
		if body != "" {
			return BodyTypeText
		}
		return BodyTypeNone
	case strings.Contains(contentType, "json"):
		return BodyTypeJSON
	case strings.Contains(contentType, "xml"):
		return BodyTypeXML
	case strings.Contains(contentType, "multipart/form-data"):
		return BodyTypeFormData
	case strings.Contains(contentType, "application/x-www-form-urlencoded"):
		return BodyTypeURLEncoded
	default:
		return BodyTypeText
	}
}

func trafficHeaderRows(request *proxyservice.HTTPMessage) []EditableRow {
	if request == nil || request.HeaderFields == nil {
		return []EditableRow{emptyRow()}
	}
	rows := EditableHeaderFieldsToRows(request.HeaderFields)
	if len(rows) == 0 {
		return []EditableRow{emptyRow()}
	}
	return rows
}

// HTTPEditorStateFromTraffic opens a captured request for editing.
func HTTPEditorStateFromTraffic(source RequestDraftSource, entry *TrafficEntry, body *proxyservice.TrafficBodyView, historyKey string) (*HTTPEditorState, error) {
	if body != nil && body.RequestBodyUnavailable {
		return nil, ErrRequestBodyUnavailable
	}
	method := entry.Method
	if method == "" {
		method = "GET"
	}
	protocol := InferRequestProtocol(entry.Request)
	requestBody, headers := "", []HeaderField(nil)
	if body != nil {
		requestBody = body.RequestBody
	}
	if entry.Request != nil {
		headers = entry.Request.HeaderFields
	}
	state := BuildEmptyHTTPEditorState(source)
	state.SourceEntryID, state.SourceHistoryKey, state.Name = entry.ID, historyKey, entry.Host
	state.Method, state.URL = method, entry.URL
	state.Params = ParseRequestQueryRows(entry.URL)
	state.Headers = ConvertRequestRouteHeaders(trafficHeaderRows(entry.Request), protocol, entry.URL)
	state.RequestBodyType, state.RequestBodyText = inferBodyType(headers, requestBody), requestBody
	state.Settings.Protocol = string(protocol)
	return state, nil
}

// WebSocketClientStateFromTraffic opens a captured WebSocket for editing.
func WebSocketClientStateFromTraffic(source RequestDraftSource, entry *TrafficEntry, historyKey string) *WebSocketClientState {
	state := BuildEmptyWebSocketClientState(source)
	state.SourceEntryID, state.SourceHistoryKey, state.Name = entry.ID, historyKey, entry.Host
	state.URL, state.Params = entry.URL, ParseRequestQueryRows(entry.URL)
	state.Headers = trafficHeaderRows(entry.Request)
	return state
}

// ApplyRequestBodyRecovery fills a request from recovered body parts.
func ApplyRequestBodyRecovery(state *HTTPEditorState, recovery *proxyservice.RequestBodyRecoveryResult) {
	if recovery == nil {
		return
	}
	state.RequestBodyType, state.RequestBodyText = string(recovery.BodyType), recovery.Text
	state.RequestBodyFile = fileFromDraft(recovery.File)
	var formData []FormDataItem
	for _, item := range recovery.FormData {
		if item == nil {
			continue
		}
		kind := "text"
		if item.ItemType == "file" {
			kind = "file"
		}
		formData = append(formData, FormDataItem{ID: newRowID(), Enabled: item.Enabled, Name: item.Name, ItemType: kind, Value: item.Value, File: fileFromDraft(item.File)})
	}
	if len(formData) == 0 {
		formData = []FormDataItem{NewFormDataRow()}
	}
	state.RequestBodyFormData = formData
	var encoded []EditableRow
	for _, item := range recovery.URLEncoded {
		if item != nil {
			encoded = append(encoded, EditableRow{Key: item.Name, Value: item.Value, Enabled: item.Enabled})
		}
	}
	if len(encoded) == 0 {
		encoded = []EditableRow{emptyRow()}
	}
	state.RequestBodyURLEncoded = encoded
}

func fileFromDraft(file *proxyservice.RequestDraftFile) *RequestFileValue {
	if file == nil || file.Path == "" {
		return nil
	}
	return &RequestFileValue{Path: file.Path, Name: file.Name, Size: file.Size}
}

var windowsDrivePattern = regexp.MustCompile(`^[a-zA-Z]:[\\/]`)

func normalizeAbsolutePath(value string, windows bool) (string, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", false
	}
	separated := raw
	if windows {
		separated = strings.ReplaceAll(raw, `\`, "/")
	}
	unc := windows && strings.HasPrefix(separated, "//")
	drive := windows && len(separated) >= 3 && separated[1] == ':' && separated[2] == '/'
	if !unc && !drive && !strings.HasPrefix(separated, "/") {
		return "", false
	}
	prefix, remainder := "", separated
	if drive {
		prefix, remainder = separated[:2], separated[2:]
	}
	cleaned := prefix + path.Clean("/"+strings.TrimLeft(remainder, "/"))
	if windows {
		cleaned = strings.ToLower(cleaned)
	}
	return cleaned, true
}

func insideDraftCache(candidate, root string) bool {
	trimmed := strings.TrimSpace(root)
	windows := windowsDrivePattern.MatchString(trimmed) || strings.HasPrefix(trimmed, `\\`) || strings.HasPrefix(trimmed, "//")
	normalizedRoot, ok := normalizeAbsolutePath(trimmed, windows)
	if !ok {
		return false
	}
	normalized, ok := normalizeAbsolutePath(candidate, windows)
	return ok && (normalized == normalizedRoot || strings.HasPrefix(normalized, normalizedRoot+"/"))
}

// ClearRequestDraftCacheFileReferences drops file references inside the draft
// cache after it was cleared, and returns how many it dropped.
func ClearRequestDraftCacheFileReferences(tabs []*WorkspaceTab, root string) int {
	cleared := 0
	for _, tab := range tabs {
		switch {
		case tab.Type == TabTypeHTTPRequest && tab.HTTP != nil:
			state := tab.HTTP
			if state.RequestBodyFile != nil && insideDraftCache(state.RequestBodyFile.Path, root) {
				state.RequestBodyFile = nil
				cleared++
			}
			for index := range state.RequestBodyFormData {
				if file := state.RequestBodyFormData[index].File; file != nil && insideDraftCache(file.Path, root) {
					state.RequestBodyFormData[index].File = nil
					cleared++
				}
			}
		case tab.Type == TabTypeWebSocketClient && tab.WebSocket != nil && tab.WebSocket.DraftFile != nil && insideDraftCache(tab.WebSocket.DraftFile.Path, root):
			tab.WebSocket.DraftFile = nil
			cleared++
		}
	}
	return cleared
}

// DeriveRequestTabTitle names a tab after the host of a URL or partial URL.
func DeriveRequestTabTitle(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	leading := trimmed
	if index := strings.IndexAny(trimmed, "/?#"); index >= 0 {
		leading = trimmed[:index]
	}
	if leading == "" {
		return ""
	}
	if parsed, err := url.Parse("http://" + leading); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return leading
}
