package proxyservice

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	bodyspool "github.com/josexy/flowlens/backend/pkg/body_spool"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
)

func TestSendHTTPRequestSpillsOnlyMatchedPluginResponsesAboveInlineLimit(t *testing.T) {
	tests := []struct {
		name       string
		size       int
		withPlugin bool
		wantFile   bool
	}{
		{name: "no plugin keeps ordinary path", size: int(bodySpoolInlineLimitForTest()) + 1},
		{name: "exact limit stays inline", size: int(bodySpoolInlineLimitForTest()), withPlugin: true},
		{name: "limit plus one spills", size: int(bodySpoolInlineLimitForTest()) + 1, withPlugin: true, wantFile: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := deterministicHTTPResponseBody(test.size)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				_, _ = w.Write(body)
			}))
			defer server.Close()

			var bodyFilePath string
			service := newTestProxyService(t, &settingservice.ProxyConfig{})
			if test.withPlugin {
				session := &fakeHTTPRequestPluginSession{
					execution: &HTTPRequestPluginExecution{},
					responseHook: func(response HTTPRequestPluginResponse) HTTPRequestPluginResponseResult {
						if response.BodyKind != "text" || !response.BodyAvailable {
							t.Fatalf("response body metadata = %+v", response)
						}
						if test.wantFile {
							if response.BodyFile == nil || len(response.Body) != 0 || response.BodyFile.Size != int64(len(body)) {
								t.Fatalf("file-backed response = %+v bodyBytes=%d", response.BodyFile, len(response.Body))
							}
							got, err := os.ReadFile(response.BodyFile.Path)
							if err != nil || !bytes.Equal(got, body) {
								t.Fatalf("response file bytes=%d err=%v", len(got), err)
							}
							bodyFilePath = response.BodyFile.Path
						} else if response.BodyFile != nil || !bytes.Equal(response.Body, body) {
							t.Fatalf("inline response file=%+v bodyBytes=%d", response.BodyFile, len(response.Body))
						}
						return HTTPRequestPluginResponseResult{Response: response}
					},
				}
				service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{session: session})
			}

			response, err := service.SendHTTPRequest(
				context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
				http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
			)
			if err != nil {
				t.Fatalf("SendHTTPRequest: %v", err)
			}
			if response.Body != string(body) || response.BodyEncoding != "" {
				t.Fatalf("response body bytes=%d encoding=%q", len(response.Body), response.BodyEncoding)
			}
			if bodyFilePath != "" {
				if _, err := os.Stat(bodyFilePath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("response spool file remained after return: %v", err)
				}
			}
		})
	}
}

func TestSendHTTPRequestInvalidPluginResponseFileFailsOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Origin", "yes")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("origin"))
	}))
	defer server.Close()

	session := &fakeHTTPRequestPluginSession{
		execution: &HTTPRequestPluginExecution{},
		responseHook: func(response HTTPRequestPluginResponse) HTTPRequestPluginResponseResult {
			response.StatusCode = http.StatusTeapot
			response.HeaderFields = []HTTPHeaderField{{Name: "X-Partial", Value: "discard"}}
			response.Body = nil
			response.BodyFile = &HTTPRequestPluginBodyFile{Path: `Z:\missing\response.bin`, Name: "response.bin", ReadOnly: true}
			response.BodyKind = "binary"
			return HTTPRequestPluginResponseResult{Response: response}
		},
	}
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{session: session})
	response, err := service.SendHTTPRequest(
		context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Outcome != RequestOutcomeCompletedWithPluginError || response.StatusCode != http.StatusAccepted || response.Body != "origin" || firstHeaderFieldValue(response.HeaderFields, "X-Origin") != "yes" {
		t.Fatalf("response was not restored: %+v", response)
	}
	if response.PluginExecution == nil || len(response.PluginExecution.Diagnostics) != 1 || response.PluginExecution.Diagnostics[0].Code != "invalid_result" {
		t.Fatalf("plugin execution = %+v", response.PluginExecution)
	}
}

func TestSendHTTPRequestMaterializesPluginResponseFileAtWailsBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("origin"))
	}))
	defer server.Close()
	replacement := []byte{0x00, 0xff, 0x10, 0x20, 0x7f}
	replacementPath := t.TempDir() + string(os.PathSeparator) + "replacement.bin"
	if err := os.WriteFile(replacementPath, replacement, 0o600); err != nil {
		t.Fatal(err)
	}
	session := &fakeHTTPRequestPluginSession{
		execution: &HTTPRequestPluginExecution{ResponseTransformed: true},
		responseHook: func(response HTTPRequestPluginResponse) HTTPRequestPluginResponseResult {
			response.Body = nil
			response.BodyFile = &HTTPRequestPluginBodyFile{
				Path: replacementPath, Name: "replacement.bin", Size: int64(len(replacement)), ReadOnly: true,
			}
			response.BodyKind = "binary"
			return HTTPRequestPluginResponseResult{Response: response}
		},
	}
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{session: session})
	response, err := service.SendHTTPRequest(
		context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Outcome != RequestOutcomeCompleted || response.BodyEncoding != "base64" || response.Body != base64.StdEncoding.EncodeToString(replacement) {
		t.Fatalf("response = %+v", response)
	}
}

func TestSendHTTPRequestCleansSpilledResponseAfterPluginTerminalResult(t *testing.T) {
	body := deterministicHTTPResponseBody(int(bodySpoolInlineLimitForTest()) + 1)
	for name, flags := range map[string]HTTPRequestPluginResponseResult{
		"failed":  {Failed: true},
		"blocked": {Blocked: true},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write(body)
			}))
			defer server.Close()
			var bodyFilePath string
			session := &fakeHTTPRequestPluginSession{
				execution: &HTTPRequestPluginExecution{},
				responseHook: func(response HTTPRequestPluginResponse) HTTPRequestPluginResponseResult {
					if response.BodyFile == nil {
						t.Fatal("large response was not file-backed")
					}
					bodyFilePath = response.BodyFile.Path
					flags.Response = response
					return flags
				},
			}
			service := newTestProxyService(t, &settingservice.ProxyConfig{})
			service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{session: session})
			response, err := service.SendHTTPRequest(
				context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
				http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
			)
			if err != nil {
				t.Fatalf("SendHTTPRequest: %v", err)
			}
			if name == "failed" && (response.Outcome != RequestOutcomeCompletedWithPluginError || response.Body != string(body)) {
				t.Fatalf("failed response = %+v", response)
			}
			if name == "blocked" && response.Outcome != RequestOutcomeBlockedResponse {
				t.Fatalf("blocked response = %+v", response)
			}
			if _, err := os.Stat(bodyFilePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("response spool file remained after %s: %v", name, err)
			}
		})
	}
}

func bodySpoolInlineLimitForTest() int64 {
	return bodyspool.DefaultInlineLimit
}

func deterministicHTTPResponseBody(size int) []byte {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-_"
	value := make([]byte, size)
	for index := range value {
		value[index] = alphabet[(index*31+7)%len(alphabet)]
	}
	return value
}

func TestSendHTTPRequestAppliesPluginResponsePresentation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("X-Origin", "one")
		w.Header().Add("Trailer", "X-Origin-Trailer")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("origin"))
		w.Header().Set("X-Origin-Trailer", "done")
	}))
	defer server.Close()

	execution := &HTTPRequestPluginExecution{ResponseTransformed: true}
	session := &fakeHTTPRequestPluginSession{
		execution: execution,
		responseHook: func(response HTTPRequestPluginResponse) HTTPRequestPluginResponseResult {
			if response.StatusCode != http.StatusCreated || string(response.Body) != "origin" || response.BodyKind != "text" || !response.BodyAvailable || response.Streaming {
				t.Fatalf("response hook input = %+v body=%q", response, response.Body)
			}
			if response.StatusText != "201 Created" || response.Protocol != "HTTP/1.1" {
				t.Fatalf("response hook metadata = %+v", response)
			}
			if response.Request.Method != http.MethodPost || response.Request.Body.Text != "request" {
				t.Fatalf("request snapshot = %+v", response.Request)
			}
			response.StatusCode = 299
			response.HeaderFields = []HTTPHeaderField{{Name: "X-Plugin", Value: "one"}, {Name: "X-Plugin", Value: "two"}}
			response.TrailerFields = []HTTPHeaderField{{Name: "X-Plugin-Trailer", Value: "yes"}}
			response.Body = []byte{0, 1, 2}
			response.BodyKind = "binary"
			return HTTPRequestPluginResponseResult{Response: response}
		},
	}
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{session: session})

	response, err := service.SendHTTPRequest(
		context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodPost, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeText, Text: "request"},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Outcome != RequestOutcomeCompleted || response.StatusCode != 299 || response.StatusText != "299" {
		t.Fatalf("response status/outcome = %+v", response)
	}
	if response.BodyEncoding != "base64" || response.Body != base64.StdEncoding.EncodeToString([]byte{0, 1, 2}) {
		t.Fatalf("response body = %q encoding=%q", response.Body, response.BodyEncoding)
	}
	if len(response.HeaderFields) != 2 || response.HeaderFields[0].Value != "one" || response.HeaderFields[1].Value != "two" {
		t.Fatalf("response headers = %#v", response.HeaderFields)
	}
	if len(response.TrailerFields) != 1 || response.TrailerFields[0].Value != "yes" {
		t.Fatalf("response trailers = %#v", response.TrailerFields)
	}
	if response.PluginExecution != execution || session.responseCount.Load() != 1 {
		t.Fatalf("execution=%+v responseCount=%d", response.PluginExecution, session.responseCount.Load())
	}
}

func TestSendHTTPRequestClassifiesXMLPluginResponse(t *testing.T) {
	const originBody = `<Envelope><Message>你好</Message></Envelope>`
	const replacementBody = `<Envelope><Message>updated</Message></Envelope>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(originBody))
	}))
	defer server.Close()

	session := &fakeHTTPRequestPluginSession{
		execution: &HTTPRequestPluginExecution{ResponseTransformed: true},
		responseHook: func(response HTTPRequestPluginResponse) HTTPRequestPluginResponseResult {
			if response.BodyKind != "xml" || !response.BodyAvailable || string(response.Body) != originBody {
				t.Fatalf("response hook input = %+v body=%q", response, response.Body)
			}
			response.Body = []byte(replacementBody)
			response.BodyKind = "xml"
			return HTTPRequestPluginResponseResult{Response: response}
		},
	}
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{session: session})

	response, err := service.SendHTTPRequest(
		context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Outcome != RequestOutcomeCompleted || response.Body != replacementBody || response.BodyEncoding != "" {
		t.Fatalf("response = %+v", response)
	}
	if session.responseCount.Load() != 1 {
		t.Fatalf("response hook count = %d", session.responseCount.Load())
	}
}

func TestSendHTTPRequestPluginResponseFailureRollsBackAndBlockHidesResponse(t *testing.T) {
	for name, resultFlags := range map[string]HTTPRequestPluginResponseResult{
		"failed":  {Failed: true},
		"blocked": {Blocked: true},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Origin", "yes")
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte("origin"))
			}))
			defer server.Close()
			execution := &HTTPRequestPluginExecution{Diagnostics: []HTTPRequestPluginDiagnostic{{Phase: "response", Code: name, Message: name}}}
			session := &fakeHTTPRequestPluginSession{
				execution: execution,
				responseHook: func(response HTTPRequestPluginResponse) HTTPRequestPluginResponseResult {
					response.StatusCode = http.StatusTeapot
					response.HeaderFields = []HTTPHeaderField{{Name: "X-Partial", Value: "discard"}}
					response.Body = []byte("partial")
					resultFlags.Response = response
					return resultFlags
				},
			}
			service := newTestProxyService(t, &settingservice.ProxyConfig{})
			service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{session: session})
			response, err := service.SendHTTPRequest(
				context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
				http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
			)
			if err != nil {
				t.Fatalf("SendHTTPRequest: %v", err)
			}
			if name == "failed" {
				if response.Outcome != RequestOutcomeCompletedWithPluginError || response.StatusCode != http.StatusAccepted || response.Body != "origin" || firstHeaderFieldValue(response.HeaderFields, "X-Origin") != "yes" {
					t.Fatalf("failed response was not rolled back: %+v", response)
				}
			} else if response.Outcome != RequestOutcomeBlockedResponse || response.StatusCode != 0 || response.Body != "" || len(response.HeaderFields) != 0 {
				t.Fatalf("blocked response exposed network data: %+v", response)
			}
			if response.PluginExecution != execution {
				t.Fatalf("execution = %+v", response.PluginExecution)
			}
		})
	}
}

func TestSendHTTPRequestRunsSSEPluginBeforeStreamingAndKeepsChunks(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Origin", "yes")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("data: untouched\n\n"))
		w.(http.Flusher).Flush()
		<-release
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	hookCalled := make(chan struct{})
	session := &fakeHTTPRequestPluginSession{
		execution: &HTTPRequestPluginExecution{ResponseTransformed: true},
		responseHook: func(response HTTPRequestPluginResponse) HTTPRequestPluginResponseResult {
			if !response.Streaming || response.BodyAvailable || response.BodyKind != "unavailable" || len(response.Body) != 0 {
				t.Fatalf("SSE hook input = %+v", response)
			}
			if response.StatusText != "200 OK" || response.Protocol != "HTTP/1.1" {
				t.Fatalf("SSE hook metadata = %+v", response)
			}
			response.StatusCode = 299
			response.HeaderFields = append(response.HeaderFields, HTTPHeaderField{Name: "X-Plugin", Value: "yes"})
			close(hookCalled)
			return HTTPRequestPluginResponseResult{Response: response}
		},
	}
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{session: session})
	events := make(chan HTTPRequestStreamEvent, 4)
	service.emitHTTPRequestEventHook = func(event HTTPRequestStreamEvent) { events <- event }

	response, err := service.SendHTTPRequest(
		context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	select {
	case <-hookCalled:
	default:
		t.Fatal("response hook was not called before SendHTTPRequest returned")
	}
	if !response.Streaming || response.StatusCode != 299 || firstHeaderFieldValue(response.HeaderFields, "X-Plugin") != "yes" {
		t.Fatalf("SSE response = %+v", response)
	}
	select {
	case event := <-events:
		if event.EventType != "chunk" {
			t.Fatalf("first event = %+v", event)
		}
		decoded, decodeErr := base64.StdEncoding.DecodeString(event.ChunkBase64)
		if decodeErr != nil || string(decoded) != "data: untouched\n\n" {
			t.Fatalf("chunk = %q decodeErr=%v", decoded, decodeErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE chunk")
	}
	_ = service.DisconnectHTTPRequestStream(response.StreamSessionID)
}

func TestSendHTTPRequestBlockedSSENeverStartsStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
	}))
	defer server.Close()
	session := &fakeHTTPRequestPluginSession{
		execution: &HTTPRequestPluginExecution{},
		responseHook: func(response HTTPRequestPluginResponse) HTTPRequestPluginResponseResult {
			return HTTPRequestPluginResponseResult{Response: response, Blocked: true}
		},
	}
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{session: session})
	var eventCount atomic.Int32
	service.emitHTTPRequestEventHook = func(HTTPRequestStreamEvent) { eventCount.Add(1) }
	response, err := service.SendHTTPRequest(
		context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Outcome != RequestOutcomeBlockedResponse || response.Streaming || response.StreamSessionID != "" || eventCount.Load() != 0 {
		t.Fatalf("blocked SSE response=%+v eventCount=%d", response, eventCount.Load())
	}
}

func TestHTTPResponsePluginValidation(t *testing.T) {
	tests := []HTTPRequestPluginResponse{
		{StatusCode: 99, BodyAvailable: true, BodyKind: "none"},
		{StatusCode: 200, HeaderFields: []HTTPHeaderField{{Name: "Bad Header", Value: "value"}}, BodyAvailable: true, BodyKind: "none"},
		{StatusCode: 200, TrailerFields: []HTTPHeaderField{{Name: ":status", Value: "200"}}, BodyAvailable: true, BodyKind: "none"},
		{StatusCode: 200, Body: []byte("not-json"), BodyAvailable: true, BodyKind: "json"},
		{StatusCode: 200, BodyAvailable: false, BodyKind: "text"},
	}
	for _, response := range tests {
		if _, err := ValidateHTTPRequestPluginResponse(response); err == nil {
			t.Fatalf("ValidateHTTPRequestPluginResponse(%+v) succeeded", response)
		}
	}
}

func TestHTTPRequestPluginResponseBodyKindRecognizesXML(t *testing.T) {
	for _, test := range []struct {
		name        string
		contentType string
		body        []byte
		want        string
	}{
		{name: "application xml", contentType: "application/xml", body: []byte(`<root/>`), want: "xml"},
		{name: "text xml", contentType: "text/xml; charset=utf-8", body: []byte(`<root>世界</root>`), want: "xml"},
		{name: "xml suffix", contentType: "application/soap+xml", body: []byte(`<Envelope/>`), want: "xml"},
		{name: "invalid xml utf8", contentType: "application/xml", body: []byte{0xff}, want: "binary"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := httpRequestPluginResponseBodyKind(test.contentType, test.body); got != test.want {
				t.Fatalf("kind = %q, want %q", got, test.want)
			}
		})
	}
}

func TestHTTPResponsePluginValidationAcceptsXML(t *testing.T) {
	response := HTTPRequestPluginResponse{
		StatusCode: 200, Body: []byte(`<root>世界</root>`), BodyKind: "xml", BodyAvailable: true,
	}
	if _, err := ValidateHTTPRequestPluginResponse(response); err != nil {
		t.Fatalf("ValidateHTTPRequestPluginResponse(XML): %v", err)
	}
	response.Body = []byte{0xff}
	if _, err := ValidateHTTPRequestPluginResponse(response); err == nil {
		t.Fatal("ValidateHTTPRequestPluginResponse accepted invalid UTF-8 XML")
	}
}
