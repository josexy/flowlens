//go:build linux

package processattribution

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	linuxAFInet                = 2
	linuxAFInet6               = 10
	linuxIPProtocolTCP         = 6
	linuxTCPStateEstablished   = 1
	linuxInetDiagRequestSize   = 56
	linuxInetDiagMessageSize   = 72
	linuxNetlinkHeaderSize     = 16
	linuxSocketSnapshotTTL     = 100 * time.Millisecond
	linuxNetlinkPollIntervalMS = 100
)

type linuxSocketOwnerRow struct {
	local  netip.AddrPort
	remote netip.AddrPort
	uid    uint32
	inode  uint64
	state  uint8
}

type linuxSocketSnapshot struct {
	rows       []linuxSocketOwnerRow
	captured   time.Time
	captureErr error
}

type linuxProcessIdentity struct {
	startToken         string
	displayName        string
	processName        string
	executablePath     string
	appID              string
	icon               string
	identityConfidence string
	metadataDenied     bool
}

type linuxProvider struct {
	mu       sync.Mutex
	snapshot linuxSocketSnapshot
	now      func() time.Time
	procRoot string
	dumpRows func(context.Context) ([]linuxSocketOwnerRow, error)
	identity func(uint32) (linuxProcessIdentity, error)
}

func init() {
	platformProviderFactory = func() Provider { return newLinuxProvider() }
}

func newLinuxProvider() *linuxProvider {
	dataDirs := linuxDesktopDataDirs()
	resolver := newLazyLinuxDesktopResolver(dataDirs)
	provider := &linuxProvider{
		now:      time.Now,
		procRoot: "/proc",
		dumpRows: captureLinuxSocketRows,
	}
	provider.identity = func(pid uint32) (linuxProcessIdentity, error) {
		return lookupLinuxProcessIdentity(pid, provider.procRoot, resolver)
	}
	return provider
}

func (p *linuxProvider) Lookup(ctx context.Context, tuple EndpointTuple) Result {
	if err := ctx.Err(); err != nil {
		return Result{Status: StatusNotFound, Source: "linux_inet_diag", Reason: "lookup_cancelled"}
	}
	tuple = normalizeEndpointTuple(tuple)
	if !tuple.Client.IsValid() || !tuple.Proxy.IsValid() || tuple.Client.Addr().Is4() != tuple.Proxy.Addr().Is4() {
		return Result{Status: StatusNotFound, Source: "linux_inet_diag", Reason: "invalid_endpoint"}
	}
	rows, err := p.socketRows(ctx, false)
	if err != nil {
		status := StatusNotFound
		if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) {
			status = StatusPermissionDenied
		}
		return Result{Status: status, Source: "linux_inet_diag", Reason: "socket_table_unavailable"}
	}
	matches := linuxRowsForTuple(rows, tuple)
	if len(matches) == 0 {
		rows, err = p.socketRows(ctx, true)
		if err != nil {
			status := StatusNotFound
			if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) {
				status = StatusPermissionDenied
			}
			return Result{Status: status, Source: "linux_inet_diag", Reason: "socket_table_unavailable"}
		}
		matches = linuxRowsForTuple(rows, tuple)
		if len(matches) == 0 {
			return Result{Status: StatusNotFound, Source: "linux_inet_diag", Reason: "socket_owner_not_found"}
		}
	}
	owners, permissionDenied, err := findLinuxSocketOwners(p.procRoot, matches, os.Geteuid() == 0)
	if err != nil {
		status := StatusNotFound
		if errors.Is(err, os.ErrPermission) {
			status = StatusPermissionDenied
		}
		return Result{Status: status, Source: "linux_inet_diag", Reason: "process_scan_unavailable"}
	}
	switch len(owners) {
	case 0:
		if permissionDenied {
			return Result{Status: StatusPermissionDenied, Source: "linux_inet_diag", Reason: "process_scan_restricted"}
		}
		return Result{Status: StatusNotFound, Source: "linux_inet_diag", Reason: "socket_owner_not_found"}
	case 1:
		return linuxResultForPID(owners[0], p.identity)
	default:
		return Result{Status: StatusAmbiguous, Source: "linux_inet_diag", Reason: "multiple_socket_owners"}
	}
}

func (p *linuxProvider) LoadIcon(ctx context.Context, result Result) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	identity, err := p.identity(result.PID)
	if err != nil || identity.startToken == "" || identity.startToken != result.StartToken {
		return nil, &IconUnavailableError{Reason: "linux_process_identity_changed"}
	}
	if identity.icon == "" {
		return nil, &IconUnavailableError{Reason: "linux_icon_name_unavailable"}
	}
	path, err := resolveLinuxIconPath(identity.icon, linuxDesktopDataDirs())
	if err != nil {
		return nil, err
	}
	icon, err := loadLinuxIconFile(path)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return icon, nil
}

func (p *linuxProvider) socketRows(
	ctx context.Context,
	forceRefresh bool,
) ([]linuxSocketOwnerRow, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := p.now()
	if !forceRefresh &&
		!p.snapshot.captured.IsZero() &&
		now.Sub(p.snapshot.captured) < linuxSocketSnapshotTTL {
		return p.snapshot.rows, p.snapshot.captureErr
	}
	rows, err := p.dumpRows(ctx)
	p.snapshot = linuxSocketSnapshot{rows: rows, captured: now, captureErr: err}
	return rows, err
}

func captureLinuxSocketRows(ctx context.Context) ([]linuxSocketOwnerRow, error) {
	var rows []linuxSocketOwnerRow
	for _, family := range []uint8{linuxAFInet, linuxAFInet6} {
		familyRows, err := queryLinuxInetDiag(ctx, family)
		if err != nil {
			return nil, err
		}
		rows = append(rows, familyRows...)
	}
	return rows, nil
}

func queryLinuxInetDiag(ctx context.Context, family uint8) ([]linuxSocketOwnerRow, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, unix.NETLINK_SOCK_DIAG)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, err
	}

	sequence := uint32(time.Now().UnixNano())
	request := makeLinuxInetDiagRequest(family, sequence)
	if err := unix.Sendto(fd, request, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, err
	}

	buffer := make([]byte, 1<<20)
	var rows []linuxSocketOwnerRow
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		ready, err := unix.Poll(pollFDs, linuxNetlinkPollIntervalMS)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return nil, err
		}
		if ready == 0 {
			continue
		}
		length, _, err := unix.Recvfrom(fd, buffer, 0)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return nil, err
		}
		messages, err := syscall.ParseNetlinkMessage(buffer[:length])
		if err != nil {
			return nil, fmt.Errorf("parse INET_DIAG netlink response: %w", err)
		}
		for _, message := range messages {
			if message.Header.Seq != sequence {
				continue
			}
			switch message.Header.Type {
			case unix.NLMSG_DONE:
				return rows, nil
			case unix.NLMSG_ERROR:
				if len(message.Data) < 4 {
					return nil, errors.New("INET_DIAG returned a truncated netlink error")
				}
				code := int32(binary.NativeEndian.Uint32(message.Data[:4]))
				if code == 0 {
					continue
				}
				if code < 0 {
					code = -code
				}
				return nil, syscall.Errno(code)
			case unix.SOCK_DIAG_BY_FAMILY:
				row, err := decodeLinuxInetDiagMessage(message.Data)
				if err != nil {
					return nil, err
				}
				rows = append(rows, row)
			}
		}
	}
}

func makeLinuxInetDiagRequest(family uint8, sequence uint32) []byte {
	request := make([]byte, linuxNetlinkHeaderSize+linuxInetDiagRequestSize)
	binary.NativeEndian.PutUint32(request[0:4], uint32(len(request)))
	binary.NativeEndian.PutUint16(request[4:6], unix.SOCK_DIAG_BY_FAMILY)
	binary.NativeEndian.PutUint16(request[6:8], unix.NLM_F_REQUEST|unix.NLM_F_DUMP)
	binary.NativeEndian.PutUint32(request[8:12], sequence)
	payload := request[linuxNetlinkHeaderSize:]
	payload[0] = family
	payload[1] = linuxIPProtocolTCP
	binary.NativeEndian.PutUint32(payload[4:8], ^uint32(0))
	for index := 48; index < 56; index++ {
		payload[index] = 0xff
	}
	return request
}

func decodeLinuxInetDiagMessage(message []byte) (linuxSocketOwnerRow, error) {
	if len(message) < linuxInetDiagMessageSize {
		return linuxSocketOwnerRow{}, errors.New("INET_DIAG response is truncated")
	}
	family := message[0]
	var localAddress, remoteAddress netip.Addr
	switch family {
	case linuxAFInet:
		localAddress = netip.AddrFrom4([4]byte(message[8:12]))
		remoteAddress = netip.AddrFrom4([4]byte(message[24:28]))
	case linuxAFInet6:
		localAddress = netip.AddrFrom16([16]byte(message[8:24]))
		remoteAddress = netip.AddrFrom16([16]byte(message[24:40]))
	default:
		return linuxSocketOwnerRow{}, fmt.Errorf("INET_DIAG returned unsupported address family %d", family)
	}
	return linuxSocketOwnerRow{
		local:  netip.AddrPortFrom(localAddress.Unmap(), binary.BigEndian.Uint16(message[4:6])),
		remote: netip.AddrPortFrom(remoteAddress.Unmap(), binary.BigEndian.Uint16(message[6:8])),
		uid:    binary.NativeEndian.Uint32(message[64:68]),
		inode:  uint64(binary.NativeEndian.Uint32(message[68:72])),
		state:  message[1],
	}, nil
}

func linuxRowsForTuple(rows []linuxSocketOwnerRow, tuple EndpointTuple) []linuxSocketOwnerRow {
	tuple = normalizeEndpointTuple(tuple)
	matches := make([]linuxSocketOwnerRow, 0, 1)
	for _, row := range rows {
		if normalizeAddrPort(row.local) == tuple.Client && normalizeAddrPort(row.remote) == tuple.Proxy {
			matches = append(matches, row)
		}
	}
	return matches
}

func findLinuxSocketOwners(procRoot string, rows []linuxSocketOwnerRow, allowCrossUID bool) ([]uint32, bool, error) {
	inodeUIDs := make(map[uint64]map[uint32]struct{})
	for _, row := range rows {
		if row.inode == 0 {
			continue
		}
		if inodeUIDs[row.inode] == nil {
			inodeUIDs[row.inode] = make(map[uint32]struct{})
		}
		inodeUIDs[row.inode][row.uid] = struct{}{}
	}
	if len(inodeUIDs) == 0 {
		return nil, false, nil
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, errors.Is(err, os.ErrPermission), err
	}
	owners := make(map[uint32]struct{})
	permissionDenied := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pidValue, err := strconv.ParseUint(entry.Name(), 10, 32)
		if err != nil || pidValue == 0 {
			continue
		}
		pidRoot := filepath.Join(procRoot, entry.Name())
		var processUIDs map[uint32]struct{}
		if !allowCrossUID {
			processUIDs, err = readLinuxProcessUIDs(filepath.Join(pidRoot, "status"))
			if err != nil {
				if errors.Is(err, os.ErrPermission) {
					permissionDenied = true
				}
				continue
			}
		}
		fdEntries, err := os.ReadDir(filepath.Join(pidRoot, "fd"))
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				permissionDenied = true
			}
			continue
		}
		for _, fdEntry := range fdEntries {
			target, err := os.Readlink(filepath.Join(pidRoot, "fd", fdEntry.Name()))
			if err != nil {
				if errors.Is(err, os.ErrPermission) {
					permissionDenied = true
				}
				continue
			}
			inode, ok := parseLinuxSocketLink(target)
			if !ok {
				continue
			}
			ownerUIDs, targeted := inodeUIDs[inode]
			if !targeted || (!allowCrossUID && !linuxUIDSetsIntersect(ownerUIDs, processUIDs)) {
				continue
			}
			owners[uint32(pidValue)] = struct{}{}
			break
		}
	}
	result := make([]uint32, 0, len(owners))
	for pid := range owners {
		result = append(result, pid)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, permissionDenied, nil
}

func parseLinuxSocketLink(target string) (uint64, bool) {
	if !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
		return 0, false
	}
	value, err := strconv.ParseUint(target[len("socket:["):len(target)-1], 10, 64)
	return value, err == nil && value != 0
}

func readLinuxProcessUIDs(path string) (map[uint32]struct{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "Uid:"))
		if len(fields) == 0 {
			break
		}
		uids := make(map[uint32]struct{}, len(fields))
		for _, field := range fields {
			value, err := strconv.ParseUint(field, 10, 32)
			if err == nil {
				uids[uint32(value)] = struct{}{}
			}
		}
		if len(uids) > 0 {
			return uids, nil
		}
	}
	return nil, errors.New("process status does not contain a valid UID")
}

func linuxUIDSetsIntersect(left, right map[uint32]struct{}) bool {
	for uid := range left {
		if _, ok := right[uid]; ok {
			return true
		}
	}
	return false
}

func linuxResultForPID(pid uint32, identityLookup func(uint32) (linuxProcessIdentity, error)) Result {
	identity, err := identityLookup(pid)
	result := Result{
		Status:             StatusResolved,
		PID:                pid,
		StartToken:         identity.startToken,
		DisplayName:        identity.displayName,
		ProcessName:        identity.processName,
		ExecutablePath:     identity.executablePath,
		AppID:              identity.appID,
		Source:             "linux_inet_diag",
		IdentityConfidence: identity.identityConfidence,
	}
	if result.DisplayName == "" {
		result.DisplayName = result.ProcessName
	}
	if result.IdentityConfidence == "" {
		result.IdentityConfidence = "none"
	}
	if err != nil || identity.metadataDenied {
		result.Reason = "metadata_denied"
	}
	return result
}

func lookupLinuxProcessIdentity(pid uint32, procRoot string, resolver *linuxDesktopResolver) (linuxProcessIdentity, error) {
	pidRoot := filepath.Join(procRoot, strconv.FormatUint(uint64(pid), 10))
	statData, err := os.ReadFile(filepath.Join(pidRoot, "stat"))
	if err != nil {
		return linuxProcessIdentity{}, err
	}
	statName, startToken, err := parseLinuxProcStat(string(statData))
	if err != nil {
		return linuxProcessIdentity{}, err
	}
	identity := linuxProcessIdentity{startToken: startToken, processName: statName, identityConfidence: "none"}

	executablePath, pathErr := os.Readlink(filepath.Join(pidRoot, "exe"))
	if pathErr == nil {
		identity.executablePath = executablePath
	} else {
		identity.metadataDenied = true
	}
	if commData, readErr := os.ReadFile(filepath.Join(pidRoot, "comm")); readErr == nil {
		if processName := strings.TrimSpace(string(commData)); processName != "" {
			identity.processName = processName
		}
	} else {
		identity.metadataDenied = true
	}
	if statusData, readErr := os.ReadFile(filepath.Join(pidRoot, "status")); readErr == nil {
		if identity.processName == "" {
			identity.processName = parseLinuxStatusName(string(statusData))
		}
	} else {
		identity.metadataDenied = true
	}
	if identity.processName == "" && identity.executablePath != "" {
		identity.processName = filepath.Base(identity.executablePath)
	}

	var appHint string
	if cgroupData, readErr := os.ReadFile(filepath.Join(pidRoot, "cgroup")); readErr == nil {
		appHint = parseLinuxCgroupAppID(string(cgroupData))
	}
	desktop := resolver.resolve(linuxDesktopProcess{
		executablePath: identity.executablePath,
		processName:    identity.processName,
		appHint:        appHint,
	})
	identity.displayName = desktop.displayName
	identity.appID = desktop.appID
	identity.icon = desktop.icon
	identity.identityConfidence = desktop.confidence
	if identity.displayName == "" {
		identity.displayName = identity.processName
	}
	if identity.appID == "" {
		identity.appID = appHint
	}
	return identity, nil
}

func parseLinuxStatusName(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if strings.HasPrefix(line, "Name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		}
	}
	return ""
}

func parseLinuxProcStat(value string) (string, string, error) {
	open := strings.IndexByte(value, '(')
	close := strings.LastIndex(value, ") ")
	if open <= 0 || close <= open {
		return "", "", errors.New("process stat has an invalid comm field")
	}
	comm := value[open+1 : close]
	fields := strings.Fields(value[close+2:])
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return "", "", errors.New("process stat is missing start ticks")
	}
	if _, err := strconv.ParseUint(fields[startTimeIndex], 10, 64); err != nil {
		return "", "", fmt.Errorf("parse process start ticks: %w", err)
	}
	return comm, fields[startTimeIndex], nil
}

func parseLinuxCgroupAppID(value string) string {
	for _, rawLine := range strings.Split(value, "\n") {
		path := rawLine
		if separator := strings.Index(rawLine, "::"); separator >= 0 {
			path = rawLine[separator+2:]
		} else if separator := strings.LastIndexByte(rawLine, ':'); separator >= 0 {
			path = rawLine[separator+1:]
		}
		segments := strings.Split(path, "/")
		for index, segment := range segments {
			if segment == "flatpak" && index+1 < len(segments) {
				return cleanLinuxAppHint(segments[index+1])
			}
			if marker := strings.Index(segment, "app-flatpak-"); marker >= 0 {
				return trimLinuxSystemdInstance(cleanLinuxAppHint(segment[marker+len("app-flatpak-"):]))
			}
			if strings.HasPrefix(segment, "snap.") {
				parts := strings.Split(strings.TrimSuffix(segment, ".scope"), ".")
				if len(parts) >= 3 {
					return cleanLinuxAppHint(parts[1] + "_" + trimLinuxSystemdInstance(parts[2]))
				}
			}
			if strings.HasPrefix(segment, "app-") && strings.HasSuffix(segment, ".scope") {
				return trimLinuxSystemdInstance(cleanLinuxAppHint(strings.TrimSuffix(strings.TrimPrefix(segment, "app-"), ".scope")))
			}
		}
	}
	return ""
}

func cleanLinuxAppHint(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), `\x2d`, "-")
}

func trimLinuxSystemdInstance(value string) string {
	value = strings.TrimSuffix(value, ".scope")
	lastDash := strings.LastIndexByte(value, '-')
	if lastDash < 0 {
		return value
	}
	suffix := value[lastDash+1:]
	if len(suffix) < 8 {
		return value
	}
	for _, character := range suffix {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') && (character < 'A' || character > 'F') {
			return value
		}
	}
	return value[:lastDash]
}
