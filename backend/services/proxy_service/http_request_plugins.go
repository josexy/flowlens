package proxyservice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	http "github.com/josexy/xhttp"
	"golang.org/x/net/http/httpguts"
)

const (
	RequestOutcomeCompleted                = "completed"
	RequestOutcomeCompletedWithPluginError = "completed_with_plugin_error"
	RequestOutcomeBlockedRequest           = "blocked_request"
	RequestOutcomeBlockedResponse          = "blocked_response"
	RequestOutcomePluginFailed             = "plugin_failed"
)

type HTTPRequestPluginTransport struct {
	Protocol         SendRequestProtocol  `json:"protocol"`
	ProxyMode        SendRequestProxyMode `json:"proxyMode"`
	TLSClientHelloID TLSClientHelloID     `json:"tlsClientHelloId"`
	HTTP2Fingerprint string               `json:"http2Fingerprint"`
}

type HTTPRequestPluginBeginRequest struct {
	ExecutionID           string                     `json:"executionId"`
	Timestamp             int64                      `json:"timestamp"`
	OriginalMethod        string                     `json:"originalMethod"`
	OriginalURL           string                     `json:"originalUrl"`
	Transport             HTTPRequestPluginTransport `json:"transport"`
	DisableManagedPlugins bool                       `json:"disableManagedPlugins"`
	InlinePythonScript    *InlinePythonScript        `json:"inlinePythonScript,omitempty"`
}

type HTTPRequestPluginRequest struct {
	Method       string                     `json:"method"`
	URL          string                     `json:"url"`
	HeaderFields []HTTPHeaderField          `json:"headerFields"`
	Body         SendRequestBody            `json:"body"`
	BodyFile     *HTTPRequestPluginBodyFile `json:"bodyFile,omitempty"`
}

type HTTPRequestPluginBodyFile struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	ReadOnly bool   `json:"readOnly"`
}

type HTTPRequestPluginResponse struct {
	StatusCode    int                        `json:"statusCode"`
	StatusText    string                     `json:"statusText"`
	Protocol      string                     `json:"protocol"`
	HeaderFields  []HTTPHeaderField          `json:"headerFields"`
	TrailerFields []HTTPHeaderField          `json:"trailerFields"`
	Body          []byte                     `json:"-"`
	BodyFile      *HTTPRequestPluginBodyFile `json:"bodyFile,omitempty"`
	ContentType   string                     `json:"contentType"`
	BodyKind      string                     `json:"bodyKind"`
	BodyAvailable bool                       `json:"bodyAvailable"`
	Streaming     bool                       `json:"streaming"`
	Request       HTTPRequestPluginRequest   `json:"request"`
}

type HTTPRequestPluginRequestResult struct {
	Request HTTPRequestPluginRequest
	Blocked bool
	Failed  bool
}

type HTTPRequestPluginResponseResult struct {
	Response HTTPRequestPluginResponse
	Blocked  bool
	Failed   bool
}

type HTTPRequestPluginMatch struct {
	PluginID string `json:"pluginId"`
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

type HTTPRequestPluginInvocation struct {
	PluginID       string `json:"pluginId"`
	Name           string `json:"name"`
	Revision       string `json:"revision"`
	Phase          string `json:"phase"`
	DurationMicros int64  `json:"durationMicros"`
	Transformed    bool   `json:"transformed"`
	Outcome        string `json:"outcome"`
}

type HTTPRequestPluginDiagnostic struct {
	PluginID string `json:"pluginId,omitempty"`
	Name     string `json:"name,omitempty"`
	Phase    string `json:"phase"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type HTTPRequestPluginExecution struct {
	ExecutionID            string                        `json:"executionId"`
	MatchedPlugins         []HTTPRequestPluginMatch      `json:"matchedPlugins"`
	Invocations            []HTTPRequestPluginInvocation `json:"invocations"`
	RequestDurationMicros  int64                         `json:"requestDurationMicros"`
	ResponseDurationMicros int64                         `json:"responseDurationMicros"`
	RequestTransformed     bool                          `json:"requestTransformed"`
	ResponseTransformed    bool                          `json:"responseTransformed"`
	Diagnostics            []HTTPRequestPluginDiagnostic `json:"diagnostics"`
}

type HTTPRequestPluginSession interface {
	RunRequest(ctx context.Context, request HTTPRequestPluginRequest) HTTPRequestPluginRequestResult
	RunResponse(ctx context.Context, response HTTPRequestPluginResponse) HTTPRequestPluginResponseResult
	Execution() *HTTPRequestPluginExecution
	Close()
}

type HTTPRequestPluginRunner interface {
	BeginRequest(ctx context.Context, request HTTPRequestPluginBeginRequest) (HTTPRequestPluginSession, error)
}

// ValidateHTTPRequestPluginRequest checks a hook result without consuming file or
// multipart bodies. The normal request pipeline remains responsible for final
// generated headers, framing, content encoding, and transport behavior.
func ValidateHTTPRequestPluginRequest(request HTTPRequestPluginRequest) (HTTPRequestPluginRequest, error) {
	return ValidateHTTPRequestPluginRequestContext(context.Background(), request)
}

func ValidateHTTPRequestPluginRequestContext(
	ctx context.Context,
	request HTTPRequestPluginRequest,
) (HTTPRequestPluginRequest, error) {
	if err := ctx.Err(); err != nil {
		return HTTPRequestPluginRequest{}, err
	}
	method, parsedURL, err := normalizeSyntheticMethodAndURL(request.Method, request.URL)
	if err != nil {
		return HTTPRequestPluginRequest{}, err
	}
	if strings.TrimSpace(request.Method) != method {
		return HTTPRequestPluginRequest{}, errors.New("plugin request method must be uppercase")
	}
	for _, field := range request.HeaderFields {
		if strings.TrimSpace(field.Name) == "" {
			return HTTPRequestPluginRequest{}, errors.New("plugin request header name cannot be empty")
		}
		if _, include, err := normalizeUserRequestHeaderField(field); err != nil {
			return HTTPRequestPluginRequest{}, err
		} else if !include {
			return HTTPRequestPluginRequest{}, errors.New("plugin request header cannot be empty")
		}
	}
	if err := validateHTTPRequestPluginRequestBody(ctx, &request); err != nil {
		return HTTPRequestPluginRequest{}, err
	}
	request.Method = method
	request.URL = parsedURL.String()
	request.HeaderFields = append([]HTTPHeaderField(nil), request.HeaderFields...)
	request.Body = cloneSendRequestBody(request.Body)
	request.BodyFile = cloneHTTPRequestPluginBodyFile(request.BodyFile)
	return request, nil
}

// ValidateHTTPRequestPluginResponse validates presentation-only mutations returned
// by a response hook. Response pseudo-header status values are derived from the
// mutable status code so the two representations cannot diverge.
func ValidateHTTPRequestPluginResponse(response HTTPRequestPluginResponse) (HTTPRequestPluginResponse, error) {
	return ValidateHTTPRequestPluginResponseContext(context.Background(), response)
}

func ValidateHTTPRequestPluginResponseContext(
	ctx context.Context,
	response HTTPRequestPluginResponse,
) (HTTPRequestPluginResponse, error) {
	if err := ctx.Err(); err != nil {
		return HTTPRequestPluginResponse{}, err
	}
	if response.StatusCode < 100 || response.StatusCode > 999 {
		return HTTPRequestPluginResponse{}, fmt.Errorf("plugin response status code %d is outside 100-999", response.StatusCode)
	}
	headerFields, err := validateHTTPRequestPluginResponseHeaderFields(response.HeaderFields, true, response.StatusCode)
	if err != nil {
		return HTTPRequestPluginResponse{}, err
	}
	trailerFields, err := validateHTTPRequestPluginResponseHeaderFields(response.TrailerFields, false, response.StatusCode)
	if err != nil {
		return HTTPRequestPluginResponse{}, fmt.Errorf("invalid plugin response trailer: %w", err)
	}
	if response.BodyAvailable {
		if response.BodyFile != nil {
			if len(response.Body) != 0 {
				return HTTPRequestPluginResponse{}, errors.New("plugin response body cannot be both inline and file-backed")
			}
			if response.BodyKind == "none" {
				return HTTPRequestPluginResponse{}, errors.New("plugin response none body cannot be file-backed")
			}
			file := cloneHTTPRequestPluginBodyFile(response.BodyFile)
			if err := validateHTTPRequestPluginBodyFile(ctx, file, response.BodyKind); err != nil {
				return HTTPRequestPluginResponse{}, err
			}
			response.BodyFile = file
		} else {
			switch response.BodyKind {
			case "none":
				if len(response.Body) != 0 {
					return HTTPRequestPluginResponse{}, errors.New("plugin response none body must be empty")
				}
			case "text", "xml":
				if !utf8.Valid(response.Body) {
					return HTTPRequestPluginResponse{}, fmt.Errorf(
						"plugin response %s body must be valid UTF-8", response.BodyKind,
					)
				}
			case "json":
				if !json.Valid(response.Body) {
					return HTTPRequestPluginResponse{}, errors.New("plugin response JSON body is invalid")
				}
			case "binary":
			default:
				return HTTPRequestPluginResponse{}, fmt.Errorf("unsupported plugin response body kind %q", response.BodyKind)
			}
		}
	} else if response.BodyKind != "unavailable" {
		return HTTPRequestPluginResponse{}, errors.New("unavailable plugin response body must use kind unavailable")
	} else if response.BodyFile != nil {
		return HTTPRequestPluginResponse{}, errors.New("unavailable plugin response body cannot be file-backed")
	}
	if response.Streaming && response.BodyAvailable {
		return HTTPRequestPluginResponse{}, errors.New("streaming plugin response body cannot be available")
	}
	response.HeaderFields = headerFields
	response.TrailerFields = trailerFields
	response.Body = append([]byte(nil), response.Body...)
	response.BodyFile = cloneHTTPRequestPluginBodyFile(response.BodyFile)
	response.Request = cloneHTTPRequestPluginRequest(response.Request)
	response.ContentType = firstHeaderFieldValue(response.HeaderFields, "Content-Type")
	return response, nil
}

func validateHTTPRequestPluginResponseHeaderFields(
	fields []HTTPHeaderField,
	allowStatus bool,
	statusCode int,
) ([]HTTPHeaderField, error) {
	validated := make([]HTTPHeaderField, 0, len(fields))
	statusSeen := false
	for _, field := range fields {
		field.Name = strings.TrimSpace(field.Name)
		if field.Name == "" {
			return nil, errors.New("plugin response header name cannot be empty")
		}
		if strings.HasPrefix(field.Name, ":") {
			if !allowStatus || field.Name != ":status" {
				return nil, fmt.Errorf("unsupported plugin response pseudo-header %q", field.Name)
			}
			if statusSeen {
				return nil, errors.New("plugin response contains multiple :status fields")
			}
			statusSeen = true
			field.Value = strconv.Itoa(statusCode)
		} else {
			if !httpguts.ValidHeaderFieldName(field.Name) {
				return nil, fmt.Errorf("invalid HTTP header name %q", field.Name)
			}
			if !httpguts.ValidHeaderFieldValue(field.Value) {
				return nil, fmt.Errorf("invalid value for HTTP header %q", field.Name)
			}
		}
		validated = append(validated, field)
	}
	return validated, nil
}

func validateHTTPRequestPluginBody(body SendRequestBody) error {
	switch body.BodyType {
	case "", SendRequestBodyTypeNone, SendRequestBodyTypeText, SendRequestBodyTypeJSON, SendRequestBodyTypeXML:
		return nil
	case SendRequestBodyTypeBinary:
		if _, err := base64.StdEncoding.DecodeString(body.Text); err != nil {
			return fmt.Errorf("invalid plugin binary body: %w", err)
		}
		return nil
	case SendRequestBodyTypeFile:
		return validateHTTPRequestPluginFile(body.File, "file body")
	case SendRequestBodyTypeFormData:
		for index, item := range body.FormData {
			if item == nil || !item.Enabled {
				continue
			}
			if strings.TrimSpace(item.Name) == "" {
				return fmt.Errorf("plugin multipart item %d has an empty name", index)
			}
			switch item.ItemType {
			case "text":
			case "file":
				if err := validateHTTPRequestPluginFile(item.File, fmt.Sprintf("multipart item %q", item.Name)); err != nil {
					return err
				}
			default:
				return fmt.Errorf("plugin multipart item %q has unsupported kind %q", item.Name, item.ItemType)
			}
		}
		return nil
	case SendRequestBodyTypeURLEncoded:
		for index, item := range body.URLEncoded {
			if item != nil && item.Enabled && strings.TrimSpace(item.Name) == "" {
				return fmt.Errorf("plugin URL-encoded item %d has an empty name", index)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported plugin request body type %q", body.BodyType)
	}
}

func validateHTTPRequestPluginFile(file *SendRequestFile, label string) error {
	if file == nil || strings.TrimSpace(file.Path) == "" {
		return fmt.Errorf("plugin %s requires a file path", label)
	}
	info, err := os.Stat(strings.TrimSpace(file.Path))
	if err != nil {
		return fmt.Errorf("invalid plugin %s path: %w", label, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("invalid plugin %s path: not a regular file", label)
	}
	return nil
}

func normalizeSyntheticMethodAndURL(method, targetURL string) (string, *url.URL, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	targetURL = strings.TrimSpace(targetURL)
	if method == "" {
		return "", nil, fmt.Errorf("method is required")
	}
	if targetURL == "" {
		return "", nil, fmt.Errorf("url is required")
	}
	parsedURL, err := url.ParseRequestURI(targetURL)
	if err != nil {
		return "", nil, fmt.Errorf("invalid url: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", nil, fmt.Errorf("unsupported url scheme: %s", parsedURL.Scheme)
	}
	if err := normalizeSyntheticURLHostname(parsedURL); err != nil {
		return "", nil, fmt.Errorf("invalid url host: %w", err)
	}
	return method, parsedURL, nil
}

func cloneSendRequestBody(body SendRequestBody) SendRequestBody {
	clone := SendRequestBody{BodyType: body.BodyType, Text: body.Text}
	if body.File != nil {
		file := *body.File
		clone.File = &file
	}
	if body.FormData != nil {
		clone.FormData = make([]*SendRequestFormDataItem, 0, len(body.FormData))
	}
	for _, item := range body.FormData {
		if item == nil {
			clone.FormData = append(clone.FormData, nil)
			continue
		}
		itemClone := *item
		if item.File != nil {
			file := *item.File
			itemClone.File = &file
		}
		clone.FormData = append(clone.FormData, &itemClone)
	}
	if body.URLEncoded != nil {
		clone.URLEncoded = make([]*SendRequestURLEncodedItem, 0, len(body.URLEncoded))
	}
	for _, item := range body.URLEncoded {
		if item == nil {
			clone.URLEncoded = append(clone.URLEncoded, nil)
			continue
		}
		itemClone := *item
		clone.URLEncoded = append(clone.URLEncoded, &itemClone)
	}
	return clone
}

func cloneHTTPRequestPluginRequest(request HTTPRequestPluginRequest) HTTPRequestPluginRequest {
	return HTTPRequestPluginRequest{
		Method:       request.Method,
		URL:          request.URL,
		HeaderFields: append([]HTTPHeaderField(nil), request.HeaderFields...),
		Body:         cloneSendRequestBody(request.Body),
		BodyFile:     cloneHTTPRequestPluginBodyFile(request.BodyFile),
	}
}

func cloneHTTPRequestPluginResponse(response HTTPRequestPluginResponse) HTTPRequestPluginResponse {
	response.HeaderFields = append([]HTTPHeaderField(nil), response.HeaderFields...)
	response.TrailerFields = append([]HTTPHeaderField(nil), response.TrailerFields...)
	response.Body = append([]byte(nil), response.Body...)
	response.BodyFile = cloneHTTPRequestPluginBodyFile(response.BodyFile)
	response.Request = cloneHTTPRequestPluginRequest(response.Request)
	return response
}

func cloneHTTPRequestPluginBodyFile(file *HTTPRequestPluginBodyFile) *HTTPRequestPluginBodyFile {
	if file == nil {
		return nil
	}
	clone := *file
	return &clone
}

func httpRequestPluginResponseBodyKind(contentType string, body []byte) string {
	if len(body) == 0 {
		return "none"
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && (strings.EqualFold(mediaType, "application/json") || strings.HasSuffix(strings.ToLower(mediaType), "+json")) && json.Valid(body) {
		return "json"
	}
	if err == nil && isXMLMediaType(mediaType) && utf8.Valid(body) {
		return "xml"
	}
	if isBinaryContentType(contentType) || !utf8.Valid(body) {
		return "binary"
	}
	return "text"
}

func isXMLMediaType(mediaType string) bool {
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/xml" || mediaType == "text/xml" || strings.HasSuffix(mediaType, "+xml")
}

func httpRequestPluginStatusText(statusCode int) string {
	statusText := http.StatusText(statusCode)
	if statusText == "" {
		return strconv.Itoa(statusCode)
	}
	return strconv.Itoa(statusCode) + " " + statusText
}

func normalizeHTTPRequestPluginExecutionID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > 128 || !utf8.ValidString(value) {
		return "", errors.New("Python plugin execution ID is invalid")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", errors.New("Python plugin execution ID contains control characters")
		}
	}
	return value, nil
}

func httpRequestPluginStartFailure(executionID string, err error) SendRequestResponse {
	execution := &HTTPRequestPluginExecution{
		ExecutionID:    executionID,
		MatchedPlugins: []HTTPRequestPluginMatch{},
		Invocations:    []HTTPRequestPluginInvocation{},
		Diagnostics: []HTTPRequestPluginDiagnostic{{
			Phase: "request", Code: "runner_start_failed",
			Message: cleanHTTPRequestPluginDiagnosticMessage(err),
		}},
	}
	return SendRequestResponse{Outcome: RequestOutcomePluginFailed, PluginExecution: execution}
}

func httpRequestPluginRequestValidationFailure(execution *HTTPRequestPluginExecution, err error) SendRequestResponse {
	result := cloneHTTPRequestPluginExecution(execution)
	result.Diagnostics = append(result.Diagnostics, HTTPRequestPluginDiagnostic{
		Phase: "request", Code: "invalid_result", Message: cleanHTTPRequestPluginDiagnosticMessage(err),
	})
	return SendRequestResponse{Outcome: RequestOutcomePluginFailed, PluginExecution: result}
}

func appendHTTPRequestPluginResponseDiagnostic(
	execution *HTTPRequestPluginExecution,
	code string,
	err error,
) *HTTPRequestPluginExecution {
	result := cloneHTTPRequestPluginExecution(execution)
	result.Diagnostics = append(result.Diagnostics, HTTPRequestPluginDiagnostic{
		Phase: "response", Code: code, Message: cleanHTTPRequestPluginDiagnosticMessage(err),
	})
	return result
}

func cloneHTTPRequestPluginExecution(execution *HTTPRequestPluginExecution) *HTTPRequestPluginExecution {
	if execution == nil {
		return &HTTPRequestPluginExecution{
			MatchedPlugins: []HTTPRequestPluginMatch{},
			Invocations:    []HTTPRequestPluginInvocation{},
			Diagnostics:    []HTTPRequestPluginDiagnostic{},
		}
	}
	clone := *execution
	clone.MatchedPlugins = append([]HTTPRequestPluginMatch(nil), execution.MatchedPlugins...)
	clone.Invocations = append([]HTTPRequestPluginInvocation(nil), execution.Invocations...)
	clone.Diagnostics = append([]HTTPRequestPluginDiagnostic(nil), execution.Diagnostics...)
	return &clone
}

func cleanHTTPRequestPluginDiagnosticMessage(err error) string {
	if err == nil {
		return "Python plugin execution failed"
	}
	if detailer, ok := errors.AsType[interface {
		error
		DiagnosticDetail() string
	}](err); ok {
		message := strings.TrimSpace(detailer.DiagnosticDetail())
		message = strings.ReplaceAll(message, "\r\n", "\n")
		message = strings.ReplaceAll(message, "\r", "\n")
		message = strings.Map(func(value rune) rune {
			if value == '\n' || value == '\t' {
				return value
			}
			if value < 0x20 || value == 0x7f {
				return -1
			}
			return value
		}, message)
		if message != "" {
			return message
		}
	}
	message := strings.Map(func(value rune) rune {
		if value == '\n' || value == '\r' || value == '\t' {
			return ' '
		}
		if value < 0x20 || value == 0x7f {
			return -1
		}
		return value
	}, strings.TrimSpace(err.Error()))
	const maxDiagnosticRunes = 2048
	runes := []rune(message)
	if len(runes) > maxDiagnosticRunes {
		message = string(runes[:maxDiagnosticRunes]) + "…"
	}
	if message == "" {
		return "Python plugin execution failed"
	}
	return message
}
