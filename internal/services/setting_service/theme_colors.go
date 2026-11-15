package settingservice

import "fmt"

const ThemeColorsChangedEvent = "app:theme-colors-changed"

const (
	defaultThemePrimaryColor = "blue"
	defaultThemeNeutralColor = "slate"
)

type themeColors struct {
	primary string
	neutral string
}

// ThemeColorState is the effective appearance, independent of settings drafts.
// SessionID and Sequence identify the one settings window allowed to preview.
type ThemeColorState struct {
	PrimaryColor string `json:"primaryColor"`
	NeutralColor string `json:"neutralColor"`
	Preview      bool   `json:"preview"`
	Revision     uint64 `json:"revision"`
	SessionID    uint64 `json:"sessionID"`
	Sequence     uint64 `json:"sequence"`
}

type ThemeColorPreviewRequest struct {
	PrimaryColor string `json:"primaryColor"`
	NeutralColor string `json:"neutralColor"`
	SessionID    uint64 `json:"sessionID"`
	Sequence     uint64 `json:"sequence"`
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

func (s *SettingService) themeColorStateLocked() ThemeColorState {
	colors := s.savedThemeColorsLocked()
	if s.themePreviewColors != nil {
		colors = *s.themePreviewColors
	}
	return ThemeColorState{
		PrimaryColor: colors.primary, NeutralColor: colors.neutral,
		Preview: s.themePreviewColors != nil, Revision: s.themeColorRevision,
		SessionID: s.themePreviewSession, Sequence: s.themePreviewSequence,
	}
}

func (s *SettingService) GetThemeColorState() (ThemeColorState, error) {
	if err := s.ensureLoaded(); err != nil {
		return ThemeColorState{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.themeColorStateLocked(), nil
}

func (s *SettingService) PreviewThemeColors(request ThemeColorPreviewRequest) (ThemeColorState, error) {
	if err := s.ensureLoaded(); err != nil {
		return ThemeColorState{}, err
	}
	s.mu.Lock()
	if request.SessionID == 0 || request.SessionID != s.themePreviewSession || request.Sequence <= s.themePreviewSequence {
		state := s.themeColorStateLocked()
		s.mu.Unlock()
		return state, nil
	}
	if !validThemePrimaryColor(request.PrimaryColor) || !validThemeNeutralColor(request.NeutralColor) {
		s.mu.Unlock()
		return ThemeColorState{}, fmt.Errorf("invalid theme color palette")
	}
	s.themePreviewSequence = request.Sequence
	s.themePreviewColors = &themeColors{request.PrimaryColor, request.NeutralColor}
	s.themeColorRevision++
	state, changed := s.themeColorStateLocked(), s.themeColorsChanged
	s.mu.Unlock()
	if changed != nil {
		changed(state)
	}
	return state, nil
}

// The app owns window sessions and event bridging; neither is persisted.
func SetThemeColorsChangedHandler(service *SettingService, handler func(ThemeColorState)) {
	service.mu.Lock()
	service.themeColorsChanged = handler
	service.mu.Unlock()
}

func BeginThemeColorPreview(service *SettingService) error {
	if err := service.ensureLoaded(); err != nil {
		return err
	}
	service.mu.Lock()
	service.themeSessionCounter++
	service.themePreviewSession = service.themeSessionCounter
	service.themePreviewSequence = 0
	service.themePreviewColors = nil
	service.themeColorRevision++
	state, changed := service.themeColorStateLocked(), service.themeColorsChanged
	service.mu.Unlock()
	if changed != nil {
		changed(state)
	}
	return nil
}

func EndThemeColorPreview(service *SettingService) {
	service.mu.Lock()
	service.themePreviewSession = 0
	service.themePreviewSequence = 0
	service.themePreviewColors = nil
	service.themeColorRevision++
	state, changed := service.themeColorStateLocked(), service.themeColorsChanged
	service.mu.Unlock()
	if changed != nil {
		changed(state)
	}
}
