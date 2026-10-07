package proxyservice

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josexy/flowlens/backend/pkg/database"
	rewriteservice "github.com/josexy/flowlens/backend/services/rewrite_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/mitmproxy-go/v2"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
)

func newRewriteTestService(t *testing.T, rules ...rewriteservice.Rule) *ProxyService {
	t.Helper()
	db, err := database.OpenAt(filepath.Join(t.TempDir(), "rewrite.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := newTestProxyService(t, nil)
	rs := rewriteservice.New(db)
	if err = rs.Load(); err != nil {
		t.Fatal(err)
	}
	state, err := rs.GetState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rules {
		state, err = rs.SaveRule(r, state.Revision)
		if err != nil {
			t.Fatal(err)
		}
		state, err = rs.SetRuleEnabled(state.Rules[len(state.Rules)-1].ID, true, state.Revision)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = rs.SetEnabled(true, state.Revision); err != nil {
		t.Fatal(err)
	}
	SetRewriteService(svc, rs)
	return svc
}
func rewriteTestRule(action string) rewriteservice.Rule {
	return rewriteservice.Rule{Name: action, Method: "ALL", URLPattern: "http://original.test/*", Action: rewriteservice.Action{Version: 1, Type: action, HostPolicy: "target", Body: rewriteservice.BodyAction{Mode: "none"}}}
}
func TestRewriteOrderedHeadersAndQuery(t *testing.T) {
	fields := []HTTPHeaderField{{"X-A", "one"}, {"X-B", ""}, {"x-a", "two"}, {"Set-Cookie", "a=1"}, {"Set-Cookie", "b=2"}}
	got := applyRewriteFields(fields, []rewriteservice.FieldOperation{{Operation: "set", Name: "x-a", Value: "three"}, {Operation: "add", Name: "X-B", Value: "end"}, {Operation: "delete", Name: "set-cookie"}})
	want := []HTTPHeaderField{{"x-a", "three"}, {"X-B", ""}, {"X-B", "end"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	query := "a=%2f&keep=%20&bare&a=%2F&A=upper&&empty="
	result := applyRewriteQuery(query, []rewriteservice.FieldOperation{{Operation: "set", Name: "a", Value: "a b"}, {Operation: "add", Name: "keep", Value: "+"}, {Operation: "delete", Name: "bare"}})
	if result != "a=a+b&keep=%20&A=upper&&empty=&keep=%2B" {
		t.Fatal(result)
	}
}
func TestRewriteOriginalMatchAndPhaseOrder(t *testing.T) {
	redirect := rewriteTestRule(rewriteservice.ActionRedirect)
	redirect.Action.TargetURL = "http://target.test/${1}"
	redirect.Action.HostPolicy = "preserve"
	request := rewriteTestRule(rewriteservice.ActionRequest)
	request.Action.Query = []rewriteservice.FieldOperation{{Operation: "add", Name: "added", Value: "yes"}}
	request.Action.Headers = []rewriteservice.FieldOperation{{Operation: "set", Name: "X-Rewritten", Value: "request"}}
	request.Action.Body = rewriteservice.BodyAction{Mode: "replace", Text: "new request"}
	response := rewriteTestRule(rewriteservice.ActionResponse)
	response.Action.Headers = []rewriteservice.FieldOperation{{Operation: "add", Name: "Set-Cookie", Value: "first=1"}, {Operation: "add", Name: "Set-Cookie", Value: "second=2"}}
	response.Action.Body = rewriteservice.BodyAction{Mode: "regex", Pattern: "original", Replacement: "new"}
	never := rewriteTestRule(rewriteservice.ActionRequest)
	never.URLPattern = "http://target.test/*"
	never.Action.Headers = []rewriteservice.FieldOperation{{Operation: "set", Name: "X-Rematched", Value: "unexpected"}}
	svc := newRewriteTestService(t, redirect, request, response, never)
	req, _ := http.NewRequest("POST", "http://original.test/path?q=%2f", strings.NewReader("original request"))
	req.Header.Set("Content-Type", "text/plain")
	resp, err := svc.httpInterceptor(nil)(newRawTCPMetadataContext(), req, mitmproxy.HTTPDelegatedInvokerFunc(func(sent *http.Request) (*http.Response, error) {
		if sent.URL.String() != "http://target.test/path?q=%2f&added=yes" || sent.Host != "original.test" || sent.Header.Get("X-Rewritten") != "request" || sent.Header.Get("X-Rematched") != "" {
			t.Fatalf("bad final request %+v", sent)
		}
		data, err := io.ReadAll(sent.Body)
		sent.Body.Close()
		if err != nil || string(data) != "new request" {
			t.Fatal(string(data), err)
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Proto: "HTTP/1.1", Header: http.Header{"Content-Type": {"text/plain"}, "Etag": {"old"}}, Body: io.NopCloser(strings.NewReader("original response")), ContentLength: 17, Request: sent}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(data) != "new response" || resp.Header.Get("ETag") != "" {
		t.Fatal(string(data), resp.Header, err)
	}
	if got := resp.Header.Values("Set-Cookie"); !reflect.DeepEqual(got, []string{"first=1", "second=2"}) {
		t.Fatal(got)
	}
	entries := svc.GetTraffic()
	if len(entries) != 1 || entries[0].URL != "http://target.test/path?q=%2f&added=yes" || len(entries[0].RewriteExecutions) != 3 || entries[0].ResponseMetricsSource != "downstream" {
		t.Fatalf("capture %+v", entries)
	}
	view, err := svc.GetTrafficBodyView(entries[0].ID)
	if err != nil || view.RequestBody != "new request" || view.ResponseBody != "new response" {
		t.Fatalf("body capture %+v %v", view, err)
	}
	if entries[0].Response.Metrics.State == HTTPMessageStateCompleted {
		t.Fatal("pre-reading marked downstream complete")
	}
}
func TestRewriteFailuresStopBeforeLaterActions(t *testing.T) {
	for _, action := range []string{rewriteservice.ActionRequest, rewriteservice.ActionResponse} {
		t.Run(action, func(t *testing.T) {
			invalid := rewriteTestRule(action)
			invalid.Action.Body = rewriteservice.BodyAction{Mode: "replace", Text: "changed"}
			later := rewriteTestRule(action)
			later.Name = "must not run"
			later.Action.Headers = []rewriteservice.FieldOperation{{Operation: "set", Name: "X-Later", Value: "unexpected"}}
			svc := newRewriteTestService(t, invalid, later)
			req, _ := http.NewRequest("POST", "http://original.test/invalid-body", strings.NewReader("\xff"))
			req.Header.Set("Content-Type", "text/plain")
			called := false
			resp, err := svc.httpInterceptor(nil)(newRawTCPMetadataContext(), req, mitmproxy.HTTPDelegatedInvokerFunc(func(req *http.Request) (*http.Response, error) {
				called = true
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/plain"}}, Body: io.NopCloser(strings.NewReader("\xff")), ContentLength: 1, Request: req}, nil
			}))
			if !errors.Is(err, mitmproxy.ErrDropHTTP) || resp != nil || called != (action == rewriteservice.ActionResponse) {
				t.Fatal(resp, err, called)
			}
			entry := svc.GetTraffic()[0]
			if entry.Error == nil || len(entry.RewriteExecutions) != 1 || entry.RewriteExecutions[0].Outcome != "failed" || !strings.Contains(entry.RewriteExecutions[0].Reason, "UTF-8") || entry.Response.Metrics.BodySize != -1 {
				t.Fatalf("failed rewrite capture %+v", entry)
			}
		})
	}
}
func TestRewriteInFlightKeepsSnapshot(t *testing.T) {
	rule := rewriteTestRule(rewriteservice.ActionResponse)
	rule.Action.Body = rewriteservice.BodyAction{Mode: "replace", Text: "old"}
	svc := newRewriteTestService(t, rule)
	invoke := func(update bool) string {
		req, _ := http.NewRequest("GET", "http://original.test/", nil)
		resp, err := svc.httpInterceptor(nil)(newRawTCPMetadataContext(), req, mitmproxy.HTTPDelegatedInvokerFunc(func(req *http.Request) (*http.Response, error) {
			if update {
				state, err := svc.rewriteService.GetState(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				r := state.Rules[0]
				r.Action.Body.Text = "new"
				if _, err := svc.rewriteService.SaveRule(r, state.Revision); err != nil {
					t.Fatal(err)
				}
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/plain"}}, Body: io.NopCloser(strings.NewReader("source")), ContentLength: 6, Request: req}, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if got := invoke(true); got != "old" {
		t.Fatal(got)
	}
	if got := invoke(false); got != "new" {
		t.Fatal(got)
	}
}
func TestRewriteBodyEncodingsLimitsAndSpoolCleanup(t *testing.T) {
	rule := rewriteTestRule(rewriteservice.ActionRequest)
	rule.Action.Body = rewriteservice.BodyAction{Mode: "replace", Text: "updated text"}
	svc := newRewriteTestService(t, rule)
	match := svc.rewriteService.Snapshot().Match("POST", "http://original.test/")[0]
	for _, encoding := range []string{"", "gzip", "deflate", "br", "zstd", "snappy", "gzip, br"} {
		t.Run(encoding, func(t *testing.T) {
			encoded, err := encodeDecodedBodyForContentEncoding([]byte("original text"), encoding)
			if err != nil {
				t.Fatal(err)
			}
			phase := newRewriteBodyPhase(context.Background(), nil, nil)
			defer phase.close()
			body, size, changed, err := phase.rewrite(io.NopCloser(bytes.NewReader(encoded)), int64(len(encoded)), http.Header{"Content-Type": {"text/plain"}, "Content-Encoding": {encoding}}, match)
			if err != nil || !changed {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(body)
			body.Close()
			if err != nil || size != int64(len(raw)) {
				t.Fatal(err, size)
			}
			decoded, err := getDecodedReader(bytes.NewReader(raw), encoding)
			if err != nil {
				t.Fatal(err)
			}
			text, _ := io.ReadAll(decoded)
			decoded.Close()
			if string(text) != "updated text" {
				t.Fatal(string(text))
			}
		})
	}
	big := strings.Repeat("a", 5<<20)
	match.Rule.Action.Body.Text = big
	phase := newRewriteBodyPhase(context.Background(), nil, nil)
	defer phase.close()
	body, _, _, err := phase.rewrite(io.NopCloser(strings.NewReader("old")), 3, http.Header{}, match)
	if err != nil {
		t.Fatal(err)
	}
	spool := body.(*rewriteSpoolBody)
	file := spool.ReadCloser.(*os.File)
	path := file.Name()
	if _, err = os.Stat(path); err != nil {
		t.Fatal(err)
	}
	body.Close()
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("spool survived %v", err)
	}
	match.Rule.Action.Body.Text = "x"
	for _, tt := range []struct{ data, contentType string }{{"abc", "application/octet-stream"}, {"data: abc\n\n", "text/event-stream"}, {"\xff", "text/plain"}, {"\x00", ""}, {strings.Repeat("a", rewriteservice.MaxBodyBytes+1), "text/plain"}} {
		phase := newRewriteBodyPhase(context.Background(), nil, nil)
		_, _, _, err := phase.rewrite(io.NopCloser(strings.NewReader(tt.data)), int64(len(tt.data)), http.Header{"Content-Type": {tt.contentType}}, match)
		phase.close()
		if err == nil {
			t.Fatal("invalid body accepted", tt.contentType)
		}
	}
}
func TestRewriteBodyCancellationAndNoEntityResponse(t *testing.T) {
	rule := rewriteTestRule(rewriteservice.ActionResponse)
	rule.Action.Body = rewriteservice.BodyAction{Mode: "replace", Text: "x"}
	svc := newRewriteTestService(t, rule)
	matches := svc.rewriteService.Snapshot().Match("GET", "http://original.test/")
	req, _ := http.NewRequest("GET", "http://original.test/", nil)
	for _, status := range []int{204, 304} {
		_, err := svc.rewriteResponse(context.Background(), req, &http.Response{StatusCode: status, Header: http.Header{}, Body: http.NoBody}, matches, nil)
		if !errors.Is(err, mitmproxy.ErrDropHTTP) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	defer writer.Close()
	phase := newRewriteBodyPhase(ctx, nil, nil)
	defer phase.close()
	done := make(chan error, 1)
	go func() { _, _, _, err := phase.rewrite(reader, -1, http.Header{}, matches[0]); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled body processing stuck")
	}
}

func TestRewriteRegexCancellationReleasesPhase(t *testing.T) {
	rule := rewriteTestRule(rewriteservice.ActionResponse)
	rule.Action.Body = rewriteservice.BodyAction{Mode: "regex", Pattern: `[ab]{1000}z`, Replacement: "x"}
	svc := newRewriteTestService(t, rule)
	match := svc.rewriteService.Snapshot().Match("GET", "http://original.test/")[0]
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	phase := newRewriteBodyPhase(ctx, nil, nil)
	start := time.Now()
	body, _, _, err := phase.rewrite(io.NopCloser(strings.NewReader(strings.Repeat("a", 1<<20))), 1<<20, http.Header{}, match)
	closeRewriteBody(body)
	phase.close()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("regex did not propagate phase cancellation: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("regex held its phase after cancellation: %v", elapsed)
	}
	if len(rewriteBodySlots) != 0 {
		t.Fatal("canceled regex retained a body processing slot")
	}
}
func TestRewrittenResponseMetricsAwaitSendAndRejectLateCallbacks(t *testing.T) {
	svc := newTestProxyService(t, nil)
	entry := svc.newTrafficEntry(TrafficEntry{Type: "http", Request: &HTTPMessage{}, Response: &HTTPMessage{}})
	x := newCaptureExchange(svc, context.Background(), entry)
	x.deferResponseTiming = true
	now := time.Now()
	x.observeHTTPExchangeTiming(mitmproxy.HTTPExchangeTimingEvent{Phase: mitmproxy.HTTPExchangeResponseEnded, Timestamp: now, Complete: true})
	x.resolveRewriteResponseTiming(true)
	x.responseBodyFinished(100, true, nil)
	if entry.Response.Metrics != nil && entry.Response.Metrics.State == HTTPMessageStateCompleted {
		t.Fatal("premature completion")
	}
	x.observeRewrittenResponseSent(mitmproxy.HTTPResponseSendResult{StartedAt: now, EndedAt: now.Add(time.Millisecond), BodyBytes: 7})
	if entry.Response.Metrics.State != HTTPMessageStateCompleted || entry.Response.Metrics.BodySize != 7 {
		t.Fatal(entry.Response.Metrics)
	}
	svc.captureLifecycleMu.Lock()
	svc.captureGeneration++
	svc.captureLifecycleMu.Unlock()
	svc.trafficEntries.Clear()
	x.observeRewrittenResponseSent(mitmproxy.HTTPResponseSendResult{Err: io.ErrUnexpectedEOF})
	if len(svc.GetTraffic()) != 0 {
		t.Fatal("late callback resurrected capture")
	}
}

func startRewriteCaptureProxy(t *testing.T, svc *ProxyService) *http.Client {
	t.Helper()
	dir := t.TempDir()
	cfg := &settingservice.ProxyConfig{CACertPath: filepath.Join(dir, "ca.crt"), CAKeyPath: filepath.Join(dir, "ca.key"), DisableProxy: true}
	if err := svc.settingService.Update(&settingservice.Settings{ProxyConfig: cfg}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.settingService.GenerateCurrentCACertificate(settingservice.GenerateCACertificateRequest{CommonName: "Rewrite test CA", ValidDays: 1}); err != nil {
		t.Fatal(err)
	}
	handler, err := mitmproxy.NewMitmProxyHandler(mitmproxy.WithCACertPath(cfg.CACertPath), mitmproxy.WithCAKeyPath(cfg.CAKeyPath), mitmproxy.WithHTTPInterceptor(svc.httpInterceptor(cfg)))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	proxyURL, _ := url.Parse("http://" + listener.Addr().String())
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableCompression: true}
	t.Cleanup(func() { transport.CloseIdleConnections(); server.Close(); handler.Cleanup() })
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}
func TestRewriteRealProxyFinalCaptureAndExport(t *testing.T) {
	var originalHits, targetHits atomic.Int32
	original := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { originalHits.Add(1); w.WriteHeader(500) }))
	defer original.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		data, err := io.ReadAll(r.Body)
		if err != nil || string(data) != "final request" || r.URL.Path != "/rewritten/path" {
			t.Errorf("final upstream %s %q %v", r.URL, string(data), err)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Trailer", "Digest")
		encoded, err := encodeDecodedBodyForContentEncoding([]byte("origin response"), "gzip")
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = w.Write(encoded)
		w.Header().Set("Digest", "old-body-digest")
	}))
	defer target.Close()
	redirect := rewriteTestRule(rewriteservice.ActionRedirect)
	redirect.URLPattern = original.URL + "/*"
	redirect.Action.TargetURL = target.URL + "/rewritten/${1}"
	request := rewriteTestRule(rewriteservice.ActionRequest)
	request.URLPattern = redirect.URLPattern
	request.Action.Body = rewriteservice.BodyAction{Mode: "replace", Text: "final request"}
	response := rewriteTestRule(rewriteservice.ActionResponse)
	response.URLPattern = redirect.URLPattern
	response.Action.Body = rewriteservice.BodyAction{Mode: "regex", Pattern: "origin", Replacement: "rewritten"}
	svc := newRewriteTestService(t, redirect, request, response)
	client := startRewriteCaptureProxy(t, svc)
	req, _ := http.NewRequest("POST", original.URL+"/path", strings.NewReader("original request"))
	req.Header.Set("Content-Type", "text/plain")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := getDecodedReader(bytes.NewReader(raw), "gzip")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(decoded)
	decoded.Close()
	if err != nil || string(data) != "rewritten response" {
		t.Fatal(string(data), err)
	}
	if len(resp.Trailer) != 0 || resp.Header.Get("Digest") != "" {
		t.Fatal("stale digest sent", resp.Trailer)
	}
	var entry *TrafficEntry
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entries := svc.GetTraffic()
		if len(entries) > 0 {
			entry = entries[0]
			if entry.Response != nil && entry.Response.Metrics != nil && entry.Response.Metrics.State == HTTPMessageStateCompleted {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	if entry == nil || entry.Response.Metrics.State != HTTPMessageStateCompleted {
		t.Fatalf("no final capture %+v", entry)
	}
	if originalHits.Load() != 0 || targetHits.Load() != 1 || len(entry.Response.TrailerFields) != 0 || entry.Response.Metrics.BodySize != int64(len(raw)) {
		t.Fatalf("wire/capture mismatch %+v trailers=%v hits=%d/%d", entry.Response.Metrics, entry.Response.TrailerFields, originalHits.Load(), targetHits.Load())
	}
	if entry.Request.Metrics.HeaderSize != logicalHTTPRequestHeaderSize(entry) || entry.Response.Metrics.HeaderSize != logicalHTTPResponseHeaderSize(entry) {
		t.Fatal("final raw size mismatch")
	}
	verifyRewriteFinalExports(t, svc, entry)
}

func TestRewriteHTTP1StalledUploadTimesOut(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer upstream.Close()
	rule := rewriteTestRule(rewriteservice.ActionRequest)
	rule.URLPattern = upstream.URL + "/*"
	rule.Action.Body = rewriteservice.BodyAction{Mode: "replace", Text: "changed"}
	svc := newRewriteTestService(t, rule)
	client := startRewriteCaptureProxy(t, svc)
	req, _ := http.NewRequest("POST", upstream.URL+"/stalled", nil)
	proxyURL, _ := client.Transport.(*http.Transport).Proxy(req)
	conn, err := net.Dial("tcp", proxyURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(rewriteBodyTimeout + 3*time.Second))
	_, err = fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: text/plain\r\nContent-Length: 100\r\n\r\nx", req.URL, req.URL.Host)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(conn)
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("rewrite timeout did not interrupt blocked HTTP/1 body read")
	}
	if len(data) != 0 || hits.Load() != 0 {
		t.Fatalf("failed rewrite leaked data: %q, upstream hits=%d", data, hits.Load())
	}
	entries := svc.GetTraffic()
	if len(entries) != 1 || entries[0].Error == nil || entries[0].Request.Metrics.State == HTTPMessageStateCompleted {
		t.Fatalf("timeout capture: %+v", entries)
	}
	if len(rewriteBodySlots) != 0 {
		t.Fatal("timed out rewrite retained body slot")
	}
}

func TestRewriteSameOriginReusesUpstreamConnection(t *testing.T) {
	for _, action := range []string{rewriteservice.ActionRequest, rewriteservice.ActionRedirect} {
		t.Run(action, func(t *testing.T) {
			var connections atomic.Int32
			origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("rewritten") != "yes" {
					t.Errorf("query was not rewritten: %s", r.URL)
				}
				_, _ = io.WriteString(w, "ok")
			}))
			origin.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					connections.Add(1)
				}
			}
			origin.Start()
			defer origin.Close()
			rule := rewriteTestRule(action)
			rule.URLPattern = origin.URL + "/*"
			if action == rewriteservice.ActionRedirect {
				rule.Action.TargetURL = origin.URL + "/changed?rewritten=yes"
			} else {
				rule.Action.Query = []rewriteservice.FieldOperation{{Operation: "add", Name: "rewritten", Value: "yes"}}
			}
			client := startRewriteCaptureProxy(t, newRewriteTestService(t, rule))
			for range 5 {
				resp, err := client.Get(origin.URL + "/item")
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if got := connections.Load(); got != 1 {
				t.Fatalf("five same-origin rewrites used %d upstream connections; want 1", got)
			}
		})
	}
}

func TestRewriteBodySlotWaitIncludesDeadline(t *testing.T) {
	for range cap(rewriteBodySlots) {
		phase := newRewriteBodyPhase(context.Background(), nil, nil)
		if err := phase.acquire(); err != nil {
			t.Fatal(err)
		}
		defer phase.close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	waiter := newRewriteBodyPhase(ctx, nil, nil)
	defer waiter.close()
	if err := waiter.acquire(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if waiter.acquired {
		t.Fatal("fifth rewrite acquired a slot")
	}
}

func verifyRewriteFinalExports(t *testing.T, svc *ProxyService, entry *TrafficEntry) {
	t.Helper()
	har, _ := decodeSingleHAR(t, svc.currentHARExportEntry(entry))
	if har.Request.URL != entry.URL || har.Request.PostData == nil || har.Request.PostData.Text != "final request" || (har.Response.Content.Text == nil || *har.Response.Content.Text != "rewritten response") || har.Response.BodySize != entry.Response.Metrics.BodySize {
		t.Fatalf("HAR did not preserve final content: %+v", har)
	}
	file, err := os.CreateTemp(t.TempDir(), "rewrite-*.hbin")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	body, err := svc.getTrafficBodyViewInner(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if body.RequestBodyReader != nil {
			body.RequestBodyReader.Close()
		}
		if body.ResponseBodyReader != nil {
			body.ResponseBodyReader.Close()
		}
	}()
	var index bytes.Buffer
	if err := hbinWriteEntry(&index, file, entry, &body); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	headerOffset := binary.BigEndian.Uint32(index.Bytes()[8:12])
	bodyOffset := binary.BigEndian.Uint32(index.Bytes()[12:16])
	decoded, err := DecodeTrafficEntry(bytes.NewReader(data[headerOffset:]))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.URL != entry.URL || len(decoded.RewriteExecutions) != 0 || decoded.ResponseMetricsSource != "" || decoded.Response.Metrics.BodySize != entry.Response.Metrics.BodySize {
		t.Fatalf("HBIN metadata: %+v", decoded)
	}
	view, err := DecodeTrafficBody(bytes.NewReader(data[bodyOffset:]))
	if err != nil || view.RequestBody != "final request" || view.ResponseBody != "rewritten response" {
		t.Fatalf("HBIN body: %+v %v", view, err)
	}
	path := filepath.Join(t.TempDir(), "rewrite.csv")
	if _, err := svc.ExportTraffic(context.Background(), TrafficExportRequest{Kind: TrafficExportCSV, Path: path, TargetType: "file", TrafficIDs: []uint64{entry.ID}}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))).ReadAll()
	if err != nil || len(rows) != 2 {
		t.Fatalf("CSV: %v %v", rows, err)
	}
	values := make(map[string]string)
	for i, column := range rows[0] {
		values[column] = rows[1][i]
	}
	if values["host"] != entry.Host || values["path"] != entry.Path {
		t.Fatalf("CSV final URL: %v", values)
	}
}

func TestRewriteSummaryRetainsTerminalFailureWithinBound(t *testing.T) {
	var summaries []RewriteExecution
	for range maxRewriteExecutions + 10 {
		summaries = appendRewriteExecutions(summaries, RewriteExecution{Outcome: "success"})
	}
	match := rewriteservice.Match{Rule: rewriteTestRule(rewriteservice.ActionRequest)}
	summaries = appendRewriteExecutions(summaries, rewriteSummary(match, errors.New(strings.Repeat("x", 1024))))
	if len(summaries) != maxRewriteExecutions || summaries[len(summaries)-1].Outcome != "failed" || len(summaries[len(summaries)-1].Reason) != 512 {
		t.Fatal("unbounded summary or lost failure")
	}
}

func TestRewriteNoOpResponsePreservesHeaderSize(t *testing.T) {
	svc := newTestProxyService(t, nil)
	entry := svc.newTrafficEntry(TrafficEntry{Request: &HTTPMessage{}, Response: &HTTPMessage{}})
	x := newCaptureExchange(svc, context.Background(), entry)
	x.deferResponseTiming = true
	x.observeHTTPExchangeTiming(mitmproxy.HTTPExchangeTimingEvent{Phase: mitmproxy.HTTPExchangeResponseStarted, Timestamp: time.Now()})
	x.resolveRewriteResponseTiming(false)
	x.installRewrittenResponse(&http.Response{StatusCode: 200, Status: "200 OK", Proto: "HTTP/1.1"}, rewriteResult{fields: []HTTPHeaderField{{Name: "Content-Length", Value: "0"}}})
	if entry.Response.Metrics.HeaderSize != logicalHTTPResponseHeaderSize(entry) {
		t.Fatal(entry.Response.Metrics)
	}
}
