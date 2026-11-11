package widgets

import (
	"image/color"
	"testing"
)

func TestMenuIconsAreMade(t *testing.T) {
	for name, icon := range map[string]func() any{
		"export":   func() any { return ExportMenuIcon() },
		"alias":    func() any { return AliasMenuIcon() },
		"delete":   func() any { return DeleteMenuIcon() },
		"show all": func() any { return ShowAllMenuIcon() },
		"swatch":   func() any { return SwatchMenuIcon("#ef4444") },
	} {
		if icon() == nil {
			t.Fatalf("no %s icon", name)
		}
	}
	if SwatchMenuIcon("#ef4444") != SwatchMenuIcon("#ef4444") {
		t.Fatal("a swatch is made more than once")
	}
}

// swatchPixels returns the swatch's center and the color just inside its
// outline.
func swatchPixels(t *testing.T, hex string) (center, outline color.RGBA) {
	t.Helper()
	svg := SwatchSVG(hex)
	if svg == nil {
		t.Fatalf("no swatch of %s", hex)
	}
	if w, h := svg.Size(); w != 16 || h != 16 {
		t.Fatalf("swatch of %s is %v × %v", hex, w, h)
	}
	image := svg.Image(64, 64, color.Black)
	// The outline runs 5 to 6 px from the center, 20 to 24 px at 4×.
	return image.RGBAAt(32, 32), image.RGBAAt(32, 32-22)
}

func TestSwatchIsItsColorWithAnOutline(t *testing.T) {
	center, outline := swatchPixels(t, "#3b82f6")
	if center != (color.RGBA{0x3b, 0x82, 0xf6, 0xff}) {
		t.Fatalf("center %v", center)
	}
	if outline.A != 0xff || outline.R >= 0x3b || outline.B >= 0xf6 {
		t.Fatalf("outline %v is not the color darkened", outline)
	}
	if corner := SwatchSVG("#3b82f6").Image(64, 64, color.Black).RGBAAt(1, 1); corner.A != 0 {
		t.Fatalf("corner %v is not clear", corner)
	}
}
