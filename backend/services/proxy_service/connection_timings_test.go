package proxyservice

import (
	"bytes"
	"context"
	"errors"
	"math"
	"net"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/mitmproxy-go/v2/metadata"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
)

func TestCaptureDNSConnectionTimingsWithCacheEnabledAndDisabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			setTestConfigDir(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			target, _ := url.Parse(server.URL)
			target.Host = net.JoinHostPort("localhost", target.Port())
			dir := t.TempDir()
			svc := newTestProxyService(t, &settingservice.ProxyConfig{
				Mode: settingservice.ProxyModeHTTP, Host: "127.0.0.1", Port: reserveTestTCPPort(t),
				CACertPath: dir + "/ca.crt", CAKeyPath: dir + "/ca.key", DisableProxy: true,
				DNSCacheEnabled: enabled, DNSCacheTTLSeconds: 60,
			})
			var lookups, cacheTime atomic.Int64
			cache := newDNSCache(dnsResolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
				lookups.Add(1)
				return dnsTestIPs("127.0.0.1"), nil
			}))
			cache.now = func() time.Time { return time.Unix(cacheTime.Load(), 0) }
			cache.configure(enabled, 60)
			svc.dnsCache = cache
			t.Cleanup(cache.close)
			t.Cleanup(func() {
				if err := svc.Shutdown(); err != nil {
					t.Error(err)
				}
			})
			writeCustomTestCA(t, svc.settingService)
			status, err := svc.Start()
			if err != nil {
				t.Fatal(err)
			}
			proxyURL, _ := url.Parse("http://" + status.Address)
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
			for i := range 4 {
				if i == 2 {
					cache.clear()
				}
				if i == 3 {
					cacheTime.Store(60)
				}
				response, err := client.Get(target.String())
				if err != nil {
					t.Fatal(err)
				}
				_ = response.Body.Close()
			}
			entries := svc.GetTraffic()
			if len(entries) != 4 {
				t.Fatalf("captured entries = %d, want 4", len(entries))
			}
			zeroLookups := 0
			for _, entry := range entries {
				if entry.Metadata == nil || entry.Metadata.ConnectionTimings == nil {
					t.Fatal("connection timings missing from captured entry")
				}
				got := entry.Metadata.ConnectionTimings
				if got.DNSStartedAtMicros < 0 || got.DNSEndedAtMicros < got.DNSStartedAtMicros ||
					got.ConnectStartedAtMicros < got.DNSEndedAtMicros || got.ConnectEndedAtMicros < got.ConnectStartedAtMicros {
					t.Fatalf("missing or reordered connection phases: %+v", got)
				}
				dnsMicros := got.DNSEndedAtMicros - got.DNSStartedAtMicros
				if dnsMicros == 0 {
					zeroLookups++
				}
				if runtime.GOOS == "windows" && !enabled && dnsMicros == 0 {
					t.Fatal("Windows lookup lost its sub-tick duration")
				}
				t.Logf("DNS cache %s: lookup duration %d microseconds", name, dnsMicros)
			}
			if enabled && zeroLookups != 1 {
				t.Fatalf("zero DNS durations = %d, want only the cache hit", zeroLookups)
			}
			if calls := lookups.Load(); (enabled && calls != 3) || (!enabled && calls != 0) {
				t.Fatalf("application resolver calls = %d with cache %s", calls, name)
			}
		})
	}
}

func TestConnectionTimingCaptureRefreshesImmutableSnapshots(t *testing.T) {
	svc := newTestProxyService(t, nil)
	md := metadata.NewMD()
	ctx := metadata.AppendToContext(context.Background(), md)
	entry := svc.newTrafficEntry(TrafficEntry{Type: "http", Request: &HTTPMessage{}})
	exchange := newCaptureExchange(svc, ctx, entry)
	startCaptureAttempt(exchange, time.UnixMicro(1000), 1, true)
	before, _ := svc.trafficEntries.Get(entry.ID)
	if before.Metadata.ConnectionTimings.DNSStartedAtMicros != -1 {
		t.Fatal("unobserved DNS was presented as zero")
	}
	md.SetDNSLookupStartTs(time.UnixMicro(1100))
	md.SetDNSLookupCompletedTs(time.UnixMicro(1234))
	md.SetSocketConnectStartTs(time.UnixMicro(1234))
	md.SetSocketConnectCompletedTs(time.UnixMicro(1567))
	md.SetRemoteConnectionEstablishedTs(time.UnixMicro(1568))
	md.SetSSLHandshakeStartTs(time.UnixMicro(1600))
	md.SetSSLHandshakeCompletedTs(time.UnixMicro(1888))
	exchange.requestEnded(time.UnixMicro(2000), true, nil)
	after, _ := svc.trafficEntries.Get(entry.ID)
	want := &HTTPConnectionTimings{1100, 1234, 1234, 1567, 1600, 1888}
	if !reflect.DeepEqual(after.Metadata.ConnectionTimings, want) {
		t.Fatalf("connection timings = %+v", after.Metadata.ConnectionTimings)
	}
	if before.Metadata.ConnectionTimings.DNSStartedAtMicros != -1 {
		t.Fatal("an already published snapshot was mutated")
	}
	if !reflect.DeepEqual(newTrafficMetricsPatch(after).Metrics.Connection, want) {
		t.Fatal("connection timings missing from frontend patch")
	}
	// The next dial fails. The old successful connection must not complete it.
	md.SetDNSLookupStartTs(time.UnixMicro(2100))
	md.SetDNSLookupCompletedTs(time.UnixMicro(2200))
	md.SetSocketConnectStartTs(time.UnixMicro(2200))
	md.SetSocketConnectCompletedTs(time.UnixMicro(2400))
	md.SetSSLHandshakeStartTs(time.UnixMicro(2500))
	exchange.fail(errors.New("dial failed"))
	failed, _ := svc.trafficEntries.Get(entry.ID)
	if got := failed.Metadata.ConnectionTimings; got.ConnectEndedAtMicros != -1 || got.TLSEndedAtMicros != -1 {
		t.Fatalf("failed phases have completion values: %+v", got)
	}
	md.SetDNSLookupStartTs(time.UnixMicro(3000))
	md.SetDNSLookupCompletedTs(time.UnixMicro(3200))
	if got := connectionTimingsFromContext(ctx); got.DNSEndedAtMicros != -1 {
		t.Fatalf("failed DNS has a completed duration: %+v", got)
	}
}

func TestHARConnectionTimingBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		connection              HTTPConnectionTimings
		dns, connect, ssl, send float64
		blocked, total, start   float64
	}{
		{"new connection inside request", HTTPConnectionTimings{1100, 1200, 1200, 1400, 1400, 1600}, .1, .4, .2, .5, -1, 3, 1000},
		{"CONNECT setup before request", HTTPConnectionTimings{100, 200, 200, 400, 400, 600}, .1, .4, .2, 1, .4, 3.9, 100},
		{"setup gaps before request", HTTPConnectionTimings{100, 200, 400, 600, 700, 800}, .1, .3, .1, 1, .5, 3.9, 100},
		{"IP address without DNS or TLS", HTTPConnectionTimings{-1, -1, 1100, 1300, -1, -1}, -1, .2, -1, .8, -1, 3, 1000},
		{"unknown", HTTPConnectionTimings{-1, -1, -1, -1, -1, -1}, -1, -1, -1, 1, -1, 3, 1000},
		{"incomplete handshake", HTTPConnectionTimings{-1, -1, 1100, 1300, 1300, -1}, -1, .2, -1, .8, -1, 3, 1000},
		{"overlapping phases", HTTPConnectionTimings{1100, 1400, 1200, 1500, -1, -1}, -1, -1, -1, 1, -1, 3, 1000},
		{"phase crosses request start", HTTPConnectionTimings{-1, -1, 900, 1300, -1, -1}, -1, .4, -1, .7, 0, 3.1, 900},
		{"observed zero", HTTPConnectionTimings{1100, 1100, 1100, 1100, 1100, 1100}, 0, 0, 0, 1, -1, 3, 1000},
		{"cached DNS before TCP", HTTPConnectionTimings{100, 100, 200, 400, 400, 600}, 0, .4, .2, 1, .5, 3.9, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := HARExportEntry{Entry: &TrafficEntry{
				Type: "https", URL: "https://example.test/", Method: "GET",
				Metadata: &Metadata{ConnectionTimings: &tc.connection},
				Request:  &HTTPMessage{Metrics: &HTTPMessageMetrics{StartedAtMicros: 1000, EndedAtMicros: 2000, State: HTTPMessageStateCompleted}},
				Response: &HTTPMessage{Metrics: &HTTPMessageMetrics{StartedAtMicros: 3000, EndedAtMicros: 4000, State: HTTPMessageStateCompleted}},
			}}
			got, raw := decodeSingleHAR(t, input)
			if got.Timings.DNS == nil || got.Timings.Connect == nil || got.Timings.SSL == nil {
				t.Fatal("connection timing fields missing")
			}
			if *got.Timings.DNS != tc.dns || math.Abs(*got.Timings.Connect-tc.connect) > 1e-9 || *got.Timings.SSL != tc.ssl || math.Abs(got.Timings.Send-tc.send) > 1e-9 {
				t.Fatalf("dns=%g connect=%g ssl=%g send=%g", *got.Timings.DNS, *got.Timings.Connect, *got.Timings.SSL, got.Timings.Send)
			}
			blocked := float64(-1)
			if got.Timings.Blocked != nil {
				blocked = *got.Timings.Blocked
			}
			if blocked != tc.blocked || got.StartedDateTime != time.UnixMicro(int64(tc.start)).UTC().Format("2006-01-02T15:04:05.000000Z") {
				t.Fatalf("blocked=%g started=%s", blocked, got.StartedDateTime)
			}
			sum := max(blocked, 0) + max(tc.dns, 0) + max(tc.connect, 0) + got.Timings.Send + got.Timings.Wait + got.Timings.Receive
			if got.Time != tc.total || math.Abs(got.Time-sum) > 1e-9 {
				t.Fatalf("total=%g phase sum=%g", got.Time, sum)
			}
			if !reflect.DeepEqual(got.ConnectionTimings, &tc.connection) || !strings.Contains(string(raw), `"_connectionTimings"`) {
				t.Fatal("connection timestamps lost in streamed HAR")
			}
			input.Entry.Request.Metrics.State = HTTPMessageStateFailed
			failed, _ := decodeSingleHAR(t, input)
			if failed.Time != -1 || failed.Timings.Send != -1 {
				t.Fatal("failed request acquired a completed timing")
			}
			if math.Abs(*failed.Timings.Connect-tc.connect) > 1e-9 {
				t.Fatal("failed HTTP send erased completed connection timing")
			}
		})
	}
}

func TestConnectionTimingsHistoryV2AndLegacyV1(t *testing.T) {
	for _, kind := range []string{"https", "tcp"} {
		t.Run(kind, func(t *testing.T) {
			entry := &TrafficEntry{Type: kind, Metadata: &Metadata{ConnectionTimings: &HTTPConnectionTimings{1000001, 1000123, 1000123, 1000789, -1, -1}}}
			var encoded bytes.Buffer
			if err := hbinWriteEntryHeader(&encoded, entry); err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeTrafficEntryWithVersion(bytes.NewReader(encoded.Bytes()), 2)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded.Metadata.ConnectionTimings, entry.Metadata.ConnectionTimings) {
				t.Fatal("timings changed in history")
			}
			cachedTimings := *entry.Metadata.ConnectionTimings
			cachedTimings.DNSEndedAtMicros = cachedTimings.DNSStartedAtMicros
			cachedEntry := &TrafficEntry{Type: kind, Metadata: &Metadata{ConnectionTimings: &cachedTimings}}
			var cachedEncoded bytes.Buffer
			if err := hbinWriteEntryHeader(&cachedEncoded, cachedEntry); err != nil {
				t.Fatal(err)
			}
			cachedDecoded, err := DecodeTrafficEntryWithVersion(bytes.NewReader(cachedEncoded.Bytes()), 2)
			if err != nil || !reflect.DeepEqual(cachedDecoded.Metadata.ConnectionTimings, cachedEntry.Metadata.ConnectionTimings) {
				t.Fatalf("zero DNS duration changed in history: %v", err)
			}
			// V1 ends before the new optional block. Decode it with body bytes
			// following, as history_service does, without consuming those bytes.
			legacy := append([]byte(nil), encoded.Bytes()[:encoded.Len()-49]...)
			legacy = append(legacy, []byte("body")...)
			r := bytes.NewReader(legacy)
			decoded, err = DecodeTrafficEntryWithVersion(r, 1)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Metadata.ConnectionTimings != nil || r.Len() != 4 {
				t.Fatal("V1 body was interpreted as connection timings")
			}
			if _, err := DecodeTrafficEntryWithVersion(bytes.NewReader(encoded.Bytes()[:encoded.Len()-1]), 2); err == nil {
				t.Fatal("truncated V2 timing accepted")
			}
		})
	}
}
