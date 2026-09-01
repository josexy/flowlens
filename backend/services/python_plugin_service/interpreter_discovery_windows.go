//go:build windows

package pythonpluginservice

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const pythonRegistryRoot = `SOFTWARE\Python`

func platformInterpreterPaths(ctx context.Context) []string {
	paths := make([]string, 0, 16)
	if launcher, err := exec.LookPath("py.exe"); err == nil && !shouldSkipInterpreterDiscoveryPath(launcher) {
		paths = append(paths, launcher)
		if output, _, err := runInterpreterDiscoveryCommand(ctx, 2*time.Second, launcher, "-0p"); err == nil {
			paths = append(paths, parsePythonLauncherPaths(output)...)
		}
	}
	paths = append(paths, pythonRegistryPaths()...)

	patterns := make([]string, 0, 12)
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		patterns = append(patterns,
			filepath.Join(localAppData, "Programs", "Python", "Python*", "python.exe"),
			filepath.Join(localAppData, "Programs", "Python", "Python*", "Scripts", "python.exe"),
			filepath.Join(localAppData, "miniconda3", "python.exe"),
			filepath.Join(localAppData, "miniconda3", "envs", "*", "python.exe"),
			filepath.Join(localAppData, "anaconda3", "python.exe"),
			filepath.Join(localAppData, "anaconda3", "envs", "*", "python.exe"),
			filepath.Join(localAppData, "miniforge3", "python.exe"),
			filepath.Join(localAppData, "miniforge3", "envs", "*", "python.exe"),
		)
	}
	for _, key := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if root := strings.TrimSpace(os.Getenv(key)); root != "" {
			patterns = append(patterns, filepath.Join(root, "Python*", "python.exe"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, directory := range []string{"miniconda3", "anaconda3", "miniforge3", "mambaforge"} {
			patterns = append(patterns,
				filepath.Join(home, directory, "python.exe"),
				filepath.Join(home, directory, "envs", "*", "python.exe"),
			)
		}
		patterns = append(patterns,
			filepath.Join(home, ".conda", "envs", "*", "python.exe"),
			filepath.Join(home, ".pyenv", "pyenv-win", "versions", "*", "python.exe"),
		)
	}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		paths = append(paths, matches...)
	}
	return paths
}

func shouldSkipInterpreterDiscoveryPath(path string) bool {
	normalized := strings.ToLower(filepath.Clean(path))
	return strings.Contains(normalized, `\microsoft\windowsapps\`)
}

func parsePythonLauncherPaths(output string) []string {
	paths := make([]string, 0, 4)
	for line := range strings.SplitSeq(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		separator := strings.Index(line, `:\`)
		if separator < 1 {
			continue
		}
		start := separator - 1
		if drive := line[start]; (drive < 'A' || drive > 'Z') && (drive < 'a' || drive > 'z') {
			continue
		}
		candidate := strings.TrimSpace(line[start:])
		lower := strings.ToLower(candidate)
		end := strings.LastIndex(lower, ".exe")
		if end < 0 {
			continue
		}
		candidate = strings.Trim(strings.TrimSpace(candidate[:end+4]), `"`)
		if filepath.IsAbs(candidate) {
			paths = append(paths, candidate)
		}
	}
	return paths
}

func pythonRegistryPaths() []string {
	paths := make([]string, 0, 8)
	views := []uint32{
		registry.READ | registry.WOW64_64KEY,
		registry.READ | registry.WOW64_32KEY,
	}
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		for _, access := range views {
			paths = append(paths, pythonRegistryPathsForView(root, access)...)
		}
	}
	return paths
}

func pythonRegistryPathsForView(root registry.Key, access uint32) []string {
	base, err := registry.OpenKey(root, pythonRegistryRoot, access)
	if err != nil {
		return nil
	}
	companies, err := base.ReadSubKeyNames(-1)
	base.Close()
	if err != nil {
		return nil
	}

	paths := make([]string, 0, len(companies))
	for _, company := range companies {
		companyPath := pythonRegistryRoot + `\` + company
		companyKey, err := registry.OpenKey(root, companyPath, access)
		if err != nil {
			continue
		}
		tags, readErr := companyKey.ReadSubKeyNames(-1)
		companyKey.Close()
		if readErr != nil {
			continue
		}
		for _, tag := range tags {
			tagPath := companyPath + `\` + tag
			if executable := registryStringValue(root, tagPath, "ExecutablePath", access); executable != "" {
				paths = append(paths, executable)
			}
			installPath := tagPath + `\InstallPath`
			if executable := registryStringValue(root, installPath, "ExecutablePath", access); executable != "" {
				paths = append(paths, executable)
				continue
			}
			if directory := registryStringValue(root, installPath, "", access); directory != "" {
				paths = append(paths, filepath.Join(directory, "python.exe"))
			}
		}
	}
	return paths
}

func registryStringValue(root registry.Key, path, name string, access uint32) string {
	key, err := registry.OpenKey(root, path, access)
	if err != nil {
		return ""
	}
	defer key.Close()
	value, valueType, err := key.GetStringValue(name)
	if err != nil {
		return ""
	}
	if valueType == registry.EXPAND_SZ {
		if expanded, expandErr := registry.ExpandString(value); expandErr == nil {
			value = expanded
		}
	}
	return strings.Trim(strings.TrimSpace(value), `"`)
}
