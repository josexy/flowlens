package proxyservice

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/websocket"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
	"github.com/josexy/xhttp/httptrace"
)

func TestCachedDialerMultipleAddressesDualStackAndTrace(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	var calls, starts, dones atomic.Int64
	cache := newDNSCache(dnsResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		calls.Add(1)
		return []net.IPAddr{{IP: net.ParseIP("::1")}, {IP: net.ParseIP("127.0.0.2")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}))
	t.Cleanup(cache.close)
	cache.configure(true, 60)
	dialer := &cachedNetworkDialer{cache: cache, base: &net.Dialer{Timeout: time.Second, FallbackDelay: time.Millisecond}}
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { starts.Add(1) },
		DNSDone:  func(httptrace.DNSDoneInfo) { dones.Add(1) },
	})
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	for range 2 {
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("dual.test", port))
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}
	if calls.Load() != 1 || starts.Load() != 2 || dones.Load() != 2 {
		t.Fatalf("DNS trace/caching calls=%d starts=%d dones=%d", calls.Load(), starts.Load(), dones.Load())
	}
	conn, err := dialer.DialContext(ctx, "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if calls.Load() != 1 || starts.Load() != 2 {
		t.Fatal("IP literal performed DNS lookup")
	}
}

func TestCachedDialerFallbackStartsBeforeHangingIPv6Completes(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	cache := newDNSCache(dnsResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("::1")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}))
	t.Cleanup(cache.close)
	cache.configure(true, 60)
	loserFinished := make(chan struct{})
	dialer := &cachedNetworkDialer{cache: cache, base: &net.Dialer{Timeout: time.Second, FallbackDelay: 10 * time.Millisecond,
		ControlContext: func(ctx context.Context, network, _ string, _ syscall.RawConn) error {
			if network == "tcp6" {
				<-ctx.Done()
				close(loserFinished)
				return ctx.Err()
			}
			return nil
		},
	}}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	conn, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort("fallback.test", port))
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	_ = (<-accepted).Close()
	select {
	case <-loserFinished:
	case <-time.After(time.Second):
		t.Fatal("losing connection attempt was not cancelled")
	}
}

func TestCachedDialerTimeoutAndDisabledBypass(t *testing.T) {
	var calls atomic.Int64
	cache := newDNSCache(dnsResolverFunc(func(ctx context.Context, _ string) ([]net.IPAddr, error) {
		calls.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	t.Cleanup(cache.close)
	cache.configure(true, 60)
	dialer := &cachedNetworkDialer{cache: cache, base: &net.Dialer{Timeout: 20 * time.Millisecond}}
	if _, err := dialer.DialContext(context.Background(), "tcp", "blocked.test:80"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DNS exceeded dial timeout: %v", err)
	}
	cache.configure(false, 60)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	dialer.base.Timeout = time.Second
	conn, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort("localhost", port))
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if calls.Load() != 1 {
		t.Fatal("disabled dialer used application resolver")
	}
}

func TestDNSCacheClearPreservesHTTP2ConnectionHostAndSNI(t *testing.T) {
	requests := make(chan string, 3)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 || r.TLS.ServerName != "shared.test" || !strings.HasPrefix(r.Host, "shared.test:") {
			t.Errorf("protocol/hostname changed: %s %s %s", r.Proto, r.TLS.ServerName, r.Host)
		}
		requests <- r.RemoteAddr
		w.WriteHeader(http.StatusNoContent)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	var calls atomic.Int64
	cache := newDNSCache(dnsResolverFunc(func(context.Context, string) ([]net.IPAddr, error) { calls.Add(1); return dnsTestIPs("127.0.0.1"), nil }))
	t.Cleanup(cache.close)
	cache.configure(true, 60)
	dialer := &cachedNetworkDialer{cache: cache, base: &net.Dialer{Timeout: time.Second}}
	transport := newSyntheticRoundTripper(true, SendRequestProtocolHTTP2, nil, TLSClientHelloGolang, dialer)
	defer transport.CloseIdleConnections()
	u, _ := url.Parse(server.URL)
	u.Host = net.JoinHostPort("shared.test", u.Port())
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	send := func() {
		t.Helper()
		response, err := client.Get(u.String())
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	send()
	first := <-requests
	cache.clear()
	send()
	if got := <-requests; got != first || calls.Load() != 1 {
		t.Fatal("clearing cache disconnected HTTP/2")
	}
	transport.CloseIdleConnections()
	send()
	<-requests
	if calls.Load() != 2 {
		t.Fatal("new connection did not resolve after clear")
	}
}

func TestDNSCacheSharedByCaptureEditorResendAndWebSocket(t *testing.T) {
	setTestConfigDir(t)
	wsMessages := make(chan string, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			_, data, err := conn.ReadMessage()
			if err == nil {
				wsMessages <- string(data)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	u.Host = net.JoinHostPort("shared.test", u.Port())
	dir := t.TempDir()
	svc := newTestProxyService(t, &settingservice.ProxyConfig{Mode: settingservice.ProxyModeHTTP, Host: "127.0.0.1", Port: reserveTestTCPPort(t), CACertPath: dir + "/ca.crt", CAKeyPath: dir + "/ca.key", DisableProxy: true, DNSCacheEnabled: true, DNSCacheTTLSeconds: 60})
	t.Cleanup(svc.baseCancel)
	var calls atomic.Int64
	cache := newDNSCache(dnsResolverFunc(func(context.Context, string) ([]net.IPAddr, error) { calls.Add(1); return dnsTestIPs("127.0.0.1"), nil }))
	cache.configure(true, 60)
	svc.dnsCache = cache
	t.Cleanup(cache.close)
	if _, err := svc.settingService.GenerateCurrentCACertificate(settingservice.GenerateCACertificateRequest{}); err != nil {
		t.Fatal(err)
	}
	status, err := svc.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()
	proxyURL, _ := url.Parse("http://" + status.Address)
	captureTransport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer captureTransport.CloseIdleConnections()
	response, err := (&http.Client{Transport: captureTransport, Timeout: 2 * time.Second}).Get(u.String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if _, err := svc.SendHTTPRequest(context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone, TimeoutMs: 2000}, http.MethodGet, u.String(), nil, SendRequestBody{BodyType: SendRequestBodyTypeNone}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.ResendRequestWithTrafficEntry(context.Background(), ResendConfig{Count: 1}, &TrafficEntry{Type: "http", Method: http.MethodGet, URL: u.String()}, nil)
	if err != nil || result.Success != 1 {
		t.Fatalf("resend: %+v %v", result, err)
	}
	connected, err := svc.ConnectWebSocket(context.Background(), WebSocketConnectRequest{URL: "ws://" + u.Host + "/ws", ProxyMode: SendRequestProxyModeNone, TimeoutMs: 2000})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.DisconnectWebSocket(connected.SessionID)
	if calls.Load() != 1 {
		t.Fatalf("entrypoints did not share DNS: %d lookups", calls.Load())
	}
	svc.ClearDNSCache()
	if err := svc.SendWebSocketMessage(WebSocketSendRequest{SessionID: connected.SessionID, MsgType: "text", Text: "still connected"}); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-wsMessages:
		if data != "still connected" {
			t.Fatal(data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("clear disconnected WebSocket")
	}
	if _, err := svc.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendHTTPRequest(context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeNone, TimeoutMs: 2000}, http.MethodGet, u.String(), nil, SendRequestBody{BodyType: SendRequestBodyTypeNone}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("editor did not resolve after clear while capture stopped")
	}
}

func TestDNSCacheProxyEndpointsPreserveRemoteDestinationResolution(t *testing.T) {
	upgrader := websocket.Upgrader{}
	originHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			_, _, _ = conn.ReadMessage()
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	httpsOrigin := httptest.NewTLSServer(originHandler)
	defer httpsOrigin.Close()
	httpOrigin := httptest.NewServer(originHandler)
	defer httpOrigin.Close()
	tunnel := connectTunnelHandler(t)
	forward := &http.Transport{}
	defer forward.CloseIdleConnections()
	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			tunnel.ServeHTTP(w, r)
			return
		}
		r.RequestURI = ""
		response, err := forward.RoundTrip(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	})
	httpProxy, httpsProxy := httptest.NewServer(proxyHandler), httptest.NewTLSServer(proxyHandler)
	defer httpProxy.Close()
	defer httpsProxy.Close()
	proxyURLs := []string{httpProxy.URL, httpsProxy.URL, startTestSOCKS5Proxy(t)}
	var calls atomic.Int64
	cache := newDNSCache(dnsResolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
		if !strings.HasSuffix(host, "-proxy.test") {
			return nil, errors.New("destination was unexpectedly resolved locally: " + host)
		}
		calls.Add(1)
		return dnsTestIPs("127.0.0.1"), nil
	}))
	t.Cleanup(cache.close)
	cache.configure(true, 60)
	svc := newTestProxyService(t, &settingservice.ProxyConfig{DNSCacheEnabled: true})
	t.Cleanup(svc.baseCancel)
	svc.dnsCache = cache
	for _, rawProxy := range proxyURLs {
		proxyURL, _ := url.Parse(rawProxy)
		proxyURL.Host = net.JoinHostPort(proxyURL.Scheme+"-proxy.test", proxyURL.Port())
		for _, rawOrigin := range []string{httpOrigin.URL, httpsOrigin.URL} {
			origin, _ := url.Parse(rawOrigin)
			origin.Host = net.JoinHostPort("localhost", origin.Port())
			for range 2 {
				response, err := svc.SendHTTPRequest(context.Background(), SendRequestConfig{ProxyMode: SendRequestProxyModeCustom, CustomProxy: proxyURL.String(), SkipVerifyTLS: true, TimeoutMs: 2000}, http.MethodGet, origin.String(), nil, SendRequestBody{BodyType: SendRequestBodyTypeNone})
				if err != nil || response.StatusCode != http.StatusNoContent {
					t.Fatalf("%s via %s: %+v %v", origin.Scheme, proxyURL.Scheme, response, err)
				}
			}
			origin.Path = "/ws"
			if origin.Scheme == "https" {
				origin.Scheme = "wss"
			} else {
				origin.Scheme = "ws"
			}
			connected, err := svc.ConnectWebSocket(context.Background(), WebSocketConnectRequest{URL: origin.String(), ProxyMode: SendRequestProxyModeCustom, CustomProxy: proxyURL.String(), SkipVerifyTLS: true, TimeoutMs: 2000})
			if err != nil {
				t.Fatalf("%s via %s: %v", origin.Scheme, proxyURL.Scheme, err)
			}
			if err := svc.DisconnectWebSocket(connected.SessionID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("proxy endpoints did not share/cache local resolution: %d", calls.Load())
	}
}
