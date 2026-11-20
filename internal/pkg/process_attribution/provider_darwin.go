package processattribution

import (
	"context"
	"errors"
	"image"
	"sync"
	"syscall"
	"time"

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
	enrich   func(context.Context, uint32, darwinProcessIdentity) darwinProcessIdentity
	loadIcon func(context.Context, uint32, string) (image.Image, error)
}

func init() {
	platformProviderFactory = func() Provider { return newDarwinProvider() }
}

func newDarwinProvider() *darwinProvider {
	return &darwinProvider{
		now:      time.Now,
		loadRows: captureDarwinSocketRows,
		identity: lookupDarwinProcessIdentity,
		enrich:   enrichDarwinProcessIdentity,
		loadIcon: loadDarwinProcessIcon,
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
		return darwinResultForPID(pids[0], func(pid uint32) (darwinProcessIdentity, error) {
			identity, err := p.identity(pid)
			if err != nil || p.enrich == nil {
				return identity, err
			}
			enriched := p.enrich(ctx, pid, identity)
			current, err := p.identity(pid)
			if err != nil || current.startToken != identity.startToken {
				return darwinProcessIdentity{}, syscall.ESRCH
			}
			if err := ctx.Err(); err != nil {
				return darwinProcessIdentity{}, err
			}
			return enriched, nil
		})
	default:
		return Result{Status: StatusAmbiguous, Source: "darwin_pcb_sysctl", Reason: "multiple_socket_owners"}
	}
}

func (p *darwinProvider) LoadIcon(ctx context.Context, result Result) (image.Image, error) {
	if result.PID == 0 || result.StartToken == "" {
		return nil, &IconUnavailableError{Reason: "darwin_process_changed"}
	}
	identity, err := p.identity(result.PID)
	if err == nil && identity.startToken == result.StartToken {
		icon, loadErr := p.loadIcon(ctx, result.PID, result.ExecutablePath)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		identity, err = p.identity(result.PID)
		if err == nil && identity.startToken == result.StartToken {
			return icon, loadErr
		}
	}
	if errors.Is(err, syscall.ESRCH) && result.ExecutablePath != "" {
		// The attributed process exited. Use only its recorded path, even if
		// another process acquires the PID before the main queue runs.
		return p.loadIcon(ctx, 0, result.ExecutablePath)
	}
	return nil, &IconUnavailableError{Reason: "darwin_process_changed"}
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
