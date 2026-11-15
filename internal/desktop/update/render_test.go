package update

import (
	"testing"

	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/theme"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
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

func TestRenderUpdate(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseIdle, appservice.UpdateApplyModeSelf)
	rendertest.ShootThemes(t, f.tester, "update", func(dark bool) { setRenderTheme(t, f.data, dark); f.settle() })
}
