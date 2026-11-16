package rendertest

import (
	"image"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// RequireSamePixels checks that a region remains visually unchanged across
// frames, for example while an unrelated operation is saving.
func RequireSamePixels(t testing.TB, before, after *image.RGBA, bounds ui.Rect, label string) {
	t.Helper()
	region := image.Rect(int(math.Floor(float64(bounds.X))), int(math.Floor(float64(bounds.Y))), int(math.Ceil(float64(bounds.X+bounds.W))), int(math.Ceil(float64(bounds.Y+bounds.H))))
	if region.Empty() || !region.In(before.Bounds()) || !region.In(after.Bounds()) {
		t.Fatalf("%s has invalid render bounds: %v", label, region)
	}
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			if before.RGBAAt(x, y) != after.RGBAAt(x, y) {
				t.Errorf("%s changed appearance during an unrelated write at (%d, %d)", label, x, y)
				return
			}
		}
	}
}
