package app

import (
	"strings"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// matchUpdaterAsset prefers the updater-specific artifacts when a release also
// contains installers or legacy packages that would otherwise match Wails'
// filename heuristic. Other platform/architecture combinations retain Wails'
// default matching behavior.
func matchUpdaterAsset(req updater.CheckRequest, assets []github.ReleaseAsset) int {
	suffix := ""
	switch req.Platform {
	case "darwin":
		suffix = "_darwin_universal.zip"
	case "windows":
		if req.Arch == "amd64" {
			suffix = "_windows_amd64.exe"
		}
	case "linux":
		if req.Arch == "amd64" {
			suffix = "_linux_x64.appimage"
		}
	}
	if suffix != "" {
		for index, asset := range assets {
			if strings.HasSuffix(strings.ToLower(asset.Name), suffix) {
				return index
			}
		}
		return -1
	}
	return github.DefaultAssetMatcher(req, assets)
}
