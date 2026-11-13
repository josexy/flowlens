package settings

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/keytest"
	"github.com/josexy/flowlens/locales"
)

func TestNavigationUsesKeyboardAndKeepsDraft(t *testing.T) {
	f := newFixture(t)
	f.draft().CommonConfig.LogDisabled = true
	f.settle()
	keytest.TabTo(t, f.tester, locales.SettingsTabProxy())
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if f.view.tab != tabProxy || !f.draft().CommonConfig.LogDisabled || !f.view.Dirty() {
		t.Fatal("keyboard navigation lost the settings draft")
	}
	f.tester.Key(ui.Shift, ui.KeyTab)
	if !f.tester.Focused(locales.SettingsTabGeneral()) {
		t.Fatal("Shift+Tab did not return to General")
	}
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if f.view.tab != tabGeneral || !f.draft().CommonConfig.LogDisabled {
		t.Fatal("Space did not return to General with its draft")
	}
}
