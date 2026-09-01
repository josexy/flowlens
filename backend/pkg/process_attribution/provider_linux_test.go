//go:build linux

package processattribution

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDecodeInetDiagIPv4Response(t *testing.T) {
	message := makeLinuxInetDiagFixture(t, 2, "127.0.0.1", 43120, "127.0.0.1", 8080, 1000, 424242)
	row, err := decodeLinuxInetDiagMessage(message)
	if err != nil {
		t.Fatalf("decodeLinuxInetDiagMessage: %v", err)
	}
	if row.local != netip.MustParseAddrPort("127.0.0.1:43120") ||
		row.remote != netip.MustParseAddrPort("127.0.0.1:8080") ||
		row.uid != 1000 || row.inode != 424242 {
		t.Fatalf("decoded row = %+v", row)
	}
}

func TestDecodeInetDiagIPv6Response(t *testing.T) {
	message := makeLinuxInetDiagFixture(t, 10, "::1", 43121, "2001:db8::1", 8443, 1001, 434343)
	row, err := decodeLinuxInetDiagMessage(message)
	if err != nil {
		t.Fatalf("decodeLinuxInetDiagMessage: %v", err)
	}
	if row.local != netip.MustParseAddrPort("[::1]:43121") ||
		row.remote != netip.MustParseAddrPort("[2001:db8::1]:8443") ||
		row.uid != 1001 || row.inode != 434343 {
		t.Fatalf("decoded row = %+v", row)
	}
}

func TestLinuxTupleDirectionMatchesClientSocket(t *testing.T) {
	tuple := EndpointTuple{
		Client: netip.MustParseAddrPort("127.0.0.1:43122"),
		Proxy:  netip.MustParseAddrPort("127.0.0.1:8080"),
	}
	rows := []linuxSocketOwnerRow{
		{local: tuple.Proxy, remote: tuple.Client, inode: 1},
		{local: tuple.Client, remote: tuple.Proxy, inode: 2},
		{local: tuple.Client, remote: netip.MustParseAddrPort("127.0.0.1:9090"), inode: 3},
	}

	matches := linuxRowsForTuple(rows, tuple)
	if len(matches) != 1 || matches[0].inode != 2 {
		t.Fatalf("matching rows = %+v, want inode 2", matches)
	}
}

func TestLinuxProviderRefreshesCachedSnapshotAfterTupleMiss(t *testing.T) {
	firstTuple := EndpointTuple{
		Client: netip.MustParseAddrPort("127.0.0.1:43123"),
		Proxy:  netip.MustParseAddrPort("127.0.0.1:8080"),
	}
	secondTuple := EndpointTuple{
		Client: netip.MustParseAddrPort("127.0.0.1:43124"),
		Proxy:  netip.MustParseAddrPort("127.0.0.1:8080"),
	}
	uid := uint32(os.Geteuid())
	firstRow := linuxSocketOwnerRow{
		local:  firstTuple.Client,
		remote: firstTuple.Proxy,
		uid:    uid,
		inode:  424242,
		state:  linuxTCPStateEstablished,
	}
	secondRow := linuxSocketOwnerRow{
		local:  secondTuple.Client,
		remote: secondTuple.Proxy,
		uid:    uid,
		inode:  434343,
		state:  linuxTCPStateEstablished,
	}

	procRoot := t.TempDir()
	for _, process := range []struct {
		pid   uint32
		inode uint64
	}{
		{pid: 4242, inode: firstRow.inode},
		{pid: 4343, inode: secondRow.inode},
	} {
		pidRoot := filepath.Join(procRoot, strconv.FormatUint(uint64(process.pid), 10))
		if err := os.MkdirAll(filepath.Join(pidRoot, "fd"), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		status := "Name:\ttest\nUid:\t" + strconv.FormatUint(uint64(uid), 10) + "\n"
		if err := os.WriteFile(filepath.Join(pidRoot, "status"), []byte(status), 0o600); err != nil {
			t.Fatalf("WriteFile status: %v", err)
		}
		socket := "socket:[" + strconv.FormatUint(process.inode, 10) + "]"
		if err := os.Symlink(socket, filepath.Join(pidRoot, "fd", "3")); err != nil {
			t.Fatalf("Symlink socket: %v", err)
		}
	}

	provider := newLinuxProvider()
	provider.procRoot = procRoot
	now := time.Now()
	provider.now = func() time.Time { return now }
	loadCalls := 0
	provider.dumpRows = func(context.Context) ([]linuxSocketOwnerRow, error) {
		loadCalls++
		if loadCalls == 1 {
			return []linuxSocketOwnerRow{firstRow}, nil
		}
		return []linuxSocketOwnerRow{firstRow, secondRow}, nil
	}
	provider.identity = func(pid uint32) (linuxProcessIdentity, error) {
		return linuxProcessIdentity{
			startToken:         strconv.FormatUint(uint64(pid), 10),
			displayName:        "test process",
			processName:        "test",
			executablePath:     "/usr/bin/test",
			identityConfidence: "exact",
		}, nil
	}

	first := provider.Lookup(context.Background(), firstTuple)
	if first.Status != StatusResolved || first.PID != 4242 {
		t.Fatalf("first lookup = %+v, want PID 4242", first)
	}
	second := provider.Lookup(context.Background(), secondTuple)
	if second.Status != StatusResolved || second.PID != 4343 {
		t.Fatalf("second lookup = %+v, want refreshed PID 4343", second)
	}
	if loadCalls != 2 {
		t.Fatalf("socket table load calls = %d, want 2", loadCalls)
	}
}

func TestParseProcStatWithSpacesAndParentheses(t *testing.T) {
	stat := "4242 (worker ) with spaces) S " + strings.Join([]string{
		"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "987654", "0",
	}, " ")
	comm, startTicks, err := parseLinuxProcStat(stat)
	if err != nil {
		t.Fatalf("parseLinuxProcStat: %v", err)
	}
	if comm != "worker ) with spaces" || startTicks != "987654" {
		t.Fatalf("parsed stat = (%q, %q)", comm, startTicks)
	}
}

func TestFindSocketOwnersReturnsAmbiguousForMultiplePIDs(t *testing.T) {
	procRoot := t.TempDir()
	for _, pid := range []uint32{100, 101} {
		pidRoot := filepath.Join(procRoot, strconv.FormatUint(uint64(pid), 10))
		if err := os.MkdirAll(filepath.Join(pidRoot, "fd"), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(pidRoot, "status"), []byte("Name:\tfixture\nUid:\t1000\t1000\t1000\t1000\n"), 0o600); err != nil {
			t.Fatalf("WriteFile status: %v", err)
		}
		if err := os.Symlink("socket:[777]", filepath.Join(pidRoot, "fd", "3")); err != nil {
			t.Fatalf("Symlink: %v", err)
		}
	}

	owners, denied, err := findLinuxSocketOwners(procRoot, []linuxSocketOwnerRow{{inode: 777, uid: 1000}}, false)
	if err != nil {
		t.Fatalf("findLinuxSocketOwners: %v", err)
	}
	if denied {
		t.Fatal("findLinuxSocketOwners unexpectedly reported permission denial")
	}
	if !reflect.DeepEqual(owners, []uint32{100, 101}) {
		t.Fatalf("owners = %v, want [100 101]", owners)
	}
}

func TestLinuxProcessCacheInvalidatesOnStartTicksChange(t *testing.T) {
	identities := []linuxProcessIdentity{
		{startToken: "100", displayName: "Old Process", processName: "worker", identityConfidence: "none"},
		{startToken: "200", displayName: "New Process", processName: "worker", identityConfidence: "none"},
	}
	index := 0
	lookup := func(uint32) (linuxProcessIdentity, error) {
		identity := identities[index]
		index++
		return identity, nil
	}
	first := linuxResultForPID(5151, lookup)
	second := linuxResultForPID(5151, lookup)
	if first.StartToken == second.StartToken || second.DisplayName != "New Process" {
		t.Fatalf("PID reuse results = first %+v, second %+v", first, second)
	}
}

func TestLinuxProviderFindsCurrentLoopbackProcess(t *testing.T) {
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
	provider := newLinuxProvider()
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

func makeLinuxInetDiagFixture(t *testing.T, family byte, local string, localPort uint16, remote string, remotePort uint16, uid, inode uint32) []byte {
	t.Helper()
	message := make([]byte, linuxInetDiagMessageSize)
	message[0] = family
	message[1] = linuxTCPStateEstablished
	binary.BigEndian.PutUint16(message[4:6], localPort)
	binary.BigEndian.PutUint16(message[6:8], remotePort)
	localAddress := netip.MustParseAddr(local)
	remoteAddress := netip.MustParseAddr(remote)
	if family == 2 {
		copy(message[8:12], localAddress.AsSlice())
		copy(message[24:28], remoteAddress.AsSlice())
	} else {
		copy(message[8:24], localAddress.AsSlice())
		copy(message[24:40], remoteAddress.AsSlice())
	}
	binary.NativeEndian.PutUint32(message[64:68], uid)
	binary.NativeEndian.PutUint32(message[68:72], inode)
	return message
}
