package editor

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"

	"github.com/josexy/flowlens/backend/host"
	"github.com/josexy/flowlens/backend/pkg/database"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/websocket"

	"path/filepath"
)

func newServiceFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	db, err := database.OpenAt(filepath.Join(t.TempDir(), "proxy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := settingservice.New(db)
	if err := settings.Load(); err != nil {
		t.Fatal(err)
	}
	bus := host.NewBus()
	service := proxyservice.New(settings)
	service.SetEventBus(bus)
	if err := service.ServiceStartup(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.ServiceShutdown() })
	f := newFixture(t, service, nil)
	f.data.Events.Connect(bus, f.data.Runtime.Dispatcher.Post)
	return f
}

func (f *fixture) pump() {
	f.window.Drain()
	f.tester.Frame()
}

func (f *fixture) waitFor(what string, condition func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		f.pump()
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.t.Fatalf("timed out waiting for %s: %v", what, f.tester.Texts())
}

func TestSendCompletesAgainstLocalServer(t *testing.T) {
	var seen atomic.Pointer[http.Header]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Clone()
		seen.Store(&header)
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc"})
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Reply", "yes")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()
	f := newServiceFixture(t)
	tab := f.newHTTP()
	state := tab.HTTP
	state.URL, state.Method = server.URL+"/path?x=1", "POST"
	state.Headers = []logic.EditableRow{{Key: "X-Custom", Value: "v", Enabled: true}}
	state.RequestBodyType, state.RequestBodyText = logic.BodyTypeJSON, `{"a":1}`
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	f.waitFor("response", func() bool { return state.Response != nil && !state.IsSending })
	if state.Response.Kind != logic.ResponseKindSuccess || state.Response.StatusCode != 200 || state.Response.Body != `{"ok":true}` {
		t.Fatalf("%+v", state.Response)
	}
	if got := seen.Load(); got == nil || got.Get("X-Custom") != "v" {
		t.Fatalf("%v", got)
	}
	if state.Response.ElapsedMicros <= 0 || !f.tester.HasText(formatElapsed(state.Response.ElapsedMicros)) {
		t.Fatalf("timing not shown: %v in %v", state.Response.ElapsedMicros, f.tester.Texts())
	}
	if !f.tester.HasText("X-Reply") || !f.tester.HasText("200") {
		t.Fatalf("%v", f.tester.Texts())
	}
	state.ActiveResponseTab = logic.ResponseTabBody
	f.settle()
	if !f.tester.HasText(`{"ok":true}`) && !strings.Contains(strings.Join(f.tester.Texts(), " "), "ok") {
		t.Fatalf("%v", f.tester.Texts())
	}
	state.ActiveResponseTab = logic.ResponseTabCookies
	f.settle()
	if !f.tester.HasText("sid") {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestStopCancelsAgainstLocalServer(t *testing.T) {
	arrived, released := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-released:
		}
	}))
	defer server.Close()
	defer close(released)
	f := newServiceFixture(t)
	tab := f.newHTTP()
	state := tab.HTTP
	state.URL = server.URL
	f.settle()
	if err := f.tester.Click(locales.WorkspaceHttpRequestSend()); err != nil {
		t.Fatal(err)
	}
	f.tester.Frame()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not arrive")
	}
	f.pump()
	if !state.IsSending || !f.tester.HasText(locales.WorkspaceHttpRequestStop()) {
		t.Fatalf("%v %v", state.IsSending, f.tester.Texts())
	}
	if err := f.tester.Click(locales.WorkspaceHttpRequestStop()); err != nil {
		t.Fatal(err)
	}
	f.waitFor("cancel", func() bool { return !state.IsSending })
	if state.Response == nil || state.Response.Kind != logic.ResponseKindCancelled {
		t.Fatalf("%+v", state.Response)
	}
	f.pump()
	if !f.tester.HasText(locales.WorkspaceHttpRequestRequestCancelledTitle()) {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestStreamingAgainstLocalServer(t *testing.T) {
	finish := make(chan struct{})
	gone := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: one\n\n")
		flusher.Flush()
		select {
		case <-finish:
			fmt.Fprint(w, "data: two\n\n")
		case <-r.Context().Done():
			gone <- struct{}{}
		}
	}))
	defer server.Close()
	f := newServiceFixture(t)

	completed := f.newHTTP().HTTP
	completed.URL = server.URL
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	f.waitFor("stream start", func() bool { return completed.IsStreaming && strings.Contains(completed.Response.Body, "one") })
	if completed.Response.StreamState != logic.StreamStateStreaming || completed.ActiveResponseTab != logic.ResponseTabBody {
		t.Fatalf("%+v", completed.Response)
	}
	close(finish)
	f.waitFor("stream end", func() bool { return !completed.IsStreaming })
	if completed.Response.StreamState != logic.StreamStateCompleted || completed.Response.Body != "data: one\n\ndata: two\n\n" {
		t.Fatalf("%+v", completed.Response)
	}

	finish = make(chan struct{})
	stopped := f.newHTTP().HTTP
	stopped.URL = server.URL
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	f.waitFor("second stream", func() bool { return stopped.IsStreaming && strings.Contains(stopped.Response.Body, "one") })
	if err := f.tester.Click(locales.WorkspaceHttpRequestStop()); err != nil {
		t.Fatal(err)
	}
	f.waitFor("stopped", func() bool { return !stopped.IsStreaming })
	if stopped.Response.StreamState != logic.StreamStateStopped {
		t.Fatalf("%+v", stopped.Response)
	}
	select {
	case <-gone:
	case <-time.After(5 * time.Second):
		t.Fatal("server stream was not ended")
	}
}

func TestTransportErrorIsShownAndCopied(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	f := newServiceFixture(t)
	state := f.newHTTP().HTTP
	state.URL = "http://" + address + "/"
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	f.waitFor("failure", func() bool { return state.Response != nil && !state.IsSending })
	if state.Response.Kind != logic.ResponseKindError || state.ActiveResponseTab != logic.ResponseTabError || state.Response.ErrorMessage == "" {
		t.Fatalf("%+v", state.Response)
	}
	f.pump()
	if !f.tester.HasText(locales.WorkspaceHttpRequestResponseErrorTitle()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.WorkspaceHttpRequestCopyResponseErrorDetails())
	if f.tester.Clipboard() != state.Response.ErrorMessage {
		t.Fatalf("%q", f.tester.Clipboard())
	}
}

func TestRedirectLimitShowsLastResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			w.Header().Set("Location", "/end")
			w.WriteHeader(http.StatusFound)
			fmt.Fprint(w, "moved")
			return
		}
		fmt.Fprint(w, "landed")
	}))
	defer server.Close()
	f := newServiceFixture(t)
	state := f.newHTTP().HTTP
	state.URL = server.URL + "/start"
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	f.waitFor("response", func() bool { return state.Response != nil && !state.IsSending })
	if state.Response.Kind != logic.ResponseKindSuccess || state.Response.StatusCode != http.StatusFound {
		t.Fatalf("%+v", state.Response)
	}
	state.Response, state.Settings.MaxRedirects = nil, 1
	f.settle()
	f.click(locales.WorkspaceHttpRequestSend())
	f.waitFor("followed", func() bool { return state.Response != nil && !state.IsSending })
	if state.Response.StatusCode != http.StatusOK || state.Response.Body != "landed" {
		t.Fatalf("%+v", state.Response)
	}
}

func TestWebSocketAgainstLocalServer(t *testing.T) {
	upgrader := websocket.Upgrader{}
	var header atomic.Pointer[http.Header]
	closeNow := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clone := r.Header.Clone()
		header.Store(&clone)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		go func() {
			<-closeNow
			_ = conn.Close()
		}()
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(kind, append([]byte("echo:"), data...)); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	f := newServiceFixture(t)
	workspace := f.data.Workspace
	key := workspace.CreateWebSocketClientTab(logic.DraftNew, nil)
	state := workspace.FindTab(key).WebSocket
	state.URL = "ws" + strings.TrimPrefix(server.URL, "http") + "/socket"
	state.Headers = []logic.EditableRow{{Key: "X-Token", Value: "abc", Enabled: true}}
	state.ActiveRightTab = socketTabResponse
	f.settle()
	f.click(locales.WorkspaceWebsocketClientConnect())
	f.waitFor("connected", func() bool { return state.ConnectionStatus == socketStatusConnected })
	if got := header.Load(); got == nil || got.Get("X-Token") != "abc" {
		t.Fatalf("%v", got)
	}
	for _, text := range []string{"first", "second"} {
		state.DraftText = text
		f.settle2()
		if err := f.tester.Click(locales.WorkspaceWebsocketClientSendMessage()); err != nil {
			t.Fatal(err)
		}
		want := "echo:" + text
		f.waitFor(want, func() bool {
			for _, message := range state.Messages {
				if message.Data == want {
					return true
				}
			}
			return false
		})
	}
	if len(state.Messages) != 4 || state.Messages[0].Data != "first" || state.Messages[1].Data != "echo:first" || state.Messages[2].Data != "second" || state.Messages[3].Data != "echo:second" {
		t.Fatalf("%+v", state.Messages)
	}
	if !f.tester.HasText("echo:second") {
		t.Fatalf("%v", f.tester.Texts())
	}
	session := state.SessionID
	f.click(locales.WorkspaceWebsocketClientClearMessages())
	if len(state.Messages) != 0 || !f.tester.HasText(locales.WorkspaceWebsocketClientNoMessages()) {
		t.Fatalf("messages after clearing: %+v, %v", state.Messages, f.tester.Texts())
	}
	if state.SessionID != session || state.ConnectionStatus != socketStatusConnected {
		t.Fatal("clearing messages changed the connection")
	}
	if button, ok := f.tester.Access(locales.WorkspaceWebsocketClientClearMessages()); !ok || !button.Disabled {
		t.Fatalf("clear button should be disabled when empty: %+v", button)
	}
	state.DraftText = "after clear"
	f.settle2()
	f.click(locales.WorkspaceWebsocketClientSendMessage())
	f.waitFor("echo after clearing", func() bool { return f.tester.HasText("echo:after clear") })
	if len(state.Messages) != 2 || state.Messages[0].Data != "after clear" || state.Messages[1].Data != "echo:after clear" {
		t.Fatalf("messages after sending again: %+v", state.Messages)
	}
	close(closeNow)
	f.waitFor("closed", func() bool {
		return state.ConnectionStatus == socketStatusClosed || state.ConnectionStatus == socketStatusError
	})
	f.pump()
	if !f.tester.HasText(locales.WorkspaceWebsocketClientConnect()) {
		t.Fatalf("reconnect offered: %v", f.tester.Texts())
	}
	closeNow = make(chan struct{})
	if err := f.tester.Click(locales.WorkspaceWebsocketClientConnect()); err != nil {
		t.Fatal(err)
	}
	f.waitFor("reconnected", func() bool { return state.ConnectionStatus == socketStatusConnected })
	if err := f.tester.Click(locales.WorkspaceWebsocketClientDisconnect()); err != nil {
		t.Fatal(err)
	}
	f.waitFor("disconnected", func() bool { return state.ConnectionStatus == socketStatusClosed })
	workspace.CloseTab(key)
	close(closeNow)
}

// settle2 settles without blocking on a long-running connection job.
func (f *fixture) settle2() {
	for range 3 {
		f.pump()
	}
}
