package proxyservice

import (
	"context"
	"io"
	"strings"
	"testing"
	"testing/synctest"

	http "github.com/josexy/xhttp"
)

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
