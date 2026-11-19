package memstatsservice

import (
	"runtime"
	"runtime/metrics"
	"testing"
)

func TestRuntimeMetricNamesAreSupported(t *testing.T) {
	for _, name := range runtimeMetricNames {
		sample := []metrics.Sample{{Name: name}}
		metrics.Read(sample)
		if kind := sample[0].Value.Kind(); kind == metrics.KindBad {
			t.Errorf("runtime metric %s is unsupported by %s", name, runtime.Version())
		}
	}
}

func TestRuntimeMemoryBreakdownAddsUp(t *testing.T) {
	sampler := newRuntimeSampler()
	keep := make([][]byte, 0, 64)
	for range 64 {
		keep = append(keep, make([]byte, 64<<10))
	}
	memory := sampler.read()
	runtime.KeepAlive(keep)

	if memory.HeapInUse == 0 || memory.Stacks == 0 || memory.Held == 0 {
		t.Fatalf("runtime memory has empty parts: %+v", memory)
	}
	if sum := memory.HeapInUse + memory.KeptForReuse + memory.Stacks + memory.Overhead; sum != memory.Held {
		t.Fatalf("parts add up to %d, held is %d: %+v", sum, memory.Held, memory)
	}
	if total := sampler.samples[metricTotal].Value.Uint64(); memory.Held+memory.Released != total {
		t.Fatalf("held %d + released %d != total %d", memory.Held, memory.Released, total)
	}
}

func TestRuntimeMemoryFollowsGarbageCollection(t *testing.T) {
	sampler := newRuntimeSampler()
	before := sampler.read()
	runtime.GC()
	after := sampler.read()
	if after.GCCycles <= before.GCCycles {
		t.Fatalf("GC cycles %d -> %d after runtime.GC", before.GCCycles, after.GCCycles)
	}
	if after.LastGC <= 0 || after.LastPauseNs < 0 || after.NextGC == 0 || after.Goroutines == 0 {
		t.Fatalf("GC values after runtime.GC: %+v", after)
	}
}

func TestSnapshotCarriesProcessAndRuntimeMemory(t *testing.T) {
	snapshot := New().GetMemStats()
	if snapshot.Timestamp <= 0 || snapshot.Runtime.Held == 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Process.Available() != (snapshot.Process.Bytes >= 0) {
		t.Fatalf("process memory = %+v", snapshot.Process)
	}
}
