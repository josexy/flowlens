package logic

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/locales"
)

// Plugin outcomes reported for a sent request.
const (
	PluginOutcomeCompleted          = "completed"
	PluginOutcomeCompletedWithError = "completed_with_plugin_error"
	PluginOutcomeBlockedRequest     = "blocked_request"
	PluginOutcomeBlockedResponse    = "blocked_response"
	PluginOutcomePluginFailed       = "plugin_failed"
	CurrentRequestScriptPluginID    = "current-request-script"
	ResponseKindSuccess             = "success"
	ResponseKindPlugin              = "plugin"
	ResponseKindError               = "error"
	ResponseKindCancelled           = "cancelled"
	StreamStateStreaming            = "streaming"
	StreamStateStopped              = "stopped"
	StreamStateCompleted            = "completed"
	StreamStateError                = "error"
	RequestTabHeaders               = "headers"
	RequestTabQuery                 = "query"
	RequestTabCookies               = "cookies"
	RequestTabBody                  = "body"
	RequestTabScript                = "script"
	RequestTabSettings              = "settings"
	ResponseTabHeaders              = "response-headers"
	ResponseTabCookies              = "response-cookies"
	ResponseTabTrailers             = "response-trailers"
	ResponseTabBody                 = "response-body"
	ResponseTabConsole              = "response-console"
	ResponseTabError                = "response-error"
	MaxRequestConsoleEntries        = 1000
)

// ValidateRequestURL returns why a URL cannot be sent, or "".
func ValidateRequestURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" {
		return locales.WorkspaceHttpRequestInvalidUrl()
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "http" && scheme != "https" {
		return locales.WorkspaceHttpRequestInvalidUrlScheme()
	}
	if parsed.Host == "" {
		return locales.WorkspaceHttpRequestInvalidUrl()
	}
	return ""
}

// ValidateWebSocketURL returns why a URL cannot be connected to, or "".
func ValidateWebSocketURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" {
		return locales.WorkspaceWebsocketClientInvalidUrl()
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme != "ws" && scheme != "wss" {
		return locales.WorkspaceWebsocketClientInvalidUrlScheme()
	}
	if parsed.Host == "" {
		return locales.WorkspaceWebsocketClientInvalidUrl()
	}
	return ""
}

// SendRequestFileOf converts a chosen file for sending.
func SendRequestFileOf(file *RequestFileValue) *proxyservice.SendRequestFile {
	if file == nil {
		return nil
	}
	return &proxyservice.SendRequestFile{Path: file.Path, Name: file.Name, Size: file.Size}
}

// BuildRequestBodyPayload returns the body to send, or why it cannot be sent.
func BuildRequestBodyPayload(state *HTTPEditorState) (proxyservice.SendRequestBody, error) {
	bodyType := state.RequestBodyType
	body := proxyservice.SendRequestBody{BodyType: proxyservice.SendRequestBodyType(bodyType)}
	switch bodyType {
	case BodyTypeNone:
	case BodyTypeJSON, BodyTypeText, BodyTypeXML:
		body.Text = state.RequestBodyText
	case BodyTypeFile:
		if state.RequestBodyFile == nil || state.RequestBodyFile.Path == "" {
			return body, errors.New(locales.WorkspaceHttpRequestFileRequired())
		}
		body.File = SendRequestFileOf(state.RequestBodyFile)
	case BodyTypeURLEncoded:
		for _, row := range state.RequestBodyURLEncoded {
			if name := strings.TrimSpace(row.Key); row.Enabled && name != "" {
				body.URLEncoded = append(body.URLEncoded, &proxyservice.SendRequestURLEncodedItem{Enabled: true, Name: name, Value: row.Value})
			}
		}
	default:
		for _, item := range state.RequestBodyFormData {
			name := strings.TrimSpace(item.Name)
			if !item.Enabled || name == "" {
				continue
			}
			if item.ItemType != FormDataItemFile {
				body.FormData = append(body.FormData, &proxyservice.SendRequestFormDataItem{Enabled: true, Name: name, ItemType: FormDataItemText, Value: item.Value})
				continue
			}
			if item.File == nil || item.File.Path == "" {
				return body, errors.New(locales.WorkspaceHttpRequestFormDataFileRequired(name))
			}
			body.FormData = append(body.FormData, &proxyservice.SendRequestFormDataItem{Enabled: true, Name: name, ItemType: FormDataItemFile, File: SendRequestFileOf(item.File)})
		}
	}
	return body, nil
}

// BuildSendConfig returns how a tab's request is sent.
func BuildSendConfig(state *HTTPEditorState, pluginExecutionID string) proxyservice.SendRequestConfig {
	settings := state.Settings
	config := proxyservice.SendRequestConfig{
		ProxyMode: proxyservice.SendRequestProxyMode(settings.ProxyMode), Protocol: proxyservice.SendRequestProtocol(settings.Protocol),
		TimeoutMs: max(settings.TimeoutMs, 0), SkipVerifyTLS: settings.SkipVerifyTLS, TLSClientHelloID: proxyservice.TLSClientHelloID(settings.TLSClientHelloID),
		HTTP2Fingerprint: settings.HTTP2Fingerprint, MaxRedirects: settings.MaxRedirects, DisablePlugins: !state.PluginsEnabled, PluginExecutionID: pluginExecutionID,
	}
	if settings.ProxyMode == ProxyModeCustom {
		config.CustomProxy = settings.CustomProxy
	}
	if state.InlineScriptEnabled {
		config.InlinePythonScript = &proxyservice.InlinePythonScript{Enabled: true, Source: state.InlineScriptSource}
	}
	return config
}

// PluginOutcomeLabel names a request's plugin outcome.
func PluginOutcomeLabel(outcome string) string {
	switch outcome {
	case PluginOutcomeCompleted:
		return locales.WorkspaceHttpRequestPluginOutcomeCompleted()
	case PluginOutcomeCompletedWithError:
		return locales.WorkspaceHttpRequestPluginOutcomeCompletedWithPluginError()
	case PluginOutcomeBlockedRequest:
		return locales.WorkspaceHttpRequestPluginOutcomeBlockedRequest()
	case PluginOutcomeBlockedResponse:
		return locales.WorkspaceHttpRequestPluginOutcomeBlockedResponse()
	default:
		return locales.WorkspaceHttpRequestPluginOutcomePluginFailed()
	}
}

// BuildPluginErrorDetails describes what went wrong in a plugin execution.
func BuildPluginErrorDetails(outcome string, execution *proxyservice.HTTPRequestPluginExecution) string {
	var details []string
	if execution != nil {
		for _, diagnostic := range execution.Diagnostics {
			owner := diagnostic.Name
			if owner == "" {
				owner = diagnostic.PluginID
			}
			if diagnostic.PluginID == CurrentRequestScriptPluginID {
				owner = locales.WorkspaceHttpRequestInlineScriptName()
			}
			if owner != "" {
				details = append(details, fmt.Sprintf("%s\n%s", owner, diagnostic.Message))
			} else {
				details = append(details, diagnostic.Message)
			}
		}
	}
	if len(details) == 0 {
		return PluginOutcomeLabel(outcome)
	}
	return strings.Join(details, "\n\n")
}

// IsPluginTerminalOutcome reports whether plugins ended the request.
func IsPluginTerminalOutcome(outcome string) bool {
	return outcome == PluginOutcomeBlockedRequest || outcome == PluginOutcomeBlockedResponse || outcome == PluginOutcomePluginFailed
}

// ResponseViewOf maps a completed send. streaming tells the body continues.
func ResponseViewOf(response proxyservice.SendRequestResponse) (view *HTTPResponseView, streaming bool) {
	terminal := IsPluginTerminalOutcome(response.Outcome)
	hasDiagnostics := terminal || (response.PluginExecution != nil && len(response.PluginExecution.Diagnostics) > 0)
	session := strings.TrimSpace(response.StreamSessionID)
	streaming = !terminal && response.Streaming && session != ""
	view = &HTTPResponseView{
		Kind: ResponseKindSuccess, Outcome: response.Outcome, PluginExecution: response.PluginExecution,
		StatusCode: response.StatusCode, StatusText: response.StatusText, Protocol: response.Protocol,
		Headers: HeaderFieldsToEditableRows(response.HeaderFields), HeadersHaveWireOrder: response.HeaderFields != nil && !response.HeaderOrderUnavailable, HeadersTruncated: response.HeadersTruncated,
		Trailers: HeaderFieldsToEditableRows(response.TrailerFields), TrailersHaveWireOrder: response.TrailerFields != nil && !response.TrailerOrderUnavailable, TrailersTruncated: response.TrailersTruncated,
		Body: response.Body,
	}
	if view.Outcome == "" {
		view.Outcome = PluginOutcomeCompleted
	}
	if terminal {
		view.Kind = ResponseKindPlugin
	}
	if streaming {
		view.StreamState = StreamStateStreaming
	}
	if response.BodyEncoding == "base64" {
		view.BodyEncoding = "base64"
	}
	if hasDiagnostics {
		view.ErrorMessage = BuildPluginErrorDetails(response.Outcome, response.PluginExecution)
	}
	return view, streaming
}

// CancelledResponseView returns the response of a stopped request.
func CancelledResponseView() *HTTPResponseView { return &HTTPResponseView{Kind: ResponseKindCancelled} }

// ErrorResponseView returns the response of a failed request.
func ErrorResponseView(err error) *HTTPResponseView {
	return &HTTPResponseView{Kind: ResponseKindError, StreamState: StreamStateError, ErrorMessage: err.Error()}
}

// ResponseTabAfterSend chooses the response tab to show after a send.
func ResponseTabAfterSend(view *HTTPResponseView, streaming, registered, inlineScript bool) string {
	switch {
	case view.Kind == ResponseKindPlugin || view.ErrorMessage != "":
		return ResponseTabError
	case streaming && registered:
		return ResponseTabBody
	case streaming:
		return ResponseTabError
	case inlineScript:
		return ResponseTabConsole
	default:
		return ResponseTabHeaders
	}
}

// HasRequestConsole reports whether a tab has console output to offer.
func HasRequestConsole(state *HTTPEditorState) bool {
	return state.InlineScriptEnabled || state.ScriptExecutionID != "" || len(state.ScriptConsoleEntries) > 0
}

// HasResponse reports whether a tab has a response to show.
func HasResponse(state *HTTPEditorState) bool {
	return state.Response != nil && state.Response.Kind != ResponseKindCancelled
}

// HasErrorResponse reports whether a tab's request failed.
func HasErrorResponse(state *HTTPEditorState) bool {
	return state.Response != nil && (state.Response.Kind == ResponseKindError || state.Response.Kind == ResponseKindPlugin)
}

// HasResponseErrorDetails reports whether a tab has error details.
func HasResponseErrorDetails(state *HTTPEditorState) bool {
	return HasErrorResponse(state) || (state.Response != nil && state.Response.ErrorMessage != "")
}

// ResponseTrailerCount counts a response's named trailers.
func ResponseTrailerCount(view *HTTPResponseView) int {
	count := 0
	if view != nil {
		for _, row := range view.Trailers {
			if strings.TrimSpace(row.Key) != "" {
				count++
			}
		}
	}
	return count
}

// ResponseSetCookieFields returns a response's Set-Cookie headers.
func ResponseSetCookieFields(view *HTTPResponseView) []HeaderField {
	if view == nil {
		return nil
	}
	return EditableRowsToHeaderFields(view.Headers)
}

// VisibleResponseTab returns the response tab that can be shown now.
func VisibleResponseTab(state *HTTPEditorState, hasCookies bool) string {
	active := state.ActiveResponseTab
	hasConsole := HasRequestConsole(state)
	switch {
	case active == ResponseTabConsole && hasConsole:
		return ResponseTabConsole
	case HasErrorResponse(state):
		return ResponseTabError
	case !HasResponse(state) && hasConsole:
		return ResponseTabConsole
	case active == ResponseTabError && !HasResponseErrorDetails(state),
		active == ResponseTabCookies && !hasCookies,
		active == ResponseTabTrailers && ResponseTrailerCount(state.Response) == 0:
		return ResponseTabHeaders
	default:
		return active
	}
}

// ToggleMITMProxyMode switches into or out of MITM mode. restore holds the
// mode to return to.
func ToggleMITMProxyMode(mode string, restore *string) string {
	if mode == ProxyModeMITM {
		if *restore == "" {
			return ProxyModeNone
		}
		return *restore
	}
	*restore = mode
	return ProxyModeMITM
}

// IsSafeUpdateURL reports whether an address from a release may be opened.
func IsSafeUpdateURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}
