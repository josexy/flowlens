//go:build !windows

package pythonpluginservice

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
)

func platformInterpreterPaths(_ context.Context) []string {
	patterns := []string{
		"/usr/bin/python3*",
		"/usr/local/bin/python3*",
		"/opt/python/*/bin/python3*",
	}
	if runtime.GOOS == "darwin" {
		patterns = append(patterns,
			"/opt/homebrew/bin/python3*",
			"/usr/local/opt/python@*/bin/python3*",
			"/Library/Frameworks/Python.framework/Versions/*/bin/python3*",
		)
	}
	if home, err := os.UserHomeDir(); err == nil {
		patterns = append(patterns,
			filepath.Join(home, ".pyenv", "versions", "*", "bin", "python*"),
			filepath.Join(home, ".local", "share", "uv", "python", "*", "bin", "python*"),
			filepath.Join(home, ".local", "share", "mise", "installs", "python", "*", "bin", "python*"),
			filepath.Join(home, "miniconda3", "bin", "python*"),
			filepath.Join(home, "miniconda3", "envs", "*", "bin", "python*"),
			filepath.Join(home, "anaconda3", "bin", "python*"),
			filepath.Join(home, "anaconda3", "envs", "*", "bin", "python*"),
			filepath.Join(home, "miniforge3", "bin", "python*"),
			filepath.Join(home, "miniforge3", "envs", "*", "bin", "python*"),
			filepath.Join(home, ".conda", "envs", "*", "bin", "python*"),
		)
	}

	paths := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			if isLikelyInterpreterExecutable(match) {
				paths = append(paths, match)
			}
		}
	}
	return paths
}

func shouldSkipInterpreterDiscoveryPath(string) bool {
	return false
}

func isLikelyInterpreterExecutable(path string) bool {
	name := filepath.Base(path)
	if name == "python" || name == "python3" {
		return true
	}
	const prefix = "python3."
	if len(name) <= len(prefix) || name[:len(prefix)] != prefix {
		return false
	}
	for _, character := range name[len(prefix):] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
