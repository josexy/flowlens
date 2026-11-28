package theme

import (
	"math"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

var (
	primaryColors = []string{"red", "orange", "amber", "yellow", "lime", "green", "emerald", "teal", "cyan", "sky", "blue", "indigo", "violet", "purple", "fuchsia", "pink", "rose"}
	neutralColors = []string{"slate", "gray", "zinc", "neutral", "stone", "taupe", "mauve", "mist", "olive"}
)

func TestAllOfferedPalettesApply(t *testing.T) {
	if len(primaryColors)+len(neutralColors) != 26 {
		t.Fatal("expected 26 palette names")
	}
	if !slices.Equal(primaryColors, nui.PrimaryPaletteNames()) || !slices.Equal(neutralColors, nui.NeutralPaletteNames()) {
		t.Fatal("offered palettes differ from the spec")
	}
	for _, name := range primaryColors {
		want, ok := nui.PaletteByName(name)
		if !ok {
			t.Fatalf("no palette %q", name)
		}
		resolved := Resolve(Appearance{PrimaryColor: name})
		if resolved.PrimaryColor != name || resolved.Config.Light.Palettes.Primary != want || resolved.Config.Dark.Palettes.Primary != want {
			t.Errorf("primary %q not applied to both themes", name)
		}
	}
	for _, name := range neutralColors {
		want, ok := nui.PaletteByName(name)
		if !ok {
			t.Fatalf("no palette %q", name)
		}
		resolved := Resolve(Appearance{NeutralColor: name})
		if resolved.NeutralColor != name || resolved.Config.Light.Palettes.Neutral != want || resolved.Config.Dark.Palettes.Neutral != want {
			t.Errorf("neutral %q not applied to both themes", name)
		}
	}
}

func TestUnknownColorsFallBack(t *testing.T) {
	for _, test := range []struct{ name, primary, neutral string }{
		{"unknown", "chartreuse", "beige"},
		{"empty", "", ""},
		{"swapped groups", "slate", "blue"},
	} {
		resolved := Resolve(Appearance{PrimaryColor: test.primary, NeutralColor: test.neutral})
		if resolved.PrimaryColor != "blue" || resolved.NeutralColor != "slate" {
			t.Errorf("%s: got %q/%q", test.name, resolved.PrimaryColor, resolved.NeutralColor)
		}
	}
}

func TestThemeModes(t *testing.T) {
	for _, test := range []struct {
		mode string
		want nui.Mode
	}{
		{ModeAuto, nui.SystemMode},
		{ModeLight, nui.LightMode},
		{ModeDark, nui.DarkMode},
		{"", nui.LightMode},
		{"neon", nui.LightMode},
	} {
		if got := Resolve(Appearance{Mode: test.mode}).Config.Mode; got != test.want {
			t.Errorf("mode %q: got %v, want %v", test.mode, got, test.want)
		}
	}
}

func TestFontSizeBounds(t *testing.T) {
	for _, test := range []struct{ size, want int }{
		{0, 16}, {11, 16}, {12, 12}, {16, 16}, {24, 24}, {25, 16}, {-3, 16},
	} {
		if got := Resolve(Appearance{AppFontSize: test.size}).AppFontSize; got != test.want {
			t.Errorf("app size %d: got %d, want %d", test.size, got, test.want)
		}
	}
	for _, test := range []struct{ size, want int }{
		{0, 13}, {9, 13}, {10, 10}, {13, 13}, {32, 32}, {33, 13},
	} {
		if got := Resolve(Appearance{CodeFontSize: test.size}).CodeFontSize; got != test.want {
			t.Errorf("code size %d: got %d, want %d", test.size, got, test.want)
		}
	}
}

func TestInterfaceFontSizeSetsControlText(t *testing.T) {
	config := Resolve(Appearance{AppFontSize: 16}).Config
	if config.Light.FontSize != 14 || config.Dark.FontSize != 14 {
		t.Fatalf("control text = %v/%v, want 14", config.Light.FontSize, config.Dark.FontSize)
	}
	if got := Resolve(Appearance{AppFontSize: 24}).Config.Light.FontSize; got != 21 {
		t.Fatalf("control text at 24 = %v, want 21", got)
	}
}

func TestFontStack(t *testing.T) {
	resolved := Resolve(Appearance{})
	if resolved.AppFont != DefaultFontFamily || resolved.CodeFont != DefaultCodeFamily {
		t.Fatalf("defaults: %q / %q", resolved.AppFont, resolved.CodeFont)
	}
	resolved = Resolve(Appearance{AppFont: " Inter ", CodeFont: "Fira Code"})
	if resolved.AppFont != "Inter, DM Sans" || resolved.CodeFont != "Fira Code, JetBrains Mono" {
		t.Fatalf("stacks: %q / %q", resolved.AppFont, resolved.CodeFont)
	}
	if resolved.Config.Light.Font != resolved.AppFont || resolved.Config.Dark.MonoFont != resolved.CodeFont {
		t.Fatal("stacks not applied to the theme")
	}
}

func TestAppearanceFromSettings(t *testing.T) {
	if got := AppearanceFromSettings(nil); got != (Appearance{}) {
		t.Fatalf("nil config = %+v", got)
	}
	got := AppearanceFromSettings(&settingservice.CommonConfig{
		DisableAnimations: true,
		ThemeMode:         "dark", ThemePrimaryColor: "rose", ThemeNeutralColor: "zinc",
		AppFontFamily: "A", AppFontSize: 14, CodeFontFamily: "B", CodeFontSize: 15,
	})
	want := Appearance{DisableAnimations: true, Mode: "dark", PrimaryColor: "rose", NeutralColor: "zinc", AppFont: "A", AppFontSize: 14, CodeFont: "B", CodeFontSize: 15}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestWebSocketPalette(t *testing.T) {
	for _, tc := range []struct {
		dark                               bool
		send, sendTag, receive, receiveTag string
	}{
		{false, "#d97706", "#b45309", "#2563eb", "#1d4ed8"},
		{true, "#fbbf24", "#f59e0b", "#60a5fa", "#3b82f6"},
	} {
		got := WebSocketPalette(tc.dark)
		for _, pair := range []struct {
			got  ui.Color
			want string
		}{{got.Send, tc.send}, {got.SendTag, tc.sendTag}, {got.Receive, tc.receive}, {got.ReceiveTag, tc.receiveTag}} {
			if pair.got != ui.Hex(pair.want) {
				t.Errorf("dark %v: %v, want %s", tc.dark, pair.got, pair.want)
			}
		}
	}
}

func TestCustomTextMatchesControlsAcrossSizesAndDPI(t *testing.T) {
	appearance := Appearance{AppFont: "Arial", AppFontSize: 16}
	tester := ui.NewTester(func(c *ui.Context) {
		palette := Begin(c, Resolve(appearance).Config)
		ui.Column(c).AlignItems(ui.Start).Children(func() {
			ui.Text(c, "Alphabet 123").FontSize(TextSize(c, 14)).FontWeight(500).LineHeight(20.0 / 14).Label("custom body")
			ui.Text(c, "Alphabet 123").FontSize(TextSize(c, 12)).Label("caption")
			ui.Text(c, "Alphabet 123").FontSize(TextSize(c, 18)).Label("heading")
			nui.Button(c, palette, "Alphabet 123", nui.ButtonOptions{Style: func(_, label, _, _ ui.Element) {
				label.Label("control body")
			}})
		})
	}, 900, 700)
	for _, dpi := range []float32{1, 1.5, 2} {
		tester.SetScale(dpi)
		for _, accessibility := range []float32{1, 1.19} {
			tester.SetPreferences(ui.Preferences{TextScale: accessibility, ReduceMotion: true})
			var small ui.Rect
			for _, size := range []int{12, 16, 24, 16} {
				appearance.AppFontSize = size
				for range 3 {
					tester.Frame()
				}
				body, _ := tester.Find("custom body")
				control, _ := tester.Find("control body")
				caption, _ := tester.Find("caption")
				heading, _ := tester.Find("heading")
				if body.W <= 0 || math.Abs(float64(body.W-control.W)) > 1 || math.Abs(float64(body.H-control.H)) > 1 {
					t.Fatalf("size %v, DPI %v, accessibility %v: custom %+v, control %+v", size, dpi, accessibility, body, control)
				}
				if caption.H >= body.H || heading.H <= body.H {
					t.Fatalf("text hierarchy lost: caption %+v, body %+v, heading %+v", caption, body, heading)
				}
				// The line height is a multiple of the font size, so it shows
				// the font size on every platform. Widths follow glyph
				// advances, which Pango on Linux hints to whole pixels, so a
				// line at twice the size is not exactly twice as wide.
				if size == 12 {
					small = body
				} else if math.Abs(float64(body.H-small.H*float32(size)/12)) > 0.5 || body.W <= small.W {
					t.Fatalf("text did not scale proportionally: size %v, small %+v, body %+v", size, small, body)
				}
			}
		}
	}
}

func TestRegisterFontsMakesBundledFamiliesAvailable(t *testing.T) {
	if err := RegisterFonts(); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{DefaultFontFamily: false, DefaultCodeFamily: false}
	for _, family := range ui.FontFamilies() {
		if _, ok := want[family.Name]; ok && family.Registered {
			want[family.Name] = true
		}
	}
	for family, found := range want {
		if !found {
			t.Errorf("family %q not registered", family)
		}
	}
}
