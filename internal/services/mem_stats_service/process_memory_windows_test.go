//go:build windows

package memstatsservice

import "testing"

func TestPrivateWorkingSetFallbackAgreesWithCounters(t *testing.T) {
	if _, ok := readPrivateWorkingSetFromCounters(); !ok {
		t.Log("PROCESS_MEMORY_COUNTERS_EX2 is unavailable; both reads use the fallback")
	}
	counters := readProcessMemory()

	original := privateWorkingSetFromCounters
	privateWorkingSetFromCounters = func() (int64, bool) { return 0, false }
	t.Cleanup(func() { privateWorkingSetFromCounters = original })
	pages := readProcessMemory()

	for _, read := range []ProcessMemory{counters, pages} {
		if read.Measure != MeasurePrivateWorkingSet || read.Bytes <= 0 || !read.Available() {
			t.Fatalf("process memory = %+v, want a positive private working set", read)
		}
	}
	larger, smaller := max(counters.Bytes, pages.Bytes), min(counters.Bytes, pages.Bytes)
	if (larger-smaller)*4 > larger {
		t.Fatalf("counters %d and working-set pages %d differ by more than 25%%", counters.Bytes, pages.Bytes)
	}
}
