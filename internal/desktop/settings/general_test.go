package settings

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/keytest"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/locales"
)

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
