package app

import (
	"testing"

	appservice "github.com/josexy/flowlens/backend/services/app_service"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestMainWindowWaitsForFrontendInEverySavedState(t *testing.T) {
	for _, test := range []struct {
		name   string
		config *settingservice.WindowConfig
	}{
		{name: "first launch"},
		{name: "normal", config: &settingservice.WindowConfig{Width: 1200, Height: 800, HasPosition: true, PositionX: 100, PositionY: 80}},
		{name: "maximized", config: &settingservice.WindowConfig{Width: 1200, Height: 800, IsMaximized: true}},
		{name: "fullscreen", config: &settingservice.WindowConfig{Width: 1200, Height: 800, IsFullScreen: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := application.WebviewWindowOptions{
				Width: appservice.DEFAULT_WINDOW_WIDTH, Height: appservice.DEFAULT_WINDOW_HEIGHT,
				StartState: application.WindowStateMaximised,
			}
			applySavedWindowOptions(&options, test.config, nil)
			if !options.Hidden || options.StartState != application.WindowStateNormal {
				t.Fatalf("window can appear before the frontend is ready: %+v", options)
			}
			if test.config == nil {
				if options.Width != appservice.DEFAULT_WINDOW_WIDTH || options.Height != appservice.DEFAULT_WINDOW_HEIGHT {
					t.Fatal("first-launch dimensions changed")
				}
			} else if options.Width != 1200 || options.Height != 800 || options.X != test.config.PositionX || options.Y != test.config.PositionY {
				t.Fatalf("saved normal bounds changed: %+v", options)
			}
		})
	}
}

func TestSecondaryWindowsWaitForFrontend(t *testing.T) {
	for _, custom := range []bool{false, true} {
		for _, mac := range []bool{false, true} {
			if !settingsWindowOptions(custom, mac, nil).Hidden || !updaterWindowOptions(custom, mac, nil).Hidden {
				t.Fatalf("secondary window appears before theme initialization: custom=%v mac=%v", custom, mac)
			}
		}
	}
}
