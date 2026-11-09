package proxyservice

import "testing"

func TestGetTrafficReturnsLatestFrontendWindowWithoutTruncatingHistory(t *testing.T) {
	svc := newTestProxyService(t, nil)
	for id := uint64(1); id <= frontendTrafficEntryLimit+2; id++ {
		svc.trafficEntries.Set(id, &TrafficEntry{ID: id, Type: "http"})
	}

	window := svc.GetTraffic()
	if len(window) != frontendTrafficEntryLimit {
		t.Fatalf("GetTraffic length = %d, want %d", len(window), frontendTrafficEntryLimit)
	}
	if window[0].ID != 3 || window[len(window)-1].ID != frontendTrafficEntryLimit+2 {
		t.Fatalf("GetTraffic IDs = %d..%d, want 3..%d", window[0].ID, window[len(window)-1].ID, frontendTrafficEntryLimit+2)
	}

	all := svc.getAllTraffic()
	if len(all) != frontendTrafficEntryLimit+2 {
		t.Fatalf("complete history snapshot length = %d, want %d", len(all), frontendTrafficEntryLimit+2)
	}
}
