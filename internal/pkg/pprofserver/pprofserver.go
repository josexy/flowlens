// Package pprofserver serves Go runtime profiles of a running FlowLens on a
// loopback address, for go tool pprof, and reports how much memory a full
// collection can reclaim. It runs only when FLOWLENS_PPROF_ADDR asks for it.
package pprofserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"runtime"
	"runtime/debug"
	"time"

	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/pprof"
)

// EnvAddr names the environment variable with the address to serve on, such
// as 127.0.0.1:6060.
const EnvAddr = "FLOWLENS_PPROF_ADDR"

// cleanupWait lets cleanups and finalizers queued by one collection run
// before the next: memory that only their tables referenced is reclaimed by
// that next collection.
const cleanupWait = time.Second

// Memory is one reading for the collection report.
type Memory struct {
	// Process is the memory the operating system's process tool shows, or -1
	// when it is unavailable.
	Process int64
	// HeapInUse is the memory of heap objects, KeptForReuse free heap memory
	// the runtime keeps, and Held all memory the runtime has not returned.
	HeapInUse, KeptForReuse, Held uint64
}

// Server serves profiles until Close.
type Server struct {
	server *http.Server
	addr   string
	done   chan struct{}
}

// Start listens on addr, a loopback IP address with a port, and serves the
// runtime profiles under /debug/pprof/ and the collection report at
// POST /debug/flowlens/collect. read takes each reading of the report.
func Start(addr string, read func() Memory) (*Server, error) {
	if read == nil {
		return nil, errors.New("pprof server needs a memory reader")
	}
	if err := checkLoopback(addr); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on pprof address %s: %w", addr, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("POST /debug/flowlens/collect", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writeCollectReport(w, collect(read))
	})
	s := &Server{
		// Profile and Trace extend their own write deadlines.
		server: &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second},
		addr:   listener.Addr().String(),
		done:   make(chan struct{}),
	}
	go func() {
		defer close(s.done)
		_ = s.server.Serve(listener)
	}()
	return s, nil
}

// Addr returns the address the server listens on.
func (s *Server) Addr() string { return s.addr }

// Close stops the server, ending a profile still being taken.
func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.server.Shutdown(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		err = s.server.Close()
	}
	<-s.done
	return err
}

// checkLoopback accepts only a literal loopback IP with a port, so profiles
// never listen on a network interface.
func checkLoopback(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("pprof address %q: %w", addr, err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() || port == "" {
		return fmt.Errorf("pprof address %q must be a loopback IP and port, such as 127.0.0.1:6060", addr)
	}
	return nil
}

type collectStage struct {
	name   string
	memory Memory
}

// collect runs two collections, with cleanups run between them, then
// returns free memory to the operating system, reading memory at each step.
func collect(read func() Memory) []collectStage {
	stages := []collectStage{{"before", read()}}
	runtime.GC()
	stages = append(stages, collectStage{"after GC", read()})
	time.Sleep(cleanupWait)
	runtime.GC()
	stages = append(stages, collectStage{"after second GC", read()})
	debug.FreeOSMemory()
	return append(stages, collectStage{"after returning memory", read()})
}

func writeCollectReport(w io.Writer, stages []collectStage) {
	fmt.Fprintf(w, "%-24s %12s %12s %15s %12s\n", "stage", "process", "heap in use", "kept for reuse", "held")
	for _, stage := range stages {
		m := stage.memory
		process := "unavailable"
		if m.Process >= 0 {
			process = mebibytes(uint64(m.Process))
		}
		fmt.Fprintf(w, "%-24s %12s %12s %15s %12s\n", stage.name, process, mebibytes(m.HeapInUse), mebibytes(m.KeptForReuse), mebibytes(m.Held))
	}
}

func mebibytes(bytes uint64) string {
	return fmt.Sprintf("%.1f MiB", float64(bytes)/(1<<20))
}
