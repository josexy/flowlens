package app

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

func TestMatchUpdaterAssetUsesUniversalMacOSArchive(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "flowlens_v1.0.2_darwin_arm64.dmg"},
		{Name: "flowlens_v1.0.2_darwin_universal.zip"},
		{Name: "flowlens_v1.0.2_macos_universal.dmg"},
	}

	for _, arch := range []string{"amd64", "arm64"} {
		if got := matchUpdaterAsset(updater.CheckRequest{Platform: "darwin", Arch: arch}, assets); got != 1 {
			t.Fatalf("arch %s: expected universal archive at index 1, got %d", arch, got)
		}
	}
	if got := matchUpdaterAsset(updater.CheckRequest{Platform: "darwin", Arch: "amd64"}, assets[:1]); got != -1 {
		t.Fatalf("missing universal archive: expected no updater asset, got %d", got)
	}
}

func TestMatchUpdaterAssetPrefersUpdaterArtifactsForWindowsAndLinux(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "flowlens_v1.0.2_windows_x64_setup.exe"},
		{Name: "flowlens_v1.0.2_windows_amd64.exe"},
		{Name: "flowlens_v1.0.2_linux_x64.AppImage"},
	}

	if got := matchUpdaterAsset(updater.CheckRequest{Platform: "windows", Arch: "amd64"}, assets); got != 1 {
		t.Fatalf("windows: expected updater executable at index 1, got %d", got)
	}
	if got := matchUpdaterAsset(updater.CheckRequest{Platform: "linux", Arch: "amd64"}, assets); got != 2 {
		t.Fatalf("linux: expected existing AppImage at index 2, got %d", got)
	}
}
