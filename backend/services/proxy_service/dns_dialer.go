package proxyservice

import (
	"context"
	"net"
	"net/netip"
	"time"

	mitmproxy "github.com/josexy/mitmproxy-go/v2"
	"github.com/josexy/xhttp/httptrace"
)

type networkContextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type cachedNetworkDialer struct {
	cache *dnsCache
	base  *net.Dialer
}

// Lookup work emits one trace per caller, rather than inheriting a single
// request's trace into the shared lookup. Cancellation/deadlines still apply.
type dnsLookupContext struct{ context.Context }

func (dnsLookupContext) Value(any) any { return nil }

func (d *cachedNetworkDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.cache.mu.Lock()
	enabled, closed := d.cache.enabled, d.cache.closed
	d.cache.mu.Unlock()
	if closed {
		return nil, net.ErrClosed
	}
	if !enabled || (network != "tcp" && network != "tcp4" && network != "tcp6") {
		return d.base.DialContext(ctx, network, address)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return d.base.DialContext(ctx, network, address)
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return d.base.DialContext(ctx, network, address)
	}
	deadline := d.base.Deadline
	if d.base.Timeout > 0 {
		timeout := time.Now().Add(d.base.Timeout)
		if deadline.IsZero() || timeout.Before(deadline) {
			deadline = timeout
		}
	}
	if !deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	if d.base.Cancel != nil { //nolint:staticcheck // Preserve the configured net.Dialer's legacy cancellation behavior.
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		go func() {
			select {
			case <-d.base.Cancel: //nolint:staticcheck // Preserve the configured net.Dialer's legacy cancellation behavior.
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	trace := httptrace.ContextClientTrace(ctx)
	if trace != nil && trace.DNSStart != nil {
		trace.DNSStart(httptrace.DNSStartInfo{Host: host})
	}
	ips, hit, err := d.cache.lookupIPAddr(dnsLookupContext{ctx}, host)
	if hit {
		mitmproxy.ReportDNSCacheHit(ctx)
	}
	if trace != nil && trace.DNSDone != nil {
		trace.DNSDone(httptrace.DNSDoneInfo{Addrs: cloneDNSAddresses(ips), Err: err})
	}
	if err != nil {
		return nil, &net.OpError{Op: "dial", Net: network, Err: err}
	}
	addresses := make([]string, 0, len(ips))
	for _, ip := range ips {
		isIPv4 := ip.IP.To4() != nil
		if (network == "tcp4" && !isIPv4) || (network == "tcp6" && isIPv4) {
			continue
		}
		if local, ok := d.base.LocalAddr.(*net.TCPAddr); ok && local != nil && !local.IP.IsUnspecified() && len(local.IP) > 0 && (local.IP.To4() != nil) != isIPv4 {
			continue
		}
		name := ip.IP.String()
		if ip.Zone != "" {
			name += "%" + ip.Zone
		}
		addresses = append(addresses, net.JoinHostPort(name, port))
	}
	if len(addresses) == 0 {
		return nil, &net.OpError{Op: "dial", Net: network, Err: &net.DNSError{Name: host, Err: "no suitable address found"}}
	}
	primaries, fallbacks := addresses, []string(nil)
	if network == "tcp" && d.base.FallbackDelay >= 0 {
		primaries = nil
		firstIPv4 := ipsForAddressIsIPv4(addresses[0])
		for _, addr := range addresses {
			if ipsForAddressIsIPv4(addr) == firstIPv4 {
				primaries = append(primaries, addr)
			} else {
				fallbacks = append(fallbacks, addr)
			}
		}
	}
	return d.dialCandidates(ctx, network, primaries, fallbacks)
}

func ipsForAddressIsIPv4(address string) bool {
	host, _, _ := net.SplitHostPort(address)
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().Is4()
}

func (d *cachedNetworkDialer) dialSerial(ctx context.Context, network string, addresses []string) (net.Conn, error) {
	var firstErr error
	for i, address := range addresses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := ctx, func() {}
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return nil, context.DeadlineExceeded
			}
			// Match net.Dialer's time sharing: each candidate gets at least
			// two seconds when the remaining overall deadline allows it.
			budget := remaining / time.Duration(len(addresses)-i)
			if budget < 2*time.Second {
				budget = min(remaining, 2*time.Second)
			}
			attemptCtx, cancel = context.WithTimeout(ctx, budget)
		}
		conn, err := d.base.DialContext(attemptCtx, network, address)
		cancel()
		if err == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

func (d *cachedNetworkDialer) dialCandidates(ctx context.Context, network string, primaries, fallbacks []string) (net.Conn, error) {
	if len(fallbacks) == 0 {
		return d.dialSerial(ctx, network, primaries)
	}
	type result struct {
		conn    net.Conn
		err     error
		primary bool
	}
	results := make(chan result)
	returned := make(chan struct{})
	defer close(returned)
	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	start := func(addresses []string, primary bool) {
		go func() {
			conn, err := d.dialSerial(raceCtx, network, addresses)
			select {
			case results <- result{conn, err, primary}:
			case <-returned:
				if conn != nil {
					_ = conn.Close()
				}
			}
		}()
	}
	start(primaries, true)
	delay := d.base.FallbackDelay
	if delay == 0 {
		delay = 300 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	fallbackStarted, finished := false, 0
	var primaryErr error
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			fallbackStarted = true
			start(fallbacks, false)
		case r := <-results:
			if r.err == nil {
				return r.conn, nil
			}
			finished++
			if r.primary {
				primaryErr = r.err
				if !fallbackStarted {
					timer.Stop()
					// A nil channel disables the timer even when it already fired.
					timer.C = nil
					fallbackStarted = true
					start(fallbacks, false)
				}
			}
			if finished == 2 {
				return nil, primaryErr
			}
		}
	}
}

func (s *ProxyService) networkDialer(base *net.Dialer) networkContextDialer {
	return &cachedNetworkDialer{cache: s.dnsCacheManager(), base: base}
}

type syntheticForwardDialer struct{ networkContextDialer }

func (d syntheticForwardDialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

func syntheticDialer(dialers []networkContextDialer) networkContextDialer {
	if len(dialers) > 0 && dialers[0] != nil {
		return dialers[0]
	}
	return &net.Dialer{}
}
