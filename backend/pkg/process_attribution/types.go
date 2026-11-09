package processattribution

import (
	"context"
	"fmt"
	"image"
	"net/netip"
	"runtime"
	"time"
)

type EndpointTuple struct {
	Client netip.AddrPort
	Proxy  netip.AddrPort
}

type Status string

const (
	StatusPending          Status = "pending"
	StatusResolved         Status = "resolved"
	StatusRemote           Status = "remote"
	StatusNotFound         Status = "not_found"
	StatusPermissionDenied Status = "permission_denied"
	StatusUnsupported      Status = "unsupported"
	StatusAmbiguous        Status = "ambiguous"
)

type Result struct {
	Status             Status
	PID                uint32
	StartToken         string
	DisplayName        string
	ProcessName        string
	ExecutablePath     string
	AppID              string
	IconKey            string
	Source             string
	IdentityConfidence string
	Reason             string
}

type Provider interface {
	Lookup(context.Context, EndpointTuple) Result
	LoadIcon(context.Context, Result) (image.Image, error)
}

type Options struct {
	Workers           int
	QueueSize         int
	LookupTimeout     time.Duration
	SocketSnapshotTTL time.Duration
	ProcessCacheSize  int
	ProcessCacheTTL   time.Duration
	NegativeCacheTTL  time.Duration
	IconWorkers       int
	IconQueueSize     int
}

func DefaultOptions() Options {
	return Options{
		Workers:           min(4, max(1, runtime.GOMAXPROCS(0))),
		QueueSize:         256,
		LookupTimeout:     time.Second,
		SocketSnapshotTTL: 100 * time.Millisecond,
		ProcessCacheSize:  1024,
		ProcessCacheTTL:   5 * time.Minute,
		NegativeCacheTTL:  time.Second,
		IconWorkers:       2,
		IconQueueSize:     128,
	}
}

type IconUnavailableError struct {
	Reason string
}

func (e *IconUnavailableError) Error() string {
	if e == nil || e.Reason == "" {
		return "process icon is unavailable"
	}
	return fmt.Sprintf("process icon is unavailable: %s", e.Reason)
}

var platformProviderFactory func() Provider

func NewPlatformProvider() Provider {
	if platformProviderFactory != nil {
		return platformProviderFactory()
	}
	return &unavailableProvider{reason: "platform_provider_unavailable"}
}

type unavailableProvider struct {
	reason string
}

func (p *unavailableProvider) Lookup(context.Context, EndpointTuple) Result {
	return Result{Status: StatusUnsupported, Reason: p.reason}
}

func (p *unavailableProvider) LoadIcon(context.Context, Result) (image.Image, error) {
	return nil, &IconUnavailableError{Reason: p.reason}
}

func normalizeEndpointTuple(tuple EndpointTuple) EndpointTuple {
	tuple.Client = normalizeAddrPort(tuple.Client)
	tuple.Proxy = normalizeAddrPort(tuple.Proxy)
	return tuple
}

func normalizeAddrPort(addrPort netip.AddrPort) netip.AddrPort {
	if !addrPort.IsValid() {
		return addrPort
	}
	return netip.AddrPortFrom(addrPort.Addr().Unmap(), addrPort.Port())
}
