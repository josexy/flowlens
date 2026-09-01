package proxyservice

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
)

func TestResendRequestCancellationDuringInitialDelay(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	t.Cleanup(svc.baseCancel)
	entry := &TrafficEntry{ID: 1, Type: "http", Method: http.MethodGet, URL: server.URL}
	callCtx, cancelCall := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, err := svc.ResendRequestWithTrafficEntry(
			callCtx,
			ResendConfig{Count: 1, DelayMs: 2000},
			entry,
			nil,
		)
		resultCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancelCall()
	assertPromptResendCancellation(t, resultCh)
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("server received %d requests after cancellation during initial delay", got)
	}
}

func TestResendRequestBindingCancellationReturnsNoBackendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	t.Cleanup(svc.baseCancel)
	entry := &TrafficEntry{ID: 1, Type: "http", Method: http.MethodGet, URL: server.URL}
	svc.trafficEntries.Set(entry.ID, entry)
	callCtx, cancelCall := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, err := svc.ResendRequest(callCtx, entry.ID, ResendConfig{Count: 1, DelayMs: 2000})
		resultCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancelCall()
	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("ResendRequest binding error = %v, want nil after frontend cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ResendRequest binding did not finish promptly after cancellation")
	}
}

func TestResendRequestCancellationDuringInterval(t *testing.T) {
	firstRequest := make(chan struct{})
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requestCount.Add(1) == 1 {
			close(firstRequest)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	t.Cleanup(svc.baseCancel)
	callCtx, cancelCall := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, err := svc.ResendRequestWithTrafficEntry(
			callCtx,
			ResendConfig{Count: 2, IntervalMs: 2000},
			&TrafficEntry{ID: 1, Type: "http", Method: http.MethodGet, URL: server.URL},
			nil,
		)
		resultCh <- err
	}()

	select {
	case <-firstRequest:
	case <-time.After(2 * time.Second):
		cancelCall()
		t.Fatal("timed out waiting for the first resend request")
	}
	time.Sleep(20 * time.Millisecond)
	cancelCall()
	assertPromptResendCancellation(t, resultCh)
	if got := requestCount.Load(); got != 1 {
		t.Fatalf("server received %d requests, want only the first request", got)
	}
}

func assertPromptResendCancellation(t *testing.T, resultCh <-chan error) {
	t.Helper()
	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("resend error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		err := <-resultCh
		t.Fatalf("resend did not stop promptly after cancellation; eventual error = %v", err)
	}
}
