package proxyservice

import (
	"runtime"
	"testing"
)

func setTestConfigDir(t *testing.T) {
	t.Helper()
	configRoot := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", configRoot)
	case "darwin":
		t.Setenv("HOME", configRoot)
	default:
		t.Setenv("XDG_CONFIG_HOME", configRoot)
	}
}
