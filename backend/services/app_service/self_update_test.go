package appservice

import "testing"

func TestCanSelfUpdatePath(t *testing.T) {
	for _, tc := range []struct {
		name, goos, executable string
		want                   bool
	}{
		{"machine-wide Windows", "windows", `C:\Program Files\FlowLens\flowlens.exe`, false},
		{"case insensitive Windows", "windows", `c:\PROGRAM FILES\FlowLens\flowlens.exe`, false},
		{"slash variants", "windows", `C:/Program Files/FlowLens/flowlens.exe`, false},
		{"dot segments", "windows", `C:\Users\..\Program Files\FlowLens\flowlens.exe`, false},
		{"x86 installation", "windows", `C:\Program Files (x86)\FlowLens\flowlens.exe`, false},
		{"portable Windows", "windows", `C:\Users\tester\FlowLens\flowlens.exe`, true},
		{"sibling prefix", "windows", `C:\Program Files Backup\flowlens.exe`, true},
		{"Linux package", "linux", "/usr/local/bin/flowlens", false},
		{"Flatpak", "linux", "/app/bin/flowlens", false},
		{"Snap", "linux", "/snap/flowlens/current/flowlens", false},
		{"AppImage", "linux", "/tmp/FlowLens.AppImage", false},
		{"mounted AppImage", "linux", "/tmp/.mount_flowlens/usr/bin/flowlens", false},
		{"unpackaged Linux", "linux", "/home/tester/flowlens", false},
		{"macOS bundle", "darwin", "/Applications/FlowLens.app/Contents/MacOS/FlowLens", true},
		{"mounted macOS DMG", "darwin", "/Volumes/FlowLens/FlowLens.app/Contents/MacOS/FlowLens", false},
		{"macOS volume sibling prefix", "darwin", "/Volumes Backup/FlowLens.app/Contents/MacOS/FlowLens", true},
		{"empty path", "windows", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := canSelfUpdatePath(tc.goos, tc.executable, `C:\Program Files`, `C:\Program Files (x86)`); got != tc.want {
				t.Fatalf("canSelfUpdatePath(%q, %q) = %v; want %v", tc.goos, tc.executable, got, tc.want)
			}
		})
	}
}
