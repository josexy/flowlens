package rewrite

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

func TestRenderRewrite(t *testing.T) {
	f := newFixture(t)
	rendertest.ShootThemes(t, f.tester, "rewrite", func(dark bool) { setRenderTheme(t, f.data, dark); f.settle() })
}
