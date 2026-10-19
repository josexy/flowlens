package appservice

import (
	pathpkg "path"
	"strings"
)

func canSelfUpdatePath(goos, executable, programFiles, programFilesX86 string) bool {
	if strings.TrimSpace(executable) == "" {
		return false
	}

	switch goos {
	case "windows":
		return !pathWithinAny(goos, executable, programFiles, programFilesX86)
	case "darwin":
		// Applications launched directly from a mounted DMG live below /Volumes
		// and cannot replace their own read-only bundle. Ask the user to install
		// the release manually instead.
		return !pathWithinAny(goos, executable, "/Volumes")
	case "linux":
		// Keep Linux on manual updates until AppImage target eligibility and
		// replacement recovery are validated. Other Linux installations have no
		// raw binary release artifact suitable for replacing their executable.
		return false
	default:
		return true
	}
}

func pathWithinAny(goos, target string, roots ...string) bool {
	if goos != "windows" {
		cleanTarget := pathpkg.Clean(target)
		for _, root := range roots {
			if strings.TrimSpace(root) == "" {
				continue
			}
			cleanRoot := pathpkg.Clean(root)
			if cleanTarget == cleanRoot || strings.HasPrefix(cleanTarget, cleanRoot+"/") {
				return true
			}
		}
		return false
	}

	// Normalize Windows paths independently of the host OS, including slash
	// variants and dot segments, before performing a case-insensitive comparison.
	normalize := func(value string) string {
		return pathpkg.Clean(strings.ToLower(strings.ReplaceAll(value, `\`, "/")))
	}
	cleanTarget := normalize(target)
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		cleanRoot := normalize(root)
		if cleanTarget == cleanRoot || strings.HasPrefix(cleanTarget, strings.TrimRight(cleanRoot, "/")+"/") {
			return true
		}
	}
	return false
}
