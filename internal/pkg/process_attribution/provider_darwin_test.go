//go:build darwin && cgo

package processattribution

import (
	"context"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
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

func TestDarwinBridgeHandlesExitedProcess(t *testing.T) {
	command := exec.Command("/usr/bin/true")
	if err := command.Run(); err != nil {
		t.Fatalf("run exited-process fixture: %v", err)
	}
	pid := uint32(command.ProcessState.Pid())
	before := darwinBridgeOutstandingAllocations()
	_, _ = lookupDarwinProcessIdentity(pid)
	if got := darwinBridgeOutstandingAllocations(); got != before {
		t.Fatalf("outstanding bridge allocations = %d, want %d", got, before)
	}
}

func TestDarwinBridgeReleasesReturnedBuffers(t *testing.T) {
	before := darwinBridgeOutstandingAllocations()
	identity, err := lookupDarwinProcessIdentity(uint32(os.Getpid()))
	if err != nil {
		t.Fatalf("lookupDarwinProcessIdentity: %v", err)
	}
	if identity.startToken == "" || identity.executablePath == "" || identity.processName == "" {
		t.Fatalf("current process identity is incomplete: %+v", identity)
	}
	_, _ = loadDarwinProcessIcon(context.Background(), uint32(os.Getpid()), identity.executablePath)
	if got := darwinBridgeOutstandingAllocations(); got != before {
		t.Fatalf("outstanding bridge allocations = %d, want %d", got, before)
	}
}

func TestDarwinProviderFindsCurrentLoopbackProcess(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
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
