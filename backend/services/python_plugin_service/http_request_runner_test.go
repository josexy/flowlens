package pythonpluginservice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

type hookRuntimeFunc func(context.Context, InvokeRequest) (InvokeResult, error)

func (f hookRuntimeFunc) Invoke(ctx context.Context, request InvokeRequest) (InvokeResult, error) {
	return f(ctx, request)
}

func TestHTTPRequestRunnerMatchesOnceUsesStableOrderAndIsolatesParamsShared(t *testing.T) {
	var mu sync.Mutex
	calls := make([]string, 0)
	runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, request.PluginID)
		params := request.Context["params"].(map[string]any)
		shared := request.Context["shared"].(map[string]any)
		if len(shared) != 0 {
			t.Fatalf("plugin %s received another plugin's shared state: %#v", request.PluginID, shared)
		}
		var wire requestWire
		value, _ := json.Marshal(request.Value)
		if err := json.Unmarshal(value, &wire); err != nil {
			t.Fatalf("decode request wire: %v", err)
		}
		wire.Headers = append(wire.Headers, proxyservice.HTTPHeaderField{
			Name: "X-Order", Value: request.PluginID,
		})
		encoded, _ := json.Marshal(wire)
		sharedValue, _ := json.Marshal(map[string]any{"plugin": request.PluginID, "param": params["value"]})
		return InvokeResult{Value: encoded, Shared: sharedValue, Transformed: true}, nil
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
	first := createRunnerPlugin(t, manager, repository, CreatePluginInput{
		ID: testPluginIDOne, Name: "First", ParamsJSON: `{"value":1}`,
	}, []CreateRuleInput{
		{ID: testRuleIDOne, Enabled: true, Method: "GET", URLPattern: "https://example.com/*"},
		{ID: "55555555-5555-4555-8555-555555555555", Enabled: true, Method: "*", URLPattern: "*"},
	})
	second := createRunnerPlugin(t, manager, repository, CreatePluginInput{
		ID: testPluginIDTwo, Name: "Second", ParamsJSON: `{"value":2}`,
	}, []CreateRuleInput{{ID: testRuleIDTwo, Enabled: true, Method: "*", URLPattern: "*"}})

	session, err := runner.BeginRequest(context.Background(), proxyservice.HTTPRequestPluginBeginRequest{
		ExecutionID: "request-1", Timestamp: time.Now().UnixMicro(),
		OriginalMethod: "GET", OriginalURL: "https://example.com/path",
		Transport: proxyservice.HTTPRequestPluginTransport{Protocol: proxyservice.SendRequestProtocolAuto},
	})
	if err != nil {
		t.Fatalf("BeginRequest: %v", err)
	}
	if session == nil {
		t.Fatal("expected matched session")
	}
	defer session.Close()
	result := session.RunRequest(context.Background(), proxyservice.HTTPRequestPluginRequest{
		Method: "GET", URL: "https://example.com/path",
		HeaderFields: []proxyservice.HTTPHeaderField{{Name: "X-Initial", Value: "yes"}},
		Body:         proxyservice.SendRequestBody{BodyType: proxyservice.SendRequestBodyTypeNone},
	})
	if result.Failed || result.Blocked {
		t.Fatalf("RunRequest result = %+v execution=%+v", result, session.Execution())
	}
	if got, want := calls, []string{first.ID, second.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("hook order = %#v, want %#v", got, want)
	}
	if len(result.Request.HeaderFields) != 3 || result.Request.HeaderFields[1].Value != first.ID || result.Request.HeaderFields[2].Value != second.ID {
		t.Fatalf("chained headers = %#v", result.Request.HeaderFields)
	}
	execution := session.Execution()
	if len(execution.MatchedPlugins) != 2 || len(execution.Invocations) != 2 || !execution.RequestTransformed {
		t.Fatalf("execution = %+v", execution)
	}
}

func TestHTTPRequestRunnerAddsInlineScriptAtNetworkBoundaryAndCanBypassManagedPlugins(t *testing.T) {
	for name, disableManaged := range map[string]bool{
		"combined":    false,
		"inline-only": true,
	} {
		t.Run(name, func(t *testing.T) {
			requestCalls := make([]string, 0, 2)
			responseCalls := make([]string, 0, 2)
			runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
				if request.ExecutionID != "inline-execution" {
					t.Fatalf("execution ID = %q", request.ExecutionID)
				}
				if request.Hook == "onRequest" {
					requestCalls = append(requestCalls, request.PluginID)
				} else {
					responseCalls = append(responseCalls, request.PluginID)
				}
				value, _ := json.Marshal(request.Value)
				return InvokeResult{Value: value, Shared: json.RawMessage(`{}`)}, nil
			})
			runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
			managed := createRunnerPlugin(t, manager, repository, CreatePluginInput{
				ID: testPluginIDOne, Name: "Managed", ParamsJSON: `{}`,
			}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
			session, err := runner.BeginRequest(context.Background(), proxyservice.HTTPRequestPluginBeginRequest{
				ExecutionID: "inline-execution", Timestamp: time.Now().UnixMicro(),
				OriginalMethod: "GET", OriginalURL: "https://example.com/",
				DisableManagedPlugins: disableManaged,
				InlinePythonScript: &proxyservice.InlinePythonScript{
					Enabled: true,
					Source: `from flowlens import *

def onRequest(context, request):
    return request

def onResponse(context, response):
    return response
`,
				},
			})
			if err != nil || session == nil {
				t.Fatalf("BeginRequest session=%v err=%v", session, err)
			}
			inlinePath := session.(*httpRequestSession).plugins[len(session.(*httpRequestSession).plugins)-1].lease.Path
			requestResult := session.RunRequest(context.Background(), minimalHTTPRequestPluginRequest())
			responseResult := session.RunResponse(context.Background(), proxyservice.HTTPRequestPluginResponse{
				StatusCode: 200, BodyAvailable: true, BodyKind: "none", Request: requestResult.Request,
			})
			if requestResult.Failed || responseResult.Failed {
				t.Fatalf("request=%+v response=%+v execution=%+v", requestResult, responseResult, session.Execution())
			}
			wantRequest := []string{inlineHTTPRequestPluginID}
			wantResponse := []string{inlineHTTPRequestPluginID}
			if !disableManaged {
				wantRequest = []string{managed.ID, inlineHTTPRequestPluginID}
				wantResponse = []string{inlineHTTPRequestPluginID, managed.ID}
			}
			if !reflect.DeepEqual(requestCalls, wantRequest) || !reflect.DeepEqual(responseCalls, wantResponse) {
				t.Fatalf("request calls=%#v response calls=%#v", requestCalls, responseCalls)
			}
			execution := session.Execution()
			if execution.ExecutionID != "inline-execution" || execution.MatchedPlugins[len(execution.MatchedPlugins)-1].PluginID != inlineHTTPRequestPluginID {
				t.Fatalf("execution = %+v", execution)
			}
			session.Close()
			if _, err := os.Stat(inlinePath); !os.IsNotExist(err) {
				t.Fatalf("inline revision was not removed: %v", err)
			}
		})
	}
}

func TestHTTPRequestRunnerGlobalDisableAndNoMatchAvoidRuntime(t *testing.T) {
	var calls int
	runtime := hookRuntimeFunc(func(_ context.Context, _ InvokeRequest) (InvokeResult, error) {
		calls++
		return InvokeResult{}, nil
	})
	for name, enabled := range map[string]bool{"disabled": false, "no-match": true} {
		t.Run(name, func(t *testing.T) {
			runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, enabled, 5000)
			createRunnerPlugin(t, manager, repository, CreatePluginInput{
				ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`,
			}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "POST", URLPattern: "https://other.example/*"}})
			session, err := runner.BeginRequest(context.Background(), proxyservice.HTTPRequestPluginBeginRequest{
				OriginalMethod: "GET", OriginalURL: "https://example.com/",
			})
			if err != nil || session != nil {
				t.Fatalf("BeginRequest session=%v err=%v", session, err)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("runtime call count = %d", calls)
	}
}

func TestHTTPRequestRunnerRejectsEnabledInlineScriptWhenRuntimeIsDisabled(t *testing.T) {
	runner, _, _, _ := newHTTPRequestRunnerHarness(t, hookRuntimeFunc(func(_ context.Context, _ InvokeRequest) (InvokeResult, error) {
		t.Fatal("runtime must not be called")
		return InvokeResult{}, nil
	}), false, 5000)
	session, err := runner.BeginRequest(context.Background(), proxyservice.HTTPRequestPluginBeginRequest{
		ExecutionID: "inline-disabled",
		InlinePythonScript: &proxyservice.InlinePythonScript{
			Enabled: true,
			Source:  "def onRequest(context, request):\n    return request",
		},
	})
	if err == nil || session != nil {
		t.Fatalf("session=%v err=%v", session, err)
	}
}

func TestHTTPRequestRunnerRequestBlockFailureAndTimeoutAreFailClosed(t *testing.T) {
	tests := map[string]struct {
		runtime hookRuntime
		want    string
	}{
		"blocked": {
			runtime: hookRuntimeFunc(func(_ context.Context, _ InvokeRequest) (InvokeResult, error) {
				return InvokeResult{Blocked: true, Shared: json.RawMessage(`{}`)}, nil
			}),
			want: "blocked",
		},
		"exception": {
			runtime: hookRuntimeFunc(func(_ context.Context, _ InvokeRequest) (InvokeResult, error) {
				return InvokeResult{}, &PythonExecutionError{Code: "hook_failed", Message: "boom", Traceback: "secret traceback"}
			}),
			want: "failed",
		},
		"timeout": {
			runtime: hookRuntimeFunc(func(ctx context.Context, _ InvokeRequest) (InvokeResult, error) {
				<-ctx.Done()
				return InvokeResult{}, ctx.Err()
			}),
			want: "failed",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			timeout := 5000
			if name == "timeout" {
				timeout = 100
			}
			runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, test.runtime, true, timeout)
			createRunnerPlugin(t, manager, repository, CreatePluginInput{
				ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`,
			}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
			session := beginRunnerSession(t, runner)
			defer session.Close()
			started := time.Now()
			result := session.RunRequest(context.Background(), minimalHTTPRequestPluginRequest())
			if test.want == "blocked" && !result.Blocked || test.want == "failed" && !result.Failed {
				t.Fatalf("result = %+v", result)
			}
			if name == "timeout" && time.Since(started) > time.Second {
				t.Fatalf("timeout took %s", time.Since(started))
			}
			execution := session.Execution()
			if len(execution.Invocations) != 1 || execution.Invocations[0].Outcome != test.want {
				t.Fatalf("execution = %+v", execution)
			}
			if test.want == "failed" && len(execution.Diagnostics) != 1 {
				t.Fatalf("diagnostics = %+v", execution.Diagnostics)
			}
		})
	}
}

func TestSanitizeRunnerDiagnosticPreservesPythonTracebackAndRedactsRevisionPath(t *testing.T) {
	revisionPath := filepath.Join(t.TempDir(), "revision")
	tail := "\nSyntaxError: invalid syntax"
	traceback := "Traceback (most recent call last):\r\n\tFile \"" +
		filepath.Join(revisionPath, mainFileName) + "\", line 3\r\n" +
		strings.Repeat("x", 3000) + tail

	got := sanitizeRunnerDiagnostic(&PythonExecutionError{
		Code: "validation_failed", Message: "invalid syntax", Traceback: traceback,
	}, revisionPath)

	if strings.Contains(got, revisionPath) {
		t.Fatalf("diagnostic exposed revision path: %q", got)
	}
	if !strings.Contains(got, filepath.Join("<plugin>", mainFileName)) {
		t.Fatalf("diagnostic did not redact revision path: %q", got)
	}
	if strings.Contains(got, "\r") || !strings.Contains(got, "\n\tFile") {
		t.Fatalf("diagnostic did not preserve traceback layout: %q", got)
	}
	if !strings.HasSuffix(got, tail) {
		t.Fatalf("diagnostic truncated traceback tail: %q", got)
	}
}

func TestHTTPRequestRunnerRejectsInvalidRequestMutations(t *testing.T) {
	tempFile := filepath.Join(t.TempDir(), "body.bin")
	if err := os.WriteFile(tempFile, []byte("body"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	tests := map[string]func(*requestWire) bool{
		"lowercase method": func(value *requestWire) bool { value.Method = "get"; return false },
		"invalid URL":      func(value *requestWire) bool { value.URL = "file:///tmp/data"; return false },
		"invalid header": func(value *requestWire) bool {
			value.Headers = []proxyservice.HTTPHeaderField{{Name: "X-Test", Value: "bad\r\nvalue"}}
			return false
		},
		"invalid binary": func(value *requestWire) bool {
			value.Body = requestBodyWire{Kind: "binary", Base64: "%%%"}
			return true
		},
		"missing file": func(value *requestWire) bool {
			value.Body = requestBodyWire{Kind: "file", Storage: "file", File: &bodyFileWire{Path: tempFile + ".missing", ReadOnly: true}}
			return true
		},
		"unavailable body": func(value *requestWire) bool {
			value.Body = requestBodyWire{Kind: "unavailable"}
			return true
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
				value, _ := json.Marshal(request.Value)
				var wire requestWire
				_ = json.Unmarshal(value, &wire)
				bodyChanged := mutate(&wire)
				encoded, _ := json.Marshal(wire)
				return InvokeResult{Value: encoded, Shared: json.RawMessage(`{}`), Transformed: true, BodyChanged: bodyChanged}, nil
			})
			runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
			createRunnerPlugin(t, manager, repository, CreatePluginInput{
				ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`,
			}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
			session := beginRunnerSession(t, runner)
			defer session.Close()
			result := session.RunRequest(context.Background(), minimalHTTPRequestPluginRequest())
			if !result.Failed || len(session.Execution().Diagnostics) != 1 || session.Execution().Diagnostics[0].Code != "invalid_result" {
				t.Fatalf("result=%+v execution=%+v", result, session.Execution())
			}
		})
	}
}

func TestHTTPRequestBodyRoundTripsAllKindsWithoutReadingDescriptors(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(filePath, []byte("payload"), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	file := &proxyservice.SendRequestFile{Path: filePath, Name: "payload.bin", Size: 7}
	bodies := []proxyservice.SendRequestBody{
		{BodyType: proxyservice.SendRequestBodyTypeNone},
		{BodyType: proxyservice.SendRequestBodyTypeText, Text: "text"},
		{BodyType: proxyservice.SendRequestBodyTypeXML, Text: "<value/>"},
		{BodyType: proxyservice.SendRequestBodyTypeJSON, Text: `{"value":1}`},
		{BodyType: proxyservice.SendRequestBodyTypeBinary, Text: base64.StdEncoding.EncodeToString([]byte{0, 1, 2})},
		{BodyType: proxyservice.SendRequestBodyTypeFile, File: file},
		{BodyType: proxyservice.SendRequestBodyTypeURLEncoded, URLEncoded: []*proxyservice.SendRequestURLEncodedItem{{Enabled: true, Name: "a", Value: "1"}}},
		{BodyType: proxyservice.SendRequestBodyTypeFormData, FormData: []*proxyservice.SendRequestFormDataItem{
			{Enabled: true, Name: "text", ItemType: "text", Value: "value"},
			{Enabled: true, Name: "file", ItemType: "file", File: file},
		}},
	}
	for _, body := range bodies {
		wire, err := httpRequestToWire(proxyservice.HTTPRequestPluginRequest{
			Method: "POST", URL: "https://example.com/", Body: body,
		})
		if err != nil {
			t.Fatalf("body %q to wire: %v", body.BodyType, err)
		}
		value, _ := json.Marshal(wire)
		result, err := httpRequestFromWire(value, proxyservice.HTTPRequestPluginRequest{Body: body}, false)
		if err != nil {
			t.Fatalf("body %q from wire: %v", body.BodyType, err)
		}
		if !reflect.DeepEqual(result.Body, body) {
			t.Fatalf("body %q round trip = %#v, want %#v", body.BodyType, result.Body, body)
		}
	}
}

func TestHTTPRequestWireRoundTripsMultipartFilename(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(filePath, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := proxyservice.HTTPRequestPluginRequest{
		Method: "POST",
		URL:    "https://example.test/upload",
		HeaderFields: []proxyservice.HTTPHeaderField{
			{Name: "X-Header", Value: "one"},
		},
		Body: proxyservice.SendRequestBody{
			BodyType: proxyservice.SendRequestBodyTypeFormData,
			FormData: []*proxyservice.SendRequestFormDataItem{{
				Enabled: true, Name: "upload", ItemType: "file",
				File:     &proxyservice.SendRequestFile{Path: filePath, Name: "payload.bin", Size: 7},
				Filename: "renamed.bin",
			}},
		},
	}
	wire, err := httpRequestToWire(request)
	if err != nil {
		t.Fatalf("httpRequestToWire: %v", err)
	}
	value, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	result, err := httpRequestFromWire(value, proxyservice.HTTPRequestPluginRequest{}, true)
	if err != nil {
		t.Fatalf("httpRequestFromWire: %v", err)
	}
	if !reflect.DeepEqual(result, request) {
		t.Fatalf("request round trip:\n got %#v\nwant %#v", result, request)
	}
}

func TestHTTPRequestFileWireUsesFileStorageWithoutInlineBytes(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(filePath, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := proxyservice.SendRequestBody{
		BodyType: proxyservice.SendRequestBodyTypeFile,
		File:     &proxyservice.SendRequestFile{Path: filePath, Name: "payload.bin", Size: 7},
	}
	wire, err := httpRequestBodyToWire(body, nil)
	if err != nil {
		t.Fatalf("httpRequestBodyToWire() error = %v", err)
	}
	if wire.Kind != "file" || wire.Storage != "file" || wire.File == nil || !wire.File.ReadOnly || wire.File.Size != 7 {
		t.Fatalf("file wire = %+v", wire)
	}
	if len(wire.Value) != 0 || wire.Base64 != "" || len(wire.Items) != 0 {
		t.Fatalf("file wire contains inline data: %+v", wire)
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "cGF5bG9hZA==") {
		t.Fatalf("file wire embedded payload bytes: %s", encoded)
	}
}

func TestHTTPRequestBodyWireRejectsConflictingStorageRepresentations(t *testing.T) {
	wire := requestBodyWire{
		Kind:    "text",
		Storage: "file",
		Value:   json.RawMessage(`"inline"`),
		File: &bodyFileWire{
			Path: filepath.Join(t.TempDir(), "body.txt"), Size: 6, ReadOnly: true,
		},
	}
	if _, _, err := httpRequestBodyFromWire(wire, proxyservice.SendRequestBody{}); err == nil {
		t.Fatalf("httpRequestBodyFromWire() accepted conflicting wire: %+v", wire)
	}
}

func TestHTTPRequestRunnerSpoolsRequestBodiesAboveInlineLimit(t *testing.T) {
	largeJSON := `{"data":"` + strings.Repeat("j", int(maxInlineHTTPRequestPluginBodyBytes)) + `"}`
	tests := []struct {
		name string
		body proxyservice.SendRequestBody
		want []byte
		kind string
	}{
		{
			name: "text", kind: "text",
			body: proxyservice.SendRequestBody{
				BodyType: proxyservice.SendRequestBodyTypeText,
				Text:     strings.Repeat("t", int(maxInlineHTTPRequestPluginBodyBytes+1)),
			},
			want: []byte(strings.Repeat("t", int(maxInlineHTTPRequestPluginBodyBytes+1))),
		},
		{
			name: "xml", kind: "xml",
			body: proxyservice.SendRequestBody{
				BodyType: proxyservice.SendRequestBodyTypeXML,
				Text:     strings.Repeat("x", int(maxInlineHTTPRequestPluginBodyBytes+1)),
			},
			want: []byte(strings.Repeat("x", int(maxInlineHTTPRequestPluginBodyBytes+1))),
		},
		{
			name: "json", kind: "json",
			body: proxyservice.SendRequestBody{
				BodyType: proxyservice.SendRequestBodyTypeJSON,
				Text:     largeJSON,
			},
			want: []byte(largeJSON),
		},
		{
			name: "binary", kind: "binary",
			body: proxyservice.SendRequestBody{
				BodyType: proxyservice.SendRequestBodyTypeBinary,
				Text: base64.StdEncoding.EncodeToString(
					bytes.Repeat([]byte{0x00, 0x7f, 0xff, 0x42}, int(maxInlineHTTPRequestPluginBodyBytes/4)+1),
				),
			},
			want: bytes.Repeat([]byte{0x00, 0x7f, 0xff, 0x42}, int(maxInlineHTTPRequestPluginBodyBytes/4)+1),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var spooledPath string
			runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
				value, _ := json.Marshal(request.Value)
				var wire requestWire
				if err := json.Unmarshal(value, &wire); err != nil {
					t.Fatal(err)
				}
				if wire.Body.Kind != test.kind || wire.Body.Storage != "file" || wire.Body.File == nil {
					t.Fatalf("body wire = %+v", wire.Body)
				}
				if len(wire.Body.Value) != 0 || wire.Body.Base64 != "" {
					t.Fatalf("file wire contains inline bytes: %+v", wire.Body)
				}
				got, err := os.ReadFile(wire.Body.File.Path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, test.want) {
					t.Fatalf("spooled bytes = %d, want %d", len(got), len(test.want))
				}
				spooledPath = wire.Body.File.Path
				return InvokeResult{Value: value, Shared: json.RawMessage(`{}`)}, nil
			})
			runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
			createRunnerPlugin(t, manager, repository, CreatePluginInput{
				ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`,
			}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
			session := beginRunnerSession(t, runner)
			result := session.RunRequest(context.Background(), proxyservice.HTTPRequestPluginRequest{
				Method: "POST", URL: "https://example.com/", Body: test.body,
			})
			if result.Failed || result.Blocked || result.Request.BodyFile == nil {
				t.Fatalf("RunRequest() result = %+v execution=%+v", result, session.Execution())
			}
			if result.Request.Body.Text != "" || result.Request.BodyFile.Size != int64(len(test.want)) {
				t.Fatalf("result body=%+v file=%+v", result.Request.Body, result.Request.BodyFile)
			}
			session.Close()
			if _, err := os.Stat(spooledPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("spooled file remained after Close(): %v", err)
			}
		})
	}
}

func TestHTTPRequestRunnerKeepsRequestAtInlineLimit(t *testing.T) {
	value := strings.Repeat("x", int(maxInlineHTTPRequestPluginBodyBytes))
	runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
		encoded, _ := json.Marshal(request.Value)
		var wire requestWire
		if err := json.Unmarshal(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		if wire.Body.Storage == "file" || wire.Body.File != nil {
			t.Fatalf("exact-limit body was spooled: %+v", wire.Body)
		}
		return InvokeResult{Value: encoded, Shared: json.RawMessage(`{}`)}, nil
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
	createRunnerPlugin(t, manager, repository, CreatePluginInput{
		ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`,
	}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
	session := beginRunnerSession(t, runner)
	defer session.Close()
	result := session.RunRequest(context.Background(), proxyservice.HTTPRequestPluginRequest{
		Method: "POST", URL: "https://example.com/",
		Body: proxyservice.SendRequestBody{BodyType: proxyservice.SendRequestBodyTypeText, Text: value},
	})
	if result.Failed || result.Request.BodyFile != nil || result.Request.Body.Text != value {
		t.Fatalf("RunRequest() result = %+v", result)
	}
}

func TestHTTPRequestRunnerStagesFileReplacementForNextPlugin(t *testing.T) {
	var firstStagedPath string
	var calls int
	runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
		calls++
		encoded, _ := json.Marshal(request.Value)
		var wire requestWire
		if err := json.Unmarshal(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		if calls == 1 {
			if request.OutputDirectory == "" {
				t.Fatal("first hook received no output directory")
			}
			firstStagedPath = filepath.Join(request.OutputDirectory, "replacement.bin")
			if err := os.WriteFile(firstStagedPath, []byte("replacement"), 0o600); err != nil {
				t.Fatal(err)
			}
			wire.Body = requestBodyWire{
				Kind: "binary", Storage: "file", Size: 11,
				File: &bodyFileWire{Path: firstStagedPath, Name: "replacement.bin", Size: 11, ReadOnly: true},
			}
			encoded, _ = json.Marshal(wire)
			return InvokeResult{Value: encoded, Shared: json.RawMessage(`{}`), Transformed: true, BodyChanged: true}, nil
		}
		if wire.Body.Storage != "file" || wire.Body.File == nil || wire.Body.File.Path != firstStagedPath {
			t.Fatalf("second hook body = %+v", wire.Body)
		}
		got, err := os.ReadFile(wire.Body.File.Path)
		if err != nil || string(got) != "replacement" {
			t.Fatalf("second hook file bytes=%q err=%v", got, err)
		}
		return InvokeResult{Value: encoded, Shared: json.RawMessage(`{}`)}, nil
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
	createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDOne, Name: "First", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
	createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDTwo, Name: "Second", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDTwo, Enabled: true, Method: "*", URLPattern: "*"}})
	session := beginRunnerSession(t, runner)
	result := session.RunRequest(context.Background(), minimalHTTPRequestPluginRequest())
	if result.Failed || result.Blocked || result.Request.BodyFile == nil || calls != 2 {
		t.Fatalf("RunRequest() result=%+v calls=%d execution=%+v", result, calls, session.Execution())
	}
	session.Close()
	if _, err := os.Stat(firstStagedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged replacement remained after Close(): %v", err)
	}
}

func TestHTTPRequestRunnerResponseUsesReverseOrderAndCarriesSharedState(t *testing.T) {
	var mu sync.Mutex
	requestCalls := make([]string, 0, 2)
	responseCalls := make([]string, 0, 2)
	runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if request.Hook == "onRequest" {
			requestCalls = append(requestCalls, request.PluginID)
			shared, _ := json.Marshal(map[string]any{"request": request.PluginID})
			value, _ := json.Marshal(request.Value)
			return InvokeResult{Value: value, Shared: shared}, nil
		}
		responseCalls = append(responseCalls, request.PluginID)
		shared := request.Context["shared"].(map[string]any)
		if shared["request"] != request.PluginID {
			t.Fatalf("plugin %s response shared = %#v", request.PluginID, shared)
		}
		value, _ := json.Marshal(request.Value)
		var wire responseWire
		if err := json.Unmarshal(value, &wire); err != nil {
			t.Fatalf("decode response wire: %v", err)
		}
		if wire.Protocol != "HTTP/2.0" {
			t.Fatalf("plugin %s response protocol = %q", request.PluginID, wire.Protocol)
		}
		if request.PluginID == testPluginIDTwo {
			if wire.Code != 200 || wire.StatusText != "200 OK" {
				t.Fatalf("second plugin response metadata = %+v", wire)
			}
			wire.Code = 201
			wire.StatusText = "201 Created"
		} else if wire.Code != 201 || wire.StatusText != "201 Created" {
			t.Fatalf("first plugin chained response metadata = %+v", wire)
		}
		wire.Headers = append(wire.Headers, proxyservice.HTTPHeaderField{Name: "X-Order", Value: request.PluginID})
		encoded, _ := json.Marshal(wire)
		return InvokeResult{Value: encoded, Shared: json.RawMessage(`{}`), Transformed: true}, nil
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
	first := createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDOne, Name: "First", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
	second := createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDTwo, Name: "Second", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDTwo, Enabled: true, Method: "*", URLPattern: "*"}})
	session := beginRunnerSession(t, runner)
	defer session.Close()
	requestResult := session.RunRequest(context.Background(), minimalHTTPRequestPluginRequest())
	if requestResult.Failed || requestResult.Blocked {
		t.Fatalf("request result = %+v", requestResult)
	}
	responseResult := session.RunResponse(context.Background(), proxyservice.HTTPRequestPluginResponse{
		StatusCode:   200,
		StatusText:   "200 OK",
		Protocol:     "HTTP/2.0",
		HeaderFields: []proxyservice.HTTPHeaderField{{Name: "X-Origin", Value: "yes"}},
		Body:         []byte("body"), BodyKind: "text", BodyAvailable: true,
		Request: requestResult.Request,
	})
	if responseResult.Failed || responseResult.Blocked {
		t.Fatalf("response result = %+v execution=%+v", responseResult, session.Execution())
	}
	if got, want := requestCalls, []string{first.ID, second.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("request order = %#v, want %#v", got, want)
	}
	if got, want := responseCalls, []string{second.ID, first.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("response order = %#v, want %#v", got, want)
	}
	fields := responseResult.Response.HeaderFields
	if len(fields) != 3 || fields[1].Value != second.ID || fields[2].Value != first.ID {
		t.Fatalf("chained response headers = %#v", fields)
	}
	if responseResult.Response.StatusCode != 201 || responseResult.Response.StatusText != "201 Created" || responseResult.Response.Protocol != "HTTP/2.0" {
		t.Fatalf("chained response metadata = %+v", responseResult.Response)
	}
	execution := session.Execution()
	if len(execution.Invocations) != 4 || !execution.ResponseTransformed || execution.ResponseDurationMicros < 0 {
		t.Fatalf("execution = %+v", execution)
	}
}

func TestHTTPRequestRunnerResponseReverseChainStabilizesFileThenReturnsInline(t *testing.T) {
	originalPath := filepath.Join(t.TempDir(), "original.txt")
	if err := os.WriteFile(originalPath, []byte("origin"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	var stagedPath string
	runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
		if request.Hook == "onRequest" {
			value, _ := json.Marshal(request.Value)
			return InvokeResult{Value: value, Shared: json.RawMessage(`{}`)}, nil
		}
		calls = append(calls, request.PluginID)
		value, _ := json.Marshal(request.Value)
		var wire responseWire
		if err := json.Unmarshal(value, &wire); err != nil {
			t.Fatal(err)
		}
		if len(calls) == 1 {
			if wire.Body.Storage != "file" || wire.Body.File == nil || wire.Body.File.Path != originalPath {
				t.Fatalf("first response hook body = %+v", wire.Body)
			}
			stagedPath = filepath.Join(request.OutputDirectory, "second.txt")
			if err := os.WriteFile(stagedPath, []byte("from-second"), 0o600); err != nil {
				t.Fatal(err)
			}
			wire.Body = requestBodyWire{
				Kind: "text", Storage: "file", Size: 11,
				File: &bodyFileWire{Path: stagedPath, Name: "second.txt", Size: 11, ReadOnly: true},
			}
		} else {
			if wire.Body.Storage != "file" || wire.Body.File == nil || wire.Body.File.Path != stagedPath {
				t.Fatalf("next response hook body = %+v", wire.Body)
			}
			got, err := os.ReadFile(wire.Body.File.Path)
			if err != nil || string(got) != "from-second" {
				t.Fatalf("stable response replacement = %q err=%v", got, err)
			}
			wire.Body = requestBodyWire{Kind: "text", Storage: "inline", Value: json.RawMessage(`"from-first"`)}
		}
		encoded, _ := json.Marshal(wire)
		return InvokeResult{
			Value: encoded, Shared: json.RawMessage(`{}`),
			Transformed: true, BodyChanged: true,
		}, nil
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
	first := createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDOne, Name: "First", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
	second := createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDTwo, Name: "Second", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDTwo, Enabled: true, Method: "*", URLPattern: "*"}})
	session := beginRunnerSession(t, runner)
	result := session.RunResponse(context.Background(), proxyservice.HTTPRequestPluginResponse{
		StatusCode: 200,
		BodyFile: &proxyservice.HTTPRequestPluginBodyFile{
			Path: originalPath, Name: "original.txt", Size: 6, ReadOnly: true,
		},
		BodyKind: "text", BodyAvailable: true,
		Request: minimalHTTPRequestPluginRequest(),
	})
	if result.Failed || result.Blocked || result.Response.BodyFile != nil || string(result.Response.Body) != "from-first" || result.Response.BodyKind != "text" {
		t.Fatalf("RunResponse() = %+v execution=%+v", result, session.Execution())
	}
	if got, want := calls, []string{second.ID, first.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("response calls = %#v, want %#v", got, want)
	}
	session.Close()
	if _, err := os.Stat(stagedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged response file remained after Close(): %v", err)
	}
}

func TestHTTPRequestRunnerResponseInlineCanBecomeFileBacked(t *testing.T) {
	var stagedPath string
	runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
		value, _ := json.Marshal(request.Value)
		var wire responseWire
		if err := json.Unmarshal(value, &wire); err != nil {
			t.Fatal(err)
		}
		stagedPath = filepath.Join(request.OutputDirectory, "replacement.bin")
		if err := os.WriteFile(stagedPath, []byte{0x00, 0xff, 0x01}, 0o600); err != nil {
			t.Fatal(err)
		}
		wire.Body = requestBodyWire{
			Kind: "binary", Storage: "file", Size: 3,
			File: &bodyFileWire{Path: stagedPath, Name: "replacement.bin", Size: 3, ReadOnly: true},
		}
		encoded, _ := json.Marshal(wire)
		return InvokeResult{Value: encoded, Shared: json.RawMessage(`{}`), Transformed: true, BodyChanged: true}, nil
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
	createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
	session := beginRunnerSession(t, runner)
	result := session.RunResponse(context.Background(), proxyservice.HTTPRequestPluginResponse{
		StatusCode: 200, Body: []byte("inline"), BodyKind: "text", BodyAvailable: true,
		Request: minimalHTTPRequestPluginRequest(),
	})
	if result.Failed || result.Blocked || result.Response.BodyFile == nil || result.Response.BodyFile.Path != stagedPath || result.Response.BodyKind != "binary" {
		t.Fatalf("RunResponse() = %+v execution=%+v", result, session.Execution())
	}
	session.Close()
	if _, err := os.Stat(stagedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged response file remained after Close(): %v", err)
	}
}

func TestHTTPRequestRunnerInvalidResponseFileFailsOpenAndCleansHandoff(t *testing.T) {
	var stagedPath string
	runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
		value, _ := json.Marshal(request.Value)
		var wire responseWire
		_ = json.Unmarshal(value, &wire)
		stagedPath = filepath.Join(request.OutputDirectory, "invalid.json")
		if err := os.WriteFile(stagedPath, []byte(`{"valid":true} trailing`), 0o600); err != nil {
			t.Fatal(err)
		}
		wire.Code = 299
		wire.Body = requestBodyWire{
			Kind: "json", Storage: "file", Size: 22,
			File: &bodyFileWire{Path: stagedPath, Name: "invalid.json", Size: 22, ReadOnly: true},
		}
		encoded, _ := json.Marshal(wire)
		return InvokeResult{Value: encoded, Shared: json.RawMessage(`{}`), Transformed: true, BodyChanged: true}, nil
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
	createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
	session := beginRunnerSession(t, runner)
	original := proxyservice.HTTPRequestPluginResponse{
		StatusCode: 200, Body: []byte("origin"), BodyKind: "text", BodyAvailable: true,
		Request: minimalHTTPRequestPluginRequest(),
	}
	result := session.RunResponse(context.Background(), original)
	if !result.Failed || result.Blocked || result.Response.StatusCode != original.StatusCode ||
		string(result.Response.Body) != string(original.Body) || result.Response.BodyFile != nil ||
		result.Response.BodyKind != original.BodyKind || !result.Response.BodyAvailable {
		t.Fatalf("RunResponse() = %+v execution=%+v", result, session.Execution())
	}
	session.Close()
	if _, err := os.Stat(stagedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid response handoff remained after Close(): %v", err)
	}
}

func TestHTTPRequestRunnerResponseFailureRollsBackWholeChain(t *testing.T) {
	var responseCalls []string
	runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
		if request.Hook == "onRequest" {
			value, _ := json.Marshal(request.Value)
			return InvokeResult{Value: value, Shared: json.RawMessage(`{}`)}, nil
		}
		responseCalls = append(responseCalls, request.PluginID)
		if len(responseCalls) == 2 {
			return InvokeResult{}, &PythonExecutionError{Code: "hook_failed", Message: "response failed", Traceback: "private"}
		}
		value, _ := json.Marshal(request.Value)
		var wire responseWire
		_ = json.Unmarshal(value, &wire)
		wire.Code = 299
		wire.Body = requestBodyWire{Kind: "text", Value: json.RawMessage(`"partial"`)}
		encoded, _ := json.Marshal(wire)
		return InvokeResult{Value: encoded, Shared: json.RawMessage(`{}`), Transformed: true, BodyChanged: true}, nil
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
	first := createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDOne, Name: "First", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
	second := createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDTwo, Name: "Second", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDTwo, Enabled: true, Method: "*", URLPattern: "*"}})
	session := beginRunnerSession(t, runner)
	defer session.Close()
	original := proxyservice.HTTPRequestPluginResponse{
		StatusCode: 200, HeaderFields: []proxyservice.HTTPHeaderField{{Name: "X-Origin", Value: "yes"}},
		Body: []byte("origin"), BodyKind: "text", BodyAvailable: true,
		Request: minimalHTTPRequestPluginRequest(),
	}
	result := session.RunResponse(context.Background(), original)
	if !result.Failed || result.Blocked || result.Response.StatusCode != original.StatusCode || string(result.Response.Body) != string(original.Body) || !reflect.DeepEqual(result.Response.HeaderFields, original.HeaderFields) {
		t.Fatalf("response rollback status=%d body=%q headers=%#v", result.Response.StatusCode, result.Response.Body, result.Response.HeaderFields)
	}
	if got, want := responseCalls, []string{second.ID, first.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("response calls = %#v, want %#v", got, want)
	}
	execution := session.Execution()
	if len(execution.Invocations) != 2 || execution.Invocations[0].Outcome != "completed" || execution.Invocations[1].Outcome != "failed" || len(execution.Diagnostics) != 1 {
		t.Fatalf("execution = %+v", execution)
	}
}

func TestHTTPRequestRunnerResponseBlockStopsReverseChain(t *testing.T) {
	var calls []string
	var outputDirectory string
	runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
		calls = append(calls, request.PluginID)
		outputDirectory = request.OutputDirectory
		return InvokeResult{Blocked: true, Shared: json.RawMessage(`{}`)}, nil
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
	createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDOne, Name: "First", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
	second := createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDTwo, Name: "Second", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDTwo, Enabled: true, Method: "*", URLPattern: "*"}})
	session := beginRunnerSession(t, runner)
	result := session.RunResponse(context.Background(), proxyservice.HTTPRequestPluginResponse{StatusCode: 200, BodyKind: "none", BodyAvailable: true})
	if !result.Blocked || result.Failed || !reflect.DeepEqual(calls, []string{second.ID}) {
		t.Fatalf("result=%+v calls=%#v", result, calls)
	}
	session.Close()
	if outputDirectory == "" {
		t.Fatal("response hook received no handoff directory")
	}
	if _, err := os.Stat(outputDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("response handoff directory remained after block: %v", err)
	}
}

func TestHTTPRequestRunnerStreamingResponseDoesNotCreateBodyHandoff(t *testing.T) {
	runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
		if request.OutputDirectory != "" {
			t.Fatalf("streaming response received output directory %q", request.OutputDirectory)
		}
		value, _ := json.Marshal(request.Value)
		var wire responseWire
		if err := json.Unmarshal(value, &wire); err != nil {
			t.Fatal(err)
		}
		if wire.Body.Kind != "unavailable" || !wire.Body.Streaming {
			t.Fatalf("streaming body wire = %+v", wire.Body)
		}
		return InvokeResult{Value: value, Shared: json.RawMessage(`{}`)}, nil
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
	createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
	session := beginRunnerSession(t, runner)
	result := session.RunResponse(context.Background(), proxyservice.HTTPRequestPluginResponse{
		StatusCode: 200, BodyKind: "unavailable", BodyAvailable: false, Streaming: true,
		Request: minimalHTTPRequestPluginRequest(),
	})
	if result.Failed || result.Blocked || !result.Response.Streaming || result.Response.BodyAvailable {
		t.Fatalf("RunResponse() = %+v execution=%+v", result, session.Execution())
	}
	concrete := session.(*httpRequestSession)
	concrete.storeMu.Lock()
	store := concrete.store
	concrete.storeMu.Unlock()
	if store != nil {
		t.Fatal("streaming response created a Body Store")
	}
	session.Close()
}

func TestHTTPResponseBodyWireRoundTripsSupportedKinds(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "body.bin")
	if err := os.WriteFile(filePath, []byte{0x00, 0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	xmlFilePath := filepath.Join(t.TempDir(), "body.xml")
	if err := os.WriteFile(xmlFilePath, []byte(`<root>世界</root>`), 0o600); err != nil {
		t.Fatal(err)
	}
	responses := []proxyservice.HTTPRequestPluginResponse{
		{StatusCode: 204, BodyKind: "none", BodyAvailable: true},
		{StatusCode: 200, Body: []byte("hello"), BodyKind: "text", BodyAvailable: true},
		{StatusCode: 200, Body: []byte(`<root>世界</root>`), BodyKind: "xml", BodyAvailable: true},
		{StatusCode: 200, Body: []byte(`{"value":1}`), BodyKind: "json", BodyAvailable: true},
		{StatusCode: 200, Body: []byte{0, 1, 2}, BodyKind: "binary", BodyAvailable: true},
		{
			StatusCode: 200, BodyKind: "binary", BodyAvailable: true,
			BodyFile: &proxyservice.HTTPRequestPluginBodyFile{
				Path: filePath, Name: "body.bin", Size: 2, ReadOnly: true,
			},
		},
		{
			StatusCode: 200, BodyKind: "xml", BodyAvailable: true,
			BodyFile: &proxyservice.HTTPRequestPluginBodyFile{
				Path: xmlFilePath, Name: "body.xml", Size: int64(len(`<root>世界</root>`)), ReadOnly: true,
			},
		},
		{StatusCode: 200, BodyKind: "unavailable", BodyAvailable: false, Streaming: true},
	}
	for _, response := range responses {
		response.HeaderFields = []proxyservice.HTTPHeaderField{{Name: "X-Header", Value: "one"}, {Name: "X-Header", Value: "two"}}
		response.TrailerFields = []proxyservice.HTTPHeaderField{{Name: "X-Trailer", Value: "done"}}
		response.Request = minimalHTTPRequestPluginRequest()
		wire, err := httpResponseToWire(response)
		if err != nil {
			t.Fatalf("kind %q to wire: %v", response.BodyKind, err)
		}
		value, _ := json.Marshal(wire)
		result, err := httpResponseFromWire(value, response, response.BodyAvailable)
		if err != nil {
			t.Fatalf("kind %q from wire: %v", response.BodyKind, err)
		}
		if result.StatusCode != response.StatusCode || result.BodyKind != response.BodyKind || result.BodyAvailable != response.BodyAvailable || result.Streaming != response.Streaming || !reflect.DeepEqual(result.Body, response.Body) || !reflect.DeepEqual(result.HeaderFields, response.HeaderFields) || !reflect.DeepEqual(result.TrailerFields, response.TrailerFields) {
			t.Fatalf("kind %q result=%+v want=%+v", response.BodyKind, result, response)
		}
	}
}

func TestHTTPRequestRunnerFileBackedResponseKeepsBodyAvailable(t *testing.T) {
	bodyPath := filepath.Join(t.TempDir(), "response.bin")
	body := bytes.Repeat([]byte{0x00, 0x7f, 0xff, 0x41}, 64*1024)
	if err := os.WriteFile(bodyPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := hookRuntimeFunc(func(_ context.Context, request InvokeRequest) (InvokeResult, error) {
		value, _ := json.Marshal(request.Value)
		var wire responseWire
		_ = json.Unmarshal(value, &wire)
		if wire.Body.Kind != "binary" || wire.Body.Storage != "file" || wire.Body.File == nil || wire.Body.File.Path != bodyPath || wire.Body.Streaming {
			t.Fatalf("file-backed body wire = %+v", wire.Body)
		}
		wire.Code = 299
		wire.Headers = append(wire.Headers, proxyservice.HTTPHeaderField{Name: "X-Plugin", Value: "yes"})
		encoded, _ := json.Marshal(wire)
		return InvokeResult{Value: encoded, Shared: json.RawMessage(`{}`), Transformed: true}, nil
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
	createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
	session := beginRunnerSession(t, runner)
	defer session.Close()
	result := session.RunResponse(context.Background(), proxyservice.HTTPRequestPluginResponse{
		StatusCode: 200, BodyFile: &proxyservice.HTTPRequestPluginBodyFile{
			Path: bodyPath, Name: "response.bin", Size: int64(len(body)), ReadOnly: true,
		}, BodyKind: "binary", BodyAvailable: true,
	})
	if result.Failed || result.Blocked || result.Response.StatusCode != 299 || result.Response.BodyFile == nil || result.Response.BodyFile.Path != bodyPath || result.Response.BodyKind != "binary" || !result.Response.BodyAvailable {
		t.Fatalf("result = %+v execution=%+v", result, session.Execution())
	}
	execution := session.Execution()
	if len(execution.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", execution.Diagnostics)
	}
}

func TestHTTPRequestRunnerResponseCancellationIsFailOpen(t *testing.T) {
	runtime := hookRuntimeFunc(func(ctx context.Context, _ InvokeRequest) (InvokeResult, error) {
		<-ctx.Done()
		return InvokeResult{}, ctx.Err()
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 5000)
	createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
	session := beginRunnerSession(t, runner)
	defer session.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	original := proxyservice.HTTPRequestPluginResponse{StatusCode: 200, Body: []byte("origin"), BodyKind: "text", BodyAvailable: true}
	result := session.RunResponse(ctx, original)
	if !result.Failed || !reflect.DeepEqual(result.Response, original) || len(session.Execution().Diagnostics) != 1 || session.Execution().Diagnostics[0].Code != "canceled" {
		t.Fatalf("result=%+v execution=%+v", result, session.Execution())
	}
}

func TestHTTPRequestRunnerResponseTimeoutCleansBodyHandoff(t *testing.T) {
	var outputDirectory string
	runtime := hookRuntimeFunc(func(ctx context.Context, request InvokeRequest) (InvokeResult, error) {
		outputDirectory = request.OutputDirectory
		<-ctx.Done()
		return InvokeResult{}, ctx.Err()
	})
	runner, manager, repository, _ := newHTTPRequestRunnerHarness(t, runtime, true, 1)
	createRunnerPlugin(t, manager, repository, CreatePluginInput{ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`}, []CreateRuleInput{{ID: testRuleIDOne, Enabled: true, Method: "*", URLPattern: "*"}})
	session := beginRunnerSession(t, runner)
	original := proxyservice.HTTPRequestPluginResponse{
		StatusCode: 200, Body: []byte("origin"), BodyKind: "text", BodyAvailable: true,
		Request: minimalHTTPRequestPluginRequest(),
	}
	result := session.RunResponse(context.Background(), original)
	if !result.Failed || result.Blocked || string(result.Response.Body) != "origin" {
		t.Fatalf("RunResponse() = %+v execution=%+v", result, session.Execution())
	}
	session.Close()
	if outputDirectory == "" {
		t.Fatal("response hook received no handoff directory")
	}
	if _, err := os.Stat(outputDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("response handoff directory remained after timeout: %v", err)
	}
}

func newHTTPRequestRunnerHarness(
	t *testing.T,
	runtime hookRuntime,
	enabled bool,
	timeoutMs int,
) (*httpRequestRunner, *packageManager, *repository, *settingservice.SettingService) {
	t.Helper()
	repository := newTestRepository(t)
	root := t.TempDir()
	manager, err := newPackageManager(
		repository, filepath.Join(root, "packages"), filepath.Join(root, "runtime"),
		&recordingRevisionValidator{},
	)
	if err != nil {
		t.Fatalf("newPackageManager: %v", err)
	}
	settings := &settingservice.SettingService{}
	if err := settings.Update(&settingservice.Settings{PythonPluginConfig: &settingservice.PythonPluginConfig{
		Enabled: enabled, InterpreterPath: `C:\Python311\python.exe`, HookTimeoutMs: timeoutMs,
	}}); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	return newHTTPRequestRunner(repository, settings, manager, runtime), manager, repository, settings
}

func createRunnerPlugin(
	t *testing.T,
	manager *packageManager,
	repository *repository,
	input CreatePluginInput,
	rules []CreateRuleInput,
) *Plugin {
	t.Helper()
	plugin, err := manager.createPlugin(context.Background(), input)
	if err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	plugin, err = manager.activateCurrent(context.Background(), plugin.ID)
	if err != nil {
		t.Fatalf("activate plugin: %v", err)
	}
	if err := repository.setPluginEnabled(context.Background(), plugin.ID, true); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	for _, rule := range rules {
		if _, err := repository.createRule(context.Background(), plugin.ID, rule); err != nil {
			t.Fatalf("create rule: %v", err)
		}
	}
	plugin, err = repository.getPlugin(context.Background(), plugin.ID)
	if err != nil {
		t.Fatalf("get plugin: %v", err)
	}
	return plugin
}

func beginRunnerSession(t *testing.T, runner *httpRequestRunner) proxyservice.HTTPRequestPluginSession {
	t.Helper()
	session, err := runner.BeginRequest(context.Background(), proxyservice.HTTPRequestPluginBeginRequest{
		ExecutionID: "request-1", Timestamp: time.Now().UnixMicro(),
		OriginalMethod: "GET", OriginalURL: "https://example.com/",
	})
	if err != nil {
		t.Fatalf("BeginRequest: %v", err)
	}
	if session == nil {
		t.Fatal("expected matched plugin session")
	}
	return session
}

func minimalHTTPRequestPluginRequest() proxyservice.HTTPRequestPluginRequest {
	return proxyservice.HTTPRequestPluginRequest{
		Method: "GET", URL: "https://example.com/",
		HeaderFields: []proxyservice.HTTPHeaderField{},
		Body:         proxyservice.SendRequestBody{BodyType: proxyservice.SendRequestBodyTypeNone},
	}
}
