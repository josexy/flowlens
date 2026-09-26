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
	case "linux":
		// Wails beta.20 replaces os.Executable(), which is inside the read-only
		// mount for AppImages. The release also has no raw Linux binary suitable
		// for replacing an unpackaged executable. Keep both on manual updates.
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
