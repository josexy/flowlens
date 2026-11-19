package pprofserver

import (
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	http "github.com/josexy/xhttp"
)

func TestStartRejectsAddressesOffLoopback(t *testing.T) {
	read := func() Memory { return Memory{} }
	for _, addr := range []string{"", "6060", ":6060", "0.0.0.0:6060", "[::]:6060", "192.168.1.10:6060", "localhost:6060", "127.0.0.1:"} {
		if server, err := Start(addr, read); err == nil {
			_ = server.Close()
			t.Errorf("Start(%q) succeeded, want an error", addr)
		}
	}
	if _, err := Start("127.0.0.1:0", nil); err == nil {
		t.Error("Start without a memory reader succeeded")
	}
}

func TestServesProfilesAndCollectReport(t *testing.T) {
	var reads atomic.Int64
	server, err := Start("127.0.0.1:0", func() Memory {
		n := reads.Add(1)
		process := int64(n << 20)
		if n == 4 {
			process = -1
		}
		return Memory{Process: process, HeapInUse: uint64(n) << 20, KeptForReuse: 1 << 20, Held: 3 << 20}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client := &http.Client{Timeout: 10 * time.Second}
	base := "http://" + server.Addr()

	get := func(path string) (int, string) {
		t.Helper()
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	if status, body := get("/debug/pprof/"); status != http.StatusOK || !strings.Contains(body, "heap") || !strings.Contains(body, "goroutine") {
		t.Fatalf("index: %d %q", status, body)
	}
	if status, body := get("/debug/pprof/heap"); status != http.StatusOK || len(body) == 0 {
		t.Fatalf("heap profile: %d, %d bytes", status, len(body))
	}
	if status, body := get("/debug/pprof/goroutine?debug=1"); status != http.StatusOK || !strings.Contains(body, "goroutine profile") {
		t.Fatalf("goroutine profile: %d %q", status, body)
	}
	if status, _ := get("/debug/flowlens/collect"); status != http.StatusMethodNotAllowed {
		t.Fatalf("GET collect: %d, want %d", status, http.StatusMethodNotAllowed)
	}

	resp, err := client.Post(base+"/debug/flowlens/collect", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	report, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	lines := strings.Split(strings.TrimSpace(string(report)), "\n")
	want := []string{"stage", "before", "after GC", "after second GC", "after returning memory"}
	if resp.StatusCode != http.StatusOK || len(lines) != len(want) {
		t.Fatalf("collect: %d\n%s", resp.StatusCode, report)
	}
	for i, prefix := range want {
		if !strings.HasPrefix(lines[i], prefix) {
			t.Fatalf("line %d = %q, want it to start with %q", i, lines[i], prefix)
		}
	}
	if !strings.Contains(lines[1], "1.0 MiB") || !strings.Contains(lines[4], "unavailable") {
		t.Fatalf("collect readings not reported:\n%s", report)
	}
	if got := reads.Load(); got != 4 {
		t.Fatalf("memory read %d times, want 4", got)
	}

	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if resp, err := client.Get(base + "/debug/pprof/"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("server still answers after Close")
	}
}
