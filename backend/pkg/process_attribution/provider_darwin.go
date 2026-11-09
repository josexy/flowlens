//go:build darwin && cgo

package processattribution

/*
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include "bridge_darwin.h"
#include <stdlib.h>
*/
import "C"

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const darwinSocketSnapshotTTL = 100 * time.Millisecond

var loadDarwinPCBLayout = sync.OnceValues(func() (darwinPCBLayout, error) {
	release, err := unix.Sysctl("kern.osrelease")
	if err != nil {
		return darwinPCBLayout{}, err
	}
	return darwinPCBLayoutForRelease(release)
})

type darwinSocketSnapshot struct {
	rows       []darwinSocketOwnerRow
	captured   time.Time
	captureErr error
}

type darwinProcessIdentity struct {
	startToken         string
	displayName        string
	processName        string
	executablePath     string
	appID              string
	identityConfidence string
	metadataDenied     bool
}

type darwinProvider struct {
	mu       sync.Mutex
	snapshot darwinSocketSnapshot
	now      func() time.Time
	loadRows func() ([]darwinSocketOwnerRow, error)
	identity func(uint32) (darwinProcessIdentity, error)
}

func init() {
	platformProviderFactory = func() Provider { return newDarwinProvider() }
}

func newDarwinProvider() *darwinProvider {
	return &darwinProvider{
		now:      time.Now,
		loadRows: captureDarwinSocketRows,
		identity: lookupDarwinProcessIdentity,
	}
}

func (p *darwinProvider) Lookup(ctx context.Context, tuple EndpointTuple) Result {
	if err := ctx.Err(); err != nil {
		return Result{Status: StatusNotFound, Source: "darwin_pcb_sysctl", Reason: "lookup_cancelled"}
	}
	tuple = normalizeEndpointTuple(tuple)
	if !tuple.Client.IsValid() || !tuple.Proxy.IsValid() || tuple.Client.Addr().Is4() != tuple.Proxy.Addr().Is4() {
		return Result{Status: StatusNotFound, Source: "darwin_pcb_sysctl", Reason: "invalid_endpoint"}
	}
	rows, err := p.socketRows(ctx, false)
	if err != nil {
		status := StatusNotFound
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			status = StatusPermissionDenied
		}
		return Result{Status: status, Source: "darwin_pcb_sysctl", Reason: "socket_table_unavailable"}
	}
	pids := darwinTupleOwnerPIDs(rows, tuple)
	if len(pids) == 0 {
		rows, err = p.socketRows(ctx, true)
		if err != nil {
			status := StatusNotFound
			if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
				status = StatusPermissionDenied
			}
			return Result{Status: status, Source: "darwin_pcb_sysctl", Reason: "socket_table_unavailable"}
		}
		pids = darwinTupleOwnerPIDs(rows, tuple)
	}
	switch len(pids) {
	case 0:
		return Result{Status: StatusNotFound, Source: "darwin_pcb_sysctl", Reason: "socket_owner_not_found"}
	case 1:
		return darwinResultForPID(pids[0], p.identity)
	default:
		return Result{Status: StatusAmbiguous, Source: "darwin_pcb_sysctl", Reason: "multiple_socket_owners"}
	}
}

func (p *darwinProvider) LoadIcon(ctx context.Context, result Result) (image.Image, error) {
	return loadDarwinProcessIcon(ctx, result.PID, result.ExecutablePath)
}

func (p *darwinProvider) socketRows(
	ctx context.Context,
	forceRefresh bool,
) ([]darwinSocketOwnerRow, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := p.now()
	if !forceRefresh &&
		!p.snapshot.captured.IsZero() &&
		now.Sub(p.snapshot.captured) < darwinSocketSnapshotTTL {
		return p.snapshot.rows, p.snapshot.captureErr
	}
	rows, err := p.loadRows()
	p.snapshot = darwinSocketSnapshot{
		rows:       rows,
		captured:   now,
		captureErr: err,
	}
	return rows, err
}

func captureDarwinSocketRows() ([]darwinSocketOwnerRow, error) {
	layout, err := loadDarwinPCBLayout()
	if err != nil {
		return nil, err
	}
	raw, err := unix.SysctlRaw("net.inet.tcp.pcblist_n")
	if err != nil {
		return nil, err
	}
	return parseDarwinTCPPCBList(raw, layout)
}

func darwinResultForPID(pid uint32, identityLookup func(uint32) (darwinProcessIdentity, error)) Result {
	identity, err := identityLookup(pid)
	result := Result{
		Status:             StatusResolved,
		PID:                pid,
		StartToken:         identity.startToken,
		DisplayName:        identity.displayName,
		ProcessName:        identity.processName,
		ExecutablePath:     identity.executablePath,
		AppID:              identity.appID,
		Source:             "darwin_pcb_sysctl",
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

func lookupDarwinProcessIdentity(pid uint32) (darwinProcessIdentity, error) {
	var copied C.flowlens_darwin_process_identity
	if code := C.flowlens_darwin_copy_process_identity(C.int32_t(pid), &copied); code != 0 {
		return darwinProcessIdentity{}, syscall.Errno(code)
	}
	defer C.flowlens_darwin_free_process_identity(&copied)
	identity := darwinProcessIdentity{
		startToken:         strconv.FormatUint(uint64(copied.start_seconds), 10) + ":" + strconv.FormatUint(uint64(copied.start_microseconds), 10),
		displayName:        C.GoString(copied.display_name),
		processName:        C.GoString(copied.process_name),
		executablePath:     C.GoString(copied.executable_path),
		appID:              C.GoString(copied.bundle_id),
		identityConfidence: "none",
		metadataDenied:     copied.metadata_denied != 0,
	}
	if identity.displayName != "" && (identity.appID != "" || identity.displayName != identity.processName) {
		identity.identityConfidence = "exact"
	}
	return identity, nil
}

func loadDarwinProcessIcon(ctx context.Context, pid uint32, executablePath string) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := C.CString(executablePath)
	defer C.free(unsafe.Pointer(path))
	var copied C.flowlens_darwin_buffer
	if code := C.flowlens_darwin_copy_process_icon(C.int32_t(pid), path, &copied); code != 0 {
		return nil, &IconUnavailableError{Reason: fmt.Sprintf("darwin_icon_%d", int(code))}
	}
	defer C.flowlens_darwin_free_buffer(&copied)
	if copied.bytes == nil || copied.length == 0 {
		return nil, &IconUnavailableError{Reason: "darwin_icon_empty"}
	}
	if uint64(copied.length) > uint64(1<<31-1) {
		return nil, &IconUnavailableError{Reason: "darwin_icon_too_large"}
	}
	data := C.GoBytes(unsafe.Pointer(copied.bytes), C.int(copied.length))
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode Darwin process icon: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return decoded, nil
}

func darwinBridgeOutstandingAllocations() uint64 {
	return uint64(C.flowlens_darwin_outstanding_allocations())
}
