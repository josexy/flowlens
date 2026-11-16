package settingservice

const (
	defaultThemePrimaryColor = "blue"
	defaultThemeNeutralColor = "slate"
)

type themeColors struct {
	primary string
	neutral string
}

func validThemePrimaryColor(color string) bool {
	switch color {
	case "red", "orange", "amber", "yellow", "lime", "green", "emerald", "teal", "cyan", "sky", "blue", "indigo", "violet", "purple", "fuchsia", "pink", "rose":
		return true
	}
	return false
}

func validThemeNeutralColor(color string) bool {
	switch color {
	case "slate", "gray", "zinc", "neutral", "stone", "taupe", "mauve", "mist", "olive":
		return true
	}
	return false
}

func normalizeThemePrimaryColor(color string) string {
	if validThemePrimaryColor(color) {
		return color
	}
	return defaultThemePrimaryColor
}

func normalizeThemeNeutralColor(color string) string {
	if validThemeNeutralColor(color) {
		return color
	}
	return defaultThemeNeutralColor
}

func commonThemeColors(common *CommonConfig) themeColors {
	if common == nil {
		return themeColors{defaultThemePrimaryColor, defaultThemeNeutralColor}
	}
	return themeColors{normalizeThemePrimaryColor(common.ThemePrimaryColor), normalizeThemeNeutralColor(common.ThemeNeutralColor)}
}

func (s *SettingService) savedThemeColorsLocked() themeColors {
	return themeColors{normalizeThemePrimaryColor(s.themeSavedColors.primary), normalizeThemeNeutralColor(s.themeSavedColors.neutral)}
}
