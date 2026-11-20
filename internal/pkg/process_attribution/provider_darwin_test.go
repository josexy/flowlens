//go:build darwin

package processattribution

import (
	"context"
	"errors"
	"image"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestDarwinSocketInfoMatchesIPv4Tuple(t *testing.T) {
	tuple := EndpointTuple{
		Client: netip.MustParseAddrPort("127.0.0.1:43120"),
		Proxy:  netip.MustParseAddrPort("127.0.0.1:8080"),
	}
	rows := []darwinSocketOwnerRow{
		{local: tuple.Proxy, remote: tuple.Client, pid: 1},
		{local: tuple.Client, remote: tuple.Proxy, pid: 2},
	}

	pids := darwinTupleOwnerPIDs(rows, tuple)
	if len(pids) != 1 || pids[0] != 2 {
		t.Fatalf("matching PIDs = %v, want [2]", pids)
	}
}

func TestDarwinSocketInfoMatchesIPv6Tuple(t *testing.T) {
	tuple := EndpointTuple{
		Client: netip.MustParseAddrPort("[::1]:43121"),
		Proxy:  netip.MustParseAddrPort("[::1]:8080"),
	}
	rows := []darwinSocketOwnerRow{
		{local: netip.MustParseAddrPort("[::ffff:127.0.0.1]:43121"), remote: tuple.Proxy, pid: 1},
		{local: tuple.Client, remote: tuple.Proxy, pid: 2},
	}

	pids := darwinTupleOwnerPIDs(rows, tuple)
	if len(pids) != 1 || pids[0] != 2 {
		t.Fatalf("matching PIDs = %v, want [2]", pids)
	}
}

func TestDarwinProviderRefreshesCachedSnapshotAfterTupleMiss(t *testing.T) {
	firstTuple := EndpointTuple{
		Client: netip.MustParseAddrPort("127.0.0.1:43122"),
		Proxy:  netip.MustParseAddrPort("127.0.0.1:8080"),
	}
	secondTuple := EndpointTuple{
		Client: netip.MustParseAddrPort("127.0.0.1:43123"),
		Proxy:  netip.MustParseAddrPort("127.0.0.1:8080"),
	}
	firstRow := darwinSocketOwnerRow{
		local:  firstTuple.Client,
		remote: firstTuple.Proxy,
		pid:    4242,
	}
	secondRow := darwinSocketOwnerRow{
		local:  secondTuple.Client,
		remote: secondTuple.Proxy,
		pid:    4343,
	}

	provider := newDarwinProvider()
	provider.enrich = nil // Headless tests do not pump the AppKit main queue.
	now := time.Now()
	provider.now = func() time.Time { return now }
	loadCalls := 0
	provider.loadRows = func() ([]darwinSocketOwnerRow, error) {
		loadCalls++
		if loadCalls == 1 {
			return []darwinSocketOwnerRow{firstRow}, nil
		}
		return []darwinSocketOwnerRow{firstRow, secondRow}, nil
	}
	provider.identity = func(pid uint32) (darwinProcessIdentity, error) {
		return darwinProcessIdentity{
			startToken:         strconv.FormatUint(uint64(pid), 10),
			displayName:        "test process",
			processName:        "test",
			executablePath:     "/Applications/Test.app/Contents/MacOS/test",
			identityConfidence: "exact",
		}, nil
	}

	first := provider.Lookup(context.Background(), firstTuple)
	if first.Status != StatusResolved || first.PID != firstRow.pid {
		t.Fatalf("first lookup = %+v, want PID %d", first, firstRow.pid)
	}
	second := provider.Lookup(context.Background(), secondTuple)
	if second.Status != StatusResolved || second.PID != secondRow.pid {
		t.Fatalf("second lookup = %+v, want refreshed PID %d", second, secondRow.pid)
	}
	if loadCalls != 2 {
		t.Fatalf("socket table load calls = %d, want 2", loadCalls)
	}
}

func TestDarwinHandlesExitedProcess(t *testing.T) {
	command := exec.Command("/usr/bin/true")
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	identity, err := lookupDarwinProcessIdentity(uint32(command.ProcessState.Pid()))
	if !errors.Is(err, syscall.ESRCH) || identity.startToken != "" {
		t.Fatalf("exited process: %+v, %v", identity, err)
	}
}

func TestDarwinCurrentProcessIdentity(t *testing.T) {
	identity, err := lookupDarwinProcessIdentity(uint32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	if identity.startToken == "" || identity.executablePath == "" || identity.processName == "" {
		t.Fatalf("incomplete identity: %+v", identity)
	}
}

func TestDarwinProviderFindsCurrentLoopbackProcess(t *testing.T) {
	for _, address := range []string{"127.0.0.1:0", "[::1]:0"} {
		t.Run(address, func(t *testing.T) { testDarwinLoopbackProcess(t, address) })
	}
}

func testDarwinLoopbackProcess(t *testing.T, address string) {
	t.Helper()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	server, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer server.Close()

	tuple := EndpointTuple{
		Client: normalizeAddrPort(client.LocalAddr().(*net.TCPAddr).AddrPort()),
		Proxy:  normalizeAddrPort(client.RemoteAddr().(*net.TCPAddr).AddrPort()),
	}
	provider := newDarwinProvider()
	provider.enrich = nil // Headless tests do not pump the AppKit main queue.
	deadline := time.Now().Add(time.Second)
	var result Result
	for time.Now().Before(deadline) {
		result = provider.Lookup(context.Background(), tuple)
		if result.Status == StatusResolved && result.PID == uint32(os.Getpid()) {
			if result.StartToken == "" || result.ProcessName == "" || result.ExecutablePath == "" || result.DisplayName == "" {
				t.Fatalf("loopback process metadata is incomplete: %+v", result)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("loopback lookup result = %+v, want current PID %d", result, os.Getpid())
}

func TestDarwinProviderRejectsPIDReuseAfterEnrichment(t *testing.T) {
	tuple := EndpointTuple{Client: netip.MustParseAddrPort("127.0.0.1:43000"), Proxy: netip.MustParseAddrPort("127.0.0.1:8080")}
	p := newDarwinProvider()
	p.loadRows = func() ([]darwinSocketOwnerRow, error) {
		return []darwinSocketOwnerRow{{local: tuple.Client, remote: tuple.Proxy, pid: 42}}, nil
	}
	token := "123:456"
	p.identity = func(uint32) (darwinProcessIdentity, error) { return darwinProcessIdentity{startToken: token}, nil }
	p.enrich = func(_ context.Context, _ uint32, value darwinProcessIdentity) darwinProcessIdentity {
		token = "789:1"
		value.appID = "stale"
		return value
	}
	result := p.Lookup(context.Background(), tuple)
	if result.StartToken != "" || result.AppID != "" {
		t.Fatalf("stale process metadata: %+v", result)
	}
}

func TestDarwinProviderRejectsPIDReuseDuringIcon(t *testing.T) {
	p := newDarwinProvider()
	token := "123:456"
	p.identity = func(uint32) (darwinProcessIdentity, error) { return darwinProcessIdentity{startToken: token}, nil }
	p.loadIcon = func(context.Context, uint32, string) (image.Image, error) {
		token = "789:1"
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
	}
	icon, err := p.LoadIcon(context.Background(), Result{PID: 42, StartToken: token, ExecutablePath: "/bin/sh"})
	if err == nil || icon != nil {
		t.Fatal("stale icon published")
	}
}

func TestDarwinProviderUsesRecordedIconPathAfterExit(t *testing.T) {
	result := Result{PID: 42, StartToken: "123:456", ExecutablePath: "/bin/sh"}
	for _, exit := range []string{"before icon lookup", "during icon lookup"} {
		t.Run(exit, func(t *testing.T) {
			p := newDarwinProvider()
			exited := exit == "before icon lookup"
			p.identity = func(uint32) (darwinProcessIdentity, error) {
				if exited {
					return darwinProcessIdentity{}, syscall.ESRCH
				}
				return darwinProcessIdentity{startToken: result.StartToken}, nil
			}
			pathIcon := image.NewRGBA(image.Rect(0, 0, 1, 1))
			p.loadIcon = func(_ context.Context, pid uint32, path string) (image.Image, error) {
				if path != result.ExecutablePath {
					t.Fatalf("icon path = %q, want %q", path, result.ExecutablePath)
				}
				if !exited && pid == result.PID {
					exited = true
					return image.NewRGBA(image.Rect(0, 0, 2, 2)), nil
				}
				if !exited || pid != 0 {
					t.Fatalf("icon lookup used PID %d, exited = %t", pid, exited)
				}
				return pathIcon, nil
			}
			if icon, err := p.LoadIcon(t.Context(), result); err != nil || icon != pathIcon {
				t.Fatalf("icon = %v, err = %v, want recorded-path icon", icon, err)
			}
		})
	}
}
