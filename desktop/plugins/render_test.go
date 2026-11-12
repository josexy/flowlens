package plugins

import (
	"testing"
	"time"

	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/pythonapi"
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

func TestRenderPlugins(t *testing.T) {
	f := newFixture(t)
	f.create("Header signer", "Signs requests")
	rendertest.ShootThemes(t, f.tester, "plugins", func(dark bool) { setRenderTheme(t, f.data, dark); f.settle() })
}

func TestRenderPythonHelp(t *testing.T) {
	f := newFixture(t)
	f.data.Workbench.OpenPythonHelp(pythonapi.ScopeInline)
	f.tester.Advance(time.Second)
	f.settle()
	rendertest.ShootThemes(t, f.tester, "python-help", func(dark bool) {
		setRenderTheme(t, f.data, dark)
		f.tester.Advance(time.Second)
		f.settle()
	})
}
