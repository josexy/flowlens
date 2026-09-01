package memstatsservice

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/josexy/flowlens/backend/pkg/logger"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const memStatsEventName = "memstats:update"

// MemSnapshot holds a point-in-time snapshot of Go runtime memory statistics.
type MemSnapshot struct {
	Timestamp    int64  `json:"timestamp"` // Unix milliseconds
	GoVersion    string `json:"goVersion"`
	GOOS         string `json:"goos"`
	GOARCH       string `json:"goarch"`
	NumGoroutine int    `json:"numGoroutine"`
	NumCPU       int    `json:"numCPU"`

	// Heap
	HeapAlloc    uint64 `json:"heapAlloc"`
	HeapSys      uint64 `json:"heapSys"`
	HeapInuse    uint64 `json:"heapInuse"`
	HeapIdle     uint64 `json:"heapIdle"`
	HeapReleased uint64 `json:"heapReleased"`
	HeapObjects  uint64 `json:"heapObjects"`

	// Stack
	StackInuse uint64 `json:"stackInuse"`
	StackSys   uint64 `json:"stackSys"`

	// Overall
	Sys        uint64 `json:"sys"`
	Alloc      uint64 `json:"alloc"`
	TotalAlloc uint64 `json:"totalAlloc"`
	Mallocs    uint64 `json:"mallocs"`
	Frees      uint64 `json:"frees"`

	// GC
	NumGC         uint32  `json:"numGC"`
	NumForcedGC   uint32  `json:"numForcedGC"`
	GCCPUFraction float64 `json:"gcCPUFraction"`
	PauseTotalNs  uint64  `json:"pauseTotalNs"`
	PauseNs       uint64  `json:"pauseNs"` // last GC pause
	NextGC        uint64  `json:"nextGC"`
	LastGC        int64   `json:"lastGC"` // Unix ms; 0 if no GC yet

	// Other system memory
	GCSys     uint64 `json:"gcSys"`
	MSpanSys  uint64 `json:"mspanSys"`
	MCacheSys uint64 `json:"mcacheSys"`
	OtherSys  uint64 `json:"otherSys"`
}

// MonitoringStatus describes the current state of memory monitoring.
type MonitoringStatus struct {
	Monitoring bool  `json:"monitoring"`
	IntervalMs int64 `json:"intervalMs"`
}

// MemStatsService exposes Go runtime memory monitoring to the Wails frontend.
type MemStatsService struct {
	mu           sync.Mutex
	baseCtx      context.Context
	baseCancel   context.CancelFunc
	shutdownOnce sync.Once
	monitorWG    sync.WaitGroup
	app          *application.App
	monitoring   bool
	shuttingDown bool
	intervalMs   int64
	stopCh       chan struct{}
}

func New() *MemStatsService {
	return &MemStatsService{
		intervalMs: 2000,
	}
}

func (s *MemStatsService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.baseCtx, s.baseCancel = context.WithCancel(ctx)
	s.app = application.Get()
	logger.G().Info("Memory monitoring service started")
	return nil
}

func (s *MemStatsService) ServiceShutdown() error {
	return s.Shutdown()
}

//wails:ignore
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
	return buildSnapshot()
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

	// Emit immediately on start so the frontend gets data right away.
	s.emit(buildSnapshot())

	for {
		select {
		case <-ticker.C:
			s.emit(buildSnapshot())
		case <-s.baseCtx.Done():
			return
		case <-stopCh:
			return
		}
	}
}

func (s *MemStatsService) emit(snapshot MemSnapshot) {
	if s.app == nil {
		return
	}
	_ = s.app.Event.Emit(memStatsEventName, snapshot)
}

func buildSnapshot() MemSnapshot {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	pauseNs := uint64(0)
	if ms.NumGC > 0 {
		pauseNs = ms.PauseNs[(ms.NumGC+255)%256]
	}
	lastGC := int64(0)
	if ms.LastGC > 0 {
		lastGC = time.Unix(0, int64(ms.LastGC)).UnixMilli()
	}

	return MemSnapshot{
		Timestamp:     time.Now().UnixMilli(),
		GoVersion:     runtime.Version(),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		NumGoroutine:  runtime.NumGoroutine(),
		NumCPU:        runtime.NumCPU(),
		HeapAlloc:     ms.HeapAlloc,
		HeapSys:       ms.HeapSys,
		HeapInuse:     ms.HeapInuse,
		HeapIdle:      ms.HeapIdle,
		HeapReleased:  ms.HeapReleased,
		HeapObjects:   ms.HeapObjects,
		StackInuse:    ms.StackInuse,
		StackSys:      ms.StackSys,
		Sys:           ms.Sys,
		Alloc:         ms.Alloc,
		TotalAlloc:    ms.TotalAlloc,
		Mallocs:       ms.Mallocs,
		Frees:         ms.Frees,
		NumGC:         ms.NumGC,
		NumForcedGC:   ms.NumForcedGC,
		GCCPUFraction: ms.GCCPUFraction,
		PauseTotalNs:  ms.PauseTotalNs,
		PauseNs:       pauseNs,
		NextGC:        ms.NextGC,
		LastGC:        lastGC,
		GCSys:         ms.GCSys,
		MSpanSys:      ms.MSpanSys,
		MCacheSys:     ms.MCacheSys,
		OtherSys:      ms.OtherSys,
	}
}
