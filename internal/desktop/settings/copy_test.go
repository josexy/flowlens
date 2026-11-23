package settings

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	shortcutservice "github.com/josexy/flowlens/internal/services/shortcut_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// declaredConstants returns the string values of the constants declared with
// typeName in the non-test files of the package in dir, so that a code added
// to a service without copy fails the test below.
func declaredConstants(t *testing.T, dir, typeName string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var values []string
	files := token.NewFileSet()
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			general, ok := decl.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value := spec.(*ast.ValueSpec)
				if ident, ok := value.Type.(*ast.Ident); !ok || ident.Name != typeName {
					continue
				}
				for _, expression := range value.Values {
					literal, ok := expression.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						t.Fatalf("%s constant in %s is not a string literal", typeName, path)
					}
					text, _ := strconv.Unquote(literal.Value)
					values = append(values, text)
				}
			}
		}
	}
	if len(values) == 0 {
		t.Fatalf("no %s constants in %s", typeName, dir)
	}
	return values
}

// catalogKeyPrefixes begin the catalog keys of the codes, which must never be
// shown.
var catalogKeyPrefixes = []string{"settingsRestartReason", "shortcutsApplyErrors", "shortcutsWarnings", "shortcutsRuntime", "settingsThemeColorsPalette", "settingsCaTrustErrors"}

func TestEveryServiceCodeHasCopyInEveryLanguage(t *testing.T) {
	const proxyDir, shortcutDir = "../../services/proxy_service", "../../services/shortcut_service"
	type mapped struct{ code, text string }
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			i18n.SetLocaleFunc(func() string { return language })
			t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
			var results []mapped
			for _, code := range declaredConstants(t, proxyDir, "ProxyRestartReason") {
				results = append(results, mapped{code, restartReasonText(proxyservice.ProxyRestartReason(code))})
			}
			for _, code := range declaredConstants(t, shortcutDir, "ShortcutApplyErrorCode") {
				text := shortcutApplyErrorText(shortcutservice.ShortcutApplyErrorCode(code))
				if text == locales.ShortcutsApplyErrorsUnknown() {
					t.Errorf("apply error %q shows the unknown error", code)
				}
				results = append(results, mapped{code, text})
			}
			for _, code := range declaredConstants(t, shortcutDir, "ShortcutWarning") {
				results = append(results, mapped{code, shortcutWarningText(shortcutservice.ShortcutWarning(code))})
			}
			for _, code := range declaredConstants(t, shortcutDir, "ShortcutRuntimeStatus") {
				results = append(results, mapped{code, shortcutRuntimeText(shortcutservice.ShortcutRuntimeStatus(code))})
			}
			for _, name := range append(nui.PrimaryPaletteNames(), nui.NeutralPaletteNames()...) {
				results = append(results, mapped{name, paletteText(name)})
			}
			for _, code := range caErrorCodes {
				text, ok := caErrorText(code)
				if !ok {
					t.Errorf("CA error %q has no copy", code)
				}
				results = append(results, mapped{code, text})
			}
			for _, result := range results {
				if result.text == "" || result.text == result.code {
					t.Errorf("%q has no copy: %q", result.code, result.text)
				}
				for _, prefix := range catalogKeyPrefixes {
					if strings.HasPrefix(result.text, prefix) {
						t.Errorf("%q shows the message key %q", result.code, result.text)
					}
				}
			}
		})
	}
}

// caErrorCodes are the certificate error codes that have copy. The services
// name them inside error text rather than as typed values.
var caErrorCodes = []string{
	"ca_builtin_unavailable", "ca_custom_generation_disabled", "ca_custom_paths_required",
	"ca_trust_certificate_changed", "ca_trust_invalid_ca", "ca_trust_invalid_pair", "ca_trust_invalid_validity",
	"ca_trust_login_keychain_unavailable", "ca_trust_timeout", "ca_trust_uninstall_first", "ca_trust_unsupported",
	"ca_trust_verification_failed",
}

func TestUnknownServiceCodesShowTheirValue(t *testing.T) {
	if got := restartReasonText("dns cache changed"); got != "dns cache changed" {
		t.Fatalf("restart reason = %q", got)
	}
	if got := shortcutWarningText("newWarning"); got != "newWarning" {
		t.Fatalf("warning = %q", got)
	}
	if got := shortcutRuntimeText("newStatus"); got != "newStatus" {
		t.Fatalf("runtime = %q", got)
	}
	if got := paletteText("black"); got != "black" {
		t.Fatalf("palette = %q", got)
	}
	for _, code := range []shortcutservice.ShortcutApplyErrorCode{"", "newCode"} {
		if got := shortcutApplyErrorText(code); got != locales.ShortcutsApplyErrorsUnknown() {
			t.Fatalf("apply error %q = %q", code, got)
		}
	}
	if got := caErrorMessage(errorText("install: ca_trust_new_code")); got != "install: ca_trust_new_code" {
		t.Fatalf("CA error = %q", got)
	}
	if got := caErrorMessage(errorText("install: ca_trust_timeout: context deadline exceeded")); got != locales.SettingsCaTrustErrorsCaTrustTimeout() {
		t.Fatalf("CA error = %q", got)
	}
}

type errorText string

func (e errorText) Error() string { return string(e) }

func TestRestartNoticeTranslatesReasonsInOrder(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			f := newFixture(t)
			i18n.SetLocaleFunc(func() string { return language })
			f.tester.SetLocale(language)
			f.proxy.result = proxyservice.ProxyConfigApplyResult{Applied: true, RestartRequired: true, RestartReasons: []proxyservice.ProxyRestartReason{
				proxyservice.ProxyRestartReasonProxyMode, proxyservice.ProxyRestartReasonListenAddress,
			}}
			f.open(locales.SettingsTabProxy())
			f.draft().ProxyConfig.Mode = settingservice.ProxyModeSOCKS5
			f.draft().ProxyConfig.Port++
			f.settle()
			f.click(locales.SettingsSave())
			f.settle()
			want := locales.SettingsSavedPartialRestartNotice(locales.SettingsRestartReasonProxyModeChanged() + ", " + locales.SettingsRestartReasonListenAddressChanged())
			if language == "zh-Hans" && !strings.Contains(want, "代理模式, 监听地址") {
				t.Fatalf("notice %q is not in %s", want, language)
			}
			if f.view.notice != want || !f.has(want) {
				t.Fatalf("notice = %q, want %q in %v", f.view.notice, want, f.tester.Texts())
			}
			for _, prefix := range catalogKeyPrefixes {
				if strings.Contains(f.view.notice, prefix) {
					t.Fatalf("notice shows a message key: %q", f.view.notice)
				}
			}
		})
	}
}

func TestRestartNoticeShowsUnrecognizedReasonAsReported(t *testing.T) {
	f := newFixture(t)
	f.proxy.result = proxyservice.ProxyConfigApplyResult{Applied: true, RestartRequired: true, RestartReasons: []proxyservice.ProxyRestartReason{
		proxyservice.ProxyRestartReasonProxyMode, "dns cache changed",
	}}
	f.open(locales.SettingsTabProxy())
	f.draft().ProxyConfig.Mode = settingservice.ProxyModeSOCKS5
	f.settle()
	f.click(locales.SettingsSave())
	f.settle()
	want := locales.SettingsSavedPartialRestartNotice(locales.SettingsRestartReasonProxyModeChanged() + ", dns cache changed")
	if f.view.notice != want {
		t.Fatalf("notice = %q, want %q", f.view.notice, want)
	}
}
