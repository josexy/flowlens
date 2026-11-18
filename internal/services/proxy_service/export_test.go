package proxyservice

import "testing"

// Hooks for the external tests that load saved captures with history_service,
// which cannot be imported by this package's own tests.

func NewSpillTestServiceForTest(t *testing.T) *ProxyService { return newSpillTestService(t) }

func CaptureSpillCandidatesForTest(t *testing.T, s *ProxyService) (uint64, uint64) {
	return captureSpillCandidates(t, s)
}

func AddSettledEntriesForTest(t *testing.T, s *ProxyService, count int) []uint64 {
	return addSettledEntries(t, s, count)
}

func (s *ProxyService) SpillAllForTest() int {
	spillAll(s)
	return s.captureSpillStore().Len()
}

func (s *ProxyService) FlushHistoryForTest() error { return s.flushHistoryToDisk(true) }
