//go:build windows

package processattribution

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsAFInet                      = 2
	windowsAFInet6                     = 23
	windowsTCPTableOwnerPIDConnections = 4
	windowsTCPStateEstablished         = 5
	windowsIPv4OwnerRowSize            = 24
	windowsIPv6OwnerRowSize            = 56
	windowsSocketSnapshotTTL           = 100 * time.Millisecond
	windowsTCPTableReadAttempts        = 4
)

var (
	windowsIPHlpAPI                  = windows.NewLazySystemDLL("iphlpapi.dll")
	windowsGetExtendedTCPTable       = windowsIPHlpAPI.NewProc("GetExtendedTcpTable")
	windowsKernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	windowsGetApplicationUserModelID = windowsKernel32.NewProc("GetApplicationUserModelId")
	windowsGetPackageFullName        = windowsKernel32.NewProc("GetPackageFullName")
)

type windowsSocketOwnerRow struct {
	local  netip.AddrPort
	remote netip.AddrPort
	pid    uint32
	state  uint32
}

type windowsSocketSnapshot struct {
	rows       []windowsSocketOwnerRow
	captured   time.Time
	captureErr error
}

type windowsProcessIdentity struct {
	startToken         string
	displayName        string
	processName        string
	executablePath     string
	appID              string
	identityConfidence string
}

type windowsProvider struct {
	mu        chan struct{}
	iconMu    chan struct{}
	ipv4      windowsSocketSnapshot
	ipv6      windowsSocketSnapshot
	now       func() time.Time
	loadTable func(uint32) ([]windowsSocketOwnerRow, error)
	identity  func(uint32) (windowsProcessIdentity, error)
	loadIcon  func(string) (image.Image, error)
}

func init() {
	platformProviderFactory = func() Provider { return newWindowsProvider() }
}

func newWindowsProvider() *windowsProvider {
	provider := &windowsProvider{
		mu:       make(chan struct{}, 1),
		iconMu:   make(chan struct{}, 1),
		now:      time.Now,
		identity: lookupWindowsProcessIdentity,
		loadIcon: loadWindowsIcon,
	}
	provider.mu <- struct{}{}
	provider.iconMu <- struct{}{}
	provider.loadTable = provider.captureTable
	return provider
}

func (p *windowsProvider) Lookup(ctx context.Context, tuple EndpointTuple) Result {
	if err := ctx.Err(); err != nil {
		return Result{Status: StatusNotFound, Source: "windows_tcp_table", Reason: "lookup_cancelled"}
	}
	tuple = normalizeEndpointTuple(tuple)
	if !tuple.Client.IsValid() || !tuple.Proxy.IsValid() || tuple.Client.Addr().Is4() != tuple.Proxy.Addr().Is4() {
		return Result{Status: StatusNotFound, Source: "windows_tcp_table", Reason: "invalid_endpoint"}
	}
	family := uint32(windowsAFInet6)
	if tuple.Client.Addr().Is4() {
		family = windowsAFInet
	}
	rows, err := p.socketRows(ctx, family, false)
	if err != nil {
		status := StatusNotFound
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			status = StatusPermissionDenied
		}
		return Result{Status: status, Source: "windows_tcp_table", Reason: "socket_table_unavailable"}
	}
	pids := windowsTupleOwnerPIDs(rows, tuple)
	if len(pids) == 0 {
		rows, err = p.socketRows(ctx, family, true)
		if err != nil {
			status := StatusNotFound
			if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				status = StatusPermissionDenied
			}
			return Result{Status: status, Source: "windows_tcp_table", Reason: "socket_table_unavailable"}
		}
		pids = windowsTupleOwnerPIDs(rows, tuple)
	}
	switch len(pids) {
	case 0:
		return Result{Status: StatusNotFound, Source: "windows_tcp_table", Reason: "socket_owner_not_found"}
	case 1:
		return windowsResultForPID(pids[0], p.identity)
	default:
		return Result{Status: StatusAmbiguous, Source: "windows_tcp_table", Reason: "multiple_socket_owners"}
	}
}

func (p *windowsProvider) LoadIcon(ctx context.Context, result Result) (image.Image, error) {
	if result.ExecutablePath == "" {
		return nil, &IconUnavailableError{Reason: "executable_path_unavailable"}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.iconMu:
	}
	defer func() { p.iconMu <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return p.loadIcon(result.ExecutablePath)
}

func (p *windowsProvider) socketRows(
	ctx context.Context,
	family uint32,
	forceRefresh bool,
) ([]windowsSocketOwnerRow, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.mu:
	}
	defer func() { p.mu <- struct{}{} }()

	now := p.now()
	snapshot := &p.ipv6
	if family == windowsAFInet {
		snapshot = &p.ipv4
	}
	if !forceRefresh &&
		!snapshot.captured.IsZero() &&
		now.Sub(snapshot.captured) < windowsSocketSnapshotTTL {
		return snapshot.rows, snapshot.captureErr
	}
	rows, err := p.loadTable(family)
	*snapshot = windowsSocketSnapshot{rows: rows, captured: now, captureErr: err}
	return rows, err
}

func (p *windowsProvider) captureTable(family uint32) ([]windowsSocketOwnerRow, error) {
	data, err := getWindowsExtendedTCPTable(family)
	if err != nil {
		return nil, err
	}
	if family == windowsAFInet {
		return parseWindowsIPv4OwnerRows(data)
	}
	return parseWindowsIPv6OwnerRows(data)
}

func getWindowsExtendedTCPTable(family uint32) ([]byte, error) {
	return readWindowsExtendedTCPTable(func(buffer []byte, size *uint32) syscall.Errno {
		var table uintptr
		if len(buffer) > 0 {
			table = uintptr(unsafe.Pointer(&buffer[0]))
		}
		result, _, _ := windowsGetExtendedTCPTable.Call(
			table,
			uintptr(unsafe.Pointer(size)),
			0,
			uintptr(family),
			windowsTCPTableOwnerPIDConnections,
			0,
		)
		return syscall.Errno(result)
	})
}

func readWindowsExtendedTCPTable(query func([]byte, *uint32) syscall.Errno) ([]byte, error) {
	var size uint32
	result := query(nil, &size)
	if result != 0 && result != windows.ERROR_INSUFFICIENT_BUFFER {
		return nil, result
	}

	for range windowsTCPTableReadAttempts {
		if size < 4 {
			return nil, errors.New("Windows TCP table returned an invalid size")
		}
		data := make([]byte, size)
		result = query(data, &size)
		if result == 0 {
			if uint64(size) > uint64(len(data)) {
				return nil, errors.New("Windows TCP table returned a size larger than its buffer")
			}
			return data[:size], nil
		}
		if result != windows.ERROR_INSUFFICIENT_BUFFER {
			return nil, result
		}
	}
	return nil, windows.ERROR_INSUFFICIENT_BUFFER
}

func parseWindowsIPv4OwnerRows(data []byte) ([]windowsSocketOwnerRow, error) {
	if len(data) < 4 {
		return nil, errors.New("Windows IPv4 TCP table is truncated")
	}
	count := int(binary.LittleEndian.Uint32(data[:4]))
	if count > (len(data)-4)/windowsIPv4OwnerRowSize {
		return nil, errors.New("Windows IPv4 TCP row count exceeds table size")
	}
	rows := make([]windowsSocketOwnerRow, 0, count)
	for index := range count {
		offset := 4 + index*windowsIPv4OwnerRowSize
		row := data[offset : offset+windowsIPv4OwnerRowSize]
		localAddress := netip.AddrFrom4([4]byte(row[4:8]))
		remoteAddress := netip.AddrFrom4([4]byte(row[12:16]))
		rows = append(rows, windowsSocketOwnerRow{
			local:  netip.AddrPortFrom(localAddress, binary.BigEndian.Uint16(row[8:10])),
			remote: netip.AddrPortFrom(remoteAddress, binary.BigEndian.Uint16(row[16:18])),
			pid:    binary.LittleEndian.Uint32(row[20:24]),
			state:  binary.LittleEndian.Uint32(row[0:4]),
		})
	}
	return rows, nil
}

func parseWindowsIPv6OwnerRows(data []byte) ([]windowsSocketOwnerRow, error) {
	if len(data) < 4 {
		return nil, errors.New("Windows IPv6 TCP table is truncated")
	}
	count := int(binary.LittleEndian.Uint32(data[:4]))
	if count > (len(data)-4)/windowsIPv6OwnerRowSize {
		return nil, errors.New("Windows IPv6 TCP row count exceeds table size")
	}
	rows := make([]windowsSocketOwnerRow, 0, count)
	for index := range count {
		offset := 4 + index*windowsIPv6OwnerRowSize
		row := data[offset : offset+windowsIPv6OwnerRowSize]
		localAddress := netip.AddrFrom16([16]byte(row[0:16]))
		remoteAddress := netip.AddrFrom16([16]byte(row[24:40]))
		rows = append(rows, windowsSocketOwnerRow{
			local:  netip.AddrPortFrom(localAddress, binary.BigEndian.Uint16(row[20:22])),
			remote: netip.AddrPortFrom(remoteAddress, binary.BigEndian.Uint16(row[44:46])),
			pid:    binary.LittleEndian.Uint32(row[52:56]),
			state:  binary.LittleEndian.Uint32(row[48:52]),
		})
	}
	return rows, nil
}

func windowsTupleOwnerPIDs(rows []windowsSocketOwnerRow, tuple EndpointTuple) []uint32 {
	tuple = normalizeEndpointTuple(tuple)
	seen := make(map[uint32]struct{})
	for _, row := range rows {
		if normalizeAddrPort(row.local) == tuple.Client && normalizeAddrPort(row.remote) == tuple.Proxy {
			seen[row.pid] = struct{}{}
		}
	}
	pids := make([]uint32, 0, len(seen))
	for pid := range seen {
		pids = append(pids, pid)
	}
	slices.Sort(pids)
	return pids
}

func windowsResultForPID(pid uint32, identityLookup func(uint32) (windowsProcessIdentity, error)) Result {
	identity, err := identityLookup(pid)
	result := Result{
		Status:             StatusResolved,
		PID:                pid,
		StartToken:         identity.startToken,
		DisplayName:        identity.displayName,
		ProcessName:        identity.processName,
		ExecutablePath:     identity.executablePath,
		AppID:              identity.appID,
		Source:             "windows_tcp_table",
		IdentityConfidence: identity.identityConfidence,
	}
	if result.DisplayName == "" {
		result.DisplayName = result.ProcessName
	}
	if result.IdentityConfidence == "" {
		result.IdentityConfidence = "none"
	}
	if err != nil {
		result.Reason = "metadata_denied"
	}
	return result
}

func lookupWindowsProcessIdentity(pid uint32) (windowsProcessIdentity, error) {
	var identity windowsProcessIdentity
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return identity, err
	}
	defer windows.CloseHandle(handle)

	var creation, exit, kernel, user windows.Filetime
	var metadataErr error
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		metadataErr = err
	} else {
		identity.startToken = windowsProcessStartToken(creation)
	}
	path, err := queryWindowsProcessPath(handle)
	if err != nil {
		metadataErr = err
	} else {
		identity.executablePath = path
		identity.processName = filepath.Base(path)
	}
	identity.appID = queryWindowsApplicationIdentity(handle)
	if identity.executablePath != "" {
		identity.displayName = queryWindowsVersionDisplayName(identity.executablePath)
		if identity.displayName != "" {
			identity.identityConfidence = "exact"
		}
	}
	if identity.displayName == "" {
		identity.displayName = identity.processName
		identity.identityConfidence = "none"
	}
	return identity, metadataErr
}

func windowsProcessStartToken(creation windows.Filetime) string {
	value := uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)
	return strconv.FormatUint(value, 10)
}

func queryWindowsProcessPath(handle windows.Handle) (string, error) {
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func queryWindowsVersionDisplayName(path string) string {
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || size == 0 {
		return ""
	}
	data := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&data[0])); err != nil {
		return ""
	}
	translations := windowsVersionTranslations(data)
	translations = append(translations, [2]uint16{0x0409, 0x04b0}, [2]uint16{0x0409, 0x04e4})
	for _, translation := range translations {
		for _, field := range []string{"ProductName", "FileDescription"} {
			query := fmt.Sprintf(`\StringFileInfo\%04x%04x\%s`, translation[0], translation[1], field)
			if value := windowsVersionString(data, query); value != "" {
				return value
			}
		}
	}
	return ""
}

func windowsVersionTranslations(data []byte) [][2]uint16 {
	var pointer unsafe.Pointer
	var length uint32
	if err := windows.VerQueryValue(
		unsafe.Pointer(&data[0]),
		`\VarFileInfo\Translation`,
		unsafe.Pointer(&pointer),
		&length,
	); err != nil || pointer == nil || length < 4 {
		return nil
	}
	values := unsafe.Slice((*uint16)(pointer), int(length)/2)
	translations := make([][2]uint16, 0, len(values)/2)
	for index := 0; index+1 < len(values); index += 2 {
		translations = append(translations, [2]uint16{values[index], values[index+1]})
	}
	return translations
}

func windowsVersionString(data []byte, query string) string {
	var pointer *uint16
	var length uint32
	if err := windows.VerQueryValue(
		unsafe.Pointer(&data[0]),
		query,
		unsafe.Pointer(&pointer),
		&length,
	); err != nil || pointer == nil || length == 0 {
		return ""
	}
	return windows.UTF16PtrToString(pointer)
}

func queryWindowsApplicationIdentity(handle windows.Handle) string {
	if value := queryWindowsProcessString(windowsGetApplicationUserModelID, handle); value != "" {
		return value
	}
	return queryWindowsProcessString(windowsGetPackageFullName, handle)
}

func queryWindowsProcessString(proc *windows.LazyProc, handle windows.Handle) string {
	var length uint32
	result, _, _ := proc.Call(uintptr(handle), uintptr(unsafe.Pointer(&length)), 0)
	if syscall.Errno(result) != windows.ERROR_INSUFFICIENT_BUFFER || length == 0 {
		return ""
	}
	buffer := make([]uint16, length)
	result, _, _ = proc.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&length)),
		uintptr(unsafe.Pointer(&buffer[0])),
	)
	if result != 0 {
		return ""
	}
	return windows.UTF16ToString(buffer)
}
