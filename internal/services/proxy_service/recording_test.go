package proxyservice

import (
	"crypto/tls"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rewriteservice "github.com/josexy/flowlens/internal/services/rewrite_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/mitmproxy-go/v2"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
	"github.com/josexy/xhttp/httptrace"
)

func startRecordingTestProxy(t *testing.T, svc *ProxyService) *http.Client {
	t.Helper()
	dir := t.TempDir()
	cfg := &settingservice.ProxyConfig{
		Mode: settingservice.ProxyModeHTTP, Host: "127.0.0.1", Port: reserveTestTCPPort(t),
		CACertPath: filepath.Join(dir, "ca.crt"), CAKeyPath: filepath.Join(dir, "ca.key"),
		DisableProxy: true, SkipVerifyTLS: true, AntiCache: true, AntiComp: true,
	}
	if err := svc.settingService.Update(&settingservice.Settings{ProxyConfig: cfg}); err != nil {
		t.Fatal(err)
	}
	writeCustomTestCA(t, svc.settingService)
	status, err := svc.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = svc.Stop(); svc.baseCancel() })
	proxyURL, _ := url.Parse("http://" + status.Address)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableCompression: true, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

func setRecordingPause(t *testing.T, svc *ProxyService, paused bool) ProxyStatus {
	t.Helper()
	status, err := svc.SetRecordingPaused(paused)
	if err != nil || !status.Running || status.RecordingPaused != paused {
		t.Fatalf("set pause %t: %+v, %v", paused, status, err)
	}
	return status
}

func TestRecordingPauseForwardsHTTPAndHTTPS(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := "HTTP"
		if secure {
			name = "HTTPS"
		}
		t.Run(name, func(t *testing.T) {
			setTestConfigDir(t)
			origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				data, err := io.ReadAll(req.Body)
				if err != nil || req.Header.Get("Accept-Encoding") != "identity" || req.Header.Get("If-None-Match") != "" {
					t.Errorf("forwarded options/body: headers=%v error=%v", req.Header, err)
				}
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write(append([]byte("echo:"), data...))
			}))
			if secure {
				origin.EnableHTTP2 = true
				origin.StartTLS()
			} else {
				origin.Start()
			}
			t.Cleanup(origin.Close)
			svc := newTestProxyService(t, nil)
			client := startRecordingTestProxy(t, svc)
			before := svc.GetStatus()
			server := svc.server
			for i, paused := range []bool{false, true, false} {
				if i > 0 {
					setRecordingPause(t, svc, paused)
				}
				req, _ := http.NewRequest(http.MethodPost, origin.URL, strings.NewReader("payload"))
				req.Header.Set("Accept-Encoding", "gzip")
				req.Header.Set("If-None-Match", "cached")
				var reused atomic.Bool
				req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused.Store(info.Reused) }}))
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil || string(data) != "echo:payload" {
					t.Fatalf("forwarded body %q: %v", data, err)
				}
				if i > 0 && !reused.Load() {
					t.Fatal("pause/resume interrupted the downstream keep-alive connection")
				}
				want := 1
				if i == 2 {
					want = 2
				}
				entries := svc.GetTraffic()
				if len(entries) != want || entries[want-1].ID != uint64(want) {
					t.Fatalf("pause=%t entries=%+v", paused, entries)
				}
				if paused {
					if _, exists := svc.trafficBodies.Load(uint64(2)); exists {
						t.Fatal("paused traffic allocated body storage")
					}
					if svc.GetStatistics().Total != 1 {
						t.Fatal("paused traffic changed the total")
					}
				}
			}
			after := svc.GetStatus()
			if svc.server != server || after.Address != before.Address || !after.Started.Equal(before.Started) {
				t.Fatal("pause changed the listener lifecycle")
			}
		})
	}
}

func TestRecordingPauseKeepsInFlightBodies(t *testing.T) {
	for _, contentType := range []string{"text/plain", "text/event-stream"} {
		t.Run(contentType, func(t *testing.T) {
			setTestConfigDir(t)
			svc := newTestProxyService(t, nil)
			svc.running = true
			intercept := func() *http.Response {
				req, _ := http.NewRequest(http.MethodGet, "http://stream.test/", nil)
				resp, err := svc.httpInterceptor(nil)(newRawTCPMetadataContext(), req, mitmproxy.HTTPDelegatedInvokerFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Proto: "HTTP/1.1", Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader("data: complete\n\n")), ContentLength: 16}, nil
				}))
				if err != nil {
					t.Fatal(err)
				}
				return resp
			}
			admitted := intercept()
			setRecordingPause(t, svc, true)
			skipped := intercept()
			body, err := io.ReadAll(admitted.Body)
			_ = admitted.Body.Close()
			if err != nil || string(body) != "data: complete\n\n" {
				t.Fatalf("admitted body %q: %v", body, err)
			}
			view, err := svc.GetTrafficBodyView(1)
			if err != nil || view.ResponseBody != string(body) {
				t.Fatalf("in-flight capture %+v: %v", view, err)
			}
			setRecordingPause(t, svc, false)
			body, err = io.ReadAll(skipped.Body)
			_ = skipped.Body.Close()
			if err != nil || string(body) != "data: complete\n\n" {
				t.Fatalf("skipped body %q: %v", body, err)
			}
			if len(svc.GetTraffic()) != 1 {
				t.Fatal("resuming retroactively captured an in-flight skipped response")
			}
		})
	}
}

type recordingFrame struct {
	*testCaptureFrame
	forwarded bool
	released  bool
}

func (f *recordingFrame) Invoke() error { f.forwarded = true; return f.testCaptureFrame.Invoke() }
func (f *recordingFrame) Release()      { f.released = true; f.testCaptureFrame.Release() }

type recordingFrameWatcher struct {
	frames chan mitmproxy.WsFrame
	ready  chan struct{}
	once   sync.Once
}

func (w *recordingFrameWatcher) Receive() <-chan mitmproxy.WsFrame {
	w.once.Do(func() { close(w.ready) })
	return w.frames
}

func TestRecordingPauseWebSocketAdmission(t *testing.T) {
	for _, initiallyPaused := range []bool{false, true} {
		name := "admitted before pause"
		if initiallyPaused {
			name = "skipped before resume"
		}
		t.Run(name, func(t *testing.T) {
			setTestConfigDir(t)
			svc := newTestProxyService(t, nil)
			svc.running = true
			setRecordingPause(t, svc, initiallyPaused)
			watcher := &recordingFrameWatcher{frames: make(chan mitmproxy.WsFrame, 2), ready: make(chan struct{})}
			req, _ := http.NewRequest(http.MethodGet, "ws://socket.test/", nil)
			done := make(chan struct{})
			go func() {
				defer close(done)
				svc.websocketInterceptor(nil)(newRawTCPMetadataContext(), req, &http.Response{StatusCode: 101, Proto: "HTTP/1.1"}, watcher)
			}()
			waitForSignal(t, watcher.ready, "WebSocket admission")
			setRecordingPause(t, svc, !initiallyPaused)
			frame := &recordingFrame{testCaptureFrame: newTestCaptureFrame(mitmproxy.WSDirection(0), 1, []byte("still forwarded"))}
			watcher.frames <- frame
			close(watcher.frames)
			waitForSignal(t, done, "WebSocket forwarding")
			if !frame.forwarded || !frame.released {
				t.Fatal("unrecorded WebSocket frames must still be forwarded and released")
			}
			if initiallyPaused {
				if len(svc.GetTraffic()) != 0 {
					t.Fatal("skipped connection began recording after resume")
				}
				if _, ok := svc.trafficWsMsgs.Load(uint64(1)); ok {
					t.Fatal("skipped connection allocated message storage")
				}
			} else {
				view, err := svc.GetTrafficBodyView(1)
				if err != nil || len(view.WebSocketMessages) != 1 {
					t.Fatalf("existing connection stopped recording: %+v %v", view, err)
				}
			}
		})
	}
}

func TestRecordingPausePreservesRewrite(t *testing.T) {
	setTestConfigDir(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, err := io.ReadAll(req.Body)
		if err != nil || string(data) != "rewritten request" {
			t.Errorf("request %q: %v", data, err)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "original response")
	}))
	t.Cleanup(origin.Close)
	request := rewriteTestRule(rewriteservice.ActionRequest)
	request.URLPattern = origin.URL + "/*"
	request.Action.Body = rewriteservice.BodyAction{Mode: "replace", Text: "rewritten request"}
	response := rewriteTestRule(rewriteservice.ActionResponse)
	response.URLPattern = request.URLPattern
	response.Action.Body = rewriteservice.BodyAction{Mode: "replace", Text: "rewritten response"}
	svc := newRewriteTestService(t, request, response)
	client := startRecordingTestProxy(t, svc)
	setRecordingPause(t, svc, true)
	resp, err := client.Post(origin.URL+"/rewrite", "text/plain", strings.NewReader("original request"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || string(data) != "rewritten response" {
		t.Fatalf("response %q: %v", data, err)
	}
	if len(svc.GetTraffic()) != 0 {
		t.Fatal("rewrite created a record while paused")
	}
}

func TestRecordingPauseLifecycle(t *testing.T) {
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	if status, err := svc.SetRecordingPaused(true); err == nil || status.Running || status.RecordingPaused {
		t.Fatalf("stopped pause: %+v %v", status, err)
	}
	startRecordingTestProxy(t, svc)
	status := setRecordingPause(t, svc, true)
	svc.lifecycleOperationHook = func(operation string) {
		if operation == "start-published" {
			if !svc.GetStatus().RecordingPaused {
				t.Error("restart published an unpaused listener")
			}
			if svc.registerTrafficEntry(newRawTCPMetadataContext(), TrafficEntry{Type: "tcp"}) != nil {
				t.Error("restart temporarily admitted traffic")
			}
		}
	}
	restarted, err := svc.Restart()
	svc.lifecycleOperationHook = nil
	if err != nil || !restarted.RecordingPaused || restarted.Revision <= status.Revision {
		t.Fatalf("restart: %+v %v", restarted, err)
	}
	if err := svc.ClearTraffic(); err != nil {
		t.Fatal(err)
	}
	if err := svc.RestartCapture(false); err != nil {
		t.Fatal(err)
	}
	if !svc.GetStatus().RecordingPaused {
		t.Fatal("capture reset resumed recording")
	}
	svc.rawTCPInterceptor()(newRawTCPMetadataContext(), mitmproxy.RawTCPTunnelEvent{Hostport: "skipped.test:1234"})
	if len(svc.GetTraffic()) != 0 {
		t.Fatal("paused TCP created a record")
	}
	if _, err := svc.Stop(); err != nil {
		t.Fatal(err)
	}
	started, err := svc.Start()
	if err != nil || !started.Running || started.RecordingPaused {
		t.Fatalf("explicit start: %+v %v", started, err)
	}
	svc.rawTCPInterceptor()(newRawTCPMetadataContext(), mitmproxy.RawTCPTunnelEvent{Hostport: "recorded.test:1234"})
	if len(svc.GetTraffic()) != 1 {
		t.Fatal("explicit start did not resume recording")
	}
}

func TestRecordingPauseSerializesAdmission(t *testing.T) {
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	svc.running = true
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for i := range 80 {
				if i%3 == 0 {
					_, _ = svc.SetRecordingPaused(i%2 == 0)
				}
				svc.registerTrafficEntry(newRawTCPMetadataContext(), TrafficEntry{Type: "tcp"})
			}
		})
	}
	wg.Wait()
	setRecordingPause(t, svc, true)
	entries := svc.GetTraffic()
	for i, entry := range entries {
		if entry.ID != uint64(i+1) {
			t.Fatalf("admission sequence has a gap at %d: %d", i, entry.ID)
		}
	}
	for range 20 {
		if entry := svc.registerTrafficEntry(newRawTCPMetadataContext(), TrafficEntry{Type: "tcp"}); entry != nil {
			t.Fatal("new entry admitted after pause returned")
		}
	}
	if len(svc.GetTraffic()) != len(entries) {
		t.Fatal("paused admission changed existing records")
	}
}
