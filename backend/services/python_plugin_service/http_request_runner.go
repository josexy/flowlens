package pythonpluginservice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	bodyspool "github.com/josexy/flowlens/backend/pkg/body_spool"
	"github.com/josexy/flowlens/backend/pkg/logger"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

type hookRuntime interface {
	Invoke(ctx context.Context, request InvokeRequest) (InvokeResult, error)
}

type httpRequestRunner struct {
	repository *repository
	settings   *settingservice.SettingService
	packages   *packageManager
	runtime    hookRuntime
}

type httpRequestPluginSnapshot struct {
	id       string
	name     string
	revision string
	params   map[string]any
	lease    *RevisionLease
}

type httpRequestSession struct {
	runtime     hookRuntime
	begin       proxyservice.HTTPRequestPluginBeginRequest
	timeout     time.Duration
	plugins     []httpRequestPluginSnapshot
	shared      map[string]json.RawMessage
	closeOnce   sync.Once
	storeMu     sync.Mutex
	store       *bodyspool.Store
	inlineLimit int64

	mu        sync.Mutex
	execution proxyservice.HTTPRequestPluginExecution
}

type requestWire struct {
	Method  string                         `json:"method"`
	URL     string                         `json:"url"`
	Headers []proxyservice.HTTPHeaderField `json:"headers"`
	Body    requestBodyWire                `json:"body"`
}

type requestBodyWire struct {
	Kind      string          `json:"kind"`
	Storage   string          `json:"storage,omitempty"`
	Value     json.RawMessage `json:"value,omitempty"`
	Base64    string          `json:"base64,omitempty"`
	File      *bodyFileWire   `json:"file,omitempty"`
	Items     json.RawMessage `json:"items,omitempty"`
	Size      int64           `json:"size,omitempty"`
	Streaming bool            `json:"streaming,omitempty"`
}

type bodyFileWire struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	ReadOnly bool   `json:"readOnly"`
}

type multipartItemWire struct {
	Enabled  bool          `json:"enabled"`
	Name     string        `json:"name"`
	Kind     string        `json:"kind"`
	Value    string        `json:"value"`
	File     *bodyFileWire `json:"file,omitempty"`
	Filename string        `json:"filename,omitempty"`
}

const (
	maxInlineHTTPRequestPluginBodyBytes int64 = bodyspool.DefaultInlineLimit
)

type responseWire struct {
	Code       int                            `json:"code"`
	StatusText string                         `json:"statusText"`
	Protocol   string                         `json:"protocol"`
	Headers    []proxyservice.HTTPHeaderField `json:"headers"`
	Trailers   []proxyservice.HTTPHeaderField `json:"trailers"`
	Body       requestBodyWire                `json:"body"`
	Request    requestWire                    `json:"request"`
}

func newHTTPRequestRunner(
	repository *repository,
	settings *settingservice.SettingService,
	packages *packageManager,
	runtime hookRuntime,
) *httpRequestRunner {
	return &httpRequestRunner{repository: repository, settings: settings, packages: packages, runtime: runtime}
}

func (r *httpRequestRunner) BeginRequest(
	ctx context.Context,
	request proxyservice.HTTPRequestPluginBeginRequest,
) (proxyservice.HTTPRequestPluginSession, error) {
	if r == nil || r.repository == nil || r.packages == nil || r.runtime == nil {
		return nil, errors.New("Python plugin runner is unavailable")
	}
	config, err := settingservice.GetPythonPluginConfig(r.settings)
	if err != nil {
		return nil, err
	}
	inlineEnabled := request.InlinePythonScript != nil && request.InlinePythonScript.Enabled
	if !config.Enabled {
		if inlineEnabled {
			return nil, errors.New("Python plugin runtime is disabled")
		}
		return nil, nil
	}
	matched := make([]*Plugin, 0)
	if !request.DisableManagedPlugins {
		plugins, listErr := r.repository.listPlugins(ctx)
		if listErr != nil {
			return nil, listErr
		}
		matched, err = matchPlugins(plugins, request.OriginalMethod, request.OriginalURL)
		if err != nil {
			return nil, err
		}
	}
	if len(matched) == 0 && !inlineEnabled {
		return nil, nil
	}
	pluginCount := len(matched)
	if inlineEnabled {
		pluginCount++
	}

	session := &httpRequestSession{
		runtime: r.runtime,
		begin:   request,
		timeout: time.Duration(config.HookTimeoutMs) * time.Millisecond,
		plugins: make([]httpRequestPluginSnapshot, 0, pluginCount),
		shared:  make(map[string]json.RawMessage, pluginCount),
		execution: proxyservice.HTTPRequestPluginExecution{
			ExecutionID:    request.ExecutionID,
			MatchedPlugins: make([]proxyservice.HTTPRequestPluginMatch, 0, pluginCount),
			Invocations:    []proxyservice.HTTPRequestPluginInvocation{},
			Diagnostics:    []proxyservice.HTTPRequestPluginDiagnostic{},
		},
		inlineLimit: maxInlineHTTPRequestPluginBodyBytes,
	}
	for _, plugin := range matched {
		params, err := decodeJSONObject(plugin.ParamsJSON)
		if err != nil {
			session.Close()
			return nil, fmt.Errorf("decode params for Python plugin %q: %w", plugin.ID, err)
		}
		lease, err := r.packages.acquireRevision(plugin.ID, plugin.ActiveRevision)
		if err != nil {
			session.Close()
			return nil, fmt.Errorf("acquire Python plugin revision %q: %w", plugin.ID, err)
		}
		session.plugins = append(session.plugins, httpRequestPluginSnapshot{
			id: plugin.ID, name: plugin.Name, revision: plugin.ActiveRevision,
			params: params, lease: lease,
		})
		session.shared[plugin.ID] = json.RawMessage(`{}`)
		session.execution.MatchedPlugins = append(session.execution.MatchedPlugins, proxyservice.HTTPRequestPluginMatch{
			PluginID: plugin.ID, Name: plugin.Name, Revision: plugin.ActiveRevision,
		})
	}
	if inlineEnabled {
		revision, lease, inlineErr := r.packages.createInlineRevision(ctx, request.ExecutionID, request.InlinePythonScript.Source)
		if inlineErr != nil {
			session.Close()
			return nil, inlineErr
		}
		session.plugins = append(session.plugins, httpRequestPluginSnapshot{
			id: inlineHTTPRequestPluginID, name: inlineHTTPRequestPluginName, revision: revision,
			params: map[string]any{}, lease: lease,
		})
		session.shared[inlineHTTPRequestPluginID] = json.RawMessage(`{}`)
		session.execution.MatchedPlugins = append(session.execution.MatchedPlugins, proxyservice.HTTPRequestPluginMatch{
			PluginID: inlineHTTPRequestPluginID, Name: inlineHTTPRequestPluginName, Revision: revision,
		})
	}
	return session, nil
}

func (s *httpRequestSession) RunRequest(
	ctx context.Context,
	request proxyservice.HTTPRequestPluginRequest,
) proxyservice.HTTPRequestPluginRequestResult {
	current := proxyservice.HTTPRequestPluginRequest{
		Method: request.Method, URL: request.URL,
		HeaderFields: append([]proxyservice.HTTPHeaderField(nil), request.HeaderFields...),
		Body:         clonePluginRequestBody(request.Body),
		BodyFile:     clonePluginBodyFile(request.BodyFile),
	}
	prepared, err := s.prepareRequestBody(ctx, current)
	if err != nil {
		if len(s.plugins) > 0 {
			s.recordRequestFailure(s.plugins[0], 0, "invalid_input", err)
		}
		return proxyservice.HTTPRequestPluginRequestResult{Request: current, Failed: true}
	}
	current = prepared
	for index := range s.plugins {
		plugin := &s.plugins[index]
		outputDirectory, err := s.newHandoffDir()
		if err != nil {
			s.recordRequestFailure(*plugin, 0, "body_storage_failed", err)
			return proxyservice.HTTPRequestPluginRequestResult{Request: current, Failed: true}
		}
		wireValue, err := httpRequestToWire(current)
		if err != nil {
			s.recordRequestFailure(*plugin, 0, "invalid_input", err)
			return proxyservice.HTTPRequestPluginRequestResult{Request: current, Failed: true}
		}
		shared, err := decodeJSONObject(string(s.shared[plugin.id]))
		if err != nil {
			s.recordRequestFailure(*plugin, 0, "invalid_shared", err)
			return proxyservice.HTTPRequestPluginRequestResult{Request: current, Failed: true}
		}
		hookContext := map[string]any{
			"id":              s.begin.ExecutionID,
			"timestamp":       s.begin.Timestamp,
			"original_url":    s.begin.OriginalURL,
			"original_method": s.begin.OriginalMethod,
			"params":          plugin.params,
			"shared":          shared,
			"transport": map[string]any{
				"protocol":                 s.begin.Transport.Protocol,
				"proxy_mode":               s.begin.Transport.ProxyMode,
				"tls_client_hello_profile": s.begin.Transport.TLSClientHelloID,
				"http2_fingerprint":        s.begin.Transport.HTTP2Fingerprint,
			},
		}
		hookContextDeadline, cancel := context.WithTimeout(ctx, s.timeout)
		started := time.Now()
		result, invokeErr := s.runtime.Invoke(hookContextDeadline, InvokeRequest{
			ExecutionID: s.begin.ExecutionID,
			PluginID:    plugin.id, PluginName: plugin.name, Revision: plugin.revision,
			Path: plugin.lease.Path, Hook: "onRequest", OutputDirectory: outputDirectory,
			Context: hookContext, Value: wireValue,
		})
		duration := time.Since(started).Microseconds()
		if invokeErr != nil {
			cancel()
			s.recordRequestFailure(*plugin, duration, executionErrorCode(invokeErr), invokeErr)
			return proxyservice.HTTPRequestPluginRequestResult{Request: current, Failed: true}
		}
		s.shared[plugin.id] = append(json.RawMessage(nil), result.Shared...)
		if result.Blocked {
			cancel()
			s.recordInvocation(*plugin, "request", duration, false, "blocked")
			return proxyservice.HTTPRequestPluginRequestResult{Request: current, Blocked: true}
		}
		transformed, err := httpRequestFromWire(result.Value, current, result.BodyChanged)
		if err == nil && result.BodyChanged {
			transformed, err = s.stabilizeRequestBodyFile(hookContextDeadline, outputDirectory, transformed)
		}
		if err == nil {
			transformed, err = proxyservice.ValidateHTTPRequestPluginRequestContext(hookContextDeadline, transformed)
		}
		cancel()
		if err != nil {
			s.recordRequestFailure(*plugin, duration, "invalid_result", err)
			return proxyservice.HTTPRequestPluginRequestResult{Request: current, Failed: true}
		}
		current = transformed
		s.recordInvocation(*plugin, "request", duration, result.Transformed, "completed")
	}
	return proxyservice.HTTPRequestPluginRequestResult{Request: current}
}

func (s *httpRequestSession) RunResponse(
	ctx context.Context,
	response proxyservice.HTTPRequestPluginResponse,
) proxyservice.HTTPRequestPluginResponseResult {
	original := clonePluginResponse(response)
	if original.Streaming {
		original.Body = nil
		original.BodyFile = nil
		original.BodyAvailable = false
		original.BodyKind = "unavailable"
	}
	validatedOriginal, err := proxyservice.ValidateHTTPRequestPluginResponseContext(ctx, original)
	if err != nil {
		if len(s.plugins) > 0 {
			s.recordResponseFailure(s.plugins[len(s.plugins)-1], 0, executionErrorCode(err), err)
		}
		return proxyservice.HTTPRequestPluginResponseResult{Response: original, Failed: true}
	}
	original = validatedOriginal
	current := clonePluginResponse(original)
	for index := len(s.plugins) - 1; index >= 0; index-- {
		plugin := &s.plugins[index]
		outputDirectory := ""
		if current.BodyAvailable {
			outputDirectory, err = s.newHandoffDir()
			if err != nil {
				s.recordResponseFailure(*plugin, 0, "body_storage_failed", err)
				return proxyservice.HTTPRequestPluginResponseResult{Response: original, Failed: true}
			}
		}
		wireValue, err := httpResponseToWire(current)
		if err != nil {
			s.recordResponseFailure(*plugin, 0, "invalid_input", err)
			return proxyservice.HTTPRequestPluginResponseResult{Response: original, Failed: true}
		}
		shared, err := decodeJSONObject(string(s.shared[plugin.id]))
		if err != nil {
			s.recordResponseFailure(*plugin, 0, "invalid_shared", err)
			return proxyservice.HTTPRequestPluginResponseResult{Response: original, Failed: true}
		}
		hookContext := map[string]any{
			"id":              s.begin.ExecutionID,
			"timestamp":       s.begin.Timestamp,
			"original_url":    s.begin.OriginalURL,
			"original_method": s.begin.OriginalMethod,
			"params":          plugin.params,
			"shared":          shared,
			"transport": map[string]any{
				"protocol":                 s.begin.Transport.Protocol,
				"proxy_mode":               s.begin.Transport.ProxyMode,
				"tls_client_hello_profile": s.begin.Transport.TLSClientHelloID,
				"http2_fingerprint":        s.begin.Transport.HTTP2Fingerprint,
			},
		}
		hookContextDeadline, cancel := context.WithTimeout(ctx, s.timeout)
		started := time.Now()
		result, invokeErr := s.runtime.Invoke(hookContextDeadline, InvokeRequest{
			ExecutionID: s.begin.ExecutionID,
			PluginID:    plugin.id, PluginName: plugin.name, Revision: plugin.revision,
			Path: plugin.lease.Path, Hook: "onResponse", OutputDirectory: outputDirectory,
			Context: hookContext, Value: wireValue,
		})
		duration := time.Since(started).Microseconds()
		if invokeErr != nil {
			cancel()
			s.recordResponseFailure(*plugin, duration, executionErrorCode(invokeErr), invokeErr)
			return proxyservice.HTTPRequestPluginResponseResult{Response: original, Failed: true}
		}
		s.shared[plugin.id] = append(json.RawMessage(nil), result.Shared...)
		if result.Blocked {
			cancel()
			s.recordInvocation(*plugin, "response", duration, false, "blocked")
			return proxyservice.HTTPRequestPluginResponseResult{Response: original, Blocked: true}
		}
		transformed, err := httpResponseFromWire(result.Value, current, result.BodyChanged)
		if err == nil && result.BodyChanged {
			transformed, err = s.stabilizeResponseBodyFile(hookContextDeadline, outputDirectory, transformed)
		}
		if err == nil {
			transformed, err = proxyservice.ValidateHTTPRequestPluginResponseContext(hookContextDeadline, transformed)
		}
		if err == nil && original.Streaming && !slices.Equal(transformed.TrailerFields, current.TrailerFields) {
			err = errors.New("SSE response hooks may only mutate status and headers")
		}
		cancel()
		if err != nil {
			s.recordResponseFailure(*plugin, duration, "invalid_result", err)
			return proxyservice.HTTPRequestPluginResponseResult{Response: original, Failed: true}
		}
		current = transformed
		s.recordInvocation(*plugin, "response", duration, result.Transformed, "completed")
	}
	return proxyservice.HTTPRequestPluginResponseResult{Response: current}
}

func (s *httpRequestSession) Execution() *proxyservice.HTTPRequestPluginExecution {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := s.execution
	clone.MatchedPlugins = append([]proxyservice.HTTPRequestPluginMatch(nil), s.execution.MatchedPlugins...)
	clone.Invocations = append([]proxyservice.HTTPRequestPluginInvocation(nil), s.execution.Invocations...)
	clone.Diagnostics = append([]proxyservice.HTTPRequestPluginDiagnostic(nil), s.execution.Diagnostics...)
	return &clone
}

func (s *httpRequestSession) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		for _, v := range slices.Backward(s.plugins) {
			v.lease.Release()
		}
		s.storeMu.Lock()
		store := s.store
		s.store = nil
		s.storeMu.Unlock()
		if store != nil {
			if err := store.Close(); err != nil {
				logger.G().Warnf("Python request plugin Body temporary storage cleanup failed: %T", err)
			}
		}
	})
}

func (s *httpRequestSession) ensureStore() (*bodyspool.Store, error) {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	if s.store != nil {
		return s.store, nil
	}
	store, err := bodyspool.New("flowlens-python-body-")
	if err != nil {
		return nil, err
	}
	s.store = store
	return store, nil
}

func (s *httpRequestSession) newHandoffDir() (string, error) {
	store, err := s.ensureStore()
	if err != nil {
		return "", err
	}
	return store.NewHandoffDir()
}

func (s *httpRequestSession) prepareRequestBody(
	ctx context.Context,
	request proxyservice.HTTPRequestPluginRequest,
) (proxyservice.HTTPRequestPluginRequest, error) {
	if request.BodyFile != nil {
		return proxyservice.ValidateHTTPRequestPluginRequestContext(ctx, request)
	}
	var reader io.Reader
	switch request.Body.BodyType {
	case proxyservice.SendRequestBodyTypeText,
		proxyservice.SendRequestBodyTypeJSON,
		proxyservice.SendRequestBodyTypeXML:
		if int64(len(request.Body.Text)) <= s.inlineLimit {
			return request, nil
		}
		reader = strings.NewReader(request.Body.Text)
	case proxyservice.SendRequestBodyTypeBinary:
		reader = base64.NewDecoder(base64.StdEncoding, strings.NewReader(request.Body.Text))
	default:
		return request, nil
	}
	store, err := s.ensureStore()
	if err != nil {
		return request, err
	}
	payload, err := store.Read(ctx, reader, s.inlineLimit)
	if err != nil {
		return request, err
	}
	if payload.File == nil {
		return request, nil
	}
	request.Body.Text = ""
	request.Body.File = nil
	request.BodyFile = &proxyservice.HTTPRequestPluginBodyFile{
		Path: payload.File.Path, Name: "request-body", Size: payload.File.Size, ReadOnly: true,
	}
	return proxyservice.ValidateHTTPRequestPluginRequestContext(ctx, request)
}

func (s *httpRequestSession) stabilizeRequestBodyFile(
	ctx context.Context,
	handoffDirectory string,
	request proxyservice.HTTPRequestPluginRequest,
) (proxyservice.HTTPRequestPluginRequest, error) {
	if request.BodyFile == nil {
		return request, nil
	}
	store, err := s.ensureStore()
	if err != nil {
		return proxyservice.HTTPRequestPluginRequest{}, err
	}
	payload, adoptErr := store.AdoptFile(ctx, handoffDirectory, request.BodyFile.Path)
	if adoptErr != nil {
		if err := ctx.Err(); err != nil {
			return proxyservice.HTTPRequestPluginRequest{}, err
		}
		payload, err = store.CopyFile(ctx, request.BodyFile.Path)
		if err != nil {
			return proxyservice.HTTPRequestPluginRequest{}, err
		}
	}
	request.BodyFile = &proxyservice.HTTPRequestPluginBodyFile{
		Path: payload.File.Path, Name: request.BodyFile.Name,
		Size: payload.File.Size, ReadOnly: true,
	}
	return request, nil
}

func (s *httpRequestSession) stabilizeResponseBodyFile(
	ctx context.Context,
	handoffDirectory string,
	response proxyservice.HTTPRequestPluginResponse,
) (proxyservice.HTTPRequestPluginResponse, error) {
	if response.BodyFile == nil {
		return response, nil
	}
	store, err := s.ensureStore()
	if err != nil {
		return proxyservice.HTTPRequestPluginResponse{}, err
	}
	payload, adoptErr := store.AdoptFile(ctx, handoffDirectory, response.BodyFile.Path)
	if adoptErr != nil {
		if err := ctx.Err(); err != nil {
			return proxyservice.HTTPRequestPluginResponse{}, err
		}
		payload, err = store.CopyFile(ctx, response.BodyFile.Path)
		if err != nil {
			return proxyservice.HTTPRequestPluginResponse{}, err
		}
	}
	response.Body = nil
	response.BodyFile = &proxyservice.HTTPRequestPluginBodyFile{
		Path: payload.File.Path, Name: response.BodyFile.Name,
		Size: payload.File.Size, ReadOnly: true,
	}
	return response, nil
}

func (s *httpRequestSession) recordInvocation(
	plugin httpRequestPluginSnapshot,
	phase string,
	duration int64,
	transformed bool,
	outcome string,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.execution.Invocations = append(s.execution.Invocations, proxyservice.HTTPRequestPluginInvocation{
		PluginID: plugin.id, Name: plugin.name, Revision: plugin.revision,
		Phase: phase, DurationMicros: duration, Transformed: transformed, Outcome: outcome,
	})
	if phase == "request" {
		s.execution.RequestDurationMicros += duration
		s.execution.RequestTransformed = s.execution.RequestTransformed || transformed
	} else {
		s.execution.ResponseDurationMicros += duration
		s.execution.ResponseTransformed = s.execution.ResponseTransformed || transformed
	}
}

func (s *httpRequestSession) recordRequestFailure(
	plugin httpRequestPluginSnapshot,
	duration int64,
	code string,
	err error,
) {
	s.recordInvocation(plugin, "request", duration, false, "failed")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.execution.Diagnostics = append(s.execution.Diagnostics, proxyservice.HTTPRequestPluginDiagnostic{
		PluginID: plugin.id, Name: plugin.name, Phase: "request", Code: code,
		Message: sanitizeRunnerDiagnostic(err, plugin.lease.Path),
	})
}

func (s *httpRequestSession) recordResponseFailure(
	plugin httpRequestPluginSnapshot,
	duration int64,
	code string,
	err error,
) {
	s.recordInvocation(plugin, "response", duration, false, "failed")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.execution.Diagnostics = append(s.execution.Diagnostics, proxyservice.HTTPRequestPluginDiagnostic{
		PluginID: plugin.id, Name: plugin.name, Phase: "response", Code: code,
		Message: sanitizeRunnerDiagnostic(err, plugin.lease.Path),
	})
}

func httpRequestToWire(request proxyservice.HTTPRequestPluginRequest) (requestWire, error) {
	body, err := httpRequestBodyToWire(request.Body, request.BodyFile)
	if err != nil {
		return requestWire{}, err
	}
	return requestWire{
		Method: request.Method, URL: request.URL,
		Headers: append([]proxyservice.HTTPHeaderField(nil), request.HeaderFields...),
		Body:    body,
	}, nil
}

func httpRequestBodyToWire(
	body proxyservice.SendRequestBody,
	bodyFile *proxyservice.HTTPRequestPluginBodyFile,
) (requestBodyWire, error) {
	if bodyFile != nil {
		kind := string(body.BodyType)
		if kind == "" {
			return requestBodyWire{}, errors.New("file-backed request body requires a semantic kind")
		}
		file := bodyFileWireFromHTTPRequestPluginBodyFile(bodyFile)
		return requestBodyWire{
			Kind: kind, Storage: "file", File: file, Size: file.Size,
		}, nil
	}
	switch body.BodyType {
	case "", proxyservice.SendRequestBodyTypeNone:
		return requestBodyWire{Kind: "none", Value: json.RawMessage(`null`)}, nil
	case proxyservice.SendRequestBodyTypeText:
		value, _ := json.Marshal(body.Text)
		return requestBodyWire{Kind: "text", Value: value}, nil
	case proxyservice.SendRequestBodyTypeXML:
		value, _ := json.Marshal(body.Text)
		return requestBodyWire{Kind: "xml", Value: value}, nil
	case proxyservice.SendRequestBodyTypeJSON:
		value, err := decodeJSONValue(body.Text)
		if err != nil {
			return requestBodyWire{}, fmt.Errorf("decode request JSON body: %w", err)
		}
		encoded, _ := json.Marshal(value)
		return requestBodyWire{Kind: "json", Value: encoded}, nil
	case proxyservice.SendRequestBodyTypeBinary:
		if _, err := base64.StdEncoding.DecodeString(body.Text); err != nil {
			return requestBodyWire{}, fmt.Errorf("decode request binary body: %w", err)
		}
		return requestBodyWire{Kind: "binary", Base64: body.Text}, nil
	case proxyservice.SendRequestBodyTypeFile:
		file := bodyFileWireFromSendRequestFile(body.File)
		if file == nil {
			return requestBodyWire{}, errors.New("Request file body requires a descriptor")
		}
		return requestBodyWire{
			Kind: "file", Storage: "file", File: file, Size: file.Size,
		}, nil
	case proxyservice.SendRequestBodyTypeURLEncoded:
		items := make([]*proxyservice.SendRequestURLEncodedItem, 0, len(body.URLEncoded))
		for _, item := range body.URLEncoded {
			if item == nil {
				continue
			}
			clone := *item
			items = append(items, &clone)
		}
		value, err := json.Marshal(items)
		return requestBodyWire{Kind: "urlencoded", Items: value}, err
	case proxyservice.SendRequestBodyTypeFormData:
		items := make([]multipartItemWire, 0, len(body.FormData))
		for _, item := range body.FormData {
			if item == nil {
				continue
			}
			kind := item.ItemType
			if kind == "" {
				kind = "text"
			}
			items = append(items, multipartItemWire{
				Enabled: item.Enabled, Name: item.Name, Kind: kind,
				Value: item.Value, File: bodyFileWireFromSendRequestFile(item.File),
				Filename: item.Filename,
			})
		}
		value, err := json.Marshal(items)
		return requestBodyWire{Kind: "multipart", Items: value}, err
	default:
		return requestBodyWire{}, fmt.Errorf("unsupported request body type %q", body.BodyType)
	}
}

func httpRequestFromWire(
	value json.RawMessage,
	original proxyservice.HTTPRequestPluginRequest,
	bodyChanged bool,
) (proxyservice.HTTPRequestPluginRequest, error) {
	if len(value) == 0 {
		return proxyservice.HTTPRequestPluginRequest{}, errors.New("Python request hook returned no request")
	}
	var wire requestWire
	if err := json.Unmarshal(value, &wire); err != nil {
		return proxyservice.HTTPRequestPluginRequest{}, fmt.Errorf("decode Python request result: %w", err)
	}
	body := clonePluginRequestBody(original.Body)
	bodyFile := clonePluginBodyFile(original.BodyFile)
	var err error
	if bodyChanged {
		body, bodyFile, err = httpRequestBodyFromWire(wire.Body, original.Body)
		if err != nil {
			return proxyservice.HTTPRequestPluginRequest{}, err
		}
	}
	return proxyservice.HTTPRequestPluginRequest{
		Method: wire.Method, URL: wire.URL,
		HeaderFields: append([]proxyservice.HTTPHeaderField(nil), wire.Headers...),
		Body:         body,
		BodyFile:     bodyFile,
	}, nil
}

func httpRequestBodyFromWire(
	wire requestBodyWire,
	original proxyservice.SendRequestBody,
) (proxyservice.SendRequestBody, *proxyservice.HTTPRequestPluginBodyFile, error) {
	storage, err := normalizedBodyWireStorage(wire)
	if err != nil {
		return proxyservice.SendRequestBody{}, nil, err
	}
	if storage == "file" {
		bodyType := proxyservice.SendRequestBodyType(wire.Kind)
		switch bodyType {
		case proxyservice.SendRequestBodyTypeText,
			proxyservice.SendRequestBodyTypeJSON,
			proxyservice.SendRequestBodyTypeXML,
			proxyservice.SendRequestBodyTypeBinary,
			proxyservice.SendRequestBodyTypeFile:
		default:
			return proxyservice.SendRequestBody{}, nil, fmt.Errorf(
				"unsupported file-backed Python request body kind %q", wire.Kind,
			)
		}
		return proxyservice.SendRequestBody{BodyType: bodyType}, httpRequestPluginBodyFileFromWire(wire.File), nil
	}
	switch wire.Kind {
	case "none":
		return proxyservice.SendRequestBody{BodyType: proxyservice.SendRequestBodyTypeNone}, nil, nil
	case "text":
		var value string
		if err := json.Unmarshal(wire.Value, &value); err != nil {
			return proxyservice.SendRequestBody{}, nil, errors.New("Python text body value must be a string")
		}
		bodyType := proxyservice.SendRequestBodyTypeText
		if original.BodyType == proxyservice.SendRequestBodyTypeXML {
			bodyType = proxyservice.SendRequestBodyTypeXML
		}
		return proxyservice.SendRequestBody{BodyType: bodyType, Text: value}, nil, nil
	case "xml":
		var value string
		if err := json.Unmarshal(wire.Value, &value); err != nil {
			return proxyservice.SendRequestBody{}, nil, errors.New("Python XML body value must be a string")
		}
		return proxyservice.SendRequestBody{BodyType: proxyservice.SendRequestBodyTypeXML, Text: value}, nil, nil
	case "json":
		if len(wire.Value) == 0 || !json.Valid(wire.Value) {
			return proxyservice.SendRequestBody{}, nil, errors.New("Python JSON body value is invalid")
		}
		compact := new(bytes.Buffer)
		if err := json.Compact(compact, wire.Value); err != nil {
			return proxyservice.SendRequestBody{}, nil, fmt.Errorf("compact Python JSON body: %w", err)
		}
		return proxyservice.SendRequestBody{BodyType: proxyservice.SendRequestBodyTypeJSON, Text: compact.String()}, nil, nil
	case "binary":
		if _, err := base64.StdEncoding.DecodeString(wire.Base64); err != nil {
			return proxyservice.SendRequestBody{}, nil, fmt.Errorf("decode Python binary body: %w", err)
		}
		return proxyservice.SendRequestBody{BodyType: proxyservice.SendRequestBodyTypeBinary, Text: wire.Base64}, nil, nil
	case "file":
		return proxyservice.SendRequestBody{}, nil, errors.New("Python file body requires file storage")
	case "urlencoded":
		var items []*proxyservice.SendRequestURLEncodedItem
		if len(wire.Items) > 0 {
			if err := json.Unmarshal(wire.Items, &items); err != nil {
				return proxyservice.SendRequestBody{}, nil, fmt.Errorf("decode Python URL-encoded body: %w", err)
			}
		}
		return proxyservice.SendRequestBody{BodyType: proxyservice.SendRequestBodyTypeURLEncoded, URLEncoded: items}, nil, nil
	case "multipart":
		var wireItems []multipartItemWire
		if len(wire.Items) > 0 {
			if err := json.Unmarshal(wire.Items, &wireItems); err != nil {
				return proxyservice.SendRequestBody{}, nil, fmt.Errorf("decode Python multipart body: %w", err)
			}
		}
		items := make([]*proxyservice.SendRequestFormDataItem, 0, len(wireItems))
		for _, item := range wireItems {
			items = append(items, &proxyservice.SendRequestFormDataItem{
				Enabled: item.Enabled, Name: item.Name, ItemType: item.Kind,
				Value: item.Value, File: sendRequestFileFromBodyFileWire(item.File),
				Filename: item.Filename,
			})
		}
		return proxyservice.SendRequestBody{BodyType: proxyservice.SendRequestBodyTypeFormData, FormData: items}, nil, nil
	case "unavailable":
		return proxyservice.SendRequestBody{}, nil, errors.New("request body cannot be unavailable")
	default:
		return proxyservice.SendRequestBody{}, nil, fmt.Errorf("unsupported Python request body kind %q", wire.Kind)
	}
}

func httpResponseToWire(response proxyservice.HTTPRequestPluginResponse) (responseWire, error) {
	body, err := httpResponseBodyToWire(response)
	if err != nil {
		return responseWire{}, err
	}
	request, err := httpRequestToWire(response.Request)
	if err != nil {
		return responseWire{}, fmt.Errorf("encode response request snapshot: %w", err)
	}
	return responseWire{
		Code:       response.StatusCode,
		StatusText: response.StatusText,
		Protocol:   response.Protocol,
		Headers:    append([]proxyservice.HTTPHeaderField(nil), response.HeaderFields...),
		Trailers:   append([]proxyservice.HTTPHeaderField(nil), response.TrailerFields...),
		Body:       body,
		Request:    request,
	}, nil
}

func httpResponseBodyToWire(response proxyservice.HTTPRequestPluginResponse) (requestBodyWire, error) {
	if !response.BodyAvailable {
		return requestBodyWire{Kind: "unavailable", Value: json.RawMessage(`null`), Streaming: response.Streaming}, nil
	}
	if response.BodyFile != nil {
		if len(response.Body) != 0 {
			return requestBodyWire{}, errors.New("file-backed response body cannot contain inline bytes")
		}
		switch response.BodyKind {
		case "text", "json", "xml", "binary":
		default:
			return requestBodyWire{}, fmt.Errorf("unsupported file-backed response body kind %q", response.BodyKind)
		}
		return requestBodyWire{
			Kind: response.BodyKind, Storage: "file", Size: response.BodyFile.Size,
			File: bodyFileWireFromHTTPRequestPluginBodyFile(response.BodyFile),
		}, nil
	}
	switch response.BodyKind {
	case "none":
		return requestBodyWire{Kind: "none", Value: json.RawMessage(`null`)}, nil
	case "text", "xml":
		value, err := json.Marshal(string(response.Body))
		return requestBodyWire{Kind: response.BodyKind, Value: value}, err
	case "json":
		value, err := decodeJSONValue(string(response.Body))
		if err != nil {
			return requestBodyWire{}, fmt.Errorf("decode response JSON body: %w", err)
		}
		encoded, err := json.Marshal(value)
		return requestBodyWire{Kind: "json", Value: encoded}, err
	case "binary":
		return requestBodyWire{Kind: "binary", Base64: base64.StdEncoding.EncodeToString(response.Body)}, nil
	default:
		return requestBodyWire{}, fmt.Errorf("unsupported response body kind %q", response.BodyKind)
	}
}

func httpResponseFromWire(
	value json.RawMessage,
	original proxyservice.HTTPRequestPluginResponse,
	bodyChanged bool,
) (proxyservice.HTTPRequestPluginResponse, error) {
	if len(value) == 0 {
		return proxyservice.HTTPRequestPluginResponse{}, errors.New("Python response hook returned no response")
	}
	var wire responseWire
	if err := json.Unmarshal(value, &wire); err != nil {
		return proxyservice.HTTPRequestPluginResponse{}, fmt.Errorf("decode Python response result: %w", err)
	}
	result := clonePluginResponse(original)
	result.StatusCode = wire.Code
	result.StatusText = wire.StatusText
	result.HeaderFields = append([]proxyservice.HTTPHeaderField(nil), wire.Headers...)
	result.TrailerFields = append([]proxyservice.HTTPHeaderField(nil), wire.Trailers...)
	if bodyChanged {
		if !original.BodyAvailable {
			return proxyservice.HTTPRequestPluginResponse{}, errors.New("an unavailable or streaming response body cannot be modified")
		}
		body, bodyFile, kind, err := httpResponseBodyFromWire(wire.Body)
		if err != nil {
			return proxyservice.HTTPRequestPluginResponse{}, err
		}
		result.Body = body
		result.BodyFile = bodyFile
		result.BodyKind = kind
	}
	return result, nil
}

func httpResponseBodyFromWire(
	wire requestBodyWire,
) ([]byte, *proxyservice.HTTPRequestPluginBodyFile, string, error) {
	storage, err := normalizedBodyWireStorage(wire)
	if err != nil {
		return nil, nil, "", err
	}
	if storage == "file" {
		switch wire.Kind {
		case "text", "json", "xml", "binary":
		default:
			return nil, nil, "", fmt.Errorf("unsupported file-backed Python response body kind %q", wire.Kind)
		}
		return nil, httpRequestPluginBodyFileFromWire(wire.File), wire.Kind, nil
	}
	switch wire.Kind {
	case "none":
		return nil, nil, "none", nil
	case "text", "xml":
		var value string
		if err := json.Unmarshal(wire.Value, &value); err != nil {
			return nil, nil, "", fmt.Errorf("Python response %s body value must be a string", wire.Kind)
		}
		return []byte(value), nil, wire.Kind, nil
	case "json":
		if len(wire.Value) == 0 || !json.Valid(wire.Value) {
			return nil, nil, "", errors.New("Python response JSON body value is invalid")
		}
		compact := new(bytes.Buffer)
		if err := json.Compact(compact, wire.Value); err != nil {
			return nil, nil, "", fmt.Errorf("compact Python response JSON body: %w", err)
		}
		return append([]byte(nil), compact.Bytes()...), nil, "json", nil
	case "binary":
		value, err := base64.StdEncoding.DecodeString(wire.Base64)
		if err != nil {
			return nil, nil, "", fmt.Errorf("decode Python response binary body: %w", err)
		}
		return value, nil, "binary", nil
	case "unavailable":
		return nil, nil, "", errors.New("Python response body cannot become unavailable")
	default:
		return nil, nil, "", fmt.Errorf("unsupported Python response body kind %q", wire.Kind)
	}
}

func clonePluginResponse(response proxyservice.HTTPRequestPluginResponse) proxyservice.HTTPRequestPluginResponse {
	response.HeaderFields = append([]proxyservice.HTTPHeaderField(nil), response.HeaderFields...)
	response.TrailerFields = append([]proxyservice.HTTPHeaderField(nil), response.TrailerFields...)
	response.Body = append([]byte(nil), response.Body...)
	response.Request = proxyservice.HTTPRequestPluginRequest{
		Method: response.Request.Method, URL: response.Request.URL,
		HeaderFields: append([]proxyservice.HTTPHeaderField(nil), response.Request.HeaderFields...),
		Body:         clonePluginRequestBody(response.Request.Body),
		BodyFile:     clonePluginBodyFile(response.Request.BodyFile),
	}
	response.BodyFile = clonePluginBodyFile(response.BodyFile)
	return response
}

func clonePluginRequestBody(body proxyservice.SendRequestBody) proxyservice.SendRequestBody {
	clone := proxyservice.SendRequestBody{BodyType: body.BodyType, Text: body.Text, File: clonePluginFile(body.File)}
	if body.FormData != nil {
		clone.FormData = make([]*proxyservice.SendRequestFormDataItem, 0, len(body.FormData))
	}
	for _, item := range body.FormData {
		if item == nil {
			clone.FormData = append(clone.FormData, nil)
			continue
		}
		copy := *item
		copy.File = clonePluginFile(item.File)
		clone.FormData = append(clone.FormData, &copy)
	}
	if body.URLEncoded != nil {
		clone.URLEncoded = make([]*proxyservice.SendRequestURLEncodedItem, 0, len(body.URLEncoded))
	}
	for _, item := range body.URLEncoded {
		if item == nil {
			clone.URLEncoded = append(clone.URLEncoded, nil)
			continue
		}
		copy := *item
		clone.URLEncoded = append(clone.URLEncoded, &copy)
	}
	return clone
}

func normalizedBodyWireStorage(wire requestBodyWire) (string, error) {
	storage := strings.TrimSpace(wire.Storage)
	if storage == "" {
		switch {
		case wire.File != nil:
			storage = "file"
		case wire.Kind == "unavailable":
			storage = "unavailable"
		default:
			storage = "inline"
		}
	}
	switch storage {
	case "inline":
		if wire.File != nil {
			return "", errors.New("Python body cannot contain a file descriptor with inline storage")
		}
	case "file":
		if wire.File == nil {
			return "", errors.New("file-backed Python body requires a descriptor")
		}
		if len(wire.Value) > 0 || wire.Base64 != "" || len(wire.Items) > 0 {
			return "", errors.New("file-backed Python body cannot contain inline data")
		}
	case "unavailable":
		if wire.Kind != "unavailable" || wire.File != nil || wire.Base64 != "" || len(wire.Items) > 0 {
			return "", errors.New("unavailable Python body has an invalid representation")
		}
	default:
		return "", fmt.Errorf("unsupported Python body storage %q", storage)
	}
	return storage, nil
}

func bodyFileWireFromSendRequestFile(file *proxyservice.SendRequestFile) *bodyFileWire {
	if file == nil {
		return nil
	}
	return &bodyFileWire{
		Path: file.Path, Name: file.Name, Size: file.Size, ReadOnly: true,
	}
}

func bodyFileWireFromHTTPRequestPluginBodyFile(file *proxyservice.HTTPRequestPluginBodyFile) *bodyFileWire {
	if file == nil {
		return nil
	}
	return &bodyFileWire{
		Path: file.Path, Name: file.Name, Size: file.Size, ReadOnly: file.ReadOnly,
	}
}

func httpRequestPluginBodyFileFromWire(file *bodyFileWire) *proxyservice.HTTPRequestPluginBodyFile {
	if file == nil {
		return nil
	}
	return &proxyservice.HTTPRequestPluginBodyFile{
		Path: file.Path, Name: file.Name, Size: file.Size, ReadOnly: file.ReadOnly,
	}
}

func clonePluginBodyFile(file *proxyservice.HTTPRequestPluginBodyFile) *proxyservice.HTTPRequestPluginBodyFile {
	if file == nil {
		return nil
	}
	clone := *file
	return &clone
}

func sendRequestFileFromBodyFileWire(file *bodyFileWire) *proxyservice.SendRequestFile {
	if file == nil {
		return nil
	}
	return &proxyservice.SendRequestFile{Path: file.Path, Name: file.Name, Size: file.Size}
}

func clonePluginFile(file *proxyservice.SendRequestFile) *proxyservice.SendRequestFile {
	if file == nil {
		return nil
	}
	clone := *file
	return &clone
}

func decodeJSONValue(value string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, errors.New("multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return result, nil
}

func decodeJSONObject(value string) (map[string]any, error) {
	decoded, err := decodeJSONValue(value)
	if err != nil {
		return nil, err
	}
	result, ok := decoded.(map[string]any)
	if !ok || result == nil {
		return nil, errors.New("value must be a JSON object")
	}
	return result, nil
}

func executionErrorCode(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	var executionError *PythonExecutionError
	if errors.As(err, &executionError) && executionError.Code != "" {
		return executionError.Code
	}
	return "worker_failed"
}

func sanitizeRunnerDiagnostic(err error, revisionPath string) string {
	if err == nil {
		return "Python plugin execution failed"
	}
	message := err.Error()
	preserveLayout := false
	if detailer, ok := errors.AsType[interface {
		error
		DiagnosticDetail() string
	}](err); ok {
		if detail := strings.TrimSpace(detailer.DiagnosticDetail()); detail != "" {
			message = detail
			preserveLayout = true
		}
	}
	if revisionPath != "" {
		message = strings.ReplaceAll(message, revisionPath, "<plugin>")
	}
	if preserveLayout {
		message = strings.ReplaceAll(message, "\r\n", "\n")
		message = strings.ReplaceAll(message, "\r", "\n")
	}
	message = strings.Map(func(value rune) rune {
		if value == '\r' || value == '\n' || value == '\t' {
			if preserveLayout && value != '\r' {
				return value
			}
			return ' '
		}
		if value < 0x20 || value == 0x7f {
			return -1
		}
		return value
	}, strings.TrimSpace(message))
	if !preserveLayout {
		runes := []rune(message)
		if len(runes) > 2048 {
			message = string(runes[:2048]) + "…"
		}
	}
	if message == "" {
		return "Python plugin execution failed"
	}
	return message
}
