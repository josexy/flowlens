package app

import (
	"slices"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/settings"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
)

func TestMainWindowPlacementFirstLaunchUsesDefaults(t *testing.T) {
	placement := mainWindowPlacement(nil, nil)
	if placement.Width != appservice.DEFAULT_WINDOW_WIDTH || placement.Height != appservice.DEFAULT_WINDOW_HEIGHT || placement.Positioned {
		t.Fatalf("placement = %+v", placement)
	}
}

func TestMainWindowPlacementRestoresSavedBounds(t *testing.T) {
	config := &settingservice.WindowConfig{Width: 1200, Height: 800, HasPosition: true, PositionX: 100, PositionY: 80}
	placement := mainWindowPlacement(config, []windowRect{{Width: 1920, Height: 1080}})
	want := windowPlacement{Width: 1200, Height: 800, X: 100, Y: 80, Positioned: true}
	if placement != want {
		t.Fatalf("placement = %+v, want %+v", placement, want)
	}
}

func TestMainWindowPlacementCentersOffScreenWindows(t *testing.T) {
	config := &settingservice.WindowConfig{Width: 1200, Height: 800, HasPosition: true, PositionX: 5000, PositionY: 80}
	placement := mainWindowPlacement(config, []windowRect{{Width: 1920, Height: 1080}})
	if placement.Positioned {
		t.Fatalf("off-screen window kept its position: %+v", placement)
	}
	if placement.Width != 1200 || placement.Height != 800 {
		t.Fatalf("size changed: %+v", placement)
	}
}

func TestMainWindowPlacementClampsToMinimumSize(t *testing.T) {
	placement := mainWindowPlacement(&settingservice.WindowConfig{Width: 10, Height: 10}, nil)
	if placement.Width != appservice.MIN_WINDOW_WIDTH || placement.Height != appservice.MIN_WINDOW_HEIGHT {
		t.Fatalf("placement = %+v", placement)
	}
}

func TestWindowRectIntersects(t *testing.T) {
	screen := windowRect{X: 0, Y: 0, Width: 100, Height: 100}
	for _, test := range []struct {
		name string
		rect windowRect
		want bool
	}{
		{"inside", windowRect{10, 10, 20, 20}, true},
		{"overlapping edge", windowRect{90, 90, 20, 20}, true},
		{"touching only", windowRect{100, 0, 20, 20}, false},
		{"outside", windowRect{200, 200, 20, 20}, false},
		{"empty", windowRect{10, 10, 0, 0}, false},
	} {
		if got := test.rect.intersects(screen); got != test.want {
			t.Errorf("%s: intersects = %v, want %v", test.name, got, test.want)
		}
	}
}

type fakeGeometryWindow struct {
	maximized, fullScreen bool
	width, height, x, y   int
	setPosition           bool
}

func (w *fakeGeometryWindow) IsMaximized() bool     { return w.maximized }
func (w *fakeGeometryWindow) IsFullScreen() bool    { return w.fullScreen }
func (w *fakeGeometryWindow) Size() (int, int)      { return w.width, w.height }
func (w *fakeGeometryWindow) Position() (int, int)  { return w.x, w.y }
func (w *fakeGeometryWindow) Maximize()             { w.maximized = true }
func (w *fakeGeometryWindow) SetFullScreen(on bool) { w.fullScreen = on }
func (w *fakeGeometryWindow) SetPosition(x, y int)  { w.x, w.y, w.setPosition = x, y, true }

func TestRestoreSavedWindowStatePrefersFullScreenThenMaximized(t *testing.T) {
	window := &fakeGeometryWindow{}
	restoreSavedWindowState(&settingservice.WindowConfig{IsFullScreen: true, IsMaximized: true}, window, nil)
	if !window.fullScreen || window.maximized {
		t.Fatalf("full screen: %+v", window)
	}
	window = &fakeGeometryWindow{}
	restoreSavedWindowState(&settingservice.WindowConfig{IsMaximized: true}, window, nil)
	if !window.maximized || window.fullScreen {
		t.Fatalf("maximized: %+v", window)
	}
}

func TestRestoreSavedWindowStateSkipsOffScreenPosition(t *testing.T) {
	window := &fakeGeometryWindow{}
	config := &settingservice.WindowConfig{Width: 1200, Height: 800, HasPosition: true, PositionX: 5000}
	restoreSavedWindowState(config, window, []windowRect{{Width: 1920, Height: 1080}})
	if window.setPosition {
		t.Fatalf("off-screen position applied: %+v", window)
	}
}

func TestNormalizeMainWindowCloseBehavior(t *testing.T) {
	tests := []struct {
		name     string
		behavior settingservice.MainWindowCloseBehavior
		want     settingservice.MainWindowCloseBehavior
	}{
		{
			name:     "hide to tray",
			behavior: settingservice.MainWindowCloseBehaviorHideToTray,
			want:     settingservice.MainWindowCloseBehaviorHideToTray,
		},
		{
			name:     "quit",
			behavior: settingservice.MainWindowCloseBehaviorQuit,
			want:     settingservice.MainWindowCloseBehaviorQuit,
		},
		{
			name:     "unknown falls back to hide to tray",
			behavior: settingservice.MainWindowCloseBehavior("unknown"),
			want:     settingservice.MainWindowCloseBehaviorHideToTray,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMainWindowCloseBehavior(tt.behavior); got != tt.want {
				t.Fatalf("normalizeMainWindowCloseBehavior(%q) = %q, want %q", tt.behavior, got, tt.want)
			}
		})
	}
}

func TestOpenDialogOptionsCarryMessageAndFilterName(t *testing.T) {
	options := openDialogOptions(settings.FilePicker{Title: "Key", Message: "Pick the key", FilterName: locales.SettingsPrivateKeyFileFilter(), Patterns: []string{"key", "pem"}}, false)
	if options.Title != "Key" || options.Message != "Pick the key" || options.Multiple {
		t.Fatalf("%+v", options)
	}
	if len(options.Filters) != 2 || options.Filters[0].Name != locales.SettingsPrivateKeyFileFilter() || !slices.Equal(options.Filters[0].Extensions, []string{"key", "pem"}) || options.Filters[1].Name != locales.SettingsAllFilesFilter() {
		t.Fatalf("%+v", options.Filters)
	}
	if plain := openDialogOptions(settings.FilePicker{Title: "Python"}, true); plain.Filters != nil || !plain.Multiple {
		t.Fatalf("%+v", plain)
	}
}
