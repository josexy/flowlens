//go:build windows

package pythonpluginservice

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getPackagesByPackageFamily = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetPackagesByPackageFamily")

func windowsStoreInterpreterPaths(ctx context.Context) []string {
	root := windowsAppAliasesRoot()
	if root == "" {
		return nil
	}
	directories, _ := filepath.Glob(filepath.Join(root, "PythonSoftwareFoundation.Python*_*"))
	paths := make([]string, 0, len(directories))
	for index, directory := range directories {
		if ctx.Err() != nil || index >= maxInterpreterDiscoveryPaths || len(paths) >= maxInterpreterDiscoveryPaths {
			break
		}
		family := filepath.Base(directory)
		if !isStorePythonFamily(family) && !isPythonManagerFamily(family) {
			continue
		}
		if !isWindowsPackageRegistered(family) {
			continue
		}
		if isStorePythonFamily(family) {
			paths = append(paths, filepath.Join(directory, "python.exe"))
			continue
		}
		manager := filepath.Join(directory, "pymanager.exe")
		if !isWindowsAppExecutionAlias(manager) {
			continue
		}
		// List local installations only; never invoke a manager as an interpreter.
		output, _, err := runInterpreterDiscoveryCommand(ctx, 2*time.Second, manager, "list", "--format=json")
		if err == nil {
			paths = append(paths, parsePythonManagerPaths(output)...)
		}
	}
	return paths[:min(len(paths), maxInterpreterDiscoveryPaths)]
}

func windowsAppAliasesRoot() string {
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if !filepath.IsAbs(localAppData) {
		return ""
	}
	return filepath.Join(localAppData, "Microsoft", "WindowsApps")
}

func windowsStorePythonAliasFamily(path string) string {
	root := windowsAppAliasesRoot()
	if root == "" {
		return ""
	}
	relative, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil {
		return ""
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) != 2 || !isStorePythonFamily(parts[0]) {
		return ""
	}
	name := strings.ToLower(parts[1])
	if name == "python.exe" || name == "python3.exe" ||
		(strings.HasPrefix(name, "python3.") && strings.HasSuffix(name, ".exe") &&
			isDecimalVersion(strings.TrimSuffix(strings.TrimPrefix(name, "python3."), ".exe"))) {
		return parts[0]
	}
	return ""
}

func isStorePythonFamily(family string) bool {
	name, publisher, found := strings.Cut(strings.ToLower(family), "_")
	const prefix = "pythonsoftwarefoundation.python.3."
	return found && publisher == "qbz5n2kfra8p0" && strings.HasPrefix(name, prefix) &&
		isDecimalVersion(strings.TrimPrefix(name, prefix))
}

func isPythonManagerFamily(family string) bool {
	name, publisher, found := strings.Cut(strings.ToLower(family), "_")
	return found && name == "pythonsoftwarefoundation.pythonmanager" &&
		(publisher == "qbz5n2kfra8p0" || publisher == "3847v3x7pw1km")
}

func isDecimalVersion(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func isWindowsPackageRegistered(family string) bool {
	if err := getPackagesByPackageFamily.Find(); err != nil {
		return false
	}
	name, err := windows.UTF16PtrFromString(family)
	if err != nil {
		return false
	}
	var count, bufferLength uint32
	result, _, _ := getPackagesByPackageFamily.Call(uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&count)), 0, uintptr(unsafe.Pointer(&bufferLength)), 0)
	return (result == 0 || result == uintptr(windows.ERROR_INSUFFICIENT_BUFFER)) && count > 0
}

func parsePythonManagerPaths(output string) []string {
	var payload struct {
		Versions []struct {
			Executable string `json:"executable"`
		} `json:"versions"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil
	}
	paths := make([]string, 0, min(len(payload.Versions), maxInterpreterDiscoveryPaths))
	for _, version := range payload.Versions {
		path := strings.TrimSpace(version.Executable)
		if filepath.IsAbs(path) {
			paths = append(paths, path)
			if len(paths) == maxInterpreterDiscoveryPaths {
				break
			}
		}
	}
	return paths
}
