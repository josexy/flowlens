package proxyservice

import "testing"

func TestGetTrafficReturnsTheLiveWindowWithoutTruncatingHistory(t *testing.T) {
	svc := newTestProxyService(t, nil)
	for id := uint64(1); id <= liveTrafficWindow+2; id++ {
		svc.trafficEntries.Set(id, &TrafficEntry{ID: id, Type: "http"})
	}

	window := svc.GetTraffic()
	if len(window) != liveTrafficWindow {
		t.Fatalf("GetTraffic length = %d, want %d", len(window), liveTrafficWindow)
	}
	if window[0].ID != 3 || window[len(window)-1].ID != liveTrafficWindow+2 {
		t.Fatalf("GetTraffic IDs = %d..%d, want 3..%d", window[0].ID, window[len(window)-1].ID, liveTrafficWindow+2)
	}

	all := svc.getAllTraffic()
	if len(all) != liveTrafficWindow+2 {
		t.Fatalf("complete history snapshot length = %d, want %d", len(all), liveTrafficWindow+2)
	}
}
