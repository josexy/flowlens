package memstatsservice

import (
	"context"
	"sync"
	"time"

	"github.com/josexy/flowlens/internal/pkg/logger"
	"github.com/josexy/flowlens/internal/serviceruntime"
)

// MemSnapshot holds one sample of FlowLens's memory.
type MemSnapshot struct {
	Timestamp int64         `json:"timestamp"` // Unix milliseconds
	Process   ProcessMemory `json:"process"`   // Bytes is -1 when unavailable
	Runtime   RuntimeMemory `json:"runtime"`
}

// MonitoringStatus describes the current state of memory monitoring.
type MonitoringStatus struct {
	Monitoring bool  `json:"monitoring"`
	IntervalMs int64 `json:"intervalMs"`
}

// Events are the values the memory monitoring service publishes. Subscribers
// must not block.
type Events struct {
	// Snapshot is each memory sample.
	Snapshot serviceruntime.Topic[MemSnapshot]
}

// MemStatsService exposes memory monitoring to the interface.
type MemStatsService struct {
	// Events publishes memory samples.
	Events       Events
	mu           sync.Mutex
	sampler      *runtimeSampler
	baseCtx      context.Context
	baseCancel   context.CancelFunc
	shutdownOnce sync.Once
	monitorWG    sync.WaitGroup
	monitoring   bool
	shuttingDown bool
	intervalMs   int64
	stopCh       chan struct{}
}

func New() *MemStatsService {
	return &MemStatsService{
		sampler:    newRuntimeSampler(),
		intervalMs: 2000,
	}
}

func (s *MemStatsService) ServiceStartup(ctx context.Context) error {
	s.baseCtx, s.baseCancel = context.WithCancel(ctx)
	logger.G().Info("Memory monitoring service started")
	return nil
}

func (s *MemStatsService) ServiceShutdown() error {
	return s.Shutdown()
}

func (s *MemStatsService) Shutdown() error {
	s.shutdownOnce.Do(func() {
		s.mu.Lock()
		s.shuttingDown = true
		wasMonitoring := s.monitoring && s.stopCh != nil
		if wasMonitoring {
			close(s.stopCh)
			s.stopCh = nil
			s.monitoring = false
		}
		s.mu.Unlock()
		if wasMonitoring {
			logger.G().Info("Memory monitoring stopped")
		}
		if s.baseCancel != nil {
			s.baseCancel()
		}
		s.monitorWG.Wait()
		logger.G().Info("Memory monitoring service stopped")
	})
	return nil
}

// GetMemStats returns a one-time snapshot without affecting monitoring state.
func (s *MemStatsService) GetMemStats() MemSnapshot {
	return s.snapshot()
}

// GetMonitoringStatus returns current monitoring state.
func (s *MemStatsService) GetMonitoringStatus() MonitoringStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return MonitoringStatus{
		Monitoring: s.monitoring,
		IntervalMs: s.intervalMs,
	}
}

// StartMonitoring starts (or restarts with new interval) the monitoring goroutine.
// intervalMs is clamped to a minimum of 100ms.
func (s *MemStatsService) StartMonitoring(intervalMs int64) {
	if intervalMs < 100 {
		intervalMs = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shuttingDown {
		return
	}

	// Stop existing goroutine before starting a new one.
	if s.monitoring && s.stopCh != nil {
		close(s.stopCh)
	}

	s.intervalMs = intervalMs
	s.stopCh = make(chan struct{})
	s.monitoring = true
	stopCh := s.stopCh
	logger.G().Infof("Memory monitoring started with interval %dms", intervalMs)
	s.monitorWG.Go(func() {
		s.monitorLoop(intervalMs, stopCh)
	})
}

// StopMonitoring stops the monitoring goroutine.
func (s *MemStatsService) StopMonitoring() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.monitoring || s.stopCh == nil {
		return
	}
	close(s.stopCh)
	s.stopCh = nil
	s.monitoring = false
	logger.G().Info("Memory monitoring stopped")
}

func (s *MemStatsService) monitorLoop(intervalMs int64, stopCh chan struct{}) {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()

	// Publish immediately on start so the interface gets data right away.
	s.emit(s.snapshot())

	for {
		select {
		case <-ticker.C:
			s.emit(s.snapshot())
		case <-s.baseCtx.Done():
			return
		case <-stopCh:
			return
		}
	}
}

func (s *MemStatsService) emit(snapshot MemSnapshot) {
	s.Events.Snapshot.Publish(snapshot)
}

// snapshot samples the process and the Go runtime without stopping the world.
func (s *MemStatsService) snapshot() MemSnapshot {
	return MemSnapshot{
		Timestamp: time.Now().UnixMilli(),
		Process:   readProcessMemory(),
		Runtime:   s.sampler.read(),
	}
}
