package proxyservice

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	http "github.com/josexy/xhttp"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/idna"
	netproxy "golang.org/x/net/proxy"
)

const (
	syntheticALPNHTTP1 = "http/1.1"
	syntheticALPNHTTP2 = "h2"
)

type idleClosingRoundTripper interface {
	http.RoundTripper
	CloseIdleConnections()
}

type syntheticRoundTripper struct {
	profileID  utls.ClientHelloID
	protocol   SendRequestProtocol
	proxyURL   *url.URL
	skipVerify bool
	plain      *http.Transport
	originsMu  sync.Mutex
	origins    map[string]syntheticOriginRoundTripper
}

type syntheticOriginRoundTripper interface {
	http.RoundTripper
	closeIdleConnections()
}

func newSyntheticRoundTripper(
	proxyConfig *settingservice.ProxyConfig,
	protocol SendRequestProtocol,
	proxyURL *url.URL,
	clientHelloID TLSClientHelloID,
) idleClosingRoundTripper {
	skipVerify := false
	if proxyConfig != nil {
		skipVerify = proxyConfig.SkipVerifyTLS
	}
	profileID := resolveUTLSClientHelloID(clientHelloID)
	plain := newSyntheticBaseTransport(protocol)
	if proxyURL != nil {
		plain.Proxy = http.ProxyURL(proxyURL)
		if normalizedProxyScheme(proxyURL) == "https" {
			plain.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				return dialSyntheticTLSDirect(
					ctx,
					network,
					address,
					proxyURL.Hostname(),
					profileID,
					[]string{syntheticALPNHTTP1},
					true,
					skipVerify,
				)
			}
		}
	}
	transport := &syntheticRoundTripper{
		profileID:  profileID,
		protocol:   protocol,
		proxyURL:   cloneURL(proxyURL),
		skipVerify: skipVerify,
		plain:      plain,
		origins:    make(map[string]syntheticOriginRoundTripper),
	}
	return transport
}

func newSyntheticBaseTransport(protocol SendRequestProtocol) *http.Transport {
	transport := &http.Transport{
		DisableCompression:  true,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     30 * time.Second,
	}
	switch protocol {
	case SendRequestProtocolHTTP1:
		transport.Protocols = new(http.Protocols)
		transport.Protocols.SetHTTP1(true)
	case SendRequestProtocolHTTP2:
		transport.ForceAttemptHTTP2 = true
		transport.Protocols = new(http.Protocols)
		transport.Protocols.SetUnencryptedHTTP2(true)
	default:
		transport.ForceAttemptHTTP2 = true
	}
	return transport
}

func (t *syntheticRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, errors.New("synthetic request URL is required")
	}
	normalizedURL := *req.URL
	if err := normalizeSyntheticURLHostname(&normalizedURL); err != nil {
		return nil, err
	}
	if normalizedURL.Host != req.URL.Host {
		normalizedReq := new(http.Request)
		*normalizedReq = *req
		normalizedReq.URL = &normalizedURL
		req = normalizedReq
	}
	_, hasHTTP2Fingerprint := http.RequestFingerprint(req)
	if !strings.EqualFold(req.URL.Scheme, "https") {
		return t.plain.RoundTrip(req)
	}

	address, serverName, err := syntheticOriginAddress(req.URL)
	if err != nil {
		return nil, err
	}
	key := strings.ToLower(address)
	if hasHTTP2Fingerprint {
		key += "\x00http2-fingerprint"
	}
	if origin := t.loadOrigin(key); origin != nil {
		return origin.RoundTrip(req)
	}

	origin, err := t.newOrigin(req.Context(), address, serverName)
	if err != nil {
		return nil, err
	}
	selected := t.storeOrigin(key, origin)
	if selected != origin {
		origin.closeIdleConnections()
	}
	return selected.RoundTrip(req)
}

func (t *syntheticRoundTripper) loadOrigin(key string) syntheticOriginRoundTripper {
	t.originsMu.Lock()
	defer t.originsMu.Unlock()
	return t.origins[key]
}

func (t *syntheticRoundTripper) storeOrigin(
	key string,
	origin syntheticOriginRoundTripper,
) syntheticOriginRoundTripper {
	t.originsMu.Lock()
	defer t.originsMu.Unlock()
	if current := t.origins[key]; current != nil {
		return current
	}
	t.origins[key] = origin
	return origin
}

func (t *syntheticRoundTripper) newOrigin(
	ctx context.Context,
	address string,
	serverName string,
) (syntheticOriginRoundTripper, error) {
	alpn := []string{syntheticALPNHTTP2, syntheticALPNHTTP1}
	forceALPN := false
	switch t.protocol {
	case SendRequestProtocolHTTP1:
		alpn = []string{syntheticALPNHTTP1}
		forceALPN = true
	case SendRequestProtocolHTTP2:
		alpn = []string{syntheticALPNHTTP2}
		forceALPN = true
	}

	conn, err := dialSyntheticTLS(
		ctx,
		"tcp",
		address,
		serverName,
		t.proxyURL,
		t.profileID,
		alpn,
		forceALPN,
		t.skipVerify,
	)
	if err != nil {
		if t.protocol == SendRequestProtocolHTTP2 && strings.Contains(strings.ToLower(err.Error()), "no application protocol") {
			return nil, fmt.Errorf("HTTP/2 requires h2: %w", err)
		}
		return nil, err
	}
	negotiated := conn.ConnectionState().NegotiatedProtocol

	switch t.protocol {
	case SendRequestProtocolHTTP1:
		if negotiated != "" && negotiated != syntheticALPNHTTP1 {
			_ = conn.Close()
			return nil, fmt.Errorf("TLS negotiated %q for an HTTP/1.1 request", negotiated)
		}
		return newSyntheticHTTP1Origin(conn, address, serverName, t), nil
	case SendRequestProtocolHTTP2:
		if negotiated != syntheticALPNHTTP2 {
			_ = conn.Close()
			return nil, fmt.Errorf("TLS negotiated %q; HTTP/2 requires h2", negotiated)
		}
		return newSyntheticHTTP2Origin(ctx, conn, address, serverName, t)
	default:
		switch negotiated {
		case syntheticALPNHTTP2:
			return newSyntheticHTTP2Origin(ctx, conn, address, serverName, t)
		case "", syntheticALPNHTTP1:
			return newSyntheticHTTP1Origin(conn, address, serverName, t), nil
		default:
			_ = conn.Close()
			return nil, fmt.Errorf("unsupported TLS ALPN protocol %q", negotiated)
		}
	}
}

func (t *syntheticRoundTripper) CloseIdleConnections() {
	t.plain.CloseIdleConnections()
	t.originsMu.Lock()
	origins := make([]syntheticOriginRoundTripper, 0, len(t.origins))
	for _, origin := range t.origins {
		origins = append(origins, origin)
	}
	t.origins = make(map[string]syntheticOriginRoundTripper)
	t.originsMu.Unlock()
	for _, origin := range origins {
		origin.closeIdleConnections()
	}
}

type syntheticHTTP1Origin struct {
	transport   *http.Transport
	firstConnMu sync.Mutex
	firstConn   net.Conn
}

func newSyntheticHTTP1Origin(
	firstConn net.Conn,
	address string,
	serverName string,
	parent *syntheticRoundTripper,
) *syntheticHTTP1Origin {
	origin := &syntheticHTTP1Origin{firstConn: firstConn}
	transport := newSyntheticBaseTransport(SendRequestProtocolHTTP1)
	transport.DialTLSContext = func(ctx context.Context, network, dialAddress string) (net.Conn, error) {
		origin.firstConnMu.Lock()
		conn := origin.firstConn
		origin.firstConn = nil
		origin.firstConnMu.Unlock()
		if conn != nil {
			return conn, nil
		}
		if dialAddress == "" {
			dialAddress = address
		}
		return dialSyntheticTLS(
			ctx,
			network,
			dialAddress,
			serverName,
			parent.proxyURL,
			parent.profileID,
			[]string{syntheticALPNHTTP1},
			true,
			parent.skipVerify,
		)
	}
	origin.transport = transport
	return origin
}

func (o *syntheticHTTP1Origin) RoundTrip(req *http.Request) (*http.Response, error) {
	return o.transport.RoundTrip(req)
}

func (o *syntheticHTTP1Origin) closeIdleConnections() {
	o.transport.CloseIdleConnections()
	o.firstConnMu.Lock()
	conn := o.firstConn
	o.firstConn = nil
	o.firstConnMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

type syntheticHTTP2Origin struct {
	mu         sync.Mutex
	clientConn *http.ClientConn
	address    string
	serverName string
	parent     *syntheticRoundTripper
}

func newSyntheticHTTP2Origin(
	ctx context.Context,
	conn net.Conn,
	address string,
	serverName string,
	parent *syntheticRoundTripper,
) (*syntheticHTTP2Origin, error) {
	clientConn, err := newSyntheticHTTP2ClientConn(ctx, conn, address)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &syntheticHTTP2Origin{
		clientConn: clientConn,
		address:    address,
		serverName: serverName,
		parent:     parent,
	}, nil
}

func (o *syntheticHTTP2Origin) RoundTrip(req *http.Request) (*http.Response, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.clientConn == nil || o.clientConn.Err() != nil || o.clientConn.Available() == 0 {
		if o.clientConn != nil {
			_ = o.clientConn.Close()
			o.clientConn = nil
		}
		conn, err := dialSyntheticTLS(
			req.Context(),
			"tcp",
			o.address,
			o.serverName,
			o.parent.proxyURL,
			o.parent.profileID,
			[]string{syntheticALPNHTTP2},
			true,
			o.parent.skipVerify,
		)
		if err != nil {
			return nil, err
		}
		if negotiated := conn.ConnectionState().NegotiatedProtocol; negotiated != syntheticALPNHTTP2 {
			_ = conn.Close()
			return nil, fmt.Errorf("TLS negotiated %q; HTTP/2 requires h2", negotiated)
		}
		clientConn, err := newSyntheticHTTP2ClientConn(req.Context(), conn, o.address)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		o.clientConn = clientConn
	}
	return o.clientConn.RoundTrip(req)
}

func newSyntheticHTTP2ClientConn(
	ctx context.Context,
	conn net.Conn,
	address string,
) (*http.ClientConn, error) {
	transport := newSyntheticBaseTransport(SendRequestProtocolHTTP2)
	var connMu sync.Mutex
	transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
		connMu.Lock()
		defer connMu.Unlock()
		if conn == nil {
			return nil, errors.New("synthetic HTTP/2 connection was already consumed")
		}
		selected := conn
		conn = nil
		return selected, nil
	}
	// The TLS handshake has already completed through uTLS. Present the
	// connection to xhttp as its unencrypted HTTP/2 path so its bundled HTTP/2
	// implementation retains xhttp's exact outgoing header-block metadata.
	return transport.NewClientConn(ctx, "http", address)
}

func (o *syntheticHTTP2Origin) closeIdleConnections() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.clientConn != nil {
		_ = o.clientConn.Close()
		o.clientConn = nil
	}
}

type syntheticTLSConn struct {
	net.Conn
	state tls.ConnectionState
}

func (c *syntheticTLSConn) ConnectionState() tls.ConnectionState {
	return c.state
}

func dialSyntheticTLS(
	ctx context.Context,
	network string,
	address string,
	serverName string,
	proxyURL *url.URL,
	profileID utls.ClientHelloID,
	alpn []string,
	forceALPN bool,
	skipVerify bool,
) (*syntheticTLSConn, error) {
	conn, err := dialSyntheticPath(ctx, network, address, proxyURL, profileID, skipVerify)
	if err != nil {
		return nil, err
	}
	return handshakeSyntheticTLS(ctx, conn, serverName, profileID, alpn, forceALPN, skipVerify)
}

func dialSyntheticTLSDirect(
	ctx context.Context,
	network string,
	address string,
	serverName string,
	profileID utls.ClientHelloID,
	alpn []string,
	forceALPN bool,
	skipVerify bool,
) (*syntheticTLSConn, error) {
	return dialSyntheticTLS(
		ctx,
		network,
		address,
		serverName,
		nil,
		profileID,
		alpn,
		forceALPN,
		skipVerify,
	)
}

func dialSyntheticPath(
	ctx context.Context,
	network string,
	address string,
	proxyURL *url.URL,
	profileID utls.ClientHelloID,
	skipVerify bool,
) (net.Conn, error) {
	var err error
	address, err = syntheticASCIIAddress(address)
	if err != nil {
		return nil, err
	}
	if proxyURL == nil {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	switch normalizedProxyScheme(proxyURL) {
	case "http", "https":
		return dialSyntheticHTTPProxyTunnel(
			ctx,
			network,
			address,
			proxyURL,
			profileID,
			skipVerify,
		)
	case "socks5", "socks5h":
		dialer, err := netproxy.FromURL(proxyURL, &net.Dialer{})
		if err != nil {
			return nil, err
		}
		contextDialer, ok := dialer.(netproxy.ContextDialer)
		if !ok {
			return nil, errors.New("SOCKS5 proxy dialer does not support context cancellation")
		}
		return contextDialer.DialContext(ctx, network, address)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}
}

func dialSyntheticHTTPProxyTunnel(
	ctx context.Context,
	network string,
	targetAddress string,
	proxyURL *url.URL,
	profileID utls.ClientHelloID,
	skipVerify bool,
) (net.Conn, error) {
	proxyAddress, err := syntheticProxyAddress(proxyURL)
	if err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, network, proxyAddress)
	if err != nil {
		return nil, err
	}
	if normalizedProxyScheme(proxyURL) == "https" {
		conn, err = handshakeSyntheticTLS(
			ctx,
			conn,
			proxyURL.Hostname(),
			profileID,
			[]string{syntheticALPNHTTP1},
			true,
			skipVerify,
		)
		if err != nil {
			return nil, fmt.Errorf("TLS handshake with HTTPS proxy: %w", err)
		}
	}

	stopWatching := watchSyntheticConnContext(ctx, conn)
	defer stopWatching()
	clearDeadline := setSyntheticConnDeadline(ctx, conn)
	defer clearDeadline()

	headers := make(http.Header)
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		credentials := proxyURL.User.Username() + ":" + password
		headers.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(credentials)))
	}
	connectRequest := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: targetAddress},
		Host:   targetAddress,
		Header: headers,
	}
	if err := connectRequest.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("write proxy CONNECT request: %w", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), connectRequest)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read proxy CONNECT response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("proxy CONNECT failed: %s", response.Status)
	}
	return conn, nil
}

func handshakeSyntheticTLS(
	ctx context.Context,
	conn net.Conn,
	serverName string,
	profileID utls.ClientHelloID,
	alpn []string,
	forceALPN bool,
	skipVerify bool,
) (*syntheticTLSConn, error) {
	serverName, err := syntheticASCIIHostname(serverName)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	config := &utls.Config{
		ServerName:         serverName,
		NextProtos:         append([]string(nil), alpn...),
		InsecureSkipVerify: skipVerify, //nolint:gosec
	}
	uconn := utls.UClient(conn, config, profileID)
	isRandomizedALPN := profileID.Client == utls.HelloRandomizedALPN.Client
	if profileID != utls.HelloGolang && (forceALPN || isRandomizedALPN) {
		if err := uconn.BuildHandshakeState(); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("build uTLS handshake state: %w", err)
		}
		if isRandomizedALPN {
			normalizeSyntheticRandomizedGroups(uconn)
		}
		if forceALPN {
			patchSyntheticALPN(uconn, alpn)
		}
	}

	stopWatching := watchSyntheticConnContext(ctx, conn)
	defer stopWatching()
	clearDeadline := setSyntheticConnDeadline(ctx, conn)
	defer clearDeadline()
	if err := uconn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("uTLS handshake: %w", syntheticContextError(ctx, err))
	}
	return &syntheticTLSConn{Conn: uconn, state: standardTLSConnectionState(uconn.ConnectionState())}, nil
}

func normalizeSyntheticRandomizedGroups(conn *utls.UConn) {
	var curves *utls.SupportedCurvesExtension
	var keyShares *utls.KeyShareExtension
	for _, extension := range conn.Extensions {
		switch current := extension.(type) {
		case *utls.SupportedCurvesExtension:
			curves = current
		case *utls.KeyShareExtension:
			keyShares = current
		}
	}
	if curves == nil || keyShares == nil {
		return
	}
	hasHybridCurve := slices.Contains(curves.Curves, utls.X25519MLKEM768)
	hasHybridKeyShare := slices.ContainsFunc(keyShares.KeyShares, func(keyShare utls.KeyShare) bool {
		return keyShare.Group == utls.X25519MLKEM768
	})
	if hasHybridCurve == hasHybridKeyShare {
		return
	}
	if hasHybridKeyShare {
		curves.Curves = append([]utls.CurveID{utls.X25519MLKEM768}, curves.Curves...)
		return
	}
	curves.Curves = slices.DeleteFunc(curves.Curves, func(curve utls.CurveID) bool {
		return curve == utls.X25519MLKEM768
	})
}

func patchSyntheticALPN(conn *utls.UConn, alpn []string) {
	extensions := make([]utls.TLSExtension, 0, len(conn.Extensions)+1)
	patchedALPN := false
	for _, extension := range conn.Extensions {
		switch current := extension.(type) {
		case *utls.ALPNExtension:
			current.AlpnProtocols = append([]string(nil), alpn...)
			patchedALPN = true
		case *utls.ApplicationSettingsExtension:
			current.SupportedProtocols = syntheticProtocolIntersection(current.SupportedProtocols, alpn)
			if len(current.SupportedProtocols) == 0 {
				continue
			}
		case *utls.ApplicationSettingsExtensionNew:
			current.SupportedProtocols = syntheticProtocolIntersection(current.SupportedProtocols, alpn)
			if len(current.SupportedProtocols) == 0 {
				continue
			}
		}
		extensions = append(extensions, extension)
	}
	if !patchedALPN {
		extensions = append(extensions, &utls.ALPNExtension{
			AlpnProtocols: append([]string(nil), alpn...),
		})
	}
	conn.Extensions = extensions
}

func syntheticProtocolIntersection(protocols, allowed []string) []string {
	filtered := make([]string, 0, len(protocols))
	for _, protocol := range protocols {
		if slices.Contains(allowed, protocol) {
			filtered = append(filtered, protocol)
		}
	}
	return filtered
}

func standardTLSConnectionState(state utls.ConnectionState) tls.ConnectionState {
	return tls.ConnectionState{
		Version:                     state.Version,
		HandshakeComplete:           state.HandshakeComplete,
		DidResume:                   state.DidResume,
		CipherSuite:                 state.CipherSuite,
		NegotiatedProtocol:          state.NegotiatedProtocol,
		NegotiatedProtocolIsMutual:  state.NegotiatedProtocolIsMutual,
		ServerName:                  state.ServerName,
		PeerCertificates:            state.PeerCertificates,
		VerifiedChains:              state.VerifiedChains,
		SignedCertificateTimestamps: state.SignedCertificateTimestamps,
		OCSPResponse:                state.OCSPResponse,
		TLSUnique:                   state.TLSUnique,
		ECHAccepted:                 state.ECHAccepted,
	}
}

func resolveUTLSClientHelloID(id TLSClientHelloID) utls.ClientHelloID {
	switch id {
	case TLSClientHelloChromeAuto:
		return utls.HelloChrome_Auto
	case TLSClientHelloFirefoxAuto:
		return utls.HelloFirefox_Auto
	case TLSClientHelloSafariAuto:
		return utls.HelloSafari_Auto
	case TLSClientHelloEdgeAuto:
		return utls.HelloEdge_Auto
	case TLSClientHelloIOSAuto:
		return utls.HelloIOS_Auto
	case TLSClientHelloAndroid11OkHTTP:
		return utls.HelloAndroid_11_OkHttp
	case TLSClientHelloRandomizedALPN:
		return utls.HelloRandomizedALPN
	default:
		return utls.HelloGolang
	}
}

func syntheticOriginAddress(target *url.URL) (address string, serverName string, err error) {
	if target == nil || target.Hostname() == "" {
		return "", "", errors.New("HTTPS request host is required")
	}
	serverName, err = syntheticASCIIHostname(target.Hostname())
	if err != nil {
		return "", "", err
	}
	port := target.Port()
	if port == "" {
		port = "443"
	}
	return net.JoinHostPort(serverName, port), serverName, nil
}

func syntheticProxyAddress(proxyURL *url.URL) (string, error) {
	if proxyURL == nil || proxyURL.Hostname() == "" {
		return "", errors.New("proxy host is required")
	}
	hostname, err := syntheticASCIIHostname(proxyURL.Hostname())
	if err != nil {
		return "", err
	}
	port := proxyURL.Port()
	if port == "" {
		switch normalizedProxyScheme(proxyURL) {
		case "https":
			port = "443"
		case "socks5", "socks5h":
			port = "1080"
		default:
			port = "80"
		}
	}
	return net.JoinHostPort(hostname, port), nil
}

func syntheticASCIIHostname(hostname string) (string, error) {
	if hostname == "" {
		return "", errors.New("host is required")
	}
	if strings.IndexFunc(hostname, func(r rune) bool { return r > 127 }) < 0 {
		return hostname, nil
	}
	asciiHostname, err := idna.Lookup.ToASCII(hostname)
	if err != nil {
		return "", fmt.Errorf("invalid internationalized hostname %q: %w", hostname, err)
	}
	return asciiHostname, nil
}

func syntheticASCIIAddress(address string) (string, error) {
	hostname, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid dial address %q: %w", address, err)
	}
	asciiHostname, err := syntheticASCIIHostname(hostname)
	if err != nil {
		return "", err
	}
	if asciiHostname == hostname {
		return address, nil
	}
	return net.JoinHostPort(asciiHostname, port), nil
}

func normalizeSyntheticURLHostname(target *url.URL) error {
	if target == nil || target.Hostname() == "" {
		return errors.New("request host is required")
	}
	hostname := target.Hostname()
	asciiHostname, err := syntheticASCIIHostname(hostname)
	if err != nil {
		return err
	}
	if asciiHostname == hostname {
		return nil
	}
	if port := target.Port(); port != "" {
		target.Host = net.JoinHostPort(asciiHostname, port)
	} else {
		target.Host = asciiHostname
	}
	return nil
}

func normalizedProxyScheme(proxyURL *url.URL) string {
	if proxyURL == nil {
		return ""
	}
	scheme := strings.ToLower(strings.TrimSpace(proxyURL.Scheme))
	if scheme == "" {
		return "http"
	}
	return scheme
}

func cloneURL(source *url.URL) *url.URL {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}

func watchSyntheticConnContext(ctx context.Context, conn net.Conn) func() {
	if ctx == nil || conn == nil {
		return func() {}
	}
	stop := context.AfterFunc(ctx, func() {
		_ = conn.Close()
	})
	return func() {
		stop()
	}
}

func setSyntheticConnDeadline(ctx context.Context, conn net.Conn) func() {
	if ctx == nil || conn == nil {
		return func() {}
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return func() {}
	}
	_ = conn.SetDeadline(deadline)
	return func() {
		_ = conn.SetDeadline(time.Time{})
	}
}

func syntheticContextError(ctx context.Context, fallback error) error {
	if ctx == nil {
		return fallback
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return fallback
}
