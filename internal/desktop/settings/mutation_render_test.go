package settings

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/pkg/logger"
	"github.com/josexy/flowlens/locales"
)

func TestSaveKeepsGeneralControlsStable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		f := newFixture(t)
		setRenderTheme(t, f.data, dark)
		f.draft().CommonConfig.AppFontSize++
		f.settle()
		f.tester.Advance(time.Second)
		before := f.tester.Image()
		f.view.committing, f.data.Settings.Saving = true, true
		for range 3 {
			f.tester.Frame()
			f.tester.Advance(100 * time.Millisecond)
			rendertest.RequireSamePixels(t, before, f.tester.Image(), ui.Rect{X: 160, Y: 0, W: 1140, H: 1300}, "settings page")
			for _, control := range []struct {
				label string
				role  ui.Role
			}{
				{locales.SettingsThemeColorsPrimary(), ui.RolePopUpButton},
				{locales.SettingsThemeColorsNeutral(), ui.RolePopUpButton},
				{locales.SettingsAnimationsLabel(), ui.RoleSwitch},
				{locales.SettingsAppFontSizeLabel(), ui.RoleTextField},
				{locales.SettingsReset(), ui.RoleButton},
			} {
				info, ok := f.tester.AccessByRole(control.role, control.label)
				if !ok || !info.Disabled {
					t.Errorf("%q lost its pending-write guard: found=%v disabled=%v", control.label, ok, info.Disabled)
				}
			}
		}
		name := "settings-saving-light"
		if dark {
			name = "settings-saving-dark"
		}
		rendertest.Shoot(t, f.tester, name)
		f.view.committing, f.data.Settings.Saving = false, false
	}
}

func TestLogSwitchKeepsRequestedValueUntilSaveCompletes(t *testing.T) {
	for _, fail := range []bool{false, true} {
		f := newFixture(t)
		f.open(locales.SettingsTabLogs())
		release := make(chan struct{})
		finish := sync.OnceFunc(func() { close(release) })
		t.Cleanup(finish)
		f.view.Host.SetLog = func(enabled bool) (*logger.LogStatus, error) {
			<-release
			if fail {
				return nil, errors.New("write failed")
			}
			return &logger.LogStatus{Enabled: enabled, Level: logger.LogLevelInfo}, nil
		}
		label := locales.SettingsLogEnabledLabel()
		info, ok := f.tester.AccessByRole(ui.RoleSwitch, label)
		if !ok || !info.Checked {
			t.Fatalf("initial log switch: %+v", info)
		}
		f.tester.ClickAt(info.Bounds.X+10, info.Bounds.Y+info.Bounds.H/2)
		for range 3 {
			f.tester.Frame()
			f.tester.Advance(100 * time.Millisecond)
			info, ok = f.tester.AccessByRole(ui.RoleSwitch, label)
			if !ok || info.Checked || !info.Disabled || !f.view.logs.status.Enabled {
				t.Fatalf("pending switch reverted or changed the saved snapshot: %+v", info)
			}
		}
		finish()
		f.settle()
		info, _ = f.tester.AccessByRole(ui.RoleSwitch, label)
		if info.Disabled || info.Checked != fail {
			t.Fatalf("switch did not settle to the persisted value: %+v", info)
		}
		if fail && !f.has(locales.SettingsLogErrorToggle("write failed")) {
			t.Fatal("save error was hidden")
		}
	}
}
