package settings

import (
	"strings"
	"testing"

	"github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/locales"
)

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
