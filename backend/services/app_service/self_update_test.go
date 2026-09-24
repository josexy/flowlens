package appservice

import (
	"path/filepath"
	"testing"
)

func TestCanSelfUpdatePath(t *testing.T) {
	if got := canSelfUpdatePath("windows", filepath.Join("C:\\", "Program Files", "FlowLens", "flowlens.exe"), "", filepath.Join("C:\\", "Program Files"), filepath.Join("C:\\", "Program Files (x86)")); got {
		t.Fatal("machine-wide Windows installation must not self-update")
	}
	if got := canSelfUpdatePath("windows", filepath.Join("C:\\", "PROGRAM FILES", "FlowLens", "flowlens.exe"), "", filepath.Join("C:\\", "Program Files"), filepath.Join("C:\\", "Program Files (x86)")); got {
		t.Fatal("Windows installation path matching must be case-insensitive")
	}
	if got := canSelfUpdatePath("windows", filepath.Join("C:\\", "Users", "tester", "FlowLens", "flowlens.exe"), "", filepath.Join("C:\\", "Program Files"), filepath.Join("C:\\", "Program Files (x86)")); !got {
		t.Fatal("portable/user Windows installation should self-update")
	}
	if got := canSelfUpdatePath("linux", "/usr/local/bin/flowlens", "", "", ""); got {
		t.Fatal("package-managed Linux installation must not self-update")
	}
	if got := canSelfUpdatePath("linux", "/app/bin/flowlens", "", "", ""); got {
		t.Fatal("Flatpak-style Linux installation must not self-update")
	}
	if got := canSelfUpdatePath("linux", "/snap/flowlens/current/flowlens", "", "", ""); got {
		t.Fatal("Snap-style Linux installation must not self-update")
	}
	if got := canSelfUpdatePath("linux", "/tmp/FlowLens.AppImage", "", "", ""); !got {
		t.Fatal("AppImage installation should self-update")
	}
	if got := canSelfUpdatePath("linux", "/tmp/.mount_flowlens/flowlens", "/home/tester/Downloads/FlowLens.AppImage", "", ""); !got {
		t.Fatal("AppImage runtime should self-update")
	}
	if got := canSelfUpdatePath("darwin", "/Applications/FlowLens.app/Contents/MacOS/FlowLens", "", "", ""); !got {
		t.Fatal("macOS app bundle should self-update")
	}
}
