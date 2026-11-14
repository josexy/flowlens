package plugins

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
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

func TestRenderPluginDrag(t *testing.T) {
	if !rendertest.Enabled() {
		return
	}
	f := newFixture(t)
	f.create("Header signer", "Signs requests")
	f.create("Response formatter", "Formats responses")
	f.create("Request logger", "")
	rendertest.ShootThemes(t, f.tester, "plugins-drag", func(dark bool) {
		f.tester.CancelPress()
		setRenderTheme(t, f.data, dark)
		f.settle()
		x, y := f.center("drag Header signer")
		f.tester.Press(x, y)
		f.tester.Move(x+32, y+pluginRowHeight*1.3)
		f.tester.Frame()
	})
	f.tester.Key(0, ui.KeyEscape)
	f.tester.CancelPress()
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
