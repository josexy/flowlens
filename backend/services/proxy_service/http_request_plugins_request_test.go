package proxyservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
)

type fakeHTTPRequestPluginRunner struct {
	beginCount atomic.Int32
	begin      HTTPRequestPluginBeginRequest
	session    *fakeHTTPRequestPluginSession
	err        error
}

func (r *fakeHTTPRequestPluginRunner) BeginRequest(_ context.Context, request HTTPRequestPluginBeginRequest) (HTTPRequestPluginSession, error) {
	r.beginCount.Add(1)
	r.begin = request
	return r.session, r.err
}

type fakeHTTPRequestPluginSession struct {
	requestCount  atomic.Int32
	responseCount atomic.Int32
	closed        atomic.Bool
	requestResult HTTPRequestPluginRequestResult
	responseHook  func(HTTPRequestPluginResponse) HTTPRequestPluginResponseResult
	execution     *HTTPRequestPluginExecution
}

func (s *fakeHTTPRequestPluginSession) RunRequest(_ context.Context, request HTTPRequestPluginRequest) HTTPRequestPluginRequestResult {
	s.requestCount.Add(1)
	if s.requestResult.Request.Method == "" && !s.requestResult.Blocked && !s.requestResult.Failed {
		s.requestResult.Request = request
	}
	return s.requestResult
}

func (s *fakeHTTPRequestPluginSession) RunResponse(_ context.Context, response HTTPRequestPluginResponse) HTTPRequestPluginResponseResult {
	s.responseCount.Add(1)
	if s.responseHook != nil {
		return s.responseHook(response)
	}
	return HTTPRequestPluginResponseResult{Response: response}
}

func (s *fakeHTTPRequestPluginSession) Execution() *HTTPRequestPluginExecution { return s.execution }
func (s *fakeHTTPRequestPluginSession) Close()                                 { s.closed.Store(true) }

func TestSendHTTPRequestRunsRequestPluginBeforeSyntheticNormalization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPatch || request.URL.Path != "/mutated" {
			t.Errorf("network request = %s %s", request.Method, request.URL.Path)
		}
		if values := request.Header.Values("X-Plugin"); len(values) != 2 || values[0] != "one" || values[1] != "two" {
			t.Errorf("X-Plugin values = %#v", values)
		}
		if contentType := request.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("Content-Type = %q", contentType)
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"mutated":true}` {
			t.Errorf("request body = %q", body)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	execution := &HTTPRequestPluginExecution{MatchedPlugins: []HTTPRequestPluginMatch{{PluginID: "plugin-1", Name: "Plugin", Revision: "rev"}}}
	session := &fakeHTTPRequestPluginSession{
		requestResult: HTTPRequestPluginRequestResult{Request: HTTPRequestPluginRequest{
			Method: http.MethodPatch,
			URL:    server.URL + "/mutated",
			HeaderFields: []HTTPHeaderField{
				{Name: "X-Plugin", Value: "one"},
				{Name: "X-Plugin", Value: "two"},
			},
			Body: SendRequestBody{BodyType: SendRequestBodyTypeJSON, Text: `{"mutated":true}`},
		}},
		execution: execution,
	}
	runner := &fakeHTTPRequestPluginRunner{session: session}
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(runner)

	response, err := service.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone, Protocol: SendRequestProtocolAuto},
		" post ", server.URL+"/original", []HTTPHeaderField{{Name: "X-Original", Value: "yes"}},
		SendRequestBody{BodyType: SendRequestBodyTypeText, Text: "original"},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Outcome != RequestOutcomeCompleted || response.PluginExecution != execution {
		t.Fatalf("response outcome = %q, execution=%+v", response.Outcome, response.PluginExecution)
	}
	if runner.begin.OriginalMethod != http.MethodPost || runner.begin.OriginalURL != server.URL+"/original" {
		t.Fatalf("begin request = %+v", runner.begin)
	}
	if runner.begin.ExecutionID == "" || runner.begin.Timestamp <= 0 {
		t.Fatalf("begin identity = %+v", runner.begin)
	}
	if session.requestCount.Load() != 1 || !session.closed.Load() {
		t.Fatalf("request count=%d closed=%v", session.requestCount.Load(), session.closed.Load())
	}
}

func TestSendHTTPRequestWritesPluginMultipartFilename(t *testing.T) {
	payload := []byte("plugin-file-payload")
	path := filepath.Join(t.TempDir(), "source.dat")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	bodyCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read multipart body: %v", err)
			return
		}
		bodyCh <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	session := &fakeHTTPRequestPluginSession{requestResult: HTTPRequestPluginRequestResult{Request: HTTPRequestPluginRequest{
		Method: http.MethodPost, URL: server.URL,
		Body: SendRequestBody{BodyType: SendRequestBodyTypeFormData, FormData: []*SendRequestFormDataItem{{
			Enabled: true, Name: "upload", ItemType: "file",
			File:     &SendRequestFile{Path: path, Name: "source.dat", Size: int64(len(payload))},
			Filename: "renamed.bin",
		}}},
	}}}
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{session: session})
	if _, err := service.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone, Protocol: SendRequestProtocolHTTP1},
		http.MethodGet, server.URL, nil,
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	); err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	body := <-bodyCh
	wantPart := "Content-Disposition: form-data; name=\"upload\"; filename=\"renamed.bin\"\r\n" +
		"Content-Type: application/octet-stream\r\n\r\n" +
		string(payload) + "\r\n"
	if !strings.Contains(body, wantPart) {
		t.Fatalf("multipart part was not preserved:\n%s", body)
	}
}

func TestSendHTTPRequestPluginRequestBlockAndFailureNeverOpenNetwork(t *testing.T) {
	for name, requestResult := range map[string]HTTPRequestPluginRequestResult{
		"blocked": {Blocked: true},
		"failed":  {Failed: true},
	} {
		t.Run(name, func(t *testing.T) {
			var networkCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				networkCount.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			execution := &HTTPRequestPluginExecution{Diagnostics: []HTTPRequestPluginDiagnostic{{Phase: "request", Code: name, Message: name}}}
			session := &fakeHTTPRequestPluginSession{requestResult: requestResult, execution: execution}
			service := newTestProxyService(t, &settingservice.ProxyConfig{})
			service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{session: session})
			response, err := service.SendHTTPRequest(
				context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
				http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
			)
			if err != nil {
				t.Fatalf("SendHTTPRequest: %v", err)
			}
			wantOutcome := RequestOutcomeBlockedRequest
			if requestResult.Failed {
				wantOutcome = RequestOutcomePluginFailed
			}
			if response.Outcome != wantOutcome || response.PluginExecution != execution {
				t.Fatalf("response = %+v", response)
			}
			if networkCount.Load() != 0 {
				t.Fatalf("network request count = %d", networkCount.Load())
			}
		})
	}
}

func TestSendHTTPRequestPluginBeginFailureIsStructuredAndFailClosed(t *testing.T) {
	var networkCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		networkCount.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{err: errors.New("revision missing")})
	response, err := service.SendHTTPRequest(
		context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Outcome != RequestOutcomePluginFailed || response.PluginExecution == nil || len(response.PluginExecution.Diagnostics) != 1 {
		t.Fatalf("response = %+v", response)
	}
	if networkCount.Load() != 0 {
		t.Fatalf("network request count = %d", networkCount.Load())
	}
}

func TestSendHTTPRequestPerTabBypassSkipsRunner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	runner := &fakeHTTPRequestPluginRunner{session: &fakeHTTPRequestPluginSession{}}
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(runner)
	response, err := service.SendHTTPRequest(
		context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone, DisablePlugins: true},
		http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Outcome != RequestOutcomeCompleted || response.PluginExecution != nil || runner.beginCount.Load() != 0 {
		t.Fatalf("bypassed response=%+v beginCount=%d", response, runner.beginCount.Load())
	}
}

func TestSendHTTPRequestRunsInlineScriptWhenManagedPluginsAreBypassed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	execution := &HTTPRequestPluginExecution{ExecutionID: "request-script-execution"}
	runner := &fakeHTTPRequestPluginRunner{session: &fakeHTTPRequestPluginSession{execution: execution}}
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(runner)
	inline := &InlinePythonScript{Enabled: true, Source: "print('inline')"}
	response, err := service.SendHTTPRequest(
		context.Background(), SendRequestConfig{
			ProxyMode: SendRequestProxyModeNone, DisablePlugins: true,
			PluginExecutionID: "request-script-execution", InlinePythonScript: inline,
		},
		http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Outcome != RequestOutcomeCompleted || response.PluginExecution != execution || runner.beginCount.Load() != 1 {
		t.Fatalf("response=%+v beginCount=%d", response, runner.beginCount.Load())
	}
	if runner.begin.ExecutionID != "request-script-execution" || !runner.begin.DisableManagedPlugins || runner.begin.InlinePythonScript != inline {
		t.Fatalf("begin request = %+v", runner.begin)
	}
}

func TestSendHTTPRequestInlineBeginFailureKeepsExecutionID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("network must not be opened")
	}))
	defer server.Close()
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{err: errors.New("inline validation failed")})
	response, err := service.SendHTTPRequest(
		context.Background(), SendRequestConfig{
			ProxyMode: SendRequestProxyModeNone, DisablePlugins: true,
			PluginExecutionID:  "request-script-failure",
			InlinePythonScript: &InlinePythonScript{Enabled: true, Source: "broken"},
		},
		http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.PluginExecution == nil || response.PluginExecution.ExecutionID != "request-script-failure" {
		t.Fatalf("response = %+v", response)
	}
}

func TestSendHTTPRequestRunsRequestPluginsOnlyOnceAcrossRedirects(t *testing.T) {
	var finalURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/redirect" {
			http.Redirect(w, request, finalURL, http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("done"))
	}))
	defer server.Close()
	finalURL = server.URL + "/final"
	session := &fakeHTTPRequestPluginSession{}
	runner := &fakeHTTPRequestPluginRunner{session: session}
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	service.SetHTTPRequestPluginRunner(runner)
	response, err := service.SendHTTPRequest(
		context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
		http.MethodGet, server.URL+"/redirect", nil, SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Body != "done" || session.requestCount.Load() != 1 || runner.beginCount.Load() != 1 {
		t.Fatalf("response=%+v requestCount=%d beginCount=%d", response, session.requestCount.Load(), runner.beginCount.Load())
	}
}

func TestSendHTTPRequestStreamsPluginBodyFileAcrossRedirects(t *testing.T) {
	for _, contentEncoding := range []string{"", "gzip"} {
		name := "plain"
		if contentEncoding != "" {
			name = contentEncoding
		}
		t.Run(name, func(t *testing.T) {
			want := bytes.Repeat([]byte("FlowLens-file-body-"), 128*1024)
			bodyPath := filepath.Join(t.TempDir(), "request.txt")
			if err := os.WriteFile(bodyPath, want, 0o600); err != nil {
				t.Fatal(err)
			}
			var finalURL string
			var requestCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				requestCount.Add(1)
				reader := io.ReadCloser(request.Body)
				if contentEncoding != "" {
					decoded, err := getDecodedReader(request.Body, contentEncoding)
					if err != nil {
						t.Errorf("getDecodedReader: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					reader = decoded
				}
				got, err := io.ReadAll(reader)
				if err != nil {
					t.Errorf("read request Body: %v", err)
				}
				if contentEncoding != "" {
					_ = reader.Close()
				}
				if !bytes.Equal(got, want) {
					t.Errorf("request Body bytes = %d, want %d", len(got), len(want))
				}
				if request.URL.Path == "/redirect" {
					http.Redirect(w, request, finalURL, http.StatusTemporaryRedirect)
					return
				}
				_, _ = w.Write([]byte("done"))
			}))
			defer server.Close()
			finalURL = server.URL + "/final"

			headerFields := []HTTPHeaderField{}
			if contentEncoding != "" {
				headerFields = append(headerFields, HTTPHeaderField{Name: "Content-Encoding", Value: contentEncoding})
			}
			session := &fakeHTTPRequestPluginSession{requestResult: HTTPRequestPluginRequestResult{Request: HTTPRequestPluginRequest{
				Method: http.MethodPost, URL: server.URL + "/redirect", HeaderFields: headerFields,
				Body: SendRequestBody{BodyType: SendRequestBodyTypeText},
				BodyFile: &HTTPRequestPluginBodyFile{
					Path: bodyPath, Name: "request.txt", Size: int64(len(want)), ReadOnly: true,
				},
			}}}
			service := newTestProxyService(t, &settingservice.ProxyConfig{})
			service.SetHTTPRequestPluginRunner(&fakeHTTPRequestPluginRunner{session: session})
			response, err := service.SendHTTPRequest(
				context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone},
				http.MethodPost, server.URL+"/original", nil,
				SendRequestBody{BodyType: SendRequestBodyTypeNone},
			)
			if err != nil {
				t.Fatalf("SendHTTPRequest: %v", err)
			}
			if response.Body != "done" || requestCount.Load() != 2 {
				t.Fatalf("response=%+v requestCount=%d", response, requestCount.Load())
			}
		})
	}
}

func TestHTTPRequestPluginExecutionDurationUsesMicroseconds(t *testing.T) {
	invocation := HTTPRequestPluginInvocation{DurationMicros: time.Millisecond.Microseconds()}
	if invocation.DurationMicros != 1000 {
		t.Fatalf("duration micros = %d", invocation.DurationMicros)
	}
}
