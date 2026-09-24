package appservice

import (
	pathpkg "path"
	"path/filepath"
	"strings"
)

func canSelfUpdatePath(goos, executable, appImage, programFiles, programFilesX86 string) bool {
	if strings.TrimSpace(executable) == "" {
		return false
	}

	switch goos {
	case "windows":
		return !pathWithinAny(goos, executable, programFiles, programFilesX86)
	case "linux":
		if strings.TrimSpace(appImage) != "" || strings.EqualFold(filepath.Ext(executable), ".appimage") {
			return true
		}
		return !pathWithinAny(goos, executable, "/usr", "/opt", "/bin", "/sbin", "/app", "/snap", "/var/lib/flatpak", "/nix/store")
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

	cleanTarget := filepath.Clean(target)
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		cleanRoot := filepath.Clean(root)
		targetKey := strings.ToLower(cleanTarget)
		rootKey := strings.TrimRight(strings.ToLower(cleanRoot), `\`)
		if strings.EqualFold(targetKey, strings.ToLower(cleanRoot)) || (rootKey != "" && strings.HasPrefix(targetKey, rootKey+`\`)) {
			return true
		}
		relative, err := filepath.Rel(cleanRoot, cleanTarget)
		if err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))) {
			return true
		}
	}
	return false
}
