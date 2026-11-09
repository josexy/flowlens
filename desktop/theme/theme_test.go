package theme

import (
	"slices"
	"testing"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	nui "github.com/josexy/nuxt-ui-go"
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
		ThemeMode: "dark", ThemePrimaryColor: "rose", ThemeNeutralColor: "zinc",
		AppFontFamily: "A", AppFontSize: 14, CodeFontFamily: "B", CodeFontSize: 15,
	})
	want := Appearance{Mode: "dark", PrimaryColor: "rose", NeutralColor: "zinc", AppFont: "A", AppFontSize: 14, CodeFont: "B", CodeFontSize: 15}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
