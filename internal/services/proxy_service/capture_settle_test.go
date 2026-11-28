package proxyservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/netip"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	processattribution "github.com/josexy/flowlens/internal/pkg/process_attribution"
	rewriteservice "github.com/josexy/flowlens/internal/services/rewrite_service"
	"github.com/josexy/mitmproxy-go/v2"
	"github.com/josexy/mitmproxy-go/v2/metadata"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
)

func newestLifecycle(t *testing.T, svc *ProxyService) (uint64, *trafficEntryLifecycle) {
	t.Helper()
	entries := svc.trafficEntries.Values()
	if len(entries) == 0 {
		t.Fatal("no traffic entry was captured")
	}
	entry := entries[len(entries)-1]
	if entry.lifecycle == nil {
		t.Fatalf("entry %d has no lifecycle", entry.ID)
	}
	return entry.ID, entry.lifecycle
}

func requireSettled(t *testing.T, lifecycle *trafficEntryLifecycle) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !lifecycle.settled() {
		if time.Now().After(deadline) {
			t.Fatalf("entry still has %d pending producers", lifecycle.pending.Load())
		}
		time.Sleep(time.Millisecond)
	}
	if got := lifecycle.pending.Load(); got != 0 {
		t.Fatalf("pending = %d, want 0", got)
	}
}

func requireUnsettled(t *testing.T, lifecycle *trafficEntryLifecycle) {
	t.Helper()
	if lifecycle.settled() {
		t.Fatal("entry settled while a producer is still running")
	}
}

// consumingInvoker reads and closes the request body, as the transport does,
// and then answers with respond.
func consumingInvoker(respond func(*http.Request) (*http.Response, error)) mitmproxy.HTTPDelegatedInvoker {
	return mitmproxy.HTTPDelegatedInvokerFunc(func(req *http.Request) (*http.Response, error) {
		if req.Body != nil {
			_, _ = io.Copy(io.Discard, req.Body)
			_ = req.Body.Close()
		}
		return respond(req)
	})
}

func textResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK, Proto: "HTTP/1.1", Request: req,
		Header:        http.Header{"Content-Type": {"text/plain"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func interceptPost(t *testing.T, svc *ProxyService, invoker mitmproxy.HTTPDelegatedInvoker) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://original.test/upload", strings.NewReader("request body"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	return svc.httpInterceptor(nil)(newRawTCPMetadataContext(), req, invoker)
}

func TestHTTPExchangeSettlesAfterTheResponseBody(t *testing.T) {
	svc := newTestProxyService(t, nil)
	resp, err := interceptPost(t, svc, consumingInvoker(func(req *http.Request) (*http.Response, error) {
		return textResponse(req, "response body"), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, lifecycle := newestLifecycle(t, svc)
	requireUnsettled(t, lifecycle)
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	requireSettled(t, lifecycle)
}

func TestHTTPExchangeSettlesOnUpstreamFailure(t *testing.T) {
	svc := newTestProxyService(t, nil)
	_, err := interceptPost(t, svc, consumingInvoker(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("upstream refused")
	}))
	if err == nil {
		t.Fatal("interceptor returned no error")
	}
	_, lifecycle := newestLifecycle(t, svc)
	requireSettled(t, lifecycle)
}

func TestHTTPExchangeSettlesWhenTheClientCancels(t *testing.T) {
	svc := newTestProxyService(t, nil)
	resp, err := interceptPost(t, svc, consumingInvoker(func(req *http.Request) (*http.Response, error) {
		return textResponse(req, strings.Repeat("partial ", 64<<10)), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, lifecycle := newestLifecycle(t, svc)
	if _, err := resp.Body.Read(make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}
	requireUnsettled(t, lifecycle)
	_ = resp.Body.Close()
	requireSettled(t, lifecycle)
}

func TestHTTPExchangeSettlesWithoutAResponseBody(t *testing.T) {
	svc := newTestProxyService(t, nil)
	_, err := interceptPost(t, svc, consumingInvoker(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Proto: "HTTP/1.1", Request: req, Body: http.NoBody}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, lifecycle := newestLifecycle(t, svc)
	requireSettled(t, lifecycle)
}

func TestHTTPExchangeSettlesWhenARewriteAborts(t *testing.T) {
	for _, action := range []string{rewriteservice.ActionRequest, rewriteservice.ActionResponse} {
		t.Run(action, func(t *testing.T) {
			invalid := rewriteTestRule(action)
			invalid.Action.Body = rewriteservice.BodyAction{Mode: "replace", Text: "changed"}
			svc := newRewriteTestService(t, invalid)
			req, _ := http.NewRequest(http.MethodPost, "http://original.test/invalid-body", strings.NewReader("\xff"))
			req.Header.Set("Content-Type", "text/plain")
			_, err := svc.httpInterceptor(nil)(newRawTCPMetadataContext(), req, consumingInvoker(func(req *http.Request) (*http.Response, error) {
				return textResponse(req, "\xff"), nil
			}))
			if !errors.Is(err, mitmproxy.ErrDropHTTP) {
				t.Fatalf("interceptor error = %v, want a dropped exchange", err)
			}
			_, lifecycle := newestLifecycle(t, svc)
			requireSettled(t, lifecycle)
		})
	}
}

func TestHTTPExchangeSettlesAfterDeleteOrClearMidExchange(t *testing.T) {
	for _, remove := range []string{"delete", "clear"} {
		t.Run(remove, func(t *testing.T) {
			svc := newTestProxyService(t, nil)
			resp, err := interceptPost(t, svc, consumingInvoker(func(req *http.Request) (*http.Response, error) {
				return textResponse(req, strings.Repeat("body ", 8<<10)), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			id, lifecycle := newestLifecycle(t, svc)
			if _, err := resp.Body.Read(make([]byte, 512)); err != nil {
				t.Fatal(err)
			}
			if remove == "delete" {
				svc.DeleteTraffic([]uint64{id})
			} else if err := svc.ClearTraffic(); err != nil {
				t.Fatal(err)
			}
			requireUnsettled(t, lifecycle)
			if _, err := io.ReadAll(resp.Body); err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			requireSettled(t, lifecycle)
		})
	}
}

func TestWebSocketEntrySettlesWhenItsConnectionCloses(t *testing.T) {
	svc := newTestProxyService(t, nil)
	req, err := http.NewRequest(http.MethodGet, "ws://settle.test/socket", nil)
	if err != nil {
		t.Fatal(err)
	}
	watcher := make(testCaptureFrameWatcher, 1)
	watcher <- newTestCaptureFrame(mitmproxy.Receive, 1, []byte("hello"))
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.websocketInterceptor(nil)(newRawTCPMetadataContext(), req,
			&http.Response{StatusCode: http.StatusSwitchingProtocols, Proto: "HTTP/1.1"}, watcher)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for svc.trafficEntries.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	_, lifecycle := newestLifecycle(t, svc)
	requireUnsettled(t, lifecycle)
	close(watcher)
	<-done
	requireSettled(t, lifecycle)
}

func TestProcessLookupHoldsItsEntryUntilFinal(t *testing.T) {
	settings := configureProcessAttributionSetting(t, true)
	provider := &proxyAttributionTestProvider{lookup: func(ctx context.Context, _ processattribution.EndpointTuple) processattribution.Result {
		<-ctx.Done()
		return processattribution.Result{Status: processattribution.StatusNotFound, Reason: "timeout"}
	}}
	manager := processattribution.NewManager(provider, nil, processattribution.Options{
		Workers: 1, QueueSize: 8, LookupTimeout: 50 * time.Millisecond, IconWorkers: 1, IconQueueSize: 8,
	})
	t.Cleanup(manager.Close)
	svc := newProxyAttributionTestService(manager, nil)
	svc.settingService = settings
	conn := svc.attributeConnection(newAddressedTestConn(t, "127.0.0.1:43190", "127.0.0.1:8080"))
	t.Cleanup(func() { _ = conn.Close() })

	md := metadata.NewMD()
	md.SetLocalConnectionAddrInfo(metadata.ConnectionAddrInfo{
		SourceAddr:      netip.MustParseAddrPort("127.0.0.1:43190"),
		DestinationAddr: netip.MustParseAddrPort("127.0.0.1:8080"),
	})
	ctx := metadata.AppendToContext(context.Background(), md)
	timedOut := svc.registerTrafficEntry(ctx, TrafficEntry{Type: "tcp"})
	deleted := svc.registerTrafficEntry(ctx, TrafficEntry{Type: "tcp"})
	if timedOut.Metadata == nil || timedOut.Metadata.Process == nil {
		t.Fatal("raw TCP entry has no process lookup")
	}
	requireUnsettled(t, timedOut.lifecycle)
	requireUnsettled(t, deleted.lifecycle)

	// Deactivating the binding ends its hold before the lookup is final.
	svc.deleteTrafficEntry(deleted.ID)
	requireSettled(t, deleted.lifecycle)
	// The lookup times out, which is final.
	requireSettled(t, timedOut.lifecycle)
	stored, ok := svc.trafficEntries.Get(timedOut.ID)
	if !ok || stored.Metadata.Process == nil || stored.Metadata.Process.Status != ProcessStatusNotFound {
		t.Fatalf("final process status was not applied before settling: %+v", stored)
	}
}

func TestRealProxyExchangesSettle(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(bytes.Repeat([]byte("upstream "), 4096))
	}))
	t.Cleanup(upstream.Close)
	header := rewriteTestRule(rewriteservice.ActionResponse)
	header.URLPattern = upstream.URL + "/rewritten*"
	header.Action.Headers = []rewriteservice.FieldOperation{{Operation: "set", Name: "X-Rewritten", Value: "yes"}}
	svc := newRewriteTestService(t, header)
	client := startRewriteCaptureProxy(t, svc)

	for _, path := range []string{"/plain", "/rewritten"} {
		resp, err := client.Post(upstream.URL+path, "text/plain", strings.NewReader("request body"))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if path == "/rewritten" && resp.Header.Get("X-Rewritten") != "yes" {
			t.Fatal("rewrite rule did not apply")
		}
		_, lifecycle := newestLifecycle(t, svc)
		requireSettled(t, lifecycle)
	}
}

func TestSpillerWakesWhenResponsesFinishWithoutNewTraffic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc := newSpillTestService(t)
		ctx, cancel := context.WithCancel(context.Background())
		svc.startCaptureSpiller(ctx)
		defer func() { cancel(); svc.waitCaptureSpiller() }()
		const count = captureMemoryWindow + 1000
		bodies := make([]io.ReadCloser, count)
		for i := range bodies {
			req, err := http.NewRequest(http.MethodGet, "http://slow.test/", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := svc.httpInterceptor(nil)(newRawTCPMetadataContext(), req, consumingInvoker(func(req *http.Request) (*http.Response, error) {
				return textResponse(req, "response"), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			bodies[i] = resp.Body
		}
		// All insertion wakes are consumed while every response is still open.
		synctest.Wait()
		if svc.captureSpillStore().Len() != 0 {
			t.Fatal("unfinished responses were spilled")
		}
		for _, body := range bodies {
			if _, err := io.Copy(io.Discard, body); err != nil {
				t.Fatal(err)
			}
			if err := body.Close(); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		if got := svc.trafficEntries.Len(); got != captureMemoryWindow {
			t.Fatalf("finished responses left %d entries in memory, want %d", got, captureMemoryWindow)
		}
		requireConsistentCapture(t, svc)
	})
}

func TestSpillerWakesForOneOldBodyBelowTheBatchThreshold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc := newSpillTestService(t)
		ctx, cancel := context.WithCancel(context.Background())
		svc.startCaptureSpiller(ctx)
		defer func() { cancel(); svc.waitCaptureSpiller() }()
		entry := svc.newTrafficEntry(TrafficEntry{Type: "http", URL: "http://slow.test/"})
		body := svc.newCaptureStreamBodyReader(io.NopCloser(strings.NewReader("last body")), entry, "", false)
		svc.storeTrafficEntry(entry)
		addSettledEntries(t, svc, captureMemoryWindow+captureSpillBatch+1)
		synctest.Wait()
		if got := svc.trafficEntries.Len(); got != captureMemoryWindow+1 {
			t.Fatalf("entries before EOF = %d", got)
		}
		if _, err := io.Copy(io.Discard, body); err != nil {
			t.Fatal(err)
		}
		if err := body.Close(); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if !svc.captureSpillStore().Has(entry.ID) || svc.trafficEntries.Len() != captureMemoryWindow {
			t.Fatal("the last old body was not spilled after EOF without a new entry")
		}
		requireConsistentCapture(t, svc)
	})
}
