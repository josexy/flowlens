package settings

import (
	"testing"

	"github.com/josexy/flowlens/locales"
)

func (f *fixture) lastPicker() FilePicker {
	f.t.Helper()
	if len(f.pickers) == 0 {
		f.t.Fatal("no file dialog opened")
	}
	return f.pickers[len(f.pickers)-1]
}

func TestFilePickersCarryMessagesAndFilters(t *testing.T) {
	f := newFixture(t)
	// Tall enough for the whole Proxy page.
	f.tester.SetSize(1300, 4000)
	f.open(locales.SettingsTabProxy())
	proxy := f.draft().ProxyConfig
	proxy.UseBuiltinCA = false
	proxy.RootCAPaths = []string{"/ca/one.pem"}
	f.view.proxy.rootsOpen = true
	f.settle()
	f.click(locales.SettingsAddClientCert())
	certificates, keys := locales.SettingsCertificateFileFilter(), locales.SettingsPrivateKeyFileFilter()
	cases := []struct {
		click           string
		title, message  string
		filter          string
		patternsPresent bool
	}{
		{locales.SettingsSelectCaCertFile(), locales.SettingsSelectCaCertFile(), locales.SettingsSelectCaCertFileMessage(), certificates, true},
		{locales.SettingsSelectCaKeyFile(), locales.SettingsSelectCaKeyFile(), locales.SettingsSelectCaKeyFileMessage(), keys, true},
		{locales.SettingsSelectRootCaFile(), locales.SettingsSelectRootCaFile(), locales.SettingsSelectRootCaFileMessage(), certificates, true},
		{locales.SettingsSelectClientCertFile(), locales.SettingsSelectClientCertFile(), locales.SettingsSelectClientCertFileMessage(), certificates, true},
		{locales.SettingsSelectClientKeyFile(), locales.SettingsSelectClientKeyFile(), locales.SettingsSelectClientKeyFileMessage(), keys, true},
	}
	for _, test := range cases {
		f.click(test.click)
		got := f.lastPicker()
		if got.Title != test.title || got.Message != test.message || got.FilterName != test.filter || (len(got.Patterns) > 0) != test.patternsPresent {
			t.Fatalf("%s: %+v", test.click, got)
		}
	}
	f.view.pickRoots(proxy)
	f.settle()
	if got := f.lastPicker(); got.Title != locales.SettingsSelectRootCaFiles() || got.Message != locales.SettingsSelectRootCaFilesMessage() || got.FilterName != certificates {
		t.Fatalf("root CA files: %+v", got)
	}
	f.open(locales.SettingsTabPythonPlugins())
	f.click(locales.SettingsPythonInterpreterBrowse())
	if got := f.lastPicker(); got.Title != locales.SettingsPythonInterpreterPickerTitle() || got.Message != locales.SettingsPythonInterpreterPickerMessage() || len(got.Patterns) != 0 {
		t.Fatalf("interpreter: %+v", got)
	}
}
