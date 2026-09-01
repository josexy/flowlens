package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGetBaseStorageDirCreatesFlowLensDir(t *testing.T) {
	configRoot := setUserConfigDir(t)

	baseDir, err := GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir() returned error: %v", err)
	}

	want := filepath.Join(configRoot, baseStorageDirName)
	if baseDir != want {
		t.Fatalf("base dir = %q, want %q", baseDir, want)
	}
	if !PathExists(want) {
		t.Fatalf("expected %q to exist", want)
	}
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(want)
		if statErr != nil {
			t.Fatalf("Stat base storage dir: %v", statErr)
		}
		if got := info.Mode().Perm(); got != PrivateDirMode {
			t.Fatalf("base storage dir mode = %04o, want %04o", got, PrivateDirMode)
		}
	}
}

func TestGetBaseStorageDirTightensExistingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not implement Unix permission bits")
	}
	configRoot := setUserConfigDir(t)
	baseDir := filepath.Join(configRoot, baseStorageDirName)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(baseDir, 0o755); err != nil {
		t.Fatalf("Chmod setup: %v", err)
	}

	gotDir, err := GetBaseStorageDir()
	if err != nil {
		t.Fatalf("GetBaseStorageDir: %v", err)
	}
	info, err := os.Stat(gotDir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != PrivateDirMode {
		t.Fatalf("tightened mode = %04o, want %04o", got, PrivateDirMode)
	}
}

func setUserConfigDir(t *testing.T) string {
	t.Helper()

	configRoot := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", configRoot)
	case "darwin":
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		configRoot = filepath.Join(homeDir, "Library", "Application Support")
	default:
		t.Setenv("XDG_CONFIG_HOME", configRoot)
	}
	return configRoot
}
