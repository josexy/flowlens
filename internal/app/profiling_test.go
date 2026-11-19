package app

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/pkg/pprofserver"
	http "github.com/josexy/xhttp"
)

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func TestStartProfilingServesOnlyOnLoopbackWhenAsked(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	for _, addr := range []string{"", "0.0.0.0:0"} {
		t.Setenv(pprofserver.EnvAddr, addr)
		startProfiling()()
	}

	addr := freeLoopbackAddr(t)
	t.Setenv(pprofserver.EnvAddr, " "+addr+" ")
	stop := startProfiling()
	resp, err := client.Get("http://" + addr + "/debug/pprof/heap?debug=1")
	if err != nil {
		stop()
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "heap profile") {
		stop()
		t.Fatalf("heap profile: %d %.80q", resp.StatusCode, body)
	}

	resp, err = client.Post("http://"+addr+"/debug/flowlens/collect", "text/plain", nil)
	if err != nil {
		stop()
		t.Fatal(err)
	}
	report, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(report), "after returning memory") || !strings.Contains(string(report), "MiB") {
		stop()
		t.Fatalf("collect report:\n%s", report)
	}

	stop()
	if resp, err := client.Get("http://" + addr + "/debug/pprof/"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("profiles still served after stop")
	}
}
