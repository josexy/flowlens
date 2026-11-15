package settingservice

import (
	"sync"
	"testing"
)

func themeState(t *testing.T, svc *SettingService) ThemeColorState {
	t.Helper()
	state, err := svc.GetThemeColorState()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func startThemePreview(t *testing.T, svc *SettingService) ThemeColorState {
	t.Helper()
	if err := BeginThemeColorPreview(svc); err != nil {
		t.Fatal(err)
	}
	return themeState(t, svc)
}

func previewColors(t *testing.T, svc *SettingService, session, sequence uint64, primary, neutral string) ThemeColorState {
	t.Helper()
	state, err := svc.PreviewThemeColors(ThemeColorPreviewRequest{
		SessionID: session, Sequence: sequence, PrimaryColor: primary, NeutralColor: neutral,
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func stageThemeColors(t *testing.T, svc *SettingService, primary, neutral string) {
	t.Helper()
	settings, err := svc.Get()
	if err != nil {
		t.Fatal(err)
	}
	copy := *settings
	common := *settings.CommonConfig
	common.ThemePrimaryColor, common.ThemeNeutralColor = primary, neutral
	copy.CommonConfig = &common
	if err := svc.UpdatePreservingShortcuts(&copy); err != nil {
		t.Fatal(err)
	}
}

func TestThemeColorDefaultsAndValidation(t *testing.T) {
	for _, colors := range []themeColors{{}, {"#fff", "red"}, {"Blue", "invalid"}, {"rose", "olive"}} {
		svc := &SettingService{}
		if err := svc.Update(&Settings{CommonConfig: &CommonConfig{
			ThemeMode: "dark", Language: "en", ThemePrimaryColor: colors.primary, ThemeNeutralColor: colors.neutral,
		}}); err != nil {
			t.Fatal(err)
		}
		settings, _ := svc.Get()
		want := themeColors{normalizeThemePrimaryColor(colors.primary), normalizeThemeNeutralColor(colors.neutral)}
		if commonThemeColors(settings.CommonConfig) != want || settings.CommonConfig.ThemePrimaryColor != want.primary || settings.CommonConfig.ThemeNeutralColor != want.neutral {
			t.Fatalf("colors were not normalized: %+v", settings.CommonConfig)
		}
		if settings.CommonConfig.ThemeMode != "dark" || settings.CommonConfig.Language != "en" {
			t.Fatal("palette normalization changed existing preferences")
		}
	}
}

func TestThemeColorsRoundTripAndLegacyDefaults(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	if _, err := svc.repository.db.Exec(`INSERT INTO app_settings(section, payload_version, payload_json, updated_at)
		VALUES ('common', 1, '{"themeMode":"dark","language":"en"}', 0)`); err != nil {
		t.Fatal(err)
	}
	if state := themeState(t, svc); state.PrimaryColor != "blue" || state.NeutralColor != "slate" {
		t.Fatalf("legacy palette: %+v", state)
	}
	stageThemeColors(t, svc, "violet", "mauve")
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded := newPersistentTestSettingService(t)
	if state := themeState(t, reloaded); state.PrimaryColor != "violet" || state.NeutralColor != "mauve" || state.Preview || state.SessionID != 0 {
		t.Fatalf("reloaded palette: %+v", state)
	}
	settings, _ := reloaded.Get()
	if settings.CommonConfig.ThemeMode != "dark" || settings.CommonConfig.Language != "en" {
		t.Fatal("palette save changed theme mode or language")
	}
}

func TestThemeColorPreviewIsIndependentOfSettingsAndPersistence(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	initial := startThemePreview(t, svc)
	previewColors(t, svc, initial.SessionID, 1, "pink", "stone")
	settings, _ := svc.Get()
	if settings.CommonConfig.ThemePrimaryColor != "blue" || settings.CommonConfig.ThemeNeutralColor != "slate" {
		t.Fatal("preview mutated the settings snapshot")
	}
	if err := svc.SetThemeMode("dark"); err != nil {
		t.Fatal(err)
	}
	// Saving a theme-mode change from another window must retain the preview.
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	if state := themeState(t, svc); !state.Preview || state.PrimaryColor != "pink" {
		t.Fatalf("unrelated save cleared preview: %+v", state)
	}
	if state := themeState(t, newPersistentTestSettingService(t)); state.PrimaryColor != "blue" || state.NeutralColor != "slate" {
		t.Fatalf("preview was persisted: %+v", state)
	}
	EndThemeColorPreview(svc)
	if state := themeState(t, svc); state.Preview || state.PrimaryColor != "blue" || state.SessionID != 0 {
		t.Fatalf("closing settings did not restore colors: %+v", state)
	}
}

func TestThemeColorPreviewRejectsInvalidAndIgnoresStaleRequests(t *testing.T) {
	svc := &SettingService{}
	_ = svc.Update(&Settings{})
	initial := startThemePreview(t, svc)
	if _, err := svc.PreviewThemeColors(ThemeColorPreviewRequest{SessionID: initial.SessionID, Sequence: 1, PrimaryColor: "invalid", NeutralColor: "slate"}); err == nil {
		t.Fatal("invalid primary was accepted")
	}
	if _, err := svc.PreviewThemeColors(ThemeColorPreviewRequest{SessionID: initial.SessionID, Sequence: 1, PrimaryColor: "blue", NeutralColor: "red"}); err == nil {
		t.Fatal("invalid neutral was accepted")
	}
	latest := previewColors(t, svc, initial.SessionID, 2, "rose", "olive")
	if state := previewColors(t, svc, initial.SessionID, 1, "green", "slate"); state != latest {
		t.Fatal("out-of-order request overwrote the newest preview")
	}
	EndThemeColorPreview(svc)
	closed := themeState(t, svc)
	if state := previewColors(t, svc, initial.SessionID, 100, "red", "gray"); state != closed {
		t.Fatal("closed window's delayed request restored its preview")
	}
	reopened := startThemePreview(t, svc)
	if reopened.SessionID == initial.SessionID {
		t.Fatal("reopened settings reused the previous session")
	}
	if state := previewColors(t, svc, initial.SessionID, 101, "red", "gray"); state != reopened {
		t.Fatal("old window affected reopened settings")
	}
	previewColors(t, svc, reopened.SessionID, 1, "green", "mist")
}

func TestThemeColorSaveCommitsPreviewAndAllowsFurtherEdits(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	var events []ThemeColorState
	SetThemeColorsChangedHandler(svc, func(state ThemeColorState) { events = append(events, state) })
	initial := startThemePreview(t, svc)
	previewColors(t, svc, initial.SessionID, 1, "teal", "taupe")
	stageThemeColors(t, svc, "teal", "taupe")
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	saved := themeState(t, svc)
	if saved.Preview || saved.PrimaryColor != "teal" || saved.SessionID != initial.SessionID {
		t.Fatalf("preview was not committed: %+v", saved)
	}
	previewColors(t, svc, initial.SessionID, 2, "orange", "zinc")
	EndThemeColorPreview(svc)
	if state := themeState(t, svc); state.PrimaryColor != "teal" || state.NeutralColor != "taupe" {
		t.Fatalf("discard restored the old baseline: %+v", state)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Revision <= events[i-1].Revision {
			t.Fatal("broadcast revisions did not increase")
		}
	}
	if len(events) != 5 {
		t.Fatalf("expected every lifecycle transition to be broadcast, got %d", len(events))
	}
}

func TestThemeColorFailedSaveKeepsPreviewAndRollsBackStagedColors(t *testing.T) {
	configureTestSettingsPath(t)
	svc := newPersistentTestSettingService(t)
	initial := startThemePreview(t, svc)
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	previewColors(t, svc, initial.SessionID, 1, "purple", "zinc")
	stageThemeColors(t, svc, "purple", "zinc")
	if _, err := svc.repository.db.Exec(`CREATE TRIGGER fail_theme_save BEFORE INSERT ON app_settings
		WHEN NEW.section = 'common' BEGIN SELECT RAISE(ABORT, 'injected save failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := svc.Save(); err == nil {
		t.Fatal("injected save failure was not reported")
	}
	if state := themeState(t, svc); !state.Preview || state.PrimaryColor != "purple" {
		t.Fatalf("failed save discarded the preview: %+v", state)
	}
	settings, _ := svc.Get()
	if settings.CommonConfig.ThemePrimaryColor != "blue" || settings.CommonConfig.ThemeNeutralColor != "slate" {
		t.Fatal("failed colors remained staged for a later save")
	}
	if _, err := svc.repository.db.Exec(`DROP TRIGGER fail_theme_save`); err != nil {
		t.Fatal(err)
	}
	_ = svc.SetLanguage("en")
	if err := svc.Save(); err != nil {
		t.Fatal(err)
	}
	EndThemeColorPreview(svc)
	if state := themeState(t, newPersistentTestSettingService(t)); state.PrimaryColor != "blue" || state.NeutralColor != "slate" {
		t.Fatalf("later unrelated save persisted failed colors: %+v", state)
	}
}

func TestThemeColorConcurrentPreviewsKeepHighestSequence(t *testing.T) {
	svc := &SettingService{}
	_ = svc.Update(&Settings{})
	initial := startThemePreview(t, svc)
	var workers sync.WaitGroup
	for sequence := uint64(1); sequence <= 100; sequence++ {
		workers.Go(func() {
			_, _ = svc.PreviewThemeColors(ThemeColorPreviewRequest{SessionID: initial.SessionID, Sequence: sequence, PrimaryColor: "cyan", NeutralColor: "mist"})
		})
	}
	workers.Wait()
	if state := themeState(t, svc); state.Sequence != 100 || state.PrimaryColor != "cyan" || !state.Preview {
		t.Fatalf("concurrent previews lost the newest state: %+v", state)
	}
}
