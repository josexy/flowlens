package settings

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/keytest"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/locales"
)

func TestAnimationRoundTripClearsUnsavedChanges(t *testing.T) {
	for _, mode := range []string{theme.ModeLight, theme.ModeDark} {
		for _, initial := range []string{"on", "off"} {
			t.Run(mode+"/"+initial, func(t *testing.T) {
				f := newFixture(t)
				if initial == "off" {
					f.clickControl(locales.SettingsAnimationsLabel())
					f.click(locales.SettingsSave())
				}
				setRenderTheme(t, f.data, mode == theme.ModeDark)
				f.view.Begin()
				f.settle()
				saved, err := f.service.Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				baseline := f.draft().Clone()
				assertDirty := func(want bool) {
					t.Helper()
					button, ok := f.tester.AccessByRole(ui.RoleButton, locales.SettingsSave())
					if !ok || button.Disabled == want || f.view.Dirty() != want {
						t.Fatalf("want dirty=%v, view=%v, save=%+v", want, f.view.Dirty(), button)
					}
				}
				assertDirty(false)
				f.clickControl(locales.SettingsAnimationsLabel())
				assertDirty(true)
				keytest.TabTo(t, f.tester, locales.SettingsAnimationsLabel())
				f.tester.Key(0, ui.KeySpace)
				f.settle()
				assertDirty(false)
				if !reflect.DeepEqual(f.draft(), baseline) || f.data.Settings.Theme().Config.DisableAnimations != baseline.CommonConfig.DisableAnimations {
					t.Fatal("round trip did not restore the draft and appearance")
				}
				if stored, err := f.service.Snapshot(); err != nil || !reflect.DeepEqual(stored, saved) {
					t.Fatalf("preview changed saved settings: %v", err)
				}
				if f.dispatch.Invoke(shortcuts.AppSave) {
					t.Fatal("save shortcut remained active without changes")
				}
				if !f.view.CloseRequested() || f.confirm.Open() {
					t.Fatal("reverted settings should close without a discard prompt")
				}
			})
		}
	}
}

func TestRevertingAnimationKeepsOtherUnsavedChanges(t *testing.T) {
	f := newFixture(t)
	common, proxy := f.draft().CommonConfig, f.draft().ProxyConfig
	fontSize, port := common.AppFontSize, proxy.Port
	common.AppFontSize++
	proxy.Port++
	f.clickControl(locales.SettingsAnimationsLabel())
	f.clickControl(locales.SettingsAnimationsLabel())
	if got := f.data.Settings.DirtySections(); !slices.Equal(got, []store.SettingsSection{store.SectionCommon, store.SectionProxy}) {
		t.Fatalf("other edits were lost: %v", got)
	}
	common.AppFontSize = fontSize
	f.settle()
	if got := f.data.Settings.DirtySections(); !slices.Equal(got, []store.SettingsSection{store.SectionProxy}) {
		t.Fatalf("reverted common section stayed dirty: %v", got)
	}
	proxy.Port = port
	f.settle()
	if f.view.Dirty() {
		t.Fatal("reverting the final change left unsaved changes")
	}
}

func TestAnimationsPreviewSaveDiscardAndReset(t *testing.T) {
	f := newFixture(t)
	label := locales.SettingsAnimationsLabel()
	if control, ok := f.tester.AccessByRole(ui.RoleSwitch, label); !ok || !control.Checked {
		t.Fatalf("animations must start enabled: %+v", control)
	}
	f.clickControl(label)
	if !f.draft().CommonConfig.DisableAnimations || !f.data.Settings.Theme().Config.DisableAnimations || !f.view.Dirty() {
		t.Fatal("turning animations off did not update the draft and preview")
	}
	f.click(locales.SettingsSave())
	stored, err := f.service.Snapshot()
	if err != nil || !stored.CommonConfig.DisableAnimations {
		t.Fatalf("animation preference was not saved: %v", err)
	}
	f.view.finish()
	f.view.Begin()
	f.settle()
	if control, ok := f.tester.AccessByRole(ui.RoleSwitch, label); !ok || control.Checked {
		t.Fatalf("saved switch state did not survive reopening: %+v", control)
	}
	keytest.TabTo(t, f.tester, label)
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if f.draft().CommonConfig.DisableAnimations || f.data.Settings.Theme().Config.DisableAnimations {
		t.Fatal("keyboard toggle did not re-enable animations")
	}
	f.view.finish()
	if !f.data.Settings.Theme().Config.DisableAnimations {
		t.Fatal("discard did not restore the saved animation preference")
	}
	f.view.Begin()
	f.view.reset()
	f.settle()
	if f.draft().CommonConfig.DisableAnimations || f.data.Settings.Theme().Config.DisableAnimations {
		t.Fatal("restoring defaults did not enable animations")
	}
	f.view.finish()
	if !f.data.Settings.Theme().Config.DisableAnimations {
		t.Fatal("discarding restored defaults lost the saved animation preference")
	}
}

func TestFontSelectionPreviewsPersistsAndReturnsToDefault(t *testing.T) {
	f := newFixture(t)
	f.clickControl(locales.SettingsAppFontLabel())
	f.click("Inter")
	f.settle()
	if got := f.draft().CommonConfig.AppFontFamily; got != "Inter" {
		t.Fatalf("font selection was lost on rebuild: %q", got)
	}
	if got := f.data.Settings.Theme().AppFont; !strings.HasPrefix(got, "Inter,") {
		t.Fatalf("font was not previewed: %q", got)
	}
	f.clickControl(locales.SettingsCodeFontLabel())
	f.click("Fira Code")
	if got := f.draft().CommonConfig.CodeFontFamily; got != "Fira Code" {
		t.Fatalf("code font selection was lost: %q", got)
	}
	f.click(locales.SettingsSave())
	stored, err := f.service.Snapshot()
	if err != nil || stored.CommonConfig.AppFontFamily != "Inter" || stored.CommonConfig.CodeFontFamily != "Fira Code" {
		t.Fatalf("fonts were not saved: %v %+v", err, stored.CommonConfig)
	}
	f.view.finish()
	f.view.Begin()
	f.settle()
	if f.draft().CommonConfig.AppFontFamily != "Inter" || f.draft().CommonConfig.CodeFontFamily != "Fira Code" {
		t.Fatal("fonts did not survive reopening settings")
	}
	f.clickControl(locales.SettingsAppFontLabel())
	f.click(locales.SettingsFontDefault())
	if f.draft().CommonConfig.AppFontFamily != "" || f.data.Settings.Theme().AppFont != theme.DefaultFontFamily {
		t.Fatal("default font selection did not preview")
	}
	f.view.finish()
	if !strings.HasPrefix(f.data.Settings.Theme().AppFont, "Inter,") {
		t.Fatal("discard did not restore saved font")
	}
}

func TestFontSizeResetRestoresPreview(t *testing.T) {
	f := newFixture(t)
	f.draft().CommonConfig.AppFontSize = 24
	f.settle()
	if f.data.Settings.Theme().AppFontSize != 24 {
		t.Fatal("size did not preview")
	}
	f.click(locales.SettingsFontSizeReset() + " " + locales.SettingsAppFontSizeLabel())
	if f.data.Settings.Theme().AppFontSize != theme.DefaultAppFontSize {
		t.Fatal("returning to the original size left the old preview active")
	}
}

func TestGeneralSettingsOfferNoCursorOption(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabGeneral())
	if !f.has(locales.SettingsAppFontLabel()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	for _, text := range f.tester.Texts() {
		lower := strings.ToLower(text)
		if strings.Contains(lower, "pointer cursor") || strings.Contains(text, "指针光标") {
			t.Fatalf("General settings show %q", text)
		}
	}
}
