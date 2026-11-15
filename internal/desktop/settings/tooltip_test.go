package settings

import (
	"testing"

	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/locales"
)

func TestShortcutRowActionsShowTooltips(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabShortcuts())
	label := locales.ShortcutsCommandsAppSave()
	f.view.shortcuts.search = "save"
	f.settle()
	rendertest.RequireTooltip(t, f.tester, locales.ShortcutsClearFor(label), locales.ShortcutsClearFor(label))
	f.click(locales.ShortcutsClearFor(label))
	rendertest.RequireTooltip(t, f.tester, locales.ShortcutsResetFor(label), locales.ShortcutsResetFor(label))
}
