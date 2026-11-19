package store

import (
	"context"

	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/serviceruntime"
	memstatsservice "github.com/josexy/flowlens/internal/services/mem_stats_service"
)

// MaxMemStatsSamples bounds the samples kept for the chart.
const MaxMemStatsSamples = 600

// DefaultMemStatsInterval is the sampling interval until one is chosen.
const DefaultMemStatsInterval int64 = 2000

// MemStatsBackend is the runtime statistics service.
type MemStatsBackend interface {
	GetMemStats() memstatsservice.MemSnapshot
	GetMonitoringStatus() memstatsservice.MonitoringStatus
	StartMonitoring(intervalMs int64)
	StopMonitoring()
}

// MemStatsStore holds the samples of the Memory Statistics section.
type MemStatsStore struct {
	Samples    []memstatsservice.MemSnapshot
	Latest     *memstatsservice.MemSnapshot
	Monitoring bool
	IntervalMs int64
	// Version changes whenever Samples does.
	Version uint64

	rt      Runtime
	backend MemStatsBackend
	off     func()
}

// NewMemStatsStore returns an empty store.
func NewMemStatsStore(rt Runtime, backend MemStatsBackend) *MemStatsStore {
	return &MemStatsStore{rt: rt, backend: backend, IntervalMs: DefaultMemStatsInterval}
}

// Add records a sample, dropping the oldest beyond the limit.
func (s *MemStatsStore) Add(sample memstatsservice.MemSnapshot) {
	s.Latest = &sample
	if len(s.Samples) >= MaxMemStatsSamples {
		// A new slice lets the dropped samples go instead of growing the old
		// array's unused prefix.
		kept := make([]memstatsservice.MemSnapshot, MaxMemStatsSamples-1, MaxMemStatsSamples)
		copy(kept, s.Samples[len(s.Samples)-MaxMemStatsSamples+1:])
		s.Samples = kept
	}
	s.Samples = append(s.Samples, sample)
	s.Version++
}

// Clear empties the samples.
func (s *MemStatsStore) Clear() {
	s.Samples, s.Latest = nil, nil
	s.Version++
}

// Initialize reads the service's state and subscribes to its samples.
func (s *MemStatsStore) Initialize(bus serviceruntime.EventBus, post func(func())) {
	if s.off != nil {
		return
	}
	s.off = bus.On(memstatsservice.MemStatsEventName, func(event serviceruntime.Event) {
		if sample, ok := event.Data.(memstatsservice.MemSnapshot); ok {
			post(func() { s.Add(sample) })
		}
	})
	runtime.Run(s.rt.Jobs, "memstats:status", func(context.Context) (memstatsservice.MonitoringStatus, error) {
		return s.backend.GetMonitoringStatus(), nil
	}, func(status memstatsservice.MonitoringStatus, err error) {
		if err != nil {
			return
		}
		s.Monitoring = status.Monitoring
		if status.IntervalMs > 0 {
			s.IntervalMs = status.IntervalMs
		}
	})
}

// Cleanup stops listening for samples.
func (s *MemStatsStore) Cleanup() {
	if s.off != nil {
		s.off()
		s.off = nil
	}
}

func (s *MemStatsStore) run(key string, work func()) {
	runtime.Run(s.rt.Jobs, "memstats:"+key, func(context.Context) (struct{}, error) {
		work()
		return struct{}{}, nil
	}, nil)
}

// Start begins monitoring at the current interval.
func (s *MemStatsStore) Start() {
	interval := s.IntervalMs
	s.Monitoring = true
	s.run("monitor", func() { s.backend.StartMonitoring(interval) })
}

// Stop pauses monitoring.
func (s *MemStatsStore) Stop() {
	s.Monitoring = false
	s.run("monitor", s.backend.StopMonitoring)
}

// SetInterval changes the interval, and monitoring continues at the new one.
func (s *MemStatsStore) SetInterval(intervalMs int64) {
	if intervalMs == s.IntervalMs || intervalMs <= 0 {
		return
	}
	s.IntervalMs = intervalMs
	if s.Monitoring {
		s.Start()
	}
}

// FetchOnce takes one sample now.
func (s *MemStatsStore) FetchOnce() {
	runtime.Run(s.rt.Jobs, "memstats:fetch", func(context.Context) (memstatsservice.MemSnapshot, error) {
		return s.backend.GetMemStats(), nil
	}, func(sample memstatsservice.MemSnapshot, err error) {
		if err == nil {
			s.Add(sample)
		}
	})
}
