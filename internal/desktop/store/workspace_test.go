package store

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

type stubRequests struct {
	disconnected []string
}

func (s *stubRequests) SendHTTPRequest(context.Context, proxyservice.SendRequestConfig, string, string, []logic.HeaderField, proxyservice.SendRequestBody) (proxyservice.SendRequestResponse, error) {
	return proxyservice.SendRequestResponse{}, nil
}
func (s *stubRequests) DisconnectHTTPRequestStream(id string) error {
	s.disconnected = append(s.disconnected, id)
	return nil
}
func (s *stubRequests) ConnectWebSocket(context.Context, proxyservice.WebSocketConnectRequest) (proxyservice.WebSocketConnectResponse, error) {
	return proxyservice.WebSocketConnectResponse{SessionID: "ws", Status: "connected"}, nil
}
func (s *stubRequests) SendWebSocketMessage(proxyservice.WebSocketSendRequest) error { return nil }
func (s *stubRequests) DisconnectWebSocket(string) error                             { return nil }
func (s *stubRequests) RecoverRequestBodyForEditing(string, []logic.HeaderField, proxyservice.TrafficBodyView) (proxyservice.RequestBodyRecoveryResult, error) {
	return proxyservice.RequestBodyRecoveryResult{}, nil
}

type workspaceHarness struct {
	store   *WorkspaceStore
	router  *EventRouter
	clock   *fakeClock
	backend *stubRequests
	window  *runtimetest.Window
	rt      Runtime
}

func newWorkspaceHarness(t *testing.T) *workspaceHarness {
	t.Helper()
	rt := NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	router := NewEventRouter()
	clock := &fakeClock{}
	backend := &stubRequests{}
	return &workspaceHarness{store: NewWorkspaceStore(rt, backend, nil, router, clock), router: router, clock: clock, backend: backend, window: window, rt: rt}
}

func (h *workspaceHarness) settle() {
	h.rt.Jobs.Wait()
	h.window.Drain()
}

func (h *workspaceHarness) emit(name string, values ...any) {
	events := make([]proxyservice.FrontendEvent, len(values))
	for index, value := range values {
		events[index] = proxyservice.FrontendEvent{Name: name, Data: value}
	}
	h.router.Dispatch(proxyservice.FrontendEventBatch{Events: events})
}

func chunk(session string, offset int64, text string) proxyservice.HTTPRequestStreamEvent {
	return proxyservice.HTTPRequestStreamEvent{SessionID: session, EventType: "chunk", Offset: &offset, ChunkBase64: base64.StdEncoding.EncodeToString([]byte(text))}
}

func terminal(session, kind string, offset int64) proxyservice.HTTPRequestStreamEvent {
	return proxyservice.HTTPRequestStreamEvent{SessionID: session, EventType: kind, Offset: &offset}
}

func (h *workspaceHarness) streamTab(session string) *logic.HTTPEditorState {
	key := h.store.CreateHTTPRequestTab(logic.DraftNew, nil)
	state := h.store.FindTab(key).HTTP
	state.Response = &logic.HTTPResponseView{Kind: logic.ResponseKindSuccess}
	if !h.store.RegisterHTTPStream(key, session) {
		panic("not registered")
	}
	return state
}

func TestStreamChunksApplyInOffsetOrder(t *testing.T) {
	h := newWorkspaceHarness(t)
	state := h.streamTab("s")
	h.emit(eventHTTPRequest, chunk("s", 5, "world"), chunk("s", 0, "hello"))
	h.clock.tick(streamFlushInterval)
	if state.Response.Body != "helloworld" {
		t.Fatal(state.Response.Body)
	}
	h.emit(eventHTTPRequest, chunk("s", 12, "!!"), terminal("s", "complete", 14))
	h.clock.tick(streamFlushInterval)
	if state.Response.Body != "helloworld" || !state.IsStreaming {
		t.Fatal("a gap defers later chunks and the terminal event", state.Response.Body)
	}
	h.emit(eventHTTPRequest, chunk("s", 10, "ab"))
	h.clock.tick(streamFlushInterval)
	if state.Response.Body != "helloworldab!!" || state.IsStreaming || state.Response.StreamState != logic.StreamStateCompleted {
		t.Fatalf("%q %v %s", state.Response.Body, state.IsStreaming, state.Response.StreamState)
	}
	h.emit(eventHTTPRequest, chunk("s", 14, "late"))
	h.clock.tick(streamFlushInterval)
	if state.Response.Body != "helloworldab!!" {
		t.Fatal("late events of a finished session are ignored")
	}
}

func TestStreamEventsBeforeRegistrationAreKept(t *testing.T) {
	h := newWorkspaceHarness(t)
	h.emit(eventHTTPRequest, chunk("early", 0, "first"))
	state := h.streamTab("early")
	h.emit(eventHTTPRequest, chunk("early", 5, "second"))
	h.clock.tick(streamFlushInterval)
	if state.Response.Body != "firstsecond" {
		t.Fatal(state.Response.Body)
	}
}

func TestStreamBinaryBodiesStayBase64(t *testing.T) {
	h := newWorkspaceHarness(t)
	key := h.store.CreateHTTPRequestTab(logic.DraftNew, nil)
	state := h.store.FindTab(key).HTTP
	state.Response = &logic.HTTPResponseView{Kind: logic.ResponseKindSuccess, BodyEncoding: "base64"}
	h.store.RegisterHTTPStream(key, "bin")
	h.emit(eventHTTPRequest, chunk("bin", 0, "\xff\x00"), chunk("bin", 2, "\xfe"))
	h.clock.tick(streamFlushInterval)
	if state.Response.Body != base64.StdEncoding.EncodeToString([]byte("\xff\x00\xfe")) {
		t.Fatal(state.Response.Body)
	}
}

func TestStreamCharacterSplitAcrossChunks(t *testing.T) {
	h := newWorkspaceHarness(t)
	state := h.streamTab("utf8")
	h.emit(eventHTTPRequest, chunk("utf8", 0, "é"[:1]))
	h.clock.tick(streamFlushInterval)
	h.emit(eventHTTPRequest, chunk("utf8", 1, "é"[1:]))
	h.clock.tick(streamFlushInterval)
	if state.Response.Body != "é" {
		t.Fatalf("%q", state.Response.Body)
	}
}

func TestStopDisconnectsAndIgnoresLaterEvents(t *testing.T) {
	h := newWorkspaceHarness(t)
	state := h.streamTab("stop")
	key := h.store.Tabs[1].Key
	h.emit(eventHTTPRequest, chunk("stop", 0, "kept"))
	h.store.StopTab(key)
	h.settle()
	if state.Response.Body != "kept" || state.IsStreaming || state.Response.StreamState != logic.StreamStateStopped {
		t.Fatalf("%+v", state.Response)
	}
	if len(h.backend.disconnected) != 1 || h.backend.disconnected[0] != "stop" {
		t.Fatalf("%v", h.backend.disconnected)
	}
	h.emit(eventHTTPRequest, chunk("stop", 4, "more"))
	h.clock.tick(streamFlushInterval)
	if state.Response.Body != "kept" {
		t.Fatal(state.Response.Body)
	}
}

func TestClosingATabEndsItsStream(t *testing.T) {
	h := newWorkspaceHarness(t)
	h.streamTab("gone")
	key := h.store.Tabs[1].Key
	h.store.CloseTab(key)
	h.settle()
	if len(h.backend.disconnected) != 1 {
		t.Fatalf("%v", h.backend.disconnected)
	}
	if h.store.ActiveKey != logic.CaptureTabKey {
		t.Fatal(h.store.ActiveKey)
	}
}

func TestWebSocketEventsBeforeConnectAreDrained(t *testing.T) {
	h := newWorkspaceHarness(t)
	key := h.store.CreateWebSocketClientTab(logic.DraftNew, nil)
	state := h.store.FindTab(key).WebSocket
	state.URL = "ws://x/"
	h.emit(eventWebSocketSession, proxyservice.WebSocketSessionEvent{SessionID: "ws", EventType: "message", Message: &proxyservice.WebSocketMessage{Data: "early"}})
	h.store.ConnectWebSocket(key, nil)
	h.settle()
	if state.ConnectionStatus != "connected" || len(state.Messages) != 1 || state.Messages[0].Data != "early" {
		t.Fatalf("%+v", state)
	}
	h.emit(eventWebSocketSession, proxyservice.WebSocketSessionEvent{SessionID: "ws", EventType: "error", Status: "error", Error: "boom"})
	if state.ConnectionStatus != "error" || state.ResponseError != "boom" || state.SessionID != "" {
		t.Fatalf("%+v", state)
	}
	h.emit(eventWebSocketSession, proxyservice.WebSocketSessionEvent{SessionID: "ws", EventType: "message", Message: &proxyservice.WebSocketMessage{Data: "late"}})
	if len(state.Messages) != 1 {
		t.Fatal("no messages after the end")
	}
}

func TestPluginLogsReachTheirExecutionOnly(t *testing.T) {
	h := newWorkspaceHarness(t)
	one := h.store.FindTab(h.store.CreateHTTPRequestTab(logic.DraftNew, nil)).HTTP
	two := h.store.FindTab(h.store.CreateHTTPRequestTab(logic.DraftNew, nil)).HTTP
	one.ScriptExecutionID, two.ScriptExecutionID = "a", "b"
	for index := range logic.MaxRequestConsoleEntries + 5 {
		h.store.queuePluginLog(logic.PluginLogEntry{EventID: uint64(index), ExecutionID: "a"})
	}
	h.store.queuePluginLog(logic.PluginLogEntry{EventID: 9999, ExecutionID: "stale"})
	h.clock.tick(pluginLogFlushInterval)
	if len(one.ScriptConsoleEntries) != logic.MaxRequestConsoleEntries || one.ScriptConsoleEntries[0].EventID != 5 || len(two.ScriptConsoleEntries) != 0 {
		t.Fatalf("%d %d", len(one.ScriptConsoleEntries), len(two.ScriptConsoleEntries))
	}
}

func TestReorderKeepsCaptureFirst(t *testing.T) {
	h := newWorkspaceHarness(t)
	a := h.store.CreateHTTPRequestTab(logic.DraftNew, nil)
	b := h.store.CreateWebSocketClientTab(logic.DraftNew, nil)
	h.store.Reorder([]string{b, logic.CaptureTabKey, a})
	if h.store.Tabs[0].Key != logic.CaptureTabKey || h.store.Tabs[1].Key != b || h.store.Tabs[2].Key != a {
		t.Fatalf("%v %v %v", h.store.Tabs[0].Key, h.store.Tabs[1].Key, h.store.Tabs[2].Key)
	}
}

func TestHistoryTabIsSingleAndPruned(t *testing.T) {
	h := newWorkspaceHarness(t)
	h.store.OpenHistoryTab("one", "One")
	h.store.OpenHistoryTab("two", "Two")
	if len(h.store.Tabs) != 2 || h.store.ActiveTab().HistoryKey != "two" || h.store.ActiveTab().Title != "Two" {
		t.Fatalf("%+v", h.store.ActiveTab())
	}
	h.store.PruneHistoryTab(func(string) bool { return true })
	if len(h.store.Tabs) != 2 {
		t.Fatal("kept")
	}
	h.store.PruneHistoryTab(func(string) bool { return false })
	if len(h.store.Tabs) != 1 || h.store.ActiveKey != logic.CaptureTabKey {
		t.Fatal("pruned")
	}
}
