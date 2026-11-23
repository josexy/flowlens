package store

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/internal/serviceruntime"
	apicollectionservice "github.com/josexy/flowlens/internal/services/api_collection_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

// stubRequests records the sinks the editor binds to each send and connect.
// A gate, when set, holds the call until it is closed, so events can arrive
// before the call returns.
type stubRequests struct {
	mu             sync.Mutex
	disconnected   []string
	wsDisconnected []string
	response       proxyservice.SendRequestResponse
	httpSinks      []proxyservice.HTTPStreamSink
	httpGate       chan struct{}
	wsResponse     proxyservice.WebSocketConnectResponse
	wsSinks        []proxyservice.WebSocketSink
	wsGate         chan struct{}
	sendErr        error
}

func (s *stubRequests) SendHTTPRequest(ctx context.Context, _ proxyservice.SendRequestConfig, _, _ string, _ []logic.HeaderField, _ proxyservice.SendRequestBody, sink proxyservice.HTTPStreamSink) (proxyservice.SendRequestResponse, error) {
	s.mu.Lock()
	s.httpSinks = append(s.httpSinks, sink)
	response, gate := s.response, s.httpGate
	s.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return proxyservice.SendRequestResponse{}, ctx.Err()
		}
	}
	return response, nil
}
func (s *stubRequests) DisconnectHTTPRequestStream(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disconnected = append(s.disconnected, id)
	return nil
}
func (s *stubRequests) ConnectWebSocket(ctx context.Context, _ proxyservice.WebSocketConnectRequest, sink proxyservice.WebSocketSink) (proxyservice.WebSocketConnectResponse, error) {
	s.mu.Lock()
	s.wsSinks = append(s.wsSinks, sink)
	response, gate := s.wsResponse, s.wsGate
	s.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return proxyservice.WebSocketConnectResponse{}, ctx.Err()
		}
	}
	if response.SessionID == "" {
		response = proxyservice.WebSocketConnectResponse{SessionID: "ws", Status: "connected"}
	}
	return response, nil
}
func (s *stubRequests) SendWebSocketMessage(proxyservice.WebSocketSendRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendErr
}
func (s *stubRequests) DisconnectWebSocket(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wsDisconnected = append(s.wsDisconnected, id)
	return nil
}
func (s *stubRequests) RecoverRequestBodyForEditing(string, []logic.HeaderField, proxyservice.TrafficBodyView) (proxyservice.RequestBodyRecoveryResult, error) {
	return proxyservice.RequestBodyRecoveryResult{}, nil
}

func (s *stubRequests) httpSink(t *testing.T, count int) proxyservice.HTTPStreamSink {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		sinks := s.httpSinks
		s.mu.Unlock()
		if len(sinks) >= count {
			return sinks[count-1]
		}
		if time.Now().After(deadline) {
			t.Fatalf("send %d did not start", count)
		}
		time.Sleep(time.Millisecond)
	}
}

func (s *stubRequests) wsSink(t *testing.T, count int) proxyservice.WebSocketSink {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		sinks := s.wsSinks
		s.mu.Unlock()
		if len(sinks) >= count {
			return sinks[count-1]
		}
		if time.Now().After(deadline) {
			t.Fatalf("connect %d did not start", count)
		}
		time.Sleep(time.Millisecond)
	}
}

type workspaceHarness struct {
	t       *testing.T
	store   *WorkspaceStore
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
	clock := &fakeClock{}
	backend := &stubRequests{}
	return &workspaceHarness{t: t, store: NewWorkspaceStore(rt, backend, nil, clock), clock: clock, backend: backend, window: window, rt: rt}
}

func (h *workspaceHarness) settle() {
	h.rt.Jobs.Wait()
	h.window.Drain()
}

// flush applies pending stream events, as the flush timer would.
func (h *workspaceHarness) flush() { h.clock.tick(streamFlushInterval) }

func chunk(session string, offset int64, text string) proxyservice.HTTPRequestStreamEvent {
	return proxyservice.HTTPRequestStreamEvent{SessionID: session, EventType: proxyservice.HTTPStreamChunk, Offset: &offset, Chunk: []byte(text)}
}

func terminal(session string, kind proxyservice.HTTPStreamEventKind, offset int64) proxyservice.HTTPRequestStreamEvent {
	return proxyservice.HTTPRequestStreamEvent{SessionID: session, EventType: kind, Offset: &offset}
}

// deliver hands events to a sink on the main thread, as the event router does.
func deliver(sink proxyservice.HTTPStreamSink, events ...proxyservice.HTTPRequestStreamEvent) {
	for _, event := range events {
		sink(event)
	}
}

func deliverWS(sink proxyservice.WebSocketSink, events ...proxyservice.WebSocketSessionEvent) {
	for _, event := range events {
		sink(event)
	}
}

// startStream sends from a tab and returns its state and the sink of the
// streaming response session.
func (h *workspaceHarness) startStream(key, session, body string, binary bool) (*logic.HTTPEditorState, proxyservice.HTTPStreamSink) {
	h.t.Helper()
	state := h.store.FindTab(key).HTTP
	state.URL = "https://example.test/events"
	h.backend.mu.Lock()
	h.backend.response = proxyservice.SendRequestResponse{StatusCode: 200, Streaming: true, StreamSessionID: session, Body: body, BodyBinary: binary}
	count := len(h.backend.httpSinks) + 1
	h.backend.mu.Unlock()
	if problem := h.store.SendTab(key, false); problem != "" {
		h.t.Fatal(problem)
	}
	h.settle()
	if !state.IsStreaming || state.StreamSessionID != session {
		h.t.Fatalf("send did not start stream %s: %+v", session, state.Response)
	}
	return state, h.backend.httpSink(h.t, count)
}

func (h *workspaceHarness) streamTab(session string) (*logic.HTTPEditorState, proxyservice.HTTPStreamSink) {
	h.t.Helper()
	return h.startStream(h.store.CreateHTTPRequestTab(logic.DraftNew, nil), session, "", false)
}

func (h *workspaceHarness) disconnected() []string {
	h.backend.mu.Lock()
	defer h.backend.mu.Unlock()
	return append([]string(nil), h.backend.disconnected...)
}

func TestStreamChunksApplyInOffsetOrder(t *testing.T) {
	h := newWorkspaceHarness(t)
	state, sink := h.streamTab("s")
	deliver(sink, chunk("s", 5, "world"), chunk("s", 0, "hello"))
	h.flush()
	if state.Response.Body != "helloworld" {
		t.Fatal(state.Response.Body)
	}
	deliver(sink, chunk("s", 12, "!!"), terminal("s", proxyservice.HTTPStreamComplete, 14))
	h.flush()
	if state.Response.Body != "helloworld" || !state.IsStreaming {
		t.Fatal("a gap defers later chunks and the terminal event", state.Response.Body)
	}
	deliver(sink, chunk("s", 10, "ab"))
	h.flush()
	if state.Response.Body != "helloworldab!!" || state.IsStreaming || state.Response.StreamState != logic.StreamStateCompleted {
		t.Fatalf("%q %v %s", state.Response.Body, state.IsStreaming, state.Response.StreamState)
	}
	deliver(sink, chunk("s", 14, "late"))
	h.flush()
	if state.Response.Body != "helloworldab!!" {
		t.Fatal("late events of a finished session are ignored")
	}
}

func TestStreamDataBeforeTheSendReturnsIsKept(t *testing.T) {
	h := newWorkspaceHarness(t)
	key := h.store.CreateHTTPRequestTab(logic.DraftNew, nil)
	state := h.store.FindTab(key).HTTP
	state.URL = "https://example.test/events"
	gate := make(chan struct{})
	h.backend.httpGate = gate
	h.backend.response = proxyservice.SendRequestResponse{StatusCode: 200, Streaming: true, StreamSessionID: "early"}
	if problem := h.store.SendTab(key, false); problem != "" {
		t.Fatal(problem)
	}
	sink := h.backend.httpSink(t, 1)
	deliver(sink, chunk("early", 0, "first"), chunk("early", 5, "sec"))
	h.flush()
	if state.Response != nil {
		t.Fatal("events applied before the send returned")
	}
	close(gate)
	h.settle()
	deliver(sink, chunk("early", 8, "ond"))
	h.flush()
	if state.Response.Body != "firstsecond" || !state.IsStreaming {
		t.Fatalf("%q streaming=%t", state.Response.Body, state.IsStreaming)
	}
}

func TestConcurrentStreamsInTwoTabsStayApart(t *testing.T) {
	h := newWorkspaceHarness(t)
	one, oneSink := h.streamTab("one")
	two, twoSink := h.streamTab("two")
	deliver(oneSink, chunk("one", 0, "alpha"))
	deliver(twoSink, chunk("two", 0, "beta"))
	deliver(oneSink, terminal("one", proxyservice.HTTPStreamComplete, 5))
	h.flush()
	if one.Response.Body != "alpha" || one.IsStreaming || one.Response.StreamState != logic.StreamStateCompleted {
		t.Fatalf("first tab = %q streaming=%t", one.Response.Body, one.IsStreaming)
	}
	if two.Response.Body != "beta" || !two.IsStreaming {
		t.Fatalf("second tab = %q streaming=%t, want only its own body, still streaming", two.Response.Body, two.IsStreaming)
	}
	deliver(twoSink, chunk("two", 4, "!"), terminal("two", proxyservice.HTTPStreamComplete, 5))
	h.flush()
	if two.Response.Body != "beta!" || two.IsStreaming || one.Response.Body != "alpha" {
		t.Fatalf("tabs = %q and %q", one.Response.Body, two.Response.Body)
	}
}

func TestStreamBinaryBodiesGrowByOffset(t *testing.T) {
	h := newWorkspaceHarness(t)
	key := h.store.CreateHTTPRequestTab(logic.DraftNew, nil)
	// The body received before streaming started counts toward the offset.
	state, sink := h.startStream(key, "bin", "\x80", true)
	// The third chunk arrives first, then the first; the middle one is late.
	deliver(sink, chunk("bin", 4, "\xfe\xfd"), chunk("bin", 1, "\xff\x00"))
	h.flush()
	if state.Response.Body != "\x80\xff\x00" || !state.Response.BodyBinary {
		t.Fatalf("after first flush %q", state.Response.Body)
	}
	deliver(sink, chunk("bin", 3, "\xc3"))
	h.flush()
	if state.Response.Body != "\x80\xff\x00\xc3\xfe\xfd" || !state.Response.BodyBinary {
		t.Fatalf("after second flush %q", state.Response.Body)
	}
	// An overlapping chunk adds only its new bytes, a duplicate nothing.
	deliver(sink, chunk("bin", 5, "\xfd\x01"), chunk("bin", 5, "\xfd\x01"))
	h.flush()
	if state.Response.Body != "\x80\xff\x00\xc3\xfe\xfd\x01" {
		t.Fatalf("after overlap %q", state.Response.Body)
	}
}

func TestStreamCharacterSplitAcrossChunks(t *testing.T) {
	h := newWorkspaceHarness(t)
	state, sink := h.streamTab("utf8")
	deliver(sink, chunk("utf8", 0, "é"[:1]))
	h.flush()
	if state.Response.Body != "" {
		t.Fatalf("half a character was shown: %q", state.Response.Body)
	}
	deliver(sink, chunk("utf8", 1, "é"[1:]+"中"[:2]))
	h.flush()
	deliver(sink, chunk("utf8", 4, "中"[2:]))
	h.flush()
	if state.Response.Body != "é中" {
		t.Fatalf("%q", state.Response.Body)
	}
}

func TestStopKeepsWhatArrivedAndDiscardsLaterEvents(t *testing.T) {
	h := newWorkspaceHarness(t)
	state, sink := h.streamTab("stop")
	key := h.store.Tabs[1].Key
	deliver(sink, chunk("stop", 0, "kept"))
	h.store.StopTab(key)
	h.settle()
	if state.Response.Body != "kept" || state.IsStreaming || state.Response.StreamState != logic.StreamStateStopped {
		t.Fatalf("%+v", state.Response)
	}
	if got := h.disconnected(); len(got) != 1 || got[0] != "stop" {
		t.Fatalf("%v", got)
	}
	deliver(sink, chunk("stop", 4, "more"), terminal("stop", proxyservice.HTTPStreamClosed, 8))
	h.flush()
	if state.Response.Body != "kept" || state.Response.StreamState != logic.StreamStateStopped {
		t.Fatal(state.Response.Body, state.Response.StreamState)
	}
}

func TestSendingAgainAfterStopShowsOnlyTheNewResponse(t *testing.T) {
	h := newWorkspaceHarness(t)
	key := h.store.CreateHTTPRequestTab(logic.DraftNew, nil)
	_, oldSink := h.startStream(key, "old", "", false)
	deliver(oldSink, chunk("old", 0, "old-1"))
	h.store.StopTab(key)
	h.settle()
	state, newSink := h.startStream(key, "new", "", false)
	// The stopped stream's remaining events arrive after the new send.
	deliver(oldSink, chunk("old", 5, "old-2"), terminal("old", proxyservice.HTTPStreamClosed, 10))
	deliver(newSink, chunk("new", 0, "fresh"))
	h.flush()
	if state.Response.Body != "fresh" || !state.IsStreaming || state.StreamSessionID != "new" {
		t.Fatalf("body = %q streaming=%t session=%q", state.Response.Body, state.IsStreaming, state.StreamSessionID)
	}
}

func TestClosingATabEndsItsStream(t *testing.T) {
	h := newWorkspaceHarness(t)
	other, otherSink := h.streamTab("other")
	_, sink := h.streamTab("gone")
	otherKey, key := h.store.Tabs[1].Key, h.store.Tabs[2].Key
	h.store.Activate(key)
	h.store.CloseTab(key)
	h.settle()
	if got := h.disconnected(); len(got) != 1 || got[0] != "gone" {
		t.Fatalf("%v", got)
	}
	if h.store.ActiveKey != otherKey {
		t.Fatal(h.store.ActiveKey)
	}
	deliver(sink, chunk("gone", 0, "late"), terminal("gone", proxyservice.HTTPStreamClosed, 4))
	deliver(otherSink, chunk("other", 0, "own"))
	h.flush()
	if other.Response.Body != "own" || !other.IsStreaming || len(h.store.Tabs) != 2 {
		t.Fatalf("another tab changed: %q streaming=%t", other.Response.Body, other.IsStreaming)
	}
}

func TestClosingATabWhileSendingDisconnectsTheReturnedStream(t *testing.T) {
	h := newWorkspaceHarness(t)
	key := h.store.CreateHTTPRequestTab(logic.DraftNew, nil)
	state := h.store.FindTab(key).HTTP
	state.URL = "https://example.test/events"
	h.backend.response = proxyservice.SendRequestResponse{StatusCode: 200, Streaming: true, StreamSessionID: "orphan"}
	gate := make(chan struct{})
	h.backend.httpGate = gate
	h.store.SendTab(key, false)
	sink := h.backend.httpSink(t, 1)
	// Detach cancellation: the backend still returns the stream it opened.
	h.backend.mu.Lock()
	h.backend.httpGate = nil
	h.backend.mu.Unlock()
	close(gate)
	h.rt.Jobs.Wait()
	h.store.CloseTab(key)
	h.window.Drain()
	h.settle()
	deliver(sink, chunk("orphan", 0, "late"))
	h.flush()
	if got := h.disconnected(); len(got) != 1 || got[0] != "orphan" {
		t.Fatalf("disconnected = %v, want the stream the closed tab started", got)
	}
}

func connectTab(h *workspaceHarness) (string, *logic.WebSocketClientState) {
	key := h.store.CreateWebSocketClientTab(logic.DraftNew, nil)
	state := h.store.FindTab(key).WebSocket
	state.URL = "ws://example.test/"
	return key, state
}

func TestWebSocketMessagesBeforeTheConnectReturnAreListed(t *testing.T) {
	h := newWorkspaceHarness(t)
	key, state := connectTab(h)
	gate := make(chan struct{})
	h.backend.wsGate = gate
	h.store.ConnectWebSocket(key, nil)
	sink := h.backend.wsSink(t, 1)
	deliverWS(sink,
		proxyservice.WebSocketSessionEvent{SessionID: "ws", EventType: proxyservice.WebSocketEventConnected, Status: "connected"},
		proxyservice.WebSocketSessionEvent{SessionID: "ws", EventType: proxyservice.WebSocketEventMessage, Message: &proxyservice.WebSocketMessage{Data: "one"}},
		proxyservice.WebSocketSessionEvent{SessionID: "ws", EventType: proxyservice.WebSocketEventMessage, Message: &proxyservice.WebSocketMessage{Data: "two"}},
	)
	if state.ConnectionStatus != logic.WebSocketConnecting || len(state.Messages) != 0 {
		t.Fatal("events applied before the connect returned")
	}
	close(gate)
	h.settle()
	if state.ConnectionStatus != logic.WebSocketConnected || state.SessionID != "ws" || len(state.Messages) != 2 || state.Messages[0].Data != "one" || state.Messages[1].Data != "two" {
		t.Fatalf("%+v", state)
	}
}

func TestWebSocketClosedByTheServerRefusesSendsAndLateEvents(t *testing.T) {
	h := newWorkspaceHarness(t)
	key, state := connectTab(h)
	h.store.ConnectWebSocket(key, nil)
	h.settle()
	sink := h.backend.wsSink(t, 1)
	deliverWS(sink, proxyservice.WebSocketSessionEvent{SessionID: "ws", EventType: proxyservice.WebSocketEventMessage, Message: &proxyservice.WebSocketMessage{Data: "hello"}})
	deliverWS(sink, proxyservice.WebSocketSessionEvent{SessionID: "ws", EventType: proxyservice.WebSocketEventClosed, Status: "closed"})
	if state.ConnectionStatus != logic.WebSocketClosed || state.SessionID != "" || len(state.Messages) != 1 {
		t.Fatalf("%+v", state)
	}
	var sendErr error
	h.store.SendWebSocketMessage(key, func(err error) { sendErr = err })
	if sendErr == nil {
		t.Fatal("a send after the server closed the session was accepted")
	}
	deliverWS(sink, proxyservice.WebSocketSessionEvent{SessionID: "ws", EventType: proxyservice.WebSocketEventMessage, Message: &proxyservice.WebSocketMessage{Data: "late"}})
	if len(state.Messages) != 1 || state.ConnectionStatus != logic.WebSocketClosed {
		t.Fatal("a late event of the closed session was applied")
	}
}

func TestWebSocketErrorEndsTheSession(t *testing.T) {
	h := newWorkspaceHarness(t)
	key, state := connectTab(h)
	h.store.ConnectWebSocket(key, nil)
	h.settle()
	sink := h.backend.wsSink(t, 1)
	deliverWS(sink, proxyservice.WebSocketSessionEvent{SessionID: "ws", EventType: proxyservice.WebSocketEventError, Status: "error", Error: "boom"})
	if state.ConnectionStatus != logic.WebSocketError || state.ResponseError != "boom" || state.SessionID != "" {
		t.Fatalf("%+v", state)
	}
	deliverWS(sink, proxyservice.WebSocketSessionEvent{SessionID: "ws", EventType: proxyservice.WebSocketEventMessage, Message: &proxyservice.WebSocketMessage{Data: "late"}})
	if len(state.Messages) != 0 {
		t.Fatal("no messages after the end")
	}
}

func TestClosingAWebSocketTabDisconnectsAndDiscardsLateEvents(t *testing.T) {
	h := newWorkspaceHarness(t)
	otherKey, other := connectTab(h)
	h.backend.wsResponse = proxyservice.WebSocketConnectResponse{SessionID: "keep", Status: "connected"}
	h.store.ConnectWebSocket(otherKey, nil)
	h.settle()
	key, state := connectTab(h)
	h.backend.wsResponse = proxyservice.WebSocketConnectResponse{SessionID: "gone", Status: "connected"}
	h.store.ConnectWebSocket(key, nil)
	h.settle()
	sink := h.backend.wsSink(t, 2)
	h.store.CloseTab(key)
	h.settle()
	h.backend.mu.Lock()
	disconnected := append([]string(nil), h.backend.wsDisconnected...)
	h.backend.mu.Unlock()
	if len(disconnected) != 1 || disconnected[0] != "gone" {
		t.Fatalf("disconnected = %v", disconnected)
	}
	deliverWS(sink, proxyservice.WebSocketSessionEvent{SessionID: "gone", EventType: proxyservice.WebSocketEventMessage, Message: &proxyservice.WebSocketMessage{Data: "late"}})
	if len(state.Messages) != 0 || len(other.Messages) != 0 || other.ConnectionStatus != logic.WebSocketConnected || other.SessionID != "keep" {
		t.Fatalf("a late event changed a tab: closed=%+v other=%+v", state.Messages, other)
	}
}

func TestReconnectDiscardsEventsOfThePreviousSession(t *testing.T) {
	h := newWorkspaceHarness(t)
	key, state := connectTab(h)
	h.backend.wsResponse = proxyservice.WebSocketConnectResponse{SessionID: "first", Status: "connected"}
	h.store.ConnectWebSocket(key, nil)
	h.settle()
	first := h.backend.wsSink(t, 1)
	h.store.DisconnectWebSocket(key, nil)
	h.settle()
	h.backend.wsResponse = proxyservice.WebSocketConnectResponse{SessionID: "second", Status: "connected"}
	h.store.ConnectWebSocket(key, nil)
	h.settle()
	deliverWS(first, proxyservice.WebSocketSessionEvent{SessionID: "first", EventType: proxyservice.WebSocketEventClosed, Status: "closed"})
	if state.ConnectionStatus != logic.WebSocketConnected || state.SessionID != "second" {
		t.Fatalf("the previous session's end closed the new one: %+v", state)
	}
}

// logTab returns a request tab running execution.
func (h *workspaceHarness) logTab(execution string) *logic.HTTPEditorState {
	state := h.store.FindTab(h.store.CreateHTTPRequestTab(logic.DraftNew, nil)).HTTP
	state.ScriptExecutionID = execution
	return state
}

// flushLogs lets the console coalescer post its batch and applies it.
func (h *workspaceHarness) flushLogs() {
	time.Sleep(runtime.CoalesceInterval)
	synctest.Wait()
	h.window.Drain()
}

func requireConsole(t *testing.T, entries []logic.PluginLogEntry, first, count int) {
	t.Helper()
	if len(entries) != count {
		t.Fatalf("console holds %d entries, want %d", len(entries), count)
	}
	for index, entry := range entries {
		if entry.EventID != uint64(first+index) {
			t.Fatalf("console entry %d = %d, want %d in order", index, entry.EventID, first+index)
		}
	}
}

func TestPluginLogsReachTheirExecutionOnly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newWorkspaceHarness(t)
		var logs serviceruntime.Topic[logic.PluginLogEntry]
		defer h.store.ConnectPluginLogs(&logs)()
		one, two := h.logTab("a"), h.logTab("b")
		for index := range logic.MaxRequestConsoleEntries + 5 {
			logs.Publish(logic.PluginLogEntry{EventID: uint64(index), ExecutionID: "a"})
		}
		logs.Publish(logic.PluginLogEntry{EventID: 9999, ExecutionID: "stale"})
		h.flushLogs()
		requireConsole(t, one.ScriptConsoleEntries, 5, logic.MaxRequestConsoleEntries)
		if len(two.ScriptConsoleEntries) != 0 {
			t.Fatalf("another execution's console has %d entries", len(two.ScriptConsoleEntries))
		}
	})
}

func TestPluginOutputBurstArrivesInOrderInBatches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newWorkspaceHarness(t)
		var logs serviceruntime.Topic[logic.PluginLogEntry]
		defer h.store.ConnectPluginLogs(&logs)()
		state := h.logTab("burst")
		const lines, burst = 5000, 500
		for start := 0; start < lines; start += burst {
			for id := start; id < start+burst; id++ {
				logs.Publish(logic.PluginLogEntry{EventID: uint64(id), ExecutionID: "burst"})
			}
			h.flushLogs()
			shown := min(start+burst, logic.MaxRequestConsoleEntries)
			requireConsole(t, state.ScriptConsoleEntries, start+burst-shown, shown)
		}
		if updates := h.window.Updated(); updates != lines/burst {
			t.Fatalf("window updates = %d, want one per batch, not per line", updates)
		}
	})
}

func TestPluginOutputDuringAStallKeepsTheNewestLines(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newWorkspaceHarness(t)
		var logs serviceruntime.Topic[logic.PluginLogEntry]
		defer h.store.ConnectPluginLogs(&logs)()
		state := h.logTab("stall")
		other := h.logTab("other")
		logs.Publish(logic.PluginLogEntry{EventID: 0, ExecutionID: "stall"})
		time.Sleep(runtime.CoalesceInterval)
		synctest.Wait()
		// The main thread has not applied the first batch while 5,000 lines arrive.
		for id := 1; id <= 5000; id++ {
			logs.Publish(logic.PluginLogEntry{EventID: uint64(id), ExecutionID: "stall"})
		}
		logs.Publish(logic.PluginLogEntry{EventID: 1, ExecutionID: "other"})
		h.window.Drain()
		h.flushLogs()
		requireConsole(t, state.ScriptConsoleEntries, 5001-logic.MaxRequestConsoleEntries, logic.MaxRequestConsoleEntries)
		requireConsole(t, other.ScriptConsoleEntries, 1, 1)
	})
}

func TestPluginOutputForAnotherSendIsDiscarded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newWorkspaceHarness(t)
		var logs serviceruntime.Topic[logic.PluginLogEntry]
		defer h.store.ConnectPluginLogs(&logs)()
		state := h.logTab("current")
		state.ScriptConsoleEntries = []logic.PluginLogEntry{{EventID: 1, ExecutionID: "current"}}
		logs.Publish(logic.PluginLogEntry{EventID: 2, ExecutionID: "finished"})
		h.flushLogs()
		requireConsole(t, state.ScriptConsoleEntries, 1, 1)
	})
}

func TestPluginOutputPendingAtShutdownIsDropped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newWorkspaceHarness(t)
		var logs serviceruntime.Topic[logic.PluginLogEntry]
		off := h.store.ConnectPluginLogs(&logs)
		state := h.logTab("a")
		logs.Publish(logic.PluginLogEntry{EventID: 1, ExecutionID: "a"})
		off()
		h.rt.Close()
		logs.Publish(logic.PluginLogEntry{EventID: 2, ExecutionID: "a"})
		h.flushLogs()
		if len(state.ScriptConsoleEntries) != 0 || h.window.Updated() != 0 {
			t.Fatalf("console = %v after shutdown, window updates = %d", state.ScriptConsoleEntries, h.window.Updated())
		}
	})
}

func TestClearingTheConsoleDropsUnshownOutput(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newWorkspaceHarness(t)
		var logs serviceruntime.Topic[logic.PluginLogEntry]
		defer h.store.ConnectPluginLogs(&logs)()
		state := h.logTab("a")
		logs.Publish(logic.PluginLogEntry{EventID: 1, ExecutionID: "a"})
		h.store.ClearScriptConsole(h.store.Tabs[1].Key)
		logs.Publish(logic.PluginLogEntry{EventID: 2, ExecutionID: "a"})
		h.flushLogs()
		requireConsole(t, state.ScriptConsoleEntries, 2, 1)
	})
}

func TestSavedTabIsDirtyOnlyWhileItDiffersFromTheSave(t *testing.T) {
	h := newWorkspaceHarness(t)
	key := h.store.CreateHTTPRequestTab(logic.DraftNew, nil)
	tab := h.store.FindTab(key)
	state := tab.HTTP
	state.URL = "https://example.test/"
	state.Headers = []logic.EditableRow{{Key: "Accept", Value: "*/*", Enabled: true}}
	state.RequestBodyType, state.RequestBodyFile = logic.BodyTypeFile, &logic.RequestFileValue{Path: "/data/body.bin"}
	logic.ApplySavedMetadata(tab, &apicollectionservice.APICollectionRequest{ID: "saved", UpdatedAt: 1})
	if h.store.IsDirty(key) {
		t.Fatal("a tab just saved is dirty")
	}
	state.URL = "https://example.test/edited"
	if !h.store.IsDirty(key) {
		t.Fatal("an edit was not marked unsaved")
	}
	state.URL = "https://example.test/"
	if h.store.IsDirty(key) {
		t.Fatal("reverting the edit kept the unsaved marker")
	}
	state.Headers[0].Value = "text/plain"
	if !h.store.IsDirty(key) {
		t.Fatal("changing a row after the save was not marked unsaved")
	}
	state.Headers[0].Value = "*/*"
	state.RequestBodyFile.Path = "/data/other.bin"
	if !h.store.IsDirty(key) {
		t.Fatal("changing a file after the save was not marked unsaved")
	}
	state.RequestBodyFile.Path = "/data/body.bin"
	state.Params = append(state.Params, logic.EditableRow{Key: " ", Enabled: true}, logic.EditableRow{Key: "off", Enabled: false})
	if h.store.IsDirty(key) {
		t.Fatal("whitespace-only or disabled rows marked the tab unsaved")
	}
}

func TestNeverSavedDraftIsDirtyWhenItHoldsSomething(t *testing.T) {
	h := newWorkspaceHarness(t)
	key := h.store.CreateHTTPRequestTab(logic.DraftNew, nil)
	tab := h.store.FindTab(key)
	for _, step := range []struct {
		name string
		edit func()
	}{
		{"empty", func() {}},
		{"whitespace header", func() { tab.HTTP.Headers = []logic.EditableRow{{Key: "  ", Enabled: true}} }},
		{"url", func() { tab.HTTP.URL = "https://example.test/" }},
	} {
		step.edit()
		if got, want := h.store.IsDirty(key), logic.HasMeaningfulRequestDraft(tab); got != want {
			t.Fatalf("%s: dirty = %t, want %t", step.name, got, want)
		}
	}
	if !h.store.IsDirty(key) {
		t.Fatal("a draft with a URL is not dirty")
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
