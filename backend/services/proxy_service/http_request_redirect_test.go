package proxyservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
)

func TestSendHTTPRequestRedirectLimit(t *testing.T) {
	const fingerprint = "1:65536;3:1000;4:6291456;6:262144|15663105|0|m,a,s,p"
	for _, transport := range []struct {
		name        string
		protocol    SendRequestProtocol
		fingerprint string
	}{
		{name: "auto", protocol: SendRequestProtocolAuto},
		{name: "http1", protocol: SendRequestProtocolHTTP1},
		{name: "http2", protocol: SendRequestProtocolHTTP2},
		{name: "http2-fingerprint", protocol: SendRequestProtocolHTTP2, fingerprint: fingerprint},
		{name: "auto-fingerprint", protocol: SendRequestProtocolAuto, fingerprint: fingerprint},
	} {
		t.Run(transport.name, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				limit int
				last  int
				loop  bool
			}{
				{name: "default-off", last: 2},
				{name: "one-hop", limit: 1, last: 2},
				{name: "finish-at-limit", limit: 2, last: 2},
				{name: "finish-before-limit", limit: 5, last: 2},
				{name: "more-than-ten", limit: 12, last: 13},
				{name: "loop", limit: 3, loop: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var requests atomic.Int32
					body := func(hop int) string { return fmt.Sprintf("hop %d\n%s", hop, strings.Repeat("body", 2048)) }
					server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						hop := int(requests.Add(1)) - 1
						if transport.fingerprint != "" {
							got, ok := http.RequestFingerprint(req)
							if !ok || got.String() != fingerprint {
								t.Errorf("hop %d lost fingerprint: %v", hop, got)
							}
						}
						if hop < tc.last || tc.loop {
							location := "/" + strconv.Itoa(hop+1)
							if tc.loop {
								location = "/0"
							}
							w.Header().Set("Location", location)
							w.WriteHeader(http.StatusFound)
						}
						_, _ = io.WriteString(w, body(hop))
					}))
					server.EnableHTTP2 = transport.protocol != SendRequestProtocolHTTP1
					server.StartTLS()
					defer server.Close()
					svc := newTestProxyService(t, &settingservice.ProxyConfig{})
					response, err := svc.SendHTTPRequest(context.Background(), SendRequestConfig{
						ProxyMode: SendRequestProxyModeNone, Protocol: transport.protocol,
						HTTP2Fingerprint: transport.fingerprint, SkipVerifyTLS: true, MaxRedirects: tc.limit,
					}, http.MethodGet, server.URL+"/0", nil, SendRequestBody{BodyType: SendRequestBodyTypeNone})
					if err != nil {
						t.Fatal(err)
					}
					lastHop := min(tc.limit, tc.last)
					if tc.loop {
						lastHop = tc.limit
					}
					wantStatus, wantLocation := http.StatusOK, ""
					if lastHop < tc.last || tc.loop {
						wantStatus, wantLocation = http.StatusFound, "/"+strconv.Itoa(lastHop+1)
						if tc.loop {
							wantLocation = "/0"
						}
					}
					if requests.Load() != int32(lastHop+1) || response.StatusCode != wantStatus || response.Body != body(lastHop) {
						t.Fatalf("requests=%d status=%d body bytes=%d; want requests=%d status=%d and complete hop %d body",
							requests.Load(), response.StatusCode, len(response.Body), lastHop+1, wantStatus, lastHop)
					}
					if got := firstHeaderFieldValue(response.HeaderFields, "Location"); got != wantLocation {
						t.Fatalf("Location=%q, want %q", got, wantLocation)
					}
				})
			}
		})
	}
}

func TestSendHTTPRequestRedirectMethodsAndFileCleanup(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, limit := range []int{0, 1, 2} {
			t.Run(fmt.Sprintf("%d/limit-%d", status, limit), func(t *testing.T) {
				const payload = "redirect file body"
				path := filepath.Join(t.TempDir(), "body.txt")
				if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
					t.Fatal(err)
				}
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					hop := int(requests.Add(1)) - 1
					wantMethod, wantBody := http.MethodPost, payload
					if hop > 0 && status < 307 {
						wantMethod, wantBody = http.MethodGet, ""
					}
					gotBody, err := io.ReadAll(req.Body)
					if err != nil || req.Method != wantMethod || string(gotBody) != wantBody {
						t.Errorf("hop %d: method=%q body=%q err=%v; want %q %q", hop, req.Method, gotBody, err, wantMethod, wantBody)
					}
					if hop < 2 {
						w.Header().Set("Location", "/next")
						w.WriteHeader(status)
					}
					_, _ = io.WriteString(w, "response body")
				}))
				defer server.Close()
				svc := newTestProxyService(t, &settingservice.ProxyConfig{})
				response, err := svc.SendHTTPRequest(context.Background(), SendRequestConfig{
					ProxyMode: SendRequestProxyModeNone, MaxRedirects: limit,
				}, http.MethodPost, server.URL, nil, SendRequestBody{
					BodyType: SendRequestBodyTypeFile, File: &SendRequestFile{Path: path},
				})
				if err != nil {
					t.Fatal(err)
				}
				wantStatus := status
				if limit == 2 {
					wantStatus = http.StatusOK
				}
				if requests.Load() != int32(limit+1) || response.StatusCode != wantStatus || response.Body != "response body" {
					t.Fatalf("requests=%d response=%+v", requests.Load(), response)
				}
				// On Windows this also detects a replay file left open at the limit.
				if err := os.Remove(path); err != nil {
					t.Fatalf("request body file is still open: %v", err)
				}
			})
		}
	}
}

func TestSendHTTPRequestRejectsNegativeRedirectLimitBeforeHooks(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	runner := &fakeHTTPRequestPluginRunner{session: &fakeHTTPRequestPluginSession{}}
	svc.SetHTTPRequestPluginRunner(runner)
	_, err := svc.SendHTTPRequest(context.Background(), SendRequestConfig{
		ProxyMode: SendRequestProxyModeNone, MaxRedirects: -1,
	}, http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone})
	if err == nil || !strings.Contains(err.Error(), "max redirects") || requests.Load() != 0 || runner.beginCount.Load() != 0 {
		t.Fatalf("err=%v requests=%d plugin starts=%d", err, requests.Load(), runner.beginCount.Load())
	}
}

func TestSendHTTPRequestRedirectLimitRunsHooksOnce(t *testing.T) {
	for _, limit := range []int{0, 1, 3} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hop := requests.Add(1) - 1
				w.Header().Set("Location", "/next")
				w.WriteHeader(http.StatusFound)
				_, _ = fmt.Fprintf(w, "hop %d", hop)
			}))
			defer server.Close()
			svc := newTestProxyService(t, &settingservice.ProxyConfig{})
			session := &fakeHTTPRequestPluginSession{responseHook: func(response HTTPRequestPluginResponse) HTTPRequestPluginResponseResult {
				if response.StatusCode != http.StatusFound || string(response.Body) != fmt.Sprintf("hop %d", limit) {
					t.Errorf("response hook did not receive last response: %+v", response)
				}
				return HTTPRequestPluginResponseResult{Response: response}
			}}
			runner := &fakeHTTPRequestPluginRunner{session: session}
			svc.SetHTTPRequestPluginRunner(runner)
			_, err := svc.SendHTTPRequest(context.Background(), SendRequestConfig{
				ProxyMode: SendRequestProxyModeNone, MaxRedirects: limit,
			}, http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone})
			if err != nil {
				t.Fatal(err)
			}
			if runner.beginCount.Load() != 1 || session.requestCount.Load() != 1 || session.responseCount.Load() != 1 || !session.closed.Load() {
				t.Fatalf("plugin starts=%d requests=%d responses=%d closed=%v", runner.beginCount.Load(), session.requestCount.Load(), session.responseCount.Load(), session.closed.Load())
			}
		})
	}
}

func TestSendHTTPRequestRedirectCancellation(t *testing.T) {
	arrived := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/" {
			w.Header().Set("Location", "/wait")
			w.WriteHeader(http.StatusFound)
			return
		}
		close(arrived)
		<-req.Context().Done()
	}))
	defer server.Close()
	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := svc.SendHTTPRequest(ctx, SendRequestConfig{
			ProxyMode: SendRequestProxyModeNone, MaxRedirects: 1, TimeoutMs: 5000,
		}, http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone})
		done <- err
	}()
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("redirect target not reached")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("cancellation error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("redirect request did not cancel")
	}
}

func TestSendHTTPRequestTimeoutCoversRedirectChain(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		timer := time.NewTimer(40 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-req.Context().Done():
			return
		case <-timer.C:
		}
		w.Header().Set("Location", "/next")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	_, err := svc.SendHTTPRequest(context.Background(), SendRequestConfig{
		ProxyMode: SendRequestProxyModeNone, MaxRedirects: 20, TimeoutMs: 150,
	}, http.MethodGet, server.URL, nil, SendRequestBody{BodyType: SendRequestBodyTypeNone})
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() || requests.Load() >= 21 {
		t.Fatalf("chain should time out before reaching the redirect limit: requests=%d err=%v", requests.Load(), err)
	}
}
