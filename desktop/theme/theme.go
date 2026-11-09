package theme

import (
	"slices"
	"strings"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	nui "github.com/josexy/nuxt-ui-go"
)

// Theme modes as they are saved in settings.
const (
	ModeAuto  = "auto"
	ModeLight = "light"
	ModeDark  = "dark"
)

// Defaults and bounds of the appearance settings.
const (
	DefaultPrimaryColor = "blue"
	DefaultNeutralColor = "slate"

	DefaultAppFontSize = 16
	MinAppFontSize     = 12
	MaxAppFontSize     = 24

	DefaultCodeFontSize = 13
	MinCodeFontSize     = 10
	MaxCodeFontSize     = 32
)

// interfaceTextRatio converts the saved interface size, which is the size of
// the root text, to the size of ordinary control text.
const interfaceTextRatio = 14.0 / 16.0

// Appearance holds the user's look and feel choices.
type Appearance struct {
	Mode         string
	PrimaryColor string
	NeutralColor string
	AppFont      string
	AppFontSize  int
	CodeFont     string
	CodeFontSize int
}

// AppearanceFromSettings reads the appearance from saved settings. A nil config
// gives the defaults.
func AppearanceFromSettings(config *settingservice.CommonConfig) Appearance {
	if config == nil {
		return Appearance{}
	}
	return Appearance{
		Mode:         config.ThemeMode,
		PrimaryColor: config.ThemePrimaryColor,
		NeutralColor: config.ThemeNeutralColor,
		AppFont:      config.AppFontFamily,
		AppFontSize:  config.AppFontSize,
		CodeFont:     config.CodeFontFamily,
		CodeFontSize: config.CodeFontSize,
	}
}

// Resolved is an Appearance with invalid values replaced by defaults, ready to
// apply to windows.
type Resolved struct {
	Config       nui.AppConfig
	Mode         string
	PrimaryColor string
	NeutralColor string
	AppFont      string
	CodeFont     string
	AppFontSize  int
	CodeFontSize int
}

// Resolve applies defaults to an Appearance and builds the themes.
func Resolve(appearance Appearance) Resolved {
	resolved := Resolved{
		Mode:         NormalizeMode(appearance.Mode),
		PrimaryColor: NormalizePrimaryColor(appearance.PrimaryColor),
		NeutralColor: NormalizeNeutralColor(appearance.NeutralColor),
		AppFont:      fontStack(appearance.AppFont, DefaultFontFamily),
		CodeFont:     fontStack(appearance.CodeFont, DefaultCodeFamily),
		AppFontSize:  NormalizeAppFontSize(appearance.AppFontSize),
		CodeFontSize: NormalizeCodeFontSize(appearance.CodeFontSize),
	}

	palettes := nui.DefaultPalettes()
	palettes.Primary, _ = nui.PaletteByName(resolved.PrimaryColor)
	palettes.Neutral, _ = nui.PaletteByName(resolved.NeutralColor)

	apply := func(dark bool) nui.Theme {
		theme := nui.NewTheme(dark, palettes)
		theme.Font = resolved.AppFont
		theme.MonoFont = resolved.CodeFont
		theme.FontSize = float32(resolved.AppFontSize) * interfaceTextRatio
		return theme
	}
	resolved.Config = nui.AppConfig{Mode: nuiMode(resolved.Mode), Light: apply(false), Dark: apply(true)}
	return resolved
}

func nuiMode(mode string) nui.Mode {
	switch mode {
	case ModeDark:
		return nui.DarkMode
	case ModeLight:
		return nui.LightMode
	default:
		return nui.SystemMode
	}
}

// NormalizeMode returns mode if it is valid and the default otherwise.
func NormalizeMode(mode string) string {
	switch mode {
	case ModeAuto, ModeLight, ModeDark:
		return mode
	default:
		return ModeLight
	}
}

// NormalizePrimaryColor returns color if FlowLens offers it as a primary color
// and the default otherwise.
func NormalizePrimaryColor(color string) string {
	if slices.Contains(nui.PrimaryPaletteNames(), color) {
		return color
	}
	return DefaultPrimaryColor
}

// NormalizeNeutralColor returns color if FlowLens offers it as a neutral color
// and the default otherwise.
func NormalizeNeutralColor(color string) string {
	if slices.Contains(nui.NeutralPaletteNames(), color) {
		return color
	}
	return DefaultNeutralColor
}

// NormalizeAppFontSize returns size if it is in range and the default otherwise.
func NormalizeAppFontSize(size int) int {
	if size >= MinAppFontSize && size <= MaxAppFontSize {
		return size
	}
	return DefaultAppFontSize
}

// NormalizeCodeFontSize returns size if it is in range and the default otherwise.
func NormalizeCodeFontSize(size int) int {
	if size >= MinCodeFontSize && size <= MaxCodeFontSize {
		return size
	}
	return DefaultCodeFontSize
}

// fontStack puts the user's family before the bundled one, which supplies the
// glyphs the chosen family lacks. The platform then falls back for other
// scripts, such as Chinese.
func fontStack(family, fallback string) string {
	family = strings.TrimSpace(family)
	if family == "" {
		return fallback
	}
	return family + ", " + fallback
}
