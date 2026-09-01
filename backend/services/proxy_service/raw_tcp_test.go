package proxyservice

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josexy/flowlens/backend/pkg/orderedmap"
	processattribution "github.com/josexy/flowlens/backend/pkg/process_attribution"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/mitmproxy-go"
	"github.com/josexy/mitmproxy-go/metadata"
	http "github.com/josexy/xhttp"
)

func TestRawTCPInterceptorMapsTunnelEvents(t *testing.T) {
	connectRequest := &http.Request{
		Method: http.MethodConnect,
		Host:   "connect.example:8443",
		Proto:  "HTTP/1.1",
		Header: http.Header{
			"X-Trace-ID": []string{"outer-connect"},
		},
	}
	tests := []struct {
		name         string
		event        mitmproxy.RawTCPTunnelEvent
		wantSource   RawTCPTunnelSource
		wantMethod   string
		wantRequest  bool
		wantTLS      bool
		wantHostport string
	}{
		{
			name: "direct",
			event: mitmproxy.RawTCPTunnelEvent{
				Source:   mitmproxy.RawTCPTunnelSourceDirect,
				Hostport: "direct.example:9000",
				Request:  connectRequest,
			},
			wantSource:   RawTCPTunnelSourceDirect,
			wantHostport: "direct.example:9000",
		},
		{
			name: "http connect",
			event: mitmproxy.RawTCPTunnelEvent{
				Source:   mitmproxy.RawTCPTunnelSourceHTTPConnect,
				Hostport: "connect.example:8443",
				Request:  connectRequest,
			},
			wantSource:   RawTCPTunnelSourceHTTPConnect,
			wantMethod:   http.MethodConnect,
			wantRequest:  true,
			wantHostport: "connect.example:8443",
		},
		{
			name: "socks5 tls",
			event: mitmproxy.RawTCPTunnelEvent{
				Source:   mitmproxy.RawTCPTunnelSourceSOCKS5,
				Hostport: "[2001:db8::5]:443",
				TLS:      true,
			},
			wantSource:   RawTCPTunnelSourceSOCKS5,
			wantTLS:      true,
			wantHostport: "[2001:db8::5]:443",
		},
		{
			name: "unknown source",
			event: mitmproxy.RawTCPTunnelEvent{
				Source:   mitmproxy.RawTCPTunnelSource(255),
				Hostport: "unknown.example:1234",
			},
			wantSource:   RawTCPTunnelSourceUnknown,
			wantHostport: "unknown.example:1234",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newRawTCPTestService()
			var emitted []*TrafficEntry
			service.emitTrafficHook = func(entry *TrafficEntry) {
				emitted = append(emitted, entry)
			}

			service.rawTCPInterceptor()(newRawTCPMetadataContext(), test.event)

			if len(emitted) != 1 {
				t.Fatalf("emitted entries = %d, want 1", len(emitted))
			}
			entry := emitted[0]
			if entry.ID != 1 || entry.Type != "tcp" || entry.StartedAt.IsZero() {
				t.Fatalf("entry identity = %+v, want ID 1 and tcp type", entry)
			}
			if entry.Method != test.wantMethod || entry.URL != "tcp://"+test.wantHostport || entry.Host != test.wantHostport {
				t.Fatalf("entry target = method %q URL %q host %q", entry.Method, entry.URL, entry.Host)
			}
			if entry.Path != "" || entry.StatusCode != 0 || entry.Status != "" || entry.Response != nil || entry.Error != nil {
				t.Fatalf("raw TCP entry populated HTTP response fields: %+v", entry)
			}
			if entry.RawTCP == nil || entry.RawTCP.Source != test.wantSource || entry.RawTCP.HostPort != test.wantHostport || entry.RawTCP.TLS != test.wantTLS {
				t.Fatalf("raw TCP info = %+v", entry.RawTCP)
			}
			if got := entry.Request != nil; got != test.wantRequest {
				t.Fatalf("request present = %t, want %t", got, test.wantRequest)
			}
			if test.wantRequest {
				if entry.Request.Proto != "HTTP/1.1" || firstHeaderFieldValue(entry.Request.HeaderFields, "X-Trace-ID") != "outer-connect" || firstHeaderFieldValue(entry.Request.HeaderFields, "Host") != "connect.example:8443" {
					t.Fatalf("outer CONNECT request = %+v", entry.Request)
				}
			}
			if entry.Metadata == nil || entry.Metadata.LocalSourceAddr != "127.0.0.1:43120" || entry.Metadata.RemoteDestinationAddr != "203.0.113.10:443" {
				t.Fatalf("metadata = %+v", entry.Metadata)
			}
			stored, ok := service.trafficEntries.Get(entry.ID)
			if !ok || stored.ID != entry.ID || stored.URL != entry.URL {
				t.Fatalf("stored entry = (%+v, %t), want emitted entry", stored, ok)
			}
			if _, ok := service.trafficBodies.Load(entry.ID); ok {
				t.Fatal("raw TCP interceptor created body capture state")
			}
			if _, ok := service.trafficWsMsgs.Load(entry.ID); ok {
				t.Fatal("raw TCP interceptor created WebSocket state")
			}
			if got := service.GetStatistics(); got.Total != 1 || got.TotalTCP != 1 || got.TotalHTTP != 0 || got.TotalWS != 0 {
				t.Fatalf("statistics = %+v, want one TCP entry", got)
			}
		})
	}
}

func TestRawTCPInterceptorConcurrentStorageAndDeletion(t *testing.T) {
	service := newRawTCPTestService()
	var emitted atomic.Int64
	service.emitTrafficHook = func(*TrafficEntry) { emitted.Add(1) }
	interceptor := service.rawTCPInterceptor()
	ctx := newRawTCPMetadataContext()

	const tunnelCount = 128
	var wg sync.WaitGroup
	for range tunnelCount {
		wg.Go(func() {
			interceptor(ctx, mitmproxy.RawTCPTunnelEvent{
				Source:   mitmproxy.RawTCPTunnelSourceSOCKS5,
				Hostport: "raw.example:7000",
			})
		})
	}
	wg.Wait()

	entries := service.GetTraffic()
	if len(entries) != tunnelCount || emitted.Load() != tunnelCount {
		t.Fatalf("stored/emitted = %d/%d, want %d/%d", len(entries), emitted.Load(), tunnelCount, tunnelCount)
	}
	seen := make(map[uint64]struct{}, tunnelCount)
	ids := make([]int64, 0, tunnelCount)
	for _, entry := range entries {
		if entry.Type != "tcp" || entry.RawTCP == nil {
			t.Fatalf("non-TCP concurrent entry = %+v", entry)
		}
		if _, duplicate := seen[entry.ID]; duplicate {
			t.Fatalf("duplicate traffic ID %d", entry.ID)
		}
		seen[entry.ID] = struct{}{}
		ids = append(ids, int64(entry.ID))
	}
	if got := service.GetStatistics(); got.Total != tunnelCount || got.TotalTCP != tunnelCount || got.TotalHTTP != 0 || got.TotalWS != 0 {
		t.Fatalf("statistics after concurrent insert = %+v", got)
	}

	service.DeleteTraffic(ids)
	if got := service.GetStatistics(); got != (TrafficStatistics{}) {
		t.Fatalf("statistics after delete = %+v, want zero", got)
	}
}

func TestRawTCPInterceptorReusesAcceptedConnectionProcessLookup(t *testing.T) {
	configureProcessAttributionSetting(t, true)
	provider := &proxyAttributionTestProvider{
		lookup: func(context.Context, processattribution.EndpointTuple) processattribution.Result {
			return proxyResolvedProcessResult(120)
		},
	}
	manager := newProxyAttributionTestManager(t, provider, nil)
	service := newProxyAttributionTestService(manager, nil)
	conn := service.attributeConnection(newAddressedTestConn(t, "127.0.0.1:43120", "127.0.0.1:8080"))
	t.Cleanup(func() { _ = conn.Close() })

	service.rawTCPInterceptor()(newRawTCPMetadataContext(), mitmproxy.RawTCPTunnelEvent{
		Source:   mitmproxy.RawTCPTunnelSourceSOCKS5,
		Hostport: "raw.example:7000",
	})

	drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	manager.Drain(drainCtx)
	stored, ok := service.trafficEntries.Get(1)
	if !ok || stored.Metadata == nil || stored.Metadata.Process == nil {
		t.Fatalf("stored raw TCP process attribution = %+v", stored)
	}
	if stored.Metadata.Process.Status != ProcessStatusResolved || stored.Metadata.Process.PID != 120 {
		t.Fatalf("stored raw TCP process = %+v, want resolved PID 120", stored.Metadata.Process)
	}
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("process provider calls = %d, want 1", calls)
	}
}

func TestBuildHandlerRegistersRawTCPInterceptor(t *testing.T) {
	dir := t.TempDir()
	cfg := &settingservice.ProxyConfig{
		CACertPath:   filepath.Join(dir, "ca.crt"),
		CAKeyPath:    filepath.Join(dir, "ca.key"),
		DisableProxy: true,
	}
	settings := &settingservice.SettingService{}
	if err := settings.Update(&settingservice.Settings{ProxyConfig: cfg}); err != nil {
		t.Fatalf("configure CA paths: %v", err)
	}
	if _, err := settings.GenerateCurrentCACertificate(settingservice.GenerateCACertificateRequest{
		CommonName: "FlowLens Raw TCP Test CA",
		ValidDays:  1,
	}); err != nil {
		t.Fatalf("generate test CA: %v", err)
	}

	service := newRawTCPTestService()
	emitted := make(chan *TrafficEntry, 1)
	service.emitTrafficHook = func(entry *TrafficEntry) { emitted <- entry }
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	handler, err := service.buildHandlerLocked(ctx, cfg)
	if err != nil {
		t.Fatalf("buildHandlerLocked: %v", err)
	}
	t.Cleanup(handler.Cleanup)

	target, closeEcho := startRawTCPEchoServer(t)
	t.Cleanup(closeEcho)
	client, server := net.Pipe()
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- handler.ServeSOCKS5(ctx, server) }()

	if _, err := client.Write([]byte{5, 1, 0}); err != nil {
		t.Fatalf("write SOCKS5 greeting: %v", err)
	}
	methodReply := make([]byte, 2)
	if _, err := io.ReadFull(client, methodReply); err != nil {
		t.Fatalf("read SOCKS5 method reply: %v", err)
	}
	if !bytes.Equal(methodReply, []byte{5, 0}) {
		t.Fatalf("SOCKS5 method reply = %v, want [5 0]", methodReply)
	}
	ip := target.IP.To4()
	request := []byte{5, 1, 0, 1, ip[0], ip[1], ip[2], ip[3], byte(target.Port >> 8), byte(target.Port)}
	if _, err := client.Write(request); err != nil {
		t.Fatalf("write SOCKS5 connect request: %v", err)
	}
	connectReply := make([]byte, 10)
	if _, err := io.ReadFull(client, connectReply); err != nil {
		t.Fatalf("read SOCKS5 connect reply: %v", err)
	}
	if connectReply[1] != 0 {
		t.Fatalf("SOCKS5 connect reply = %v, want success", connectReply)
	}

	// Start with a non-HTTP method byte so the dependency can classify the
	// tunnel as raw without waiting for its bounded request-line sniff buffer.
	payload := []byte{0, 1, 2, 'r', 'a', 'w'}
	if _, err := client.Write(payload); err != nil {
		t.Fatalf("write raw TCP payload: %v", err)
	}
	echoed := make([]byte, len(payload))
	if _, err := io.ReadFull(client, echoed); err != nil {
		t.Fatalf("read raw TCP echo: %v", err)
	}
	if !bytes.Equal(echoed, payload) {
		t.Fatalf("raw TCP echo = %q, want %q", echoed, payload)
	}

	select {
	case entry := <-emitted:
		if entry.Type != "tcp" || entry.RawTCP == nil || entry.RawTCP.Source != RawTCPTunnelSourceSOCKS5 || entry.RawTCP.HostPort != target.String() {
			t.Fatalf("wired raw TCP entry = %+v", entry)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not emit a raw TCP tunnel entry")
	}
	_ = client.Close()
	select {
	case <-serveErr:
	case <-time.After(time.Second):
		t.Fatal("SOCKS5 handler did not stop after client close")
	}
}

func TestTrafficStatisticsClassifiesKnownTypesExplicitly(t *testing.T) {
	service := newRawTCPTestService()
	for i, trafficType := range []string{"http", "https", "ws", "wss", "tcp", "future"} {
		service.storeTrafficEntry(&TrafficEntry{ID: uint64(i + 1), Type: trafficType})
	}
	if got := service.GetStatistics(); got.Total != 6 || got.TotalHTTP != 2 || got.TotalWS != 2 || got.TotalTCP != 1 {
		t.Fatalf("statistics = %+v, want total=6 HTTP=2 WS=2 TCP=1", got)
	}
	for id := uint64(1); id <= 6; id++ {
		service.deleteTrafficEntry(id)
	}
	if got := service.GetStatistics(); got != (TrafficStatistics{}) {
		t.Fatalf("statistics after delete = %+v, want zero", got)
	}
}

func TestResendRequestWithTrafficEntryRejectsRawTCP(t *testing.T) {
	service := newTestProxyService(t, &settingservice.ProxyConfig{})
	_, err := service.ResendRequestWithTrafficEntry(context.Background(), ResendConfig{}, &TrafficEntry{
		ID:     7,
		Type:   "tcp",
		Method: http.MethodConnect,
		URL:    "tcp://raw.example:443",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "can only resend HTTP/HTTPS requests, got tcp") {
		t.Fatalf("ResendRequestWithTrafficEntry error = %v", err)
	}
}

func newRawTCPTestService() *ProxyService {
	return &ProxyService{
		baseCtx: context.Background(),
		trafficEntries: &TrafficEntryWithStatics{
			OrderedMap: orderedmap.NewWithCapacity[uint64, *TrafficEntry](128),
			Statistics: &TrafficStatistics{},
		},
	}
}

func newRawTCPMetadataContext() context.Context {
	md := metadata.NewMD()
	md.SetLocalConnectionAddrInfo(metadata.ConnectionAddrInfo{
		SourceAddr:      netip.MustParseAddrPort("127.0.0.1:43120"),
		DestinationAddr: netip.MustParseAddrPort("127.0.0.1:8080"),
	})
	md.SetRemoteConnectionAddrInfo(metadata.ConnectionAddrInfo{
		SourceAddr:      netip.MustParseAddrPort("192.0.2.20:51000"),
		DestinationAddr: netip.MustParseAddrPort("203.0.113.10:443"),
	})
	md.SetLocalConnectionEstablishedTs(time.Unix(1_700_000_000, 0))
	md.SetRemoteConnectionEstablishedTs(time.Unix(1_700_000_001, 0))
	md.SetRequestReceivedTs(time.Unix(1_700_000_002, 0))
	return metadata.AppendToContext(context.Background(), md)
}

func startRawTCPEchoServer(t *testing.T) (*net.TCPAddr, func()) {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("listen for raw TCP echo: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, acceptErr := listener.AcceptTCP()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()
	closeServer := func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
	return listener.Addr().(*net.TCPAddr), closeServer
}
