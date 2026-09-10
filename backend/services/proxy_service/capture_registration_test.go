package proxyservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/josexy/mitmproxy-go/v2"
	http "github.com/josexy/xhttp"
)

type closedCaptureFrameWatcher struct{}

func (closedCaptureFrameWatcher) Receive() <-chan mitmproxy.WsFrame {
	frames := make(chan mitmproxy.WsFrame)
	close(frames)
	return frames
}

func TestCaptureRegistrationOrdersConcurrentHTTPWebSocketAndTCP(t *testing.T) {
	svc := newTestProxyService(t, nil)
	const count = 192
	batches := make(chan frontendEventBatch, 1)
	svc.frontendEventBatcher = newFrontendEventBatcher(frontendEventBatcherOptions{
		FlushInterval: time.Hour, MaxPendingEvents: count * 4, MaxPendingBytes: 4 << 20,
	}, func(batch frontendEventBatch) { batches <- batch })
	t.Cleanup(svc.frontendEventBatcher.Close)
	var wg sync.WaitGroup
	for i := range count {
		wg.Go(func() {
			ctx := newRawTCPMetadataContext()
			switch i % 3 {
			case 0:
				req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://capture.test/%d", i), nil)
				if err != nil {
					t.Error(err)
					return
				}
				_, err = svc.httpInterceptor(nil)(ctx, req, mitmproxy.HTTPDelegatedInvokerFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusNoContent, Proto: "HTTP/1.1", Body: http.NoBody}, nil
				}))
				if err != nil {
					t.Error(err)
				}
			case 1:
				req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("ws://capture.test/%d", i), nil)
				if err != nil {
					t.Error(err)
					return
				}
				svc.websocketInterceptor(nil)(ctx, req,
					&http.Response{StatusCode: http.StatusSwitchingProtocols, Proto: "HTTP/1.1"}, closedCaptureFrameWatcher{})
			case 2:
				svc.rawTCPInterceptor()(ctx, mitmproxy.RawTCPTunnelEvent{
					Source: mitmproxy.RawTCPTunnelSourceSOCKS5, Hostport: "capture.test:443",
				})
			}
		})
	}
	wg.Wait()
	svc.frontendEventBatcher.Close()
	batch := <-batches
	if len(batch.Dropped) != 0 {
		t.Fatalf("unexpected dropped events: %v", batch.Dropped)
	}
	revisions := make(map[uint64]uint64)
	var lastID uint64
	for _, event := range batch.Events {
		switch event.Name {
		case trafficEventName:
			var entry TrafficEntry
			if err := json.Unmarshal(event.Data, &entry); err != nil {
				t.Fatal(err)
			}
			if entry.ID != lastID+1 {
				t.Fatalf("initial event ID = %d after %d", entry.ID, lastID)
			}
			lastID = entry.ID
			revisions[entry.ID] = entry.Revision
		case trafficPatchEventName:
			var patch TrafficEntryPatch
			if err := json.Unmarshal(event.Data, &patch); err != nil {
				t.Fatal(err)
			}
			previous, exists := revisions[patch.TrafficID]
			if !exists || patch.Revision <= previous {
				t.Fatalf("patch preceded initial event or regressed: %+v, previous %d", patch, previous)
			}
			revisions[patch.TrafficID] = patch.Revision
		}
	}
	if lastID != count {
		t.Fatalf("published %d initial entries, want %d", lastID, count)
	}
	for i, entry := range svc.GetTraffic() {
		if entry.ID != uint64(i+1) {
			t.Fatalf("snapshot[%d].ID = %d", i, entry.ID)
		}
	}
	if got := svc.GetStatistics(); got.Total != count || got.TotalHTTP != count/3 || got.TotalWS != count/3 || got.TotalTCP != count/3 {
		t.Fatalf("statistics = %+v", got)
	}
}

func TestCaptureRegistrationPrecedesBodyAndSlowInvocation(t *testing.T) {
	svc := newTestProxyService(t, nil)
	invoking := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	done := make(chan error, 1)
	req, err := http.NewRequest(http.MethodPost, "http://slow.test/", strings.NewReader("request body"))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, invokeErr := svc.httpInterceptor(nil)(newRawTCPMetadataContext(), req,
			mitmproxy.HTTPDelegatedInvokerFunc(func(request *http.Request) (*http.Response, error) {
				close(invoking)
				<-release
				_, readErr := io.Copy(io.Discard, request.Body)
				_ = request.Body.Close()
				if readErr != nil {
					return nil, readErr
				}
				return &http.Response{StatusCode: http.StatusNoContent, Proto: "HTTP/1.1", Body: http.NoBody}, nil
			}))
		done <- invokeErr
	}()
	waitForSignal(t, invoking, "slow invocation")
	entries := svc.GetTraffic()
	if len(entries) != 1 || entries[0].ID != 1 || !entries[0].StartedAt.IsZero() {
		t.Fatalf("initial entry before any transport callback = %+v", entries)
	}
	metrics := entries[0].Request.Metrics
	if metrics == nil || metrics.StartedAtMicros != -1 || metrics.EndedAtMicros != -1 ||
		metrics.BodySize != -1 || metrics.HeaderSize != -1 || metrics.State != HTTPMessageStatePending {
		t.Fatalf("unobserved metrics = %+v", metrics)
	}
	if _, ok := svc.trafficBodies.Load(uint64(1)); !ok {
		t.Fatal("request body was not associated with its registered ID")
	}
	if _, ok := svc.trafficBodies.Load(uint64(0)); ok {
		t.Fatal("request body was created before ID allocation")
	}
	registered := make(chan struct{})
	go func() {
		svc.rawTCPInterceptor()(newRawTCPMetadataContext(), mitmproxy.RawTCPTunnelEvent{
			Source: mitmproxy.RawTCPTunnelSourceSOCKS5, Hostport: "fast.test:443",
		})
		close(registered)
	}()
	waitForSignal(t, registered, "registration while HTTP is blocked")
	if entries := svc.GetTraffic(); len(entries) != 2 || entries[0].ID != 1 || entries[1].ID != 2 {
		t.Fatalf("entries while first request is blocked = %+v", entries)
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRegisteredCaptureRestartRejectsLateTimingAndBody(t *testing.T) {
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	ctx := newRawTCPMetadataContext()
	stale := svc.registerTrafficEntry(ctx, TrafficEntry{Type: "http", Request: &HTTPMessage{}})
	exchange := newCaptureExchange(svc, ctx, stale)
	if err := svc.RestartCapture(false); err != nil {
		t.Fatal(err)
	}
	fresh := svc.registerTrafficEntry(ctx, TrafficEntry{Type: "http", URL: "http://fresh.test/"})
	if fresh.ID != 1 {
		t.Fatalf("restarted ID = %d", fresh.ID)
	}
	var events int
	svc.emitTrafficHook = func(*TrafficEntry) { events++ }
	svc.emitTrafficPatchHook = func(TrafficEntryPatch) { events++ }
	exchange.requestStarted(time.Now(), 1)
	exchange.fail(errors.New("old request failed"))
	body := io.NopCloser(strings.NewReader("stale body"))
	if got := svc.newCaptureStreamBodyReader(body, stale, "", true); got != body {
		t.Fatal("old body was registered under a reused ID")
	}
	if entries := svc.GetTraffic(); len(entries) != 1 || entries[0].URL != fresh.URL || entries[0].Revision != fresh.Revision || events != 0 {
		t.Fatalf("late callbacks changed fresh capture: entries=%+v, events=%d", entries, events)
	}
}
