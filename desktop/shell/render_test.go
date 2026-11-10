package shell

import (
	"testing"

	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/theme"
)

func setRenderTheme(t *testing.T, data *store.Store, dark bool) {
	t.Helper()
	mode := theme.ModeLight
	if dark {
		mode = theme.ModeDark
	}
	if err := data.Settings.SetThemeMode(mode); err != nil {
		t.Fatal(err)
	}
}

func TestRenderShell(t *testing.T) {
	f := newFixture(t)
	f.tester.SetSize(rendertest.Width, rendertest.Height)
	rendertest.ShootThemes(t, f.tester, "shell", func(dark bool) { setRenderTheme(t, f.data, dark); f.tester.Frame() })
}
