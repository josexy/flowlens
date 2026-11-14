package inspect

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	desktoptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
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

func TestTLSListsKeepNaturalHeight(t *testing.T) {
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	if err := desktoptheme.RegisterFonts(); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		for _, size := range []int{16, 24} {
			t.Run(fmt.Sprintf("dark=%v/size=%v", dark, size), func(t *testing.T) {
				mode := desktoptheme.ModeLight
				if dark {
					mode = desktoptheme.ModeDark
				}
				entry := renderEntry()
				// Find must identify the supported values, not a duplicate in
				// the negotiated-version/ALPN rows above them.
				entry.Metadata.TLS.SelectedVersion, entry.Metadata.TLS.SelectedALPN = "", ""
				entry.Metadata.TLS.SelectedCipherSuite = "TLS_AES_256_GCM_SHA384"
				entry.Metadata.TLS.SupportedVersion = []string{"GREASE (0x2A2A)", "TLS1.3", "TLS1.2"}
				entry.Metadata.TLS.SupportedCipherSuites = []string{
					"GREASE (0x6A6A)", "TLS_AES_128_GCM_SHA256", "TLS_CHACHA20_POLY1305_SHA256",
					"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256", "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
					"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256", "TLS_RSA_WITH_AES_256_CBC_SHA",
				}
				entry.Metadata.Certificate = &proxyservice.ServerCertificate{Version: 3}
				panel := &Panel{overview: overviewState{tls: true, cert: true}}
				var lineHeight float32
				tester := ui.NewTester(func(c *ui.Context) {
					palette := nui.App(c, desktoptheme.Resolve(desktoptheme.Appearance{Mode: mode, AppFontSize: size}).Config)
					lineHeight = desktoptheme.TextSize(c, 14) * 1.45
					ui.Scroll(c).Fill().Children(func() {
						ui.Column(c).FillWidth().Padding(10).Children(func() {
							panel.tlsSection(c, palette, entry)
							panel.certificateSection(c, palette, entry)
						})
					})
				}, 525, 1100)
				tester.SetDark(dark)
				tester.SetLocale("zh-Hans")
				for range 4 {
					tester.Frame()
				}
				values := append([]string{}, entry.Metadata.TLS.SupportedVersion...)
				values = append(values, entry.Metadata.TLS.SupportedALPN...)
				values = append(values, entry.Metadata.TLS.SupportedCipherSuites...)
				var bottom float32
				for _, value := range values {
					r, ok := tester.Find(value)
					if !ok || r.H < lineHeight-1 || r.Y < bottom {
						t.Fatalf("%q at %+v overlaps or has no line height; previous bottom %v", value, r, bottom)
					}
					bottom = r.Y + r.H
				}
				cert, ok := tester.Find(locales.DetailCertInfo())
				if !ok || cert.Y < bottom {
					t.Fatalf("certificate at %+v overlaps TLS ending at %v", cert, bottom)
				}
				rendertest.Shoot(t, tester, fmt.Sprintf("tls-lists-dark-%v-size-%v", dark, size))
			})
		}
	}
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
