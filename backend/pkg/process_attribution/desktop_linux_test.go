//go:build linux

package processattribution

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopEntryMatchesExecutableAfterRemovingExecFieldCodes(t *testing.T) {
	dataRoot := filepath.Join("testdata", "linux")
	resolver, err := newLinuxDesktopResolver([]string{dataRoot})
	if err != nil {
		t.Fatalf("newLinuxDesktopResolver: %v", err)
	}
	identity := resolver.resolve(linuxDesktopProcess{
		executablePath: "/opt/Flow Lens/bin/flowlens-fixture",
		processName:    "flowlens-fixture",
	})
	if identity.displayName != "FlowLens Fixture" || identity.confidence != "exact" {
		t.Fatalf("resolved desktop identity = %+v", identity)
	}
}

func TestDesktopEntryPrefersFlatpakOrSnapAppID(t *testing.T) {
	dataRoot := filepath.Join("testdata", "linux")
	resolver, err := newLinuxDesktopResolver([]string{dataRoot})
	if err != nil {
		t.Fatalf("newLinuxDesktopResolver: %v", err)
	}
	identity := resolver.resolve(linuxDesktopProcess{
		executablePath: "/app/bin/flowlens-fixture",
		processName:    "flowlens-fixture",
		appHint:        "org.flowlens.Fixture",
	})
	if identity.appID != "org.flowlens.Fixture" || identity.displayName != "FlowLens Flatpak Fixture" || identity.confidence != "exact" {
		t.Fatalf("resolved sandbox identity = %+v", identity)
	}
}

func TestIconThemeResolvesAbsolutePNG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absolute.png")
	writeLinuxTestPNG(t, path)
	resolved, err := resolveLinuxIconPath(path, nil)
	if err != nil {
		t.Fatalf("resolveLinuxIconPath: %v", err)
	}
	if resolved != path {
		t.Fatalf("resolved path = %q, want %q", resolved, path)
	}
}

func TestIconThemeFollowsHicolorInheritance(t *testing.T) {
	fixtureRoot := t.TempDir()
	userRoot := filepath.Join(fixtureRoot, "user")
	systemRoot := filepath.Join(fixtureRoot, "system")
	hicolor := filepath.Join(systemRoot, "icons", "hicolor")
	systemInherited := filepath.Join(systemRoot, "icons", "flowlens-test")
	userInherited := filepath.Join(userRoot, "icons", "flowlens-test")
	if err := os.MkdirAll(filepath.Join(userInherited, "64x64", "apps"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(systemInherited, 0o755); err != nil {
		t.Fatalf("MkdirAll system theme: %v", err)
	}
	if err := os.MkdirAll(hicolor, 0o755); err != nil {
		t.Fatalf("MkdirAll hicolor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hicolor, "index.theme"), []byte("[Icon Theme]\nName=Hicolor\nInherits=flowlens-test\nDirectories=\n"), 0o600); err != nil {
		t.Fatalf("WriteFile hicolor index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(systemInherited, "index.theme"), []byte("[Icon Theme]\nName=Fixture\nDirectories=64x64/apps\n\n[64x64/apps]\nSize=64\nType=Fixed\nContext=Applications\n"), 0o600); err != nil {
		t.Fatalf("WriteFile inherited index: %v", err)
	}
	iconPath := filepath.Join(userInherited, "64x64", "apps", "flowlens-fixture.png")
	writeLinuxTestPNG(t, iconPath)

	resolved, err := resolveLinuxIconPath("flowlens-fixture", []string{userRoot, systemRoot})
	if err != nil {
		t.Fatalf("resolveLinuxIconPath: %v", err)
	}
	if resolved != iconPath {
		t.Fatalf("resolved path = %q, want %q", resolved, iconPath)
	}
}

func TestLinuxSVGIconRasterizesToPNGSourceImage(t *testing.T) {
	path := filepath.Join("testdata", "linux", "icons", "flowlens-fixture.svg")
	icon, err := loadLinuxIconFile(path)
	if err != nil {
		t.Fatalf("loadLinuxIconFile: %v", err)
	}
	if icon == nil {
		t.Fatal("rasterized icon is nil")
	}
	if icon.Bounds().Dx() != 64 || icon.Bounds().Dy() != 64 {
		t.Fatalf("rasterized icon bounds = %v", icon.Bounds())
	}
	_, _, _, alpha := icon.At(32, 32).RGBA()
	if alpha == 0 {
		t.Fatal("rasterized currentColor SVG is transparent at its center")
	}
}

func TestLinuxUnsupportedXPMFallsBackWithoutError(t *testing.T) {
	path := filepath.Join("testdata", "linux", "icons", "flowlens-fixture.xpm")
	icon, err := loadLinuxIconFile(path)
	if icon != nil {
		t.Fatalf("XPM icon = %v, want nil", icon)
	}
	var unavailable *IconUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("XPM error = %v, want IconUnavailableError", err)
	}
}

func writeLinuxTestPNG(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll PNG parent: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create PNG: %v", err)
	}
	imageData := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			imageData.SetNRGBA(x, y, color.NRGBA{R: 0x24, G: 0x8b, B: 0xd2, A: 0xff})
		}
	}
	if err := png.Encode(file, imageData); err != nil {
		file.Close()
		t.Fatalf("Encode PNG: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close PNG: %v", err)
	}
}
