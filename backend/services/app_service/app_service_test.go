package appservice

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestBuildEnvironmentInfoIncludesApplicationVersion(t *testing.T) {
	info := buildEnvironmentInfo(nil, application.EnvironmentInfo{})
	want := "v" + APP_VERSION
	if info.AppVersion != want {
		t.Fatalf("AppVersion = %q, want %q", info.AppVersion, want)
	}
}
