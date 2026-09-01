package proxyservice

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"net"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/websocket"
	http "github.com/josexy/xhttp"
	"github.com/josexy/xhttp/httptest"
	utls "github.com/refraction-networking/utls"
)

func TestResolveUTLSClientHelloID(t *testing.T) {
	tests := []struct {
		configured TLSClientHelloID
		want       utls.ClientHelloID
	}{
		{configured: TLSClientHelloGolang, want: utls.HelloGolang},
		{configured: TLSClientHelloChromeAuto, want: utls.HelloChrome_Auto},
		{configured: TLSClientHelloFirefoxAuto, want: utls.HelloFirefox_Auto},
		{configured: TLSClientHelloSafariAuto, want: utls.HelloSafari_Auto},
		{configured: TLSClientHelloEdgeAuto, want: utls.HelloEdge_Auto},
		{configured: TLSClientHelloIOSAuto, want: utls.HelloIOS_Auto},
		{configured: TLSClientHelloAndroid11OkHTTP, want: utls.HelloAndroid_11_OkHttp},
		{configured: TLSClientHelloRandomizedALPN, want: utls.HelloRandomizedALPN},
		{configured: TLSClientHelloID("unknown"), want: utls.HelloGolang},
	}

	for _, tt := range tests {
		if got := resolveUTLSClientHelloID(tt.configured); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("resolveUTLSClientHelloID(%q) = %+v, want %+v", tt.configured, got, tt.want)
		}
	}
}

func TestSyntheticAddressesNormalizeIDNHostnames(t *testing.T) {
	target, err := url.Parse("https://bücher.example/path")
	if err != nil {
		t.Fatal(err)
	}
	address, serverName, err := syntheticOriginAddress(target)
	if err != nil {
		t.Fatal(err)
	}
	if address != "xn--bcher-kva.example:443" || serverName != "xn--bcher-kva.example" {
		t.Fatalf("origin address=%q serverName=%q", address, serverName)
	}
	if err := normalizeSyntheticURLHostname(target); err != nil {
		t.Fatal(err)
	}
	if target.Host != "xn--bcher-kva.example" {
		t.Fatalf("normalized URL host=%q", target.Host)
	}
	dialAddress, err := syntheticASCIIAddress("bücher.example:9443")
	if err != nil {
		t.Fatal(err)
	}
	if dialAddress != "xn--bcher-kva.example:9443" {
		t.Fatalf("dial address=%q", dialAddress)
	}

	proxyURL, err := url.Parse("https://bücher.example:8443")
	if err != nil {
		t.Fatal(err)
	}
	proxyAddress, err := syntheticProxyAddress(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	if proxyAddress != "xn--bcher-kva.example:8443" {
		t.Fatalf("proxy address=%q", proxyAddress)
	}
}

func TestNormalizeSyntheticRandomizedGroups(t *testing.T) {
	tests := []struct {
		name      string
		curves    []utls.CurveID
		keyShares []utls.KeyShare
		want      []utls.CurveID
	}{
		{
			name:      "remove hybrid curve without key share",
			curves:    []utls.CurveID{utls.X25519MLKEM768, utls.X25519, utls.CurveP256},
			keyShares: []utls.KeyShare{{Group: utls.X25519}},
			want:      []utls.CurveID{utls.X25519, utls.CurveP256},
		},
		{
			name:      "add hybrid curve for key share",
			curves:    []utls.CurveID{utls.X25519, utls.CurveP256},
			keyShares: []utls.KeyShare{{Group: utls.X25519MLKEM768}, {Group: utls.X25519}},
			want:      []utls.CurveID{utls.X25519MLKEM768, utls.X25519, utls.CurveP256},
		},
		{
			name:      "keep matching groups",
			curves:    []utls.CurveID{utls.X25519MLKEM768, utls.X25519},
			keyShares: []utls.KeyShare{{Group: utls.X25519MLKEM768}, {Group: utls.X25519}},
			want:      []utls.CurveID{utls.X25519MLKEM768, utls.X25519},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			curves := &utls.SupportedCurvesExtension{Curves: slices.Clone(tt.curves)}
			conn := &utls.UConn{Extensions: []utls.TLSExtension{
				curves,
				&utls.KeyShareExtension{KeyShares: slices.Clone(tt.keyShares)},
			}}
			normalizeSyntheticRandomizedGroups(conn)
			if !slices.Equal(curves.Curves, tt.want) {
				t.Fatalf("curves = %#v, want %#v", curves.Curves, tt.want)
			}
		})
	}
}

func TestConfiguredClientHelloProfilesReachHTTPSWire(t *testing.T) {
	server, captured := newClientHelloCaptureServer(t, true, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	profiles := []struct {
		name             string
		configured       TLSClientHelloID
		profile          utls.ClientHelloID
		shuffledExtOrder bool
		randomized       bool
	}{
		{name: "golang", configured: TLSClientHelloGolang, profile: utls.HelloGolang},
		{name: "chrome", configured: TLSClientHelloChromeAuto, profile: utls.HelloChrome_Auto, shuffledExtOrder: true},
		{name: "firefox", configured: TLSClientHelloFirefoxAuto, profile: utls.HelloFirefox_Auto},
		{name: "safari", configured: TLSClientHelloSafariAuto, profile: utls.HelloSafari_Auto},
		{name: "edge", configured: TLSClientHelloEdgeAuto, profile: utls.HelloEdge_Auto},
		{name: "ios", configured: TLSClientHelloIOSAuto, profile: utls.HelloIOS_Auto},
		{name: "android", configured: TLSClientHelloAndroid11OkHTTP, profile: utls.HelloAndroid_11_OkHttp},
		{name: "randomized alpn", configured: TLSClientHelloRandomizedALPN, profile: utls.HelloRandomizedALPN, randomized: true},
	}

	for _, tt := range profiles {
		t.Run(tt.name, func(t *testing.T) {
			transport := newSyntheticRoundTripper(
				&settingservice.ProxyConfig{SkipVerifyTLS: true},
				SendRequestProtocolAuto,
				nil,
				tt.configured,
			)
			client := &http.Client{Transport: transport}
			resp, err := client.Get(server.URL)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			responseProtocol := resp.Proto
			_ = resp.Body.Close()
			transport.CloseIdleConnections()

			var raw []byte
			select {
			case raw = <-captured:
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for ClientHello capture")
			}
			shape, err := parseClientHelloShape(raw)
			if err != nil {
				t.Fatalf("parse ClientHello: %v", err)
			}
			if len(shape.cipherSuites) == 0 || len(shape.extensions) == 0 {
				t.Fatalf("empty ClientHello shape: %+v", shape)
			}
			if len(shape.alpn) == 0 && responseProtocol != "HTTP/1.1" {
				t.Fatalf("ClientHello without ALPN used response protocol %q, want HTTP/1.1", responseProtocol)
			}
			if len(shape.alpn) > 0 && !slices.Contains(shape.alpn, syntheticALPNHTTP1) && !slices.Contains(shape.alpn, syntheticALPNHTTP2) {
				t.Fatalf("ClientHello ALPN = %#v, want an HTTP protocol", shape.alpn)
			}

			if tt.randomized {
				if len(shape.alpn) == 0 {
					t.Fatal("randomized_alpn ClientHello did not contain ALPN")
				}
				return
			}

			wantShape := captureReferenceClientHelloShape(t, server.URL, captured, tt.profile)
			if !slices.Equal(shape.cipherSuites, wantShape.cipherSuites) {
				t.Fatalf("cipher suite order:\n got %#v\nwant %#v", shape.cipherSuites, wantShape.cipherSuites)
			}
			gotExtensions := slices.Clone(shape.extensions)
			wantExtensions := slices.Clone(wantShape.extensions)
			if tt.shuffledExtOrder {
				slices.Sort(gotExtensions)
				slices.Sort(wantExtensions)
			}
			if !slices.Equal(gotExtensions, wantExtensions) {
				t.Fatalf("extension order:\n got %#v\nwant %#v", shape.extensions, wantExtensions)
			}
		})
	}
}

func captureReferenceClientHelloShape(
	t *testing.T,
	serverURL string,
	captured <-chan []byte,
	profile utls.ClientHelloID,
) clientHelloShape {
	t.Helper()
	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse reference server URL: %v", err)
	}
	rawConn, err := net.Dial("tcp", parsedURL.Host)
	if err != nil {
		t.Fatalf("dial reference server: %v", err)
	}
	uconn := utls.UClient(rawConn, &utls.Config{
		ServerName:         parsedURL.Hostname(),
		NextProtos:         []string{syntheticALPNHTTP2, syntheticALPNHTTP1},
		InsecureSkipVerify: true, //nolint:gosec
	}, profile)
	if err := uconn.HandshakeContext(context.Background()); err != nil {
		_ = rawConn.Close()
		t.Fatalf("reference uTLS handshake: %v", err)
	}
	_ = uconn.Close()

	select {
	case raw := <-captured:
		shape, err := parseClientHelloShape(raw)
		if err != nil {
			t.Fatalf("parse reference ClientHello: %v", err)
		}
		return shape
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reference ClientHello capture")
		return clientHelloShape{}
	}
}

func TestSyntheticHTTPSProtocolNegotiation(t *testing.T) {
	tests := []struct {
		name        string
		protocol    SendRequestProtocol
		enableH2    bool
		wantProto   string
		wantErrPart string
	}{
		{name: "auto http1", protocol: SendRequestProtocolAuto, wantProto: "HTTP/1.1"},
		{name: "auto http2", protocol: SendRequestProtocolAuto, enableH2: true, wantProto: "HTTP/2.0"},
		{name: "explicit http1", protocol: SendRequestProtocolHTTP1, enableH2: true, wantProto: "HTTP/1.1"},
		{name: "explicit http2", protocol: SendRequestProtocolHTTP2, enableH2: true, wantProto: "HTTP/2.0"},
		{name: "http2 does not downgrade", protocol: SendRequestProtocolHTTP2, wantErrPart: "HTTP/2 requires h2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			server.EnableHTTP2 = tt.enableH2
			server.StartTLS()
			defer server.Close()

			transport := newSyntheticRoundTripper(
				&settingservice.ProxyConfig{SkipVerifyTLS: true},
				tt.protocol,
				nil,
				TLSClientHelloChromeAuto,
			)
			defer transport.CloseIdleConnections()
			resp, err := (&http.Client{Transport: transport}).Get(server.URL)
			if tt.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("GET error = %v, want substring %q", err, tt.wantErrPart)
				}
				return
			}
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			if resp.Proto != tt.wantProto {
				t.Fatalf("response protocol = %q, want %q", resp.Proto, tt.wantProto)
			}
		})
	}
}

func TestSyntheticHTTPSCertificateVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	verified := newSyntheticRoundTripper(
		&settingservice.ProxyConfig{},
		SendRequestProtocolAuto,
		nil,
		TLSClientHelloFirefoxAuto,
	)
	_, err := (&http.Client{Transport: verified}).Get(server.URL)
	verified.CloseIdleConnections()
	if err == nil {
		t.Fatal("request with an untrusted certificate unexpectedly succeeded")
	}

	insecure := newSyntheticRoundTripper(
		&settingservice.ProxyConfig{SkipVerifyTLS: true},
		SendRequestProtocolAuto,
		nil,
		TLSClientHelloFirefoxAuto,
	)
	resp, err := (&http.Client{Transport: insecure}).Get(server.URL)
	if err != nil {
		t.Fatalf("request with skip verification: %v", err)
	}
	_ = resp.Body.Close()
	insecure.CloseIdleConnections()
}

func TestSyntheticTLSHandshakeHonorsContextDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	serverDone := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			defer conn.Close()
			<-serverDone
		}
	}()
	defer close(serverDone)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = dialSyntheticTLSDirect(
		ctx,
		"tcp",
		listener.Addr().String(),
		"localhost",
		utls.HelloSafari_Auto,
		[]string{syntheticALPNHTTP1},
		true,
		true,
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("handshake error = %v, want context deadline exceeded", err)
	}
}

func TestSyntheticHTTPSProxyPathsPreserveSelectedClientHello(t *testing.T) {
	target, targetHellos := newClientHelloCaptureServer(t, true, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	httpProxy := httptest.NewServer(connectTunnelHandler(t))
	defer httpProxy.Close()
	httpsProxy, httpsProxyHellos := newClientHelloCaptureServer(t, false, connectTunnelHandler(t))
	defer httpsProxy.Close()
	socksProxyURL := startTestSOCKS5Proxy(t)

	tests := []struct {
		name              string
		proxy             string
		proxyClientHellos <-chan []byte
	}{
		{name: "direct"},
		{name: "http connect", proxy: httpProxy.URL},
		{name: "https connect", proxy: httpsProxy.URL, proxyClientHellos: httpsProxyHellos},
		{name: "socks5", proxy: socksProxyURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var proxyURL *url.URL
			if tt.proxy != "" {
				var err error
				proxyURL, err = url.Parse(tt.proxy)
				if err != nil {
					t.Fatalf("parse proxy URL: %v", err)
				}
			}
			transport := newSyntheticRoundTripper(
				&settingservice.ProxyConfig{SkipVerifyTLS: true},
				SendRequestProtocolAuto,
				proxyURL,
				TLSClientHelloChromeAuto,
			)
			resp, err := (&http.Client{Transport: transport}).Get(target.URL)
			if err != nil {
				t.Fatalf("GET through %s: %v", tt.name, err)
			}
			_ = resp.Body.Close()
			transport.CloseIdleConnections()

			gotTarget := receiveClientHelloShape(t, targetHellos)
			wantTarget := captureReferenceClientHelloShape(t, target.URL, targetHellos, utls.HelloChrome_Auto)
			assertClientHelloShape(t, gotTarget, wantTarget, true)
			if tt.proxyClientHellos != nil {
				proxyShape := receiveClientHelloShape(t, tt.proxyClientHellos)
				if !slices.Equal(proxyShape.alpn, []string{syntheticALPNHTTP1}) {
					t.Fatalf("HTTPS proxy ALPN = %#v, want HTTP/1.1 only", proxyShape.alpn)
				}
				wantProxy := wantTarget
				wantProxy.extensions = slices.DeleteFunc(slices.Clone(wantTarget.extensions), func(extensionID uint16) bool {
					return extensionID == 0x4469 || extensionID == 0x44cd // ALPS old/new code points
				})
				assertClientHelloShape(t, proxyShape, wantProxy, true)
			}
		})
	}
}

func TestHTTPRequestUsesConfiguredClientHello(t *testing.T) {
	server, captured := newClientHelloCaptureServer(t, true, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	svc := newTestProxyService(t, &settingservice.ProxyConfig{SkipVerifyTLS: true})

	response, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{
			ProxyMode:        SendRequestProxyModeNone,
			Protocol:         SendRequestProtocolAuto,
			TLSClientHelloID: TLSClientHelloFirefoxAuto,
		},
		http.MethodGet,
		server.URL,
		nil,
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Protocol != "HTTP/2.0" {
		t.Fatalf("response protocol = %q, want HTTP/2.0", response.Protocol)
	}
	got := receiveClientHelloShape(t, captured)
	want := captureReferenceClientHelloShape(t, server.URL, captured, utls.HelloFirefox_Auto)
	assertClientHelloShape(t, got, want, false)
}

func TestHTTPRequestUsesConfiguredHTTP2Fingerprint(t *testing.T) {
	const encoded = "1:65536;3:1000;4:6291456;6:262144|15663105|3:0:0:201,5:1:3:101|s,m,a,p"

	for _, protocol := range []SendRequestProtocol{SendRequestProtocolAuto, SendRequestProtocolHTTP2} {
		t.Run(string(protocol), func(t *testing.T) {
			observed := make(chan http.Fingerprint, 1)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				fingerprint, ok := http.RequestFingerprint(req)
				if !ok {
					http.Error(w, "missing HTTP/2 fingerprint", http.StatusInternalServerError)
					return
				}
				observed <- fingerprint
				w.WriteHeader(http.StatusNoContent)
			}))
			server.EnableHTTP2 = true
			server.StartTLS()
			defer server.Close()

			svc := newTestProxyService(t, &settingservice.ProxyConfig{SkipVerifyTLS: true})
			response, err := svc.SendHTTPRequest(
				context.Background(),
				SendRequestConfig{
					ProxyMode:        SendRequestProxyModeNone,
					Protocol:         protocol,
					TLSClientHelloID: TLSClientHelloChromeAuto,
					HTTP2Fingerprint: encoded,
				},
				http.MethodGet,
				server.URL,
				nil,
				SendRequestBody{BodyType: SendRequestBodyTypeNone},
			)
			if err != nil {
				t.Fatalf("SendHTTPRequest: %v", err)
			}
			if response.Protocol != "HTTP/2.0" {
				t.Fatalf("response protocol = %q, want HTTP/2.0", response.Protocol)
			}
			if len(response.HeaderFields) == 0 || response.HeaderFields[0] != (HTTPHeaderField{Name: ":status", Value: "204"}) {
				t.Fatalf("HTTP/2 response fields = %#v, want leading :status", response.HeaderFields)
			}
			select {
			case fingerprint := <-observed:
				if got := fingerprint.String(); got != encoded {
					t.Fatalf("HTTP/2 fingerprint = %q, want %q", got, encoded)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for HTTP/2 fingerprint")
			}
		})
	}
}

func TestHTTPRequestHTTP2FingerprintRebuildsRedirectHeaderBlock(t *testing.T) {
	const encoded = "1:65536;3:1000;4:6291456;6:262144|15663105|0|m,a,s,p"
	type observedRequest struct {
		method        string
		host          string
		requestURI    string
		authorize     string
		cookie        string
		fingerprint   string
		requestEditor string
	}
	observed := make(chan observedRequest, 1)
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fingerprint, ok := http.RequestFingerprint(req)
		if !ok {
			http.Error(w, "missing HTTP/2 fingerprint", http.StatusInternalServerError)
			return
		}
		observed <- observedRequest{
			method:        req.Method,
			host:          req.Host,
			requestURI:    req.URL.RequestURI(),
			authorize:     req.Header.Get("Authorization"),
			cookie:        req.Header.Get("Cookie"),
			fingerprint:   fingerprint.String(),
			requestEditor: req.Header.Get("X-Request-Editor"),
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	target.EnableHTTP2 = true
	target.StartTLS()
	defer target.Close()
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	targetURL.Host = net.JoinHostPort("localhost", targetURL.Port())
	targetURL.Path = "/redirect-target"

	redirect := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", targetURL.String())
		w.WriteHeader(http.StatusFound)
	}))
	redirect.EnableHTTP2 = true
	redirect.StartTLS()
	defer redirect.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{SkipVerifyTLS: true})
	response, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{
			ProxyMode:        SendRequestProxyModeNone,
			Protocol:         SendRequestProtocolHTTP2,
			HTTP2Fingerprint: encoded,
		},
		http.MethodGet,
		redirect.URL+"/redirect-source",
		[]HTTPHeaderField{
			{Name: "Authorization", Value: "Bearer secret"},
			{Name: "Cookie", Value: "session=secret"},
			{Name: "X-Request-Editor", Value: "kept"},
		},
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	got := <-observed
	if got.method != http.MethodGet || got.host != targetURL.Host || got.requestURI != targetURL.RequestURI() {
		t.Fatalf("redirect target observed method=%q host=%q requestURI=%q", got.method, got.host, got.requestURI)
	}
	if got.authorize != "" || got.cookie != "" {
		t.Fatalf("cross-host redirect leaked authorization=%q cookie=%q", got.authorize, got.cookie)
	}
	if got.fingerprint != encoded {
		t.Fatalf("redirect fingerprint=%q, want %q", got.fingerprint, encoded)
	}
	if got.requestEditor != "kept" {
		t.Fatalf("redirect X-Request-Editor=%q, want kept", got.requestEditor)
	}
}

func TestHTTPRequestHTTP2FingerprintPreserves307RedirectBody(t *testing.T) {
	const encoded = "1:65536;3:1000;4:6291456;6:262144|15663105|0|m,a,s,p"
	type observedRequest struct {
		method        string
		host          string
		requestURI    string
		body          string
		contentLength int64
	}
	observed := make(chan observedRequest, 1)
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read redirected body: %v", err)
			return
		}
		observed <- observedRequest{
			method:        req.Method,
			host:          req.Host,
			requestURI:    req.URL.RequestURI(),
			body:          string(body),
			contentLength: req.ContentLength,
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	target.EnableHTTP2 = true
	target.StartTLS()
	defer target.Close()

	redirect := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL+"/redirect-target")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	redirect.EnableHTTP2 = true
	redirect.StartTLS()
	defer redirect.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{SkipVerifyTLS: true})
	response, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{
			ProxyMode:        SendRequestProxyModeNone,
			Protocol:         SendRequestProtocolHTTP2,
			HTTP2Fingerprint: encoded,
		},
		http.MethodPost,
		redirect.URL+"/redirect-source",
		nil,
		SendRequestBody{BodyType: SendRequestBodyTypeText, Text: "body"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	got := <-observed
	if got.method != http.MethodPost || got.host != strings.TrimPrefix(target.URL, "https://") || got.requestURI != "/redirect-target" {
		t.Fatalf("redirect target observed method=%q host=%q requestURI=%q", got.method, got.host, got.requestURI)
	}
	if got.body != "body" || got.contentLength != 4 {
		t.Fatalf("redirect body=%q contentLength=%d", got.body, got.contentLength)
	}
}

func TestHTTPRequestHTTP2FingerprintDoesNotForceHTTP2(t *testing.T) {
	const encoded = "1:65536;3:1000;4:6291456;6:262144|15663105|0|m,a,s,p"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{SkipVerifyTLS: true})
	response, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{
			ProxyMode:        SendRequestProxyModeNone,
			Protocol:         SendRequestProtocolAuto,
			HTTP2Fingerprint: encoded,
		},
		http.MethodGet,
		server.URL,
		[]HTTPHeaderField{{Name: "X-Request-Editor", Value: "one"}},
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Protocol != "HTTP/1.1" {
		t.Fatalf("response protocol = %q, want HTTP/1.1", response.Protocol)
	}
}

func TestHTTPRequestUsesConfiguredHTTP2FingerprintThroughHTTPProxy(t *testing.T) {
	const encoded = "1:65536;3:1000;4:6291456;6:262144|15663105|0|s,m,a,p"
	observed := make(chan http.Fingerprint, 1)
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fingerprint, ok := http.RequestFingerprint(req)
		if !ok {
			http.Error(w, "missing HTTP/2 fingerprint", http.StatusInternalServerError)
			return
		}
		observed <- fingerprint
		w.WriteHeader(http.StatusNoContent)
	}))
	target.EnableHTTP2 = true
	target.StartTLS()
	defer target.Close()
	proxy := httptest.NewServer(connectTunnelHandler(t))
	defer proxy.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{SkipVerifyTLS: true})
	response, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{
			ProxyMode:        SendRequestProxyModeCustom,
			Protocol:         SendRequestProtocolHTTP2,
			CustomProxy:      proxy.URL,
			TLSClientHelloID: TLSClientHelloFirefoxAuto,
			HTTP2Fingerprint: encoded,
		},
		http.MethodGet,
		target.URL,
		nil,
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Protocol != "HTTP/2.0" {
		t.Fatalf("response protocol = %q, want HTTP/2.0", response.Protocol)
	}
	select {
	case fingerprint := <-observed:
		if got := fingerprint.String(); got != encoded {
			t.Fatalf("HTTP/2 fingerprint = %q, want %q", got, encoded)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for HTTP/2 fingerprint")
	}
}

func TestHTTPRequestUsesConfiguredHTTP2FingerprintOverH2C(t *testing.T) {
	const encoded = "1:65536;3:1000;4:6291456;6:262144|15663105|0|m,a,s,p"
	observed := make(chan http.Fingerprint, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fingerprint, ok := http.RequestFingerprint(req)
		if !ok {
			http.Error(w, "missing HTTP/2 fingerprint", http.StatusInternalServerError)
			return
		}
		observed <- fingerprint
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.Protocols = new(http.Protocols)
	server.Config.Protocols.SetUnencryptedHTTP2(true)
	server.Start()
	defer server.Close()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	response, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{
			ProxyMode:        SendRequestProxyModeNone,
			Protocol:         SendRequestProtocolHTTP2,
			HTTP2Fingerprint: encoded,
		},
		http.MethodGet,
		server.URL,
		nil,
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("SendHTTPRequest: %v", err)
	}
	if response.Protocol != "HTTP/2.0" {
		t.Fatalf("response protocol = %q, want HTTP/2.0", response.Protocol)
	}
	select {
	case fingerprint := <-observed:
		if got := fingerprint.String(); got != encoded {
			t.Fatalf("HTTP/2 fingerprint = %q, want %q", got, encoded)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for HTTP/2 fingerprint")
	}
}

func TestHTTPRequestHTTP2FingerprintValidation(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	svc := newTestProxyService(t, &settingservice.ProxyConfig{SkipVerifyTLS: true})

	response, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone, HTTP2Fingerprint: ""},
		http.MethodGet,
		server.URL,
		nil,
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("empty HTTP/2 fingerprint changed the default request path: %v", err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("empty HTTP/2 fingerprint status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	if response.Protocol != "HTTP/2.0" {
		t.Fatalf("empty HTTP/2 fingerprint protocol = %q, want HTTP/2.0", response.Protocol)
	}

	_, err = svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{ProxyMode: SendRequestProxyModeNone, HTTP2Fingerprint: "invalid"},
		http.MethodGet,
		server.URL,
		nil,
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err == nil || !strings.Contains(err.Error(), "invalid HTTP/2 fingerprint") {
		t.Fatalf("invalid HTTP/2 fingerprint error = %v", err)
	}

	response, err = svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{
			ProxyMode:        SendRequestProxyModeNone,
			Protocol:         SendRequestProtocolHTTP1,
			HTTP2Fingerprint: "invalid",
		},
		http.MethodGet,
		server.URL,
		nil,
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("HTTP/1 request parsed unused HTTP/2 fingerprint: %v", err)
	}
	if response.StatusCode != http.StatusNoContent || response.Protocol != "HTTP/1.1" {
		t.Fatalf("HTTP/1 response status=%d protocol=%q", response.StatusCode, response.Protocol)
	}
}

func TestResendUsesDefaultClientHello(t *testing.T) {
	server, captured := newClientHelloCaptureServer(t, true, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	svc := newTestProxyService(t, &settingservice.ProxyConfig{SkipVerifyTLS: true})
	entry := &TrafficEntry{
		ID:     701,
		Type:   "https",
		Method: http.MethodGet,
		URL:    server.URL,
		Request: &HTTPMessage{
			Proto: "HTTP/2.0",
		},
	}

	result, err := svc.ResendRequestWithTrafficEntry(context.Background(), ResendConfig{Count: 1}, entry, nil)
	if err != nil {
		t.Fatalf("ResendRequestWithTrafficEntry: %v", err)
	}
	if result.Success != 1 || result.Failed != 0 {
		t.Fatalf("resend result = %+v, want one success", result)
	}
	got := receiveClientHelloShape(t, captured)
	want := captureReferenceClientHelloShape(t, server.URL, captured, utls.HelloGolang)
	assertClientHelloShape(t, got, want, false)
}

func TestWebSocketSessionUsesConfiguredClientHelloAndHTTP1ALPN(t *testing.T) {
	serverDone := make(chan struct{})
	server, captured := newClientHelloCaptureServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, req, nil)
		if err != nil {
			t.Errorf("upgrade WebSocket: %v", err)
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
		close(serverDone)
	}))
	defer server.Close()
	svc := newTestProxyService(t, &settingservice.ProxyConfig{SkipVerifyTLS: true})

	connected, err := svc.ConnectWebSocket(context.Background(), WebSocketConnectRequest{
		URL:              strings.Replace(server.URL, "https://", "wss://", 1),
		TLSClientHelloID: TLSClientHelloIOSAuto,
	})
	if err != nil {
		t.Fatalf("ConnectWebSocket: %v", err)
	}
	got := receiveClientHelloShape(t, captured)
	if !slices.Equal(got.alpn, []string{syntheticALPNHTTP1}) {
		t.Fatalf("WSS ClientHello ALPN = %#v, want HTTP/1.1 only", got.alpn)
	}
	want := captureReferenceClientHelloShape(t, server.URL, captured, utls.HelloIOS_Auto)
	assertClientHelloShape(t, got, want, false)
	if err := svc.DisconnectWebSocket(connected.SessionID); err != nil {
		t.Fatalf("DisconnectWebSocket: %v", err)
	}
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for WebSocket server shutdown")
	}
}

func TestMITMCaptureStillMirrorsDownstreamClientHello(t *testing.T) {
	target, targetHellos := newClientHelloCaptureServer(t, false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}
	targetURL.Host = net.JoinHostPort("localhost", targetURL.Port())
	targetAddress := targetURL.String()

	port := reserveTestTCPPort(t)
	certDir := t.TempDir()
	svc := newTestProxyService(t, &settingservice.ProxyConfig{
		Mode:          settingservice.ProxyModeHTTP,
		Host:          "127.0.0.1",
		Port:          port,
		CACertPath:    certDir + "/ca.crt",
		CAKeyPath:     certDir + "/ca.key",
		DisableProxy:  true,
		SkipVerifyTLS: true,
	})
	t.Cleanup(svc.baseCancel)
	if _, err := svc.settingService.GenerateCurrentCACertificate(settingservice.GenerateCACertificateRequest{}); err != nil {
		t.Fatalf("generate MITM CA: %v", err)
	}
	status, err := svc.Start()
	if err != nil {
		t.Fatalf("start MITM proxy: %v", err)
	}
	defer func() {
		if _, err := svc.Stop(); err != nil {
			t.Errorf("stop MITM proxy: %v", err)
		}
	}()
	proxyURL, err := url.Parse("http://" + status.Address)
	if err != nil {
		t.Fatalf("parse MITM proxy URL: %v", err)
	}
	requestResponse, err := svc.SendHTTPRequest(
		context.Background(),
		SendRequestConfig{
			ProxyMode:        SendRequestProxyModeMITM,
			Protocol:         SendRequestProtocolAuto,
			TLSClientHelloID: TLSClientHelloChromeAuto,
		},
		http.MethodGet,
		targetAddress,
		nil,
		SendRequestBody{BodyType: SendRequestBodyTypeNone},
	)
	if err != nil {
		t.Fatalf("HTTP Request through MITM proxy: %v", err)
	}
	if requestResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("HTTP Request status = %d, want %d", requestResponse.StatusCode, http.StatusNoContent)
	}
	requestHello := receiveClientHelloShape(t, targetHellos)
	wantRequestHello := captureReferenceClientHelloShape(t, targetAddress, targetHellos, utls.HelloChrome_Auto)
	assertClientHelloShape(t, requestHello, wantRequestHello, true)

	// The HTTP request selected Chrome. Use a Firefox downstream
	// ClientHello to verify the ordinary MITM path continues mirroring its
	// client instead of reading the request selection.
	conn, err := dialSyntheticTLS(
		context.Background(),
		"tcp",
		targetURL.Host,
		targetURL.Hostname(),
		proxyURL,
		utls.HelloFirefox_Auto,
		[]string{syntheticALPNHTTP1},
		true,
		true,
	)
	if err != nil {
		t.Fatalf("dial through MITM proxy: %v", err)
	}
	defer conn.Close()
	request, err := http.NewRequest(http.MethodGet, targetAddress, nil)
	if err != nil {
		t.Fatalf("build target request: %v", err)
	}
	if err := request.Write(conn); err != nil {
		t.Fatalf("write target request: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		t.Fatalf("read target response: %v", err)
	}
	_ = response.Body.Close()

	got := receiveClientHelloShape(t, targetHellos)
	want := captureReferenceClientHelloShape(t, targetAddress, targetHellos, utls.HelloFirefox_Auto)
	assertClientHelloShape(t, got, want, false)
}

func reserveTestTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve TCP port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release reserved TCP port: %v", err)
	}
	return port
}

func assertClientHelloShape(t *testing.T, got, want clientHelloShape, shuffledExtensions bool) {
	t.Helper()
	if !slices.Equal(got.cipherSuites, want.cipherSuites) {
		t.Fatalf("cipher suite order:\n got %#v\nwant %#v", got.cipherSuites, want.cipherSuites)
	}
	gotExtensions := slices.Clone(got.extensions)
	wantExtensions := slices.Clone(want.extensions)
	if shuffledExtensions {
		slices.Sort(gotExtensions)
		slices.Sort(wantExtensions)
	}
	if !slices.Equal(gotExtensions, wantExtensions) {
		t.Fatalf("extensions:\n got %#v\nwant %#v", got.extensions, want.extensions)
	}
}

func receiveClientHelloShape(t *testing.T, captured <-chan []byte) clientHelloShape {
	t.Helper()
	select {
	case raw := <-captured:
		shape, err := parseClientHelloShape(raw)
		if err != nil {
			t.Fatalf("parse captured ClientHello: %v", err)
		}
		return shape
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ClientHello capture")
		return clientHelloShape{}
	}
}

func connectTunnelHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		upstream, err := net.Dial("tcp", req.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			_ = upstream.Close()
			t.Errorf("proxy response writer does not support hijacking")
			return
		}
		client, buffered, err := hijacker.Hijack()
		if err != nil {
			_ = upstream.Close()
			t.Errorf("hijack CONNECT: %v", err)
			return
		}
		defer client.Close()
		defer upstream.Close()
		if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			t.Errorf("write CONNECT response: %v", err)
			return
		}
		if err := buffered.Flush(); err != nil {
			t.Errorf("flush CONNECT response: %v", err)
			return
		}
		copyDone := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstream, client)
			_ = upstream.Close()
			close(copyDone)
		}()
		_, _ = io.Copy(client, upstream)
		<-copyDone
	})
}

func startTestSOCKS5Proxy(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen SOCKS5: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveTestSOCKS5Conn(ctx, conn)
		}
	}()
	return "socks5://" + listener.Addr().String()
}

func serveTestSOCKS5Conn(ctx context.Context, client net.Conn) {
	defer client.Close()
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()
	header := make([]byte, 2)
	if _, err := io.ReadFull(client, header); err != nil || header[0] != 5 {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(client, methods); err != nil {
		return
	}
	if _, err := client.Write([]byte{5, 0}); err != nil {
		return
	}
	request := make([]byte, 4)
	if _, err := io.ReadFull(client, request); err != nil || request[0] != 5 || request[1] != 1 {
		return
	}
	host, err := readTestSOCKS5Host(client, request[3])
	if err != nil {
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(client, portBytes); err != nil {
		return
	}
	upstream, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(portBytes)))))
	if err != nil {
		_, _ = client.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()
	if _, err := client.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	copyDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(upstream, client)
		_ = upstream.Close()
		close(copyDone)
	}()
	_, _ = io.Copy(client, upstream)
	<-copyDone
}

func readTestSOCKS5Host(reader io.Reader, addressType byte) (string, error) {
	switch addressType {
	case 1:
		address := make([]byte, net.IPv4len)
		if _, err := io.ReadFull(reader, address); err != nil {
			return "", err
		}
		return net.IP(address).String(), nil
	case 3:
		length := []byte{0}
		if _, err := io.ReadFull(reader, length); err != nil {
			return "", err
		}
		host := make([]byte, int(length[0]))
		if _, err := io.ReadFull(reader, host); err != nil {
			return "", err
		}
		return string(host), nil
	case 4:
		address := make([]byte, net.IPv6len)
		if _, err := io.ReadFull(reader, address); err != nil {
			return "", err
		}
		return net.IP(address).String(), nil
	default:
		return "", errors.New("unsupported SOCKS5 address type")
	}
}

type clientHelloShape struct {
	cipherSuites []uint16
	extensions   []uint16
	alpn         []string
}

func parseClientHelloShape(raw []byte) (clientHelloShape, error) {
	if len(raw) < 4 || raw[0] != 1 {
		return clientHelloShape{}, errors.New("invalid ClientHello handshake header")
	}
	handshakeLength := int(raw[1])<<16 | int(raw[2])<<8 | int(raw[3])
	if len(raw) < 4+handshakeLength {
		return clientHelloShape{}, errors.New("truncated ClientHello handshake")
	}
	body := raw[4 : 4+handshakeLength]
	offset := 2 + 32
	if offset >= len(body) {
		return clientHelloShape{}, errors.New("truncated ClientHello random")
	}
	sessionIDLength := int(body[offset])
	offset++
	if offset+sessionIDLength+2 > len(body) {
		return clientHelloShape{}, errors.New("truncated ClientHello session ID")
	}
	offset += sessionIDLength
	cipherLength := int(binary.BigEndian.Uint16(body[offset : offset+2]))
	offset += 2
	if cipherLength%2 != 0 || offset+cipherLength+1 > len(body) {
		return clientHelloShape{}, errors.New("invalid ClientHello cipher suites")
	}
	shape := clientHelloShape{cipherSuites: make([]uint16, 0, cipherLength/2)}
	for end := offset + cipherLength; offset < end; offset += 2 {
		shape.cipherSuites = append(shape.cipherSuites, normalizeGREASE(binary.BigEndian.Uint16(body[offset:offset+2])))
	}
	compressionLength := int(body[offset])
	offset++
	if offset+compressionLength == len(body) {
		return shape, nil
	}
	if offset+compressionLength+2 > len(body) {
		return clientHelloShape{}, errors.New("truncated ClientHello compression methods")
	}
	offset += compressionLength
	extensionsLength := int(binary.BigEndian.Uint16(body[offset : offset+2]))
	offset += 2
	if offset+extensionsLength > len(body) {
		return clientHelloShape{}, errors.New("truncated ClientHello extensions")
	}
	for end := offset + extensionsLength; offset < end; {
		if offset+4 > end {
			return clientHelloShape{}, errors.New("truncated ClientHello extension header")
		}
		extensionID := binary.BigEndian.Uint16(body[offset : offset+2])
		extensionLength := int(binary.BigEndian.Uint16(body[offset+2 : offset+4]))
		offset += 4
		if offset+extensionLength > end {
			return clientHelloShape{}, errors.New("truncated ClientHello extension data")
		}
		extensionData := body[offset : offset+extensionLength]
		offset += extensionLength
		shape.extensions = append(shape.extensions, normalizeGREASE(extensionID))
		if extensionID == 16 {
			alpn, err := parseClientHelloALPN(extensionData)
			if err != nil {
				return clientHelloShape{}, err
			}
			shape.alpn = alpn
		}
	}
	return shape, nil
}

func parseClientHelloALPN(data []byte) ([]string, error) {
	if len(data) < 2 {
		return nil, errors.New("truncated ClientHello ALPN")
	}
	length := int(binary.BigEndian.Uint16(data[:2]))
	if length != len(data)-2 {
		return nil, errors.New("invalid ClientHello ALPN length")
	}
	var protocols []string
	for offset := 2; offset < len(data); {
		protocolLength := int(data[offset])
		offset++
		if offset+protocolLength > len(data) {
			return nil, errors.New("truncated ClientHello ALPN protocol")
		}
		protocols = append(protocols, string(data[offset:offset+protocolLength]))
		offset += protocolLength
	}
	return protocols, nil
}

func normalizeGREASE(value uint16) uint16 {
	if value&0x0f0f == 0x0a0a && byte(value>>8) == byte(value) {
		return 0x0a0a
	}
	return value
}

type clientHelloCaptureListener struct {
	net.Listener
	captured chan<- []byte
}

func (l *clientHelloCaptureListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &clientHelloCaptureConn{Conn: conn, captured: l.captured}, nil
}

type clientHelloCaptureConn struct {
	net.Conn
	captured chan<- []byte
	buffer   []byte
	once     sync.Once
}

func (c *clientHelloCaptureConn) Read(buffer []byte) (int, error) {
	n, err := c.Conn.Read(buffer)
	if n > 0 {
		c.buffer = append(c.buffer, buffer[:n]...)
		if hello, ok := extractClientHelloHandshake(c.buffer); ok {
			c.once.Do(func() {
				c.captured <- hello
				c.buffer = nil
			})
		}
	}
	return n, err
}

func extractClientHelloHandshake(records []byte) ([]byte, bool) {
	var handshake []byte
	for offset := 0; offset+5 <= len(records); {
		recordLength := int(binary.BigEndian.Uint16(records[offset+3 : offset+5]))
		if offset+5+recordLength > len(records) {
			return nil, false
		}
		if records[offset] == 22 {
			handshake = append(handshake, records[offset+5:offset+5+recordLength]...)
			if len(handshake) >= 4 && handshake[0] == 1 {
				handshakeLength := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
				if len(handshake) >= 4+handshakeLength {
					return slices.Clone(handshake[:4+handshakeLength]), true
				}
			}
		}
		offset += 5 + recordLength
	}
	return nil, false
}

func newClientHelloCaptureServer(
	t *testing.T,
	enableHTTP2 bool,
	handler http.Handler,
) (*httptest.Server, <-chan []byte) {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	captured := make(chan []byte, 16)
	server.Listener = &clientHelloCaptureListener{Listener: server.Listener, captured: captured}
	server.EnableHTTP2 = enableHTTP2
	server.TLS = &tls.Config{Certificates: []tls.Certificate{newTestLeafTLSCertificate(t)}}
	server.StartTLS()
	return server, captured
}

func newTestLeafTLSCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate TLS key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificateDER, err := x509.CreateCertificate(cryptorand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create TLS certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(certificateDER)
	if err != nil {
		t.Fatalf("parse TLS certificate: %v", err)
	}
	return tls.Certificate{
		Certificate: [][]byte{certificateDER},
		PrivateKey:  key,
		Leaf:        leaf,
	}
}

var _ io.Reader = (*clientHelloCaptureConn)(nil)
