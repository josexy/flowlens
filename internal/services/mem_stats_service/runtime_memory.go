package memstatsservice

import (
	"runtime/debug"
	"runtime/metrics"
	"sync"

	"github.com/josexy/flowlens/internal/pkg/logger"
)

// RuntimeMemory holds the Go runtime values the section shows. HeapInUse,
// KeptForReuse, Stacks, and Overhead add up to Held exactly; Released is apart
// from that total.
type RuntimeMemory struct {
	// Held is the memory the Go runtime has mapped and not returned to the OS.
	Held uint64 `json:"held"`
	// HeapInUse is the memory of heap objects, including ones not yet collected.
	HeapInUse uint64 `json:"heapInUse"`
	// KeptForReuse is free heap memory the runtime keeps rather than returning.
	KeptForReuse uint64 `json:"keptForReuse"`
	Stacks       uint64 `json:"stacks"`
	// Overhead is the rest of Held: metadata, profiling, and other runtime use.
	Overhead uint64 `json:"overhead"`
	// Released is heap memory returned to the OS and not yet reused.
	Released   uint64 `json:"released"`
	NextGC     uint64 `json:"nextGC"`
	GCCycles   uint64 `json:"gcCycles"`
	Goroutines uint64 `json:"goroutines"`
	// LastGC is when the last GC finished in Unix milliseconds, or 0 before one.
	LastGC      int64 `json:"lastGC"`
	LastPauseNs int64 `json:"lastPauseNs"`
}

const (
	metricHeapObjects = iota
	metricHeapFree
	metricHeapUnused
	metricHeapStacks
	metricOSStacks
	metricHeapReleased
	metricTotal
	metricHeapGoal
	metricGCCycles
	metricGoroutines
	metricCount
)

// runtimeMetricNames are read together; all /memory/classes values come from
// one aggregate, so they add up to the total.
var runtimeMetricNames = [metricCount]string{
	metricHeapObjects:  "/memory/classes/heap/objects:bytes",
	metricHeapFree:     "/memory/classes/heap/free:bytes",
	metricHeapUnused:   "/memory/classes/heap/unused:bytes",
	metricHeapStacks:   "/memory/classes/heap/stacks:bytes",
	metricOSStacks:     "/memory/classes/os-stacks:bytes",
	metricHeapReleased: "/memory/classes/heap/released:bytes",
	metricTotal:        "/memory/classes/total:bytes",
	metricHeapGoal:     "/gc/heap/goal:bytes",
	metricGCCycles:     "/gc/cycles/total:gc-cycles",
	metricGoroutines:   "/sched/goroutines:goroutines",
}

// runtimeSampler reads RuntimeMemory without stopping the world. It reuses its
// buffers, so samples taken at once are serialized.
type runtimeSampler struct {
	mu          sync.Mutex
	samples     [metricCount]metrics.Sample
	gc          debug.GCStats
	unsupported [metricCount]bool
}

func newRuntimeSampler() *runtimeSampler {
	sampler := &runtimeSampler{}
	for index, name := range runtimeMetricNames {
		sampler.samples[index].Name = name
	}
	return sampler
}

func (r *runtimeSampler) read() RuntimeMemory {
	r.mu.Lock()
	defer r.mu.Unlock()
	metrics.Read(r.samples[:])
	debug.ReadGCStats(&r.gc)

	total := r.value(metricTotal)
	released := r.value(metricHeapReleased)
	memory := RuntimeMemory{
		HeapInUse:    r.value(metricHeapObjects),
		KeptForReuse: r.value(metricHeapFree) + r.value(metricHeapUnused),
		Stacks:       r.value(metricHeapStacks) + r.value(metricOSStacks),
		Released:     released,
		Held:         subtractFloor(total, released),
		NextGC:       r.value(metricHeapGoal),
		GCCycles:     r.value(metricGCCycles),
		Goroutines:   r.value(metricGoroutines),
	}
	memory.Overhead = subtractFloor(memory.Held, memory.HeapInUse+memory.KeptForReuse+memory.Stacks)
	if r.gc.NumGC > 0 {
		memory.LastGC = r.gc.LastGC.UnixMilli()
		if len(r.gc.Pause) > 0 {
			memory.LastPauseNs = int64(r.gc.Pause[0])
		}
	}
	return memory
}

// value reads a sample. An unsupported metric reads as 0 and is logged once.
func (r *runtimeSampler) value(index int) uint64 {
	sample := &r.samples[index]
	if sample.Value.Kind() != metrics.KindUint64 {
		if !r.unsupported[index] {
			r.unsupported[index] = true
			logger.G().Warnf("Runtime metric %s is unsupported (kind %d)", sample.Name, sample.Value.Kind())
		}
		return 0
	}
	return sample.Value.Uint64()
}

func subtractFloor(value, minus uint64) uint64 {
	if minus > value {
		return 0
	}
	return value - minus
}
