package widgets

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/resources"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestWindowControlHoverReachesTitleBarBottom(t *testing.T) {
	closed := false
	frame := WindowFrame{Custom: true, Title: "FlowLens", Close: func() { closed = true }}
	theme := nui.NewTheme(true, nui.DefaultPalettes())
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			frame.Build(c, theme)
			ui.Box(c).FillWidth().Grow(1).Background(theme.MutedBackground).Label("content")
		})
	}, 360, 80)
	for _, scale := range []float32{1, 1.25, 1.5, 2} {
		tester.SetScale(scale)
		for _, zoom := range []float32{1, 1.19} {
			tester.SetZoom(zoom)
			content, ok := tester.Find("content")
			if !ok {
				t.Fatal("content missing")
			}
			y := int(math.Round(float64(content.Y*scale))) - 1
			frame := tester.Image()
			if got := frame.RGBAAt(4, y); !sameColor(got, theme.Border) {
				t.Fatalf("scale %v, zoom %v: bottom pixel %v, want divider %v", scale, zoom, got, theme.Border)
			}
			if got := frame.RGBAAt(4, y-1); !sameColor(got, theme.Background) {
				t.Fatalf("scale %v, zoom %v: pixel above divider %v, want background %v", scale, zoom, got, theme.Background)
			}
		}
	}
	const scale = 1.5
	tester.SetScale(scale)
	tester.SetZoom(1)
	button, ok := tester.Find("Close")
	if !ok {
		t.Fatal("close button missing")
	}
	content, ok := tester.Find("content")
	if !ok {
		t.Fatal("content missing")
	}
	x, bottom := button.X+4, content.Y
	px, py := int(x*scale), int(bottom*scale)-1
	tester.Move(x, button.Y+button.H/2)
	if got := tester.Image().RGBAAt(px, py); !sameColor(got, closeHoverColor()) {
		t.Fatalf("close hover bottom pixel is %v, want %v", got, closeHoverColor())
	}
	tester.ClickAt(x, bottom-0.5)
	if !closed {
		t.Fatal("click at the title bar bottom did not close the window")
	}
}

// bitmapPixelBytes inspects the pinned MyGo bitmap's actual retained CPU pixel
// buffers after painting, including lazily-created mipmaps. Reflection only
// reads slice capacities; it neither mutates MyGo nor depends on unsafe access.
func bitmapPixelBytes(bitmap *ui.Bitmap) int {
	v := reflect.ValueOf(bitmap).Elem()
	bytes := v.FieldByName("img").Elem().FieldByName("Pix").Cap()
	levels := v.FieldByName("levels")
	for i := range levels.Len() {
		bytes += levels.Index(i).Elem().FieldByName("Pix").Cap()
	}
	return bytes
}

func TestAppIconSharesBoundedPixelsAcrossWindows(t *testing.T) {
	icon := AppIcon()
	if icon == nil {
		t.Fatal("application icon failed to decode")
	}
	if w, h := icon.Size(); w <= 0 || h <= 0 || w > 256 || h > 256 {
		t.Fatalf("icon size %dx%d exceeds the shared thumbnail bound", w, h)
	}
	for range 3 {
		tester := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Fill().Children(func() {
				WindowFrame{Custom: true, Title: "FlowLens"}.Build(c, nui.NewTheme(false, nui.DefaultPalettes()))
				ui.Image(c, AppIcon()).Size(48, 48)
			})
		}, 360, 100)
		for _, scale := range []float32{1, 1.5, 2, 4} {
			tester.SetScale(scale)
			tester.Frame()
			_ = tester.Image()
			if got := AppIcon(); got != icon {
				t.Fatal("another window decoded another bitmap")
			}
		}
	}
	bytes := bitmapPixelBytes(icon)
	if bytes < 256*256*4 || bytes >= 512*1024 {
		t.Fatalf("retained bitmap pixels = %d bytes, want base pixels plus mipmaps below 0.5 MiB", bytes)
	}
	t.Logf("shared bitmap retains %d bytes including rendered mipmaps", bytes)
	// Measure the previous full decode with the same display sizes. This
	// isolates pixels from global font/renderer caches and GC sampling noise.
	legacy, err := ui.DecodeBitmap(resources.AppIcon)
	if err != nil {
		t.Fatal(err)
	}
	legacyTester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() {
			ui.Image(c, legacy).Size(18, 18)
			ui.Image(c, legacy).Size(48, 48)
		})
	}, 100, 100)
	legacyTester.Frame()
	_ = legacyTester.Image()
	t.Logf("previous full-size bitmap retains %d bytes at the same display sizes", bitmapPixelBytes(legacy))
}

func TestWindowFrameRemainsUsableWithoutAppIcon(t *testing.T) {
	load := appIcon
	appIcon = func() *ui.Bitmap { return nil }
	t.Cleanup(func() { appIcon = load })
	closed := false
	tester := ui.NewTester(func(c *ui.Context) {
		WindowFrame{Custom: true, Title: "FlowLens", Close: func() { closed = true }}.Build(c, nui.NewTheme(false, nui.DefaultPalettes()))
	}, 360, 80)
	tester.Frame()
	if _, ok := tester.Find("FlowLens"); !ok {
		t.Fatal("missing icon hid the window title")
	}
	if err := tester.Click("Close"); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("missing icon disabled the window controls")
	}
}

func TestRenderAppIcon(t *testing.T) {
	for _, language := range []string{"en", "zh-Hans"} {
		for _, dark := range []bool{false, true} {
			theme := nui.NewTheme(dark, nui.DefaultPalettes())
			titles := []string{"FlowLens", "Settings", "Software Update"}
			if language == "zh-Hans" {
				titles = []string{"FlowLens", "设置", "软件更新"}
			}
			tester := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Background(theme.Background).Children(func() {
					for _, title := range titles {
						WindowFrame{Custom: true, Title: title}.Build(c, theme)
					}
					ui.Image(c, AppIcon()).Size(48, 48).Margin(12)
				})
			}, 360, 180)
			for _, scale := range []float32{1, 1.5, 2, 4} {
				tester.SetScale(scale)
				rendertest.Shoot(t, tester, fmt.Sprintf("app-icon-%s-dark-%t-scale-%g", language, dark, scale))
			}
		}
	}
}
