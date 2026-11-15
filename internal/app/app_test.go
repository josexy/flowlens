package app

import (
	"testing"

	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
)

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
