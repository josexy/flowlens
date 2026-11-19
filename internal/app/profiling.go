package app

import (
	"os"
	"strings"

	"github.com/josexy/flowlens/internal/pkg/logger"
	"github.com/josexy/flowlens/internal/pkg/pprofserver"
	memstatsservice "github.com/josexy/flowlens/internal/services/mem_stats_service"
)

// startProfiling serves Go profiles when FLOWLENS_PPROF_ADDR names a loopback
// address. It returns how to stop them, which does nothing when they are off.
func startProfiling() (stop func()) {
	addr := strings.TrimSpace(os.Getenv(pprofserver.EnvAddr))
	if addr == "" {
		return func() {}
	}
	// A sampler of its own: reading memory needs no started service.
	sampler := memstatsservice.New()
	server, err := pprofserver.Start(addr, func() pprofserver.Memory {
		snapshot := sampler.GetMemStats()
		return pprofserver.Memory{
			Process:      snapshot.Process.Bytes,
			HeapInUse:    snapshot.Runtime.HeapInUse,
			KeptForReuse: snapshot.Runtime.KeptForReuse,
			Held:         snapshot.Runtime.Held,
		}
	})
	if err != nil {
		logger.G().Warnf("Profiling server not started: %v", err)
		return func() {}
	}
	logger.G().Infof("Profiling server listening on http://%s/debug/pprof/", server.Addr())
	return func() { _ = server.Close() }
}
