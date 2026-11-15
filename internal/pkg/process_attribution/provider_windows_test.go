//go:build windows

package processattribution

import (
	"context"
	"encoding/binary"
	"image"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsProviderSerializesIconLoads(t *testing.T) {
	provider := newWindowsProvider()
	var active atomic.Int32
	var maximum atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(2)
	provider.loadIcon = func(string) (image.Image, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		return image.NewNRGBA(image.Rect(0, 0, 1, 1)), nil
	}

	var wait sync.WaitGroup
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			if _, err := provider.LoadIcon(context.Background(), Result{ExecutablePath: "C:/test/app.exe"}); err != nil {
				t.Errorf("LoadIcon: %v", err)
			}
		}()
	}
	ready.Wait()
	close(start)
	<-entered
	select {
	case <-entered:
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	wait.Wait()
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent icon loads = %d, want 1", got)
	}
}

func TestParseWindowsIPv4OwnerRows(t *testing.T) {
	table := make([]byte, 4+windowsIPv4OwnerRowSize)
	binary.LittleEndian.PutUint32(table[:4], 1)
	row := table[4:]
	binary.LittleEndian.PutUint32(row[0:4], windowsTCPStateEstablished)
	copy(row[4:8], netip.MustParseAddr("127.0.0.1").AsSlice())
	binary.BigEndian.PutUint16(row[8:10], 43120)
	copy(row[12:16], netip.MustParseAddr("127.0.0.1").AsSlice())
	binary.BigEndian.PutUint16(row[16:18], 8080)
	binary.LittleEndian.PutUint32(row[20:24], 4242)

	rows, err := parseWindowsIPv4OwnerRows(table)
	if err != nil {
		t.Fatalf("parseWindowsIPv4OwnerRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	wantLocal := netip.MustParseAddrPort("127.0.0.1:43120")
	wantRemote := netip.MustParseAddrPort("127.0.0.1:8080")
	if rows[0].local != wantLocal || rows[0].remote != wantRemote || rows[0].pid != 4242 || rows[0].state != windowsTCPStateEstablished {
		t.Fatalf("parsed row = %+v", rows[0])
	}
}

func TestParseWindowsIPv6OwnerRows(t *testing.T) {
	table := make([]byte, 4+windowsIPv6OwnerRowSize)
	binary.LittleEndian.PutUint32(table[:4], 1)
	row := table[4:]
	copy(row[0:16], netip.MustParseAddr("::1").AsSlice())
	binary.LittleEndian.PutUint32(row[16:20], 0)
	binary.BigEndian.PutUint16(row[20:22], 43121)
	copy(row[24:40], netip.MustParseAddr("::1").AsSlice())
	binary.LittleEndian.PutUint32(row[40:44], 0)
	binary.BigEndian.PutUint16(row[44:46], 8080)
	binary.LittleEndian.PutUint32(row[48:52], windowsTCPStateEstablished)
	binary.LittleEndian.PutUint32(row[52:56], 4343)

	rows, err := parseWindowsIPv6OwnerRows(table)
	if err != nil {
		t.Fatalf("parseWindowsIPv6OwnerRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	wantLocal := netip.MustParseAddrPort("[::1]:43121")
	wantRemote := netip.MustParseAddrPort("[::1]:8080")
	if rows[0].local != wantLocal || rows[0].remote != wantRemote || rows[0].pid != 4343 || rows[0].state != windowsTCPStateEstablished {
		t.Fatalf("parsed row = %+v", rows[0])
	}
}

func TestReadWindowsExtendedTCPTableRetriesResize(t *testing.T) {
	calls := 0
	data, err := readWindowsExtendedTCPTable(func(buffer []byte, size *uint32) syscall.Errno {
		calls++
		switch calls {
		case 1:
			if len(buffer) != 0 {
				t.Fatalf("size probe buffer length = %d, want 0", len(buffer))
			}
			*size = 8
			return windows.ERROR_INSUFFICIENT_BUFFER
		case 2:
			if len(buffer) != 8 {
				t.Fatalf("first data buffer length = %d, want 8", len(buffer))
			}
			*size = 16
			return windows.ERROR_INSUFFICIENT_BUFFER
		case 3:
			if len(buffer) != 16 {
				t.Fatalf("resized data buffer length = %d, want 16", len(buffer))
			}
			copy(buffer, []byte("table-result"))
			*size = uint32(len("table-result"))
			return 0
		default:
			t.Fatalf("unexpected query call %d", calls)
			return windows.ERROR_INVALID_PARAMETER
		}
	})
	if err != nil {
		t.Fatalf("readWindowsExtendedTCPTable: %v", err)
	}
	if calls != 3 || string(data) != "table-result" {
		t.Fatalf("result = %q after %d calls, want table-result after 3 calls", data, calls)
	}
}

func TestWindowsTupleDirectionMatchesClientSocket(t *testing.T) {
	tuple := EndpointTuple{
		Client: netip.MustParseAddrPort("127.0.0.1:43122"),
		Proxy:  netip.MustParseAddrPort("127.0.0.1:8080"),
	}
	rows := []windowsSocketOwnerRow{
		{local: tuple.Proxy, remote: tuple.Client, pid: 1, state: windowsTCPStateEstablished},
		{local: tuple.Client, remote: tuple.Proxy, pid: 2, state: windowsTCPStateEstablished},
		{local: netip.MustParseAddrPort("127.0.0.1:43122"), remote: netip.MustParseAddrPort("127.0.0.1:9090"), pid: 3, state: windowsTCPStateEstablished},
	}

	pids := windowsTupleOwnerPIDs(rows, tuple)
	if len(pids) != 1 || pids[0] != 2 {
		t.Fatalf("matching PIDs = %v, want [2]", pids)
	}
}

func TestWindowsProviderRefreshesCachedSnapshotAfterTupleMiss(t *testing.T) {
	firstTuple := EndpointTuple{
		Client: netip.MustParseAddrPort("127.0.0.1:43123"),
		Proxy:  netip.MustParseAddrPort("127.0.0.1:8080"),
	}
	secondTuple := EndpointTuple{
		Client: netip.MustParseAddrPort("127.0.0.1:43124"),
		Proxy:  netip.MustParseAddrPort("127.0.0.1:8080"),
	}
	firstRow := windowsSocketOwnerRow{
		local:  firstTuple.Client,
		remote: firstTuple.Proxy,
		pid:    4242,
		state:  windowsTCPStateEstablished,
	}
	secondRow := windowsSocketOwnerRow{
		local:  secondTuple.Client,
		remote: secondTuple.Proxy,
		pid:    4343,
		state:  windowsTCPStateEstablished,
	}

	provider := newWindowsProvider()
	now := time.Now()
	provider.now = func() time.Time { return now }
	loadCalls := 0
	provider.loadTable = func(uint32) ([]windowsSocketOwnerRow, error) {
		loadCalls++
		if loadCalls == 1 {
			return []windowsSocketOwnerRow{firstRow}, nil
		}
		return []windowsSocketOwnerRow{firstRow, secondRow}, nil
	}
	provider.identity = func(pid uint32) (windowsProcessIdentity, error) {
		return windowsProcessIdentity{
			startToken:         strconv.FormatUint(uint64(pid), 10),
			displayName:        "test process",
			processName:        "test.exe",
			executablePath:     `C:\test.exe`,
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
		t.Fatalf("TCP table load calls = %d, want 2", loadCalls)
	}
}

func TestWindowsProcessStartTokenUsesCreationTime(t *testing.T) {
	creation := windows.Filetime{LowDateTime: 0x89abcdef, HighDateTime: 0x01234567}
	want := strconv.FormatUint(0x0123456789abcdef, 10)
	if got := windowsProcessStartToken(creation); got != want {
		t.Fatalf("windowsProcessStartToken = %q, want %q", got, want)
	}
}

func TestWindowsMetadataDeniedRetainsPID(t *testing.T) {
	result := windowsResultForPID(5151, func(uint32) (windowsProcessIdentity, error) {
		return windowsProcessIdentity{}, windows.ERROR_ACCESS_DENIED
	})
	if result.Status != StatusResolved || result.PID != 5151 {
		t.Fatalf("metadata-denied result = %+v", result)
	}
	if result.Reason != "metadata_denied" || result.Source != "windows_tcp_table" {
		t.Fatalf("metadata-denied degradation = %+v", result)
	}
}

func TestWindowsProviderFindsCurrentLoopbackProcess(t *testing.T) {
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
	provider := newWindowsProvider()
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
	t.Fatalf("loopback lookup = %+v, want PID %d", result, os.Getpid())
}
