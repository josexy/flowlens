package inspect

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/locales"
)

func tlsEntryFixture(t *testing.T) *fixture {
	t.Helper()
	entry := renderEntry()
	entry.Metadata.Certificate = &proxyservice.ServerCertificate{Version: 3, Sha1Fingerprint: "AA:BB", Sha256Fingerprint: "CC:DD", DNSNames: []string{"a", "b"}}
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{})
	f.panel.overview.tls, f.panel.overview.cert = true, true
	f.settle()
	return f
}

func TestTLSLabelsAreLocalized(t *testing.T) {
	f := tlsEntryFixture(t)
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	f.tester.SetLocale("zh-Hans")
	f.settle()
	for _, english := range []string{"ALPN", "CIPHER SUITE", "SUPPORTED", "SHA-1", "SHA-256", "SHA1 FINGERPRINT", "SHA256 FINGERPRINT"} {
		for _, text := range f.tester.Texts() {
			if text == english || strings.HasPrefix(text, "SUPPORTED") || strings.HasPrefix(text, "CIPHER") {
				t.Fatalf("English label %q under zh-Hans in %v", text, f.tester.Texts())
			}
		}
	}
	for _, label := range []string{locales.DetailAlpn(), locales.DetailCipherSuite(), locales.DetailSupportedTlsVersions(), locales.DetailSupportedAlpns(), locales.DetailSupportedCipherSuites(), locales.DetailCertSha1Fingerprint(), locales.DetailCertSha256Fingerprint()} {
		if !strings.ContainsAny(label, "协议加密支持证书") {
			t.Fatalf("label %q is not Chinese", label)
		}
		f.has(strings.ToUpper(label))
	}
}

func TestSupportedCipherSuitesOnePerLine(t *testing.T) {
	f := tlsEntryFixture(t)
	suites := []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384", "TLS_CHACHA20_POLY1305_SHA256"}
	var ys []float32
	for _, suite := range suites {
		rect, ok := f.tester.Find(suite)
		if !ok {
			t.Fatalf("no %s in %v", suite, f.tester.Texts())
		}
		ys = append(ys, rect.Y)
	}
	if !(ys[0] < ys[1] && ys[1] < ys[2]) {
		t.Fatalf("suites at %v, want three lines", ys)
	}
	// The first suite is also the selected one, shown in its own row above.
	for _, text := range f.tester.Texts() {
		if strings.Contains(text, ", TLS_") {
			t.Fatalf("suites joined in %q", text)
		}
	}
	f.has("a, b")
}
