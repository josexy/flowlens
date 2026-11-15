package proxyservice

import (
	"io"
	"strings"
	"testing"
)

func TestCaptureRestartResetsIDsAndRejectsLateEntry(t *testing.T) {
	setTestConfigDir(t)
	service := newTestProxyService(t, nil)

	stale := service.newTrafficEntry(TrafficEntry{
		Type: "https",
		URL:  "https://old.example/",
	})
	if stale.ID != 1 {
		t.Fatalf("stale entry ID = %d, want 1", stale.ID)
	}
	if !service.storeTrafficEntry(stale) {
		t.Fatal("initial traffic entry was rejected")
	}

	if err := service.RestartCapture(false); err != nil {
		t.Fatalf("restart capture: %v", err)
	}

	fresh := service.newTrafficEntry(TrafficEntry{
		Type: "https",
		URL:  "https://new.example/",
	})
	if fresh.ID != 1 {
		t.Fatalf("fresh entry ID = %d, want restarted ID 1", fresh.ID)
	}
	if !service.storeTrafficEntry(fresh) {
		t.Fatal("fresh traffic entry was rejected")
	}

	emittedURLs := make([]string, 0, 1)
	service.emitTrafficHook = func(entry *TrafficEntry) {
		emittedURLs = append(emittedURLs, entry.URL)
	}

	stale.StatusCode = 200
	if service.storeTrafficEntry(stale) {
		t.Fatal("late entry from the previous capture generation was stored")
	}
	service.emitTraffic(stale)
	if len(emittedURLs) != 0 {
		t.Fatalf("late entry emitted after restart: %v", emittedURLs)
	}

	lateBody := io.NopCloser(strings.NewReader("late response"))
	if wrapped := service.newCaptureStreamBodyReader(lateBody, stale, "", false); wrapped != lateBody {
		t.Fatal("stale response body was wrapped for capture")
	}
	if _, ok := service.trafficBodies.Load(stale.ID); ok {
		t.Fatal("stale response body recreated capture state for a reused ID")
	}

	service.emitTraffic(fresh)

	entries := service.GetTraffic()
	if len(entries) != 1 || entries[0].ID != fresh.ID || entries[0].URL != fresh.URL {
		t.Fatalf("traffic entries = %#v, want only the fresh entry", entries)
	}
	if len(emittedURLs) != 1 || emittedURLs[0] != fresh.URL {
		t.Fatalf("emitted URLs = %v, want only %q", emittedURLs, fresh.URL)
	}
	if got := service.GetStatistics(); got.Total != 1 || got.TotalHTTP != 1 {
		t.Fatalf("statistics = %+v, want one HTTP entry", got)
	}
}
