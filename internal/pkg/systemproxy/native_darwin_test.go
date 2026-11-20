package systemproxy

import (
	"errors"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
)

func nativeProxyForTest(t *testing.T) *darwinProxyAPI {
	t.Helper()
	a, err := loadDarwinProxy()
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func proxyFixture(t *testing.T, a *darwinProxyAPI) uintptr {
	t.Helper()
	// XML input exercises full property-list decoding; production snapshots
	// are binary. Unknown, PAC and array values must survive unchanged.
	config, err := a.dictionary([]byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>ExceptionsList</key><array><string>*.local</string><string>例子.test</string></array><key>ProxyAutoConfigURLString</key><string>https://example.test/proxy.pac</string><key>UnknownFutureKey</key><data>AQID</data></dict></plist>`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.release(config) })
	return config
}

func TestDarwinProxyNativeBindingsAndRoundTrip(t *testing.T) {
	a := nativeProxyForTest(t)
	if a.text(a.keys["kSCPropNetProxiesHTTPEnable"]) != "HTTPEnable" {
		t.Fatal("CFString variable was not dereferenced")
	}
	if **(**uintptr)(unsafe.Pointer(&a.keyCallbacks)) != 0 || **(**uintptr)(unsafe.Pointer(&a.valueCallbacks)) != 0 {
		t.Fatal("callback structure version/address mismatch")
	}
	config := proxyFixture(t, a)
	data, err := a.serialize(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 8 || string(data[:8]) != "bplist00" {
		t.Fatal("snapshot is not a binary plist")
	}
	decoded, err := a.dictionary(data)
	if err != nil {
		t.Fatal(err)
	}
	defer a.release(decoded)
	if a.equal(config, decoded) == 0 {
		t.Fatal("complete dictionary changed")
	}
	for _, input := range [][]byte{nil, []byte("broken"), []byte(`<plist version="1.0"><string>wrong type</string></plist>`)} {
		if ref, err := a.dictionary(input); err == nil {
			a.release(ref)
			t.Fatal("invalid snapshot accepted")
		}
	}
	for _, mode := range []Mode{ModeHTTP, ModeSOCKS5} {
		managed, err := a.managedDictionary(config, Endpoint{Mode: mode, Host: "::1", Port: 8080})
		if err != nil {
			t.Fatal(err)
		}
		if got := a.proxyURL(managed); got != string(mode)+"://[::1]:8080" {
			t.Fatalf("managed URL: %s", got)
		}
		for _, key := range []string{"ExceptionsList", "ProxyAutoConfigURLString", "UnknownFutureKey"} {
			name := a.string(key)
			if a.equal(a.dictGet(config, name), a.dictGet(managed, name)) == 0 {
				t.Fatalf("lost auxiliary key %s", key)
			}
			a.release(name)
		}
		a.release(managed)
	}
}

func TestDarwinProxyValueTypes(t *testing.T) {
	a := nativeProxyForTest(t)
	for _, tc := range []struct{ enable, host, port, want string }{
		{"<true/>", "proxy.test", "<integer>8080</integer>", "http://proxy.test:8080"},
		{"<integer>1</integer>", "::1", "<integer>8080</integer>", "http://[::1]:8080"},
		{"<false/>", "proxy.test", "<integer>8080</integer>", "socks5://fallback.test:1080"},
		{"<string>yes</string>", "proxy.test", "<integer>8080</integer>", "socks5://fallback.test:1080"},
		{"<integer>0</integer>", "proxy.test", "<integer>8080</integer>", "socks5://fallback.test:1080"},
		{"<true/>", "例子.test", "<string>invalid</string>", "http://%E4%BE%8B%E5%AD%90.test"},
	} {
		config, err := a.dictionary([]byte(`<plist version="1.0"><dict><key>HTTPEnable</key>` + tc.enable + `<key>HTTPProxy</key><string>` + tc.host + `</string><key>HTTPPort</key>` + tc.port + `<key>SOCKSEnable</key><true/><key>SOCKSProxy</key><string>fallback.test</string><key>SOCKSPort</key><integer>1080</integer></dict></plist>`))
		if err != nil {
			t.Fatal(err)
		}
		got := a.proxyURL(config)
		a.release(config)
		if got != tc.want {
			t.Fatalf("%s %s %s: got %s, want %s", tc.enable, tc.host, tc.port, got, tc.want)
		}
	}
}

func TestDarwinProxyMissingBindings(t *testing.T) {
	failure := errors.New("missing API")
	if _, err := bindDarwinProxy(func(string, int) (uintptr, error) { return 0, failure }, nil); err == nil {
		t.Fatal("missing library accepted")
	}
	for _, missing := range []string{"CFRelease", "SCPreferencesLock", "AuthorizationCreate", "kCFTypeDictionaryKeyCallBacks", "kSCPropNetProxiesHTTPEnable"} {
		_, err := bindDarwinProxy(purego.Dlopen, func(h uintptr, name string) (uintptr, error) {
			if name == missing {
				return 0, failure
			}
			return purego.Dlsym(h, name)
		})
		if err == nil {
			t.Fatalf("missing %s accepted", missing)
		}
	}
}

func TestDarwinProxyNativeCandidateOrdering(t *testing.T) {
	a := nativeProxyForTest(t)
	const http = `<key>HTTPEnable</key><true/><key>HTTPProxy</key><string>http.test</string><key>HTTPPort</key><integer>8080</integer>`
	const https = `<key>HTTPSEnable</key><true/><key>HTTPSProxy</key><string>https.test</string><key>HTTPSPort</key><integer>8443</integer>`
	const socks = `<key>SOCKSEnable</key><true/><key>SOCKSProxy</key><string>socks.test</string><key>SOCKSPort</key><integer>1080</integer>`
	for _, tc := range []struct{ name, fields, want string }{
		{"httpFirst", socks + https + http, "http://http.test:8080"},
		{"httpsBeforeSOCKS", socks + https, "http://https.test:8443"},
		{"disabledHTTP", `<key>HTTPEnable</key><false/><key>HTTPProxy</key><string>http.test</string>` + https + socks, "http://https.test:8443"},
		{"missingHost", `<key>HTTPEnable</key><true/>` + https, "http://https.test:8443"},
		{"wrongHostType", `<key>HTTPEnable</key><true/><key>HTTPProxy</key><integer>42</integer>` + https, "http://https.test:8443"},
		{"missingEnable", `<key>HTTPProxy</key><string>http.test</string>` + socks, "socks5://socks.test:1080"},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := a.dictionary([]byte(`<plist version="1.0"><dict>` + tc.fields + `</dict></plist>`))
			if err != nil {
				t.Fatal(err)
			}
			defer a.release(config)
			if got := a.proxyURL(config); got != tc.want {
				t.Fatalf("native proxy URL = %q, want %q", got, tc.want)
			}
		})
	}
	wrongType := a.string("not a dictionary")
	defer a.release(wrongType)
	for _, config := range []uintptr{0, wrongType} {
		if got := a.proxyURL(config); got != "" {
			t.Fatalf("invalid configuration produced proxy %q", got)
		}
	}
}

func TestDarwinProxyReadOnlySmoke(t *testing.T) {
	a := nativeProxyForTest(t)
	proxies := a.copyProxies(0)
	if proxies == 0 {
		t.Fatal("SCDynamicStoreCopyProxies returned no dictionary")
	}
	defer a.release(proxies)
	if a.typeID(proxies) != a.dictType() {
		t.Fatal("invalid native proxy type")
	}
	_ = a.proxyURL(proxies)
	id := a.primaryService()
	if id == "" {
		t.Log("no primary network service; discovery ran, snapshot requires an active service")
		return
	}
	config, err := a.readConfiguration(id)
	if err != nil {
		t.Fatal(err)
	}
	defer a.release(config)
	if _, err := a.serialize(config); err != nil {
		t.Fatal(err)
	}
}

func TestDarwinProxyPrimaryServiceFallback(t *testing.T) {
	base := nativeProxyForTest(t)
	for _, tc := range []struct{ name, ipv4, ipv6, want string }{
		{"ipv4First", `<string>ipv4-service</string>`, `<string>ipv6-service</string>`, "ipv4-service"},
		{"ipv6Fallback", "", `<string>ipv6-service</string>`, "ipv6-service"},
		{"invalidIPv4Service", `<integer>42</integer>`, `<string>ipv6-service</string>`, "ipv6-service"},
		{"emptyIPv4Service", `<string></string>`, `<string>ipv6-service</string>`, "ipv6-service"},
		{"missingServices", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			states := make(map[string]uintptr)
			for key, value := range map[string]string{"State:/Network/Global/IPv4": tc.ipv4, "State:/Network/Global/IPv6": tc.ipv6} {
				if value == "" {
					continue
				}
				state, err := base.dictionary([]byte(`<plist version="1.0"><dict><key>PrimaryService</key>` + value + `</dict></plist>`))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { base.release(state) })
				states[key] = state
			}
			a := *base
			a.copyValue = func(_ uintptr, key uintptr) uintptr {
				if state := states[a.text(key)]; state != 0 {
					return base.retain(state)
				}
				return 0
			}
			if id := a.primaryService(); id != tc.want {
				t.Fatalf("primary service = %q, want %q", id, tc.want)
			}
		})
	}
}

func TestDarwinProxyMissingConfigurationUsesEmptyDictionary(t *testing.T) {
	base := nativeProxyForTest(t)
	a := *base
	a.configuration = func(uintptr) uintptr { return 0 }
	for _, protocol := range []uintptr{0, 1} {
		config, err := a.copyConfiguration(protocol)
		if err != nil {
			t.Fatal(err)
		}
		empty := base.emptyDictionary()
		if base.equal(config, empty) == 0 {
			t.Fatal("missing configuration is not empty")
		}
		base.release(empty)
		base.release(config)
	}
}

// SystemConfiguration/Security are injected; CF encoding and lifetime calls
// still exercise the real framework. These tests never mutate network settings
// or prompt for authorization. Every owned native reference is counted.
type proxyTransactionFixture struct {
	api                darwinProxyAPI
	base               *darwinProxyAPI
	current, staged    uintptr
	refs               map[uintptr]int
	locked, authorized bool
	commits, applies   int
	fail               string
	external           uintptr
}

func transactionFixture(t *testing.T) *proxyTransactionFixture {
	t.Helper()
	base := nativeProxyForTest(t)
	f := &proxyTransactionFixture{api: *base, base: base, current: proxyFixture(t, base), refs: map[uintptr]int{}}
	own := func(ref uintptr) uintptr {
		if ref != 0 {
			f.refs[ref]++
		}
		return ref
	}
	f.api.release = func(ref uintptr) {
		if f.refs[ref] <= 0 {
			t.Fatalf("release unowned reference %#x", ref)
		}
		f.refs[ref]--
		if ref > 100 {
			base.release(ref)
		}
	}
	f.api.retain = func(ref uintptr) uintptr { return own(base.retain(ref)) }
	f.api.stringCreate = func(a uintptr, b *byte, c int64, d uint32, e uint8) uintptr {
		return own(base.stringCreate(a, b, c, d, e))
	}
	f.api.dataCreate = func(a uintptr, b *byte, c int64) uintptr { return own(base.dataCreate(a, b, c)) }
	f.api.decode = func(a, b uintptr, c uint64, d *int64, e *uintptr) uintptr {
		value := base.decode(a, b, c, d, e)
		own(*e)
		return own(value)
	}
	f.api.encode = func(a, b uintptr, c int64, d uint64, e *uintptr) uintptr {
		if f.fail == "serialize" {
			return 0
		}
		value := base.encode(a, b, c, d, e)
		own(*e)
		return own(value)
	}
	f.api.dictCreate = func(a uintptr, b int64, c, d uintptr) uintptr { return own(base.dictCreate(a, b, c, d)) }
	f.api.dictCopy = func(a uintptr, b int64, c uintptr) uintptr { return own(base.dictCopy(a, b, c)) }
	f.api.numberCreate = func(a uintptr, b int64, c *int32) uintptr {
		if f.fail == "number" {
			return 0
		}
		return own(base.numberCreate(a, b, c))
	}
	f.api.authorize = func(_, _ uintptr, _ uint32, auth *uintptr) int32 {
		if f.fail == "authorize" {
			return -60005
		}
		f.authorized = true
		*auth = 1
		return 0
	}
	f.api.freeAuthorization = func(uintptr, uint32) int32 {
		if f.locked {
			t.Fatal("authorization freed before unlock")
		}
		f.authorized = false
		return 0
	}
	f.api.prefsAuthorized = func(uintptr, uintptr, uintptr, uintptr) uintptr {
		if f.fail == "preferences" {
			return 0
		}
		return own(2)
	}
	f.api.prefsCreate = func(uintptr, uintptr, uintptr) uintptr { return own(6) }
	f.api.lock = func(uintptr, uint8) uint8 {
		if f.fail == "lock" {
			return 0
		}
		f.locked = true
		if f.fail == "externalBeforeLock" {
			f.current = f.external
		}
		return 1
	}
	f.api.unlock = func(uintptr) uint8 { f.locked = false; return 1 }
	f.api.services = func(uintptr) uintptr { return own(3) }
	f.api.arrayCount = func(uintptr) int64 {
		if f.fail == "service" {
			return 0
		}
		return 1
	}
	f.api.arrayGet = func(uintptr, int64) uintptr { return 4 }
	serviceID := base.string("fixture-service")
	t.Cleanup(func() { base.release(serviceID) })
	f.api.serviceID = func(uintptr) uintptr { return serviceID }
	f.api.protocol = func(uintptr, uintptr) uintptr { return own(5) }
	f.api.configuration = func(uintptr) uintptr { return f.current }
	f.api.setConfiguration = func(_ uintptr, value uintptr) uint8 {
		if !f.locked {
			t.Fatal("write outside lock")
		}
		if f.staged != 0 {
			base.release(f.staged)
		}
		f.staged = base.retain(value)
		return 1
	}
	f.api.commit = func(uintptr) uint8 {
		f.commits++
		if f.fail == "commit" {
			return 0
		}
		f.current = f.staged
		return 1
	}
	f.api.apply = func(uintptr) uint8 {
		f.applies++
		if f.fail == "applyAlways" || (f.applies == 1 && (f.fail == "apply" || f.fail == "externalAfterCommit")) {
			return 0
		}
		return 1
	}
	f.api.synchronize = func(uintptr) {
		if f.fail == "externalAfterCommit" {
			f.current = f.external
		}
	}
	t.Cleanup(func() {
		if f.staged != 0 {
			base.release(f.staged)
		}
		if f.locked || f.authorized {
			t.Error("native transaction resource retained")
		}
		for ref, count := range f.refs {
			if count != 0 {
				t.Errorf("leaked reference %#x: %d", ref, count)
			}
		}
	})
	return f
}

func TestDarwinProxyTransactionFailures(t *testing.T) {
	for _, failure := range []string{"authorize", "preferences", "lock", "service", "number", "serialize", "commit", "apply", "applyAlways", "externalBeforeLock", "externalAfterCommit"} {
		t.Run(failure, func(t *testing.T) {
			f := transactionFixture(t)
			original := f.current
			expected, err := f.base.serialize(original)
			if err != nil {
				t.Fatal(err)
			}
			f.external = f.base.emptyDictionary()
			defer f.base.release(f.external)
			f.fail = failure
			_, err = f.api.writeConfiguration("fixture-service", expected, func(current uintptr) (uintptr, error) {
				return f.api.managedDictionary(current, Endpoint{Mode: ModeHTTP, Host: "127.0.0.1", Port: 8080})
			})
			if err == nil {
				t.Fatal("failure reported success")
			}
			switch failure {
			case "service":
				if !errors.Is(err, ErrChangedExternally) || f.commits != 0 || f.base.equal(f.current, original) == 0 {
					t.Fatalf("missing service was not an external change: %v", err)
				}
			case "externalBeforeLock", "externalAfterCommit":
				if !errors.Is(err, ErrChangedExternally) || f.current != f.external {
					t.Fatalf("external edit overwritten: %v", err)
				}
			case "applyAlways":
				if !errors.Is(err, errProxyOwnershipLost) {
					t.Fatalf("failed recovery retained ownership: %v", err)
				}
			default:
				if f.base.equal(f.current, original) == 0 {
					t.Fatalf("failed %s changed configuration", failure)
				}
			}
		})
	}
}

func TestDarwinProxyTransactionRestore(t *testing.T) {
	for _, mode := range []Mode{ModeHTTP, ModeSOCKS5} {
		t.Run(string(mode), func(t *testing.T) {
			f := transactionFixture(t)
			original, err := f.base.serialize(f.current)
			if err != nil {
				t.Fatal(err)
			}
			d := &darwinPlatformDriver{api: &f.api, serviceID: "fixture-service", originalConfig: original}
			if err := d.Apply(Endpoint{Mode: mode, Host: "::1", Port: 8888}); err != nil {
				t.Fatal(err)
			}
			if len(d.managedConfig) == 0 || f.commits != 1 || f.applies != 1 {
				t.Fatal("write not committed/applied")
			}
			if err := d.Restore(darwinProxySnapshot{ServiceID: d.serviceID, Config: original}); err != nil {
				t.Fatal(err)
			}
			if len(d.managedConfig) != 0 || f.commits != 2 || f.applies != 2 {
				t.Fatal("restore not committed/applied")
			}
		})
	}
}

// A removed network service ends ownership: Matches reports a mismatch so the
// controller clears its state, and restore never writes another service.
func TestDarwinProxyMissingServiceEndsOwnership(t *testing.T) {
	f := transactionFixture(t)
	original, err := f.base.serialize(f.current)
	if err != nil {
		t.Fatal(err)
	}
	d := &darwinPlatformDriver{api: &f.api, serviceID: "fixture-service", originalConfig: original}
	if err := d.Apply(Endpoint{Mode: ModeHTTP, Host: "127.0.0.1", Port: 8080}); err != nil {
		t.Fatal(err)
	}
	if matches, err := d.Matches(Endpoint{}); err != nil || !matches {
		t.Fatalf("Matches before removal = %v, %v", matches, err)
	}
	f.fail = "service"
	if matches, err := d.Matches(Endpoint{}); err != nil || matches {
		t.Fatalf("Matches after removal = %v, %v; want false, nil", matches, err)
	}
	commits := f.commits
	if err := d.Restore(darwinProxySnapshot{ServiceID: d.serviceID, Config: original}); !errors.Is(err, ErrChangedExternally) {
		t.Fatalf("Restore after removal = %v, want ErrChangedExternally", err)
	}
	if f.commits != commits {
		t.Fatal("restore wrote after the service disappeared")
	}
}
