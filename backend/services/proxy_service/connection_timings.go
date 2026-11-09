package proxyservice

import (
	"context"
	"time"

	"github.com/josexy/mitmproxy-go/v2/metadata"
)

func connectionTimingsFromContext(ctx context.Context) *HTTPConnectionTimings {
	md, ok := metadata.FromContext(ctx)
	if !ok {
		return nil
	}
	value := md.MD()
	timings := &HTTPConnectionTimings{}
	timings.DNSStartedAtMicros, timings.DNSEndedAtMicros = connectionPhaseTimestamps(value.DNSLookupStartTs, value.DNSLookupCompletedTs)
	timings.ConnectStartedAtMicros, timings.ConnectEndedAtMicros = connectionPhaseTimestamps(value.SocketConnectStartTs, value.SocketConnectCompletedTs)
	timings.TLSStartedAtMicros, timings.TLSEndedAtMicros = connectionPhaseTimestamps(value.SSLHandshakeStartTs, value.SSLHandshakeCompletedTs)
	// The dialer also records terminal timestamps on failure. Only a successful
	// lookup reaches socket creation, and a successful dial establishes a remote
	// connection. A previous connection's success must not complete a new attempt.
	if value.SocketConnectStartTs.Before(value.DNSLookupCompletedTs) {
		timings.DNSEndedAtMicros = -1
	}
	if value.RemoteConnectionEstablishedTs.Before(value.SocketConnectCompletedTs) {
		timings.ConnectEndedAtMicros = -1
	}
	return timings
}

func connectionPhaseTimestamps(start, end time.Time) (int64, int64) {
	if start.IsZero() {
		return -1, -1
	}
	if end.IsZero() || end.Before(start) {
		return start.UnixMicro(), -1
	}
	return start.UnixMicro(), end.UnixMicro()
}

func (x *captureExchange) refreshConnectionTimingsLocked() {
	timings := connectionTimingsFromContext(x.ctx)
	if timings == nil {
		return
	}
	if x.entry.Metadata == nil {
		x.entry.Metadata = &Metadata{}
	}
	// Replace the immutable value so published snapshots remain unchanged.
	x.entry.Metadata.ConnectionTimings = timings
}
