package settings

import (
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	shortcutservice "github.com/josexy/flowlens/internal/services/shortcut_service"
	"github.com/josexy/flowlens/locales"
)

// The service codes below map to their copy here. A code without copy shows
// as the service reported it, so a new code is never shown as a message key.

// restartReasonText names a change that needs a proxy restart.
func restartReasonText(reason proxyservice.ProxyRestartReason) string {
	switch reason {
	case proxyservice.ProxyRestartReasonProxyMode:
		return locales.SettingsRestartReasonProxyModeChanged()
	case proxyservice.ProxyRestartReasonListenAddress:
		return locales.SettingsRestartReasonListenAddressChanged()
	case proxyservice.ProxyRestartReasonMITMCA:
		return locales.SettingsRestartReasonMitmCaChanged()
	default:
		return string(reason)
	}
}

// shortcutApplyErrorText explains why a shortcut change was not applied.
func shortcutApplyErrorText(code shortcutservice.ShortcutApplyErrorCode) string {
	switch code {
	case shortcutservice.ApplyErrorInvalidConfig:
		return locales.ShortcutsApplyErrorsInvalidConfig()
	case shortcutservice.ApplyErrorUnsupported:
		return locales.ShortcutsApplyErrorsUnsupported()
	case shortcutservice.ApplyErrorRegistration:
		return locales.ShortcutsApplyErrorsRegistrationConflict()
	case shortcutservice.ApplyErrorPersistence:
		return locales.ShortcutsApplyErrorsPersistenceFailed()
	case shortcutservice.ApplyErrorUnregister:
		return locales.ShortcutsApplyErrorsUnregisterFailed()
	case shortcutservice.ApplyErrorRollbackIncomplete:
		return locales.ShortcutsApplyErrorsRollbackIncomplete()
	default:
		return locales.ShortcutsApplyErrorsUnknown()
	}
}

// shortcutWarningText describes a platform limit of system-wide shortcuts.
func shortcutWarningText(warning shortcutservice.ShortcutWarning) string {
	switch warning {
	case shortcutservice.WarningWaylandPortalControlsBinding:
		return locales.ShortcutsWarningsWaylandPortalControlsBinding()
	case shortcutservice.WarningMacNonANSICompatibility:
		return locales.ShortcutsWarningsMacNonANSICompatibility()
	default:
		return string(warning)
	}
}

// shortcutRuntimeText describes the state of a system-wide shortcut.
func shortcutRuntimeText(status shortcutservice.ShortcutRuntimeStatus) string {
	switch status {
	case shortcutservice.ShortcutStatusActive:
		return locales.ShortcutsRuntimeActive()
	case shortcutservice.ShortcutStatusConflict:
		return locales.ShortcutsRuntimeConflict()
	case shortcutservice.ShortcutStatusUnsupported:
		return locales.ShortcutsRuntimeUnsupported()
	case shortcutservice.ShortcutStatusPortalPending:
		return locales.ShortcutsRuntimePortalPending()
	default:
		return string(status)
	}
}

// paletteText names a theme palette offered in Settings.
func paletteText(name string) string {
	switch name {
	case "red":
		return locales.SettingsThemeColorsPaletteRed()
	case "orange":
		return locales.SettingsThemeColorsPaletteOrange()
	case "amber":
		return locales.SettingsThemeColorsPaletteAmber()
	case "yellow":
		return locales.SettingsThemeColorsPaletteYellow()
	case "lime":
		return locales.SettingsThemeColorsPaletteLime()
	case "green":
		return locales.SettingsThemeColorsPaletteGreen()
	case "emerald":
		return locales.SettingsThemeColorsPaletteEmerald()
	case "teal":
		return locales.SettingsThemeColorsPaletteTeal()
	case "cyan":
		return locales.SettingsThemeColorsPaletteCyan()
	case "sky":
		return locales.SettingsThemeColorsPaletteSky()
	case "blue":
		return locales.SettingsThemeColorsPaletteBlue()
	case "indigo":
		return locales.SettingsThemeColorsPaletteIndigo()
	case "violet":
		return locales.SettingsThemeColorsPaletteViolet()
	case "purple":
		return locales.SettingsThemeColorsPalettePurple()
	case "fuchsia":
		return locales.SettingsThemeColorsPaletteFuchsia()
	case "pink":
		return locales.SettingsThemeColorsPalettePink()
	case "rose":
		return locales.SettingsThemeColorsPaletteRose()
	case "slate":
		return locales.SettingsThemeColorsPaletteSlate()
	case "gray":
		return locales.SettingsThemeColorsPaletteGray()
	case "zinc":
		return locales.SettingsThemeColorsPaletteZinc()
	case "neutral":
		return locales.SettingsThemeColorsPaletteNeutral()
	case "stone":
		return locales.SettingsThemeColorsPaletteStone()
	case "taupe":
		return locales.SettingsThemeColorsPaletteTaupe()
	case "mauve":
		return locales.SettingsThemeColorsPaletteMauve()
	case "mist":
		return locales.SettingsThemeColorsPaletteMist()
	case "olive":
		return locales.SettingsThemeColorsPaletteOlive()
	default:
		return name
	}
}

// caErrorText translates an error code of the certificate operations. ok is
// false for a code without copy.
func caErrorText(code string) (text string, ok bool) {
	switch code {
	case "ca_builtin_unavailable":
		return locales.SettingsCaTrustErrorsCaBuiltinUnavailable(), true
	case "ca_custom_generation_disabled":
		return locales.SettingsCaTrustErrorsCaCustomGenerationDisabled(), true
	case "ca_custom_paths_required":
		return locales.SettingsCaTrustErrorsCaCustomPathsRequired(), true
	case "ca_trust_certificate_changed":
		return locales.SettingsCaTrustErrorsCaTrustCertificateChanged(), true
	case "ca_trust_invalid_ca":
		return locales.SettingsCaTrustErrorsCaTrustInvalidCa(), true
	case "ca_trust_invalid_pair":
		return locales.SettingsCaTrustErrorsCaTrustInvalidPair(), true
	case "ca_trust_invalid_validity":
		return locales.SettingsCaTrustErrorsCaTrustInvalidValidity(), true
	case "ca_trust_login_keychain_unavailable":
		return locales.SettingsCaTrustErrorsCaTrustLoginKeychainUnavailable(), true
	case "ca_trust_timeout":
		return locales.SettingsCaTrustErrorsCaTrustTimeout(), true
	case "ca_trust_uninstall_first":
		return locales.SettingsCaTrustErrorsCaTrustUninstallFirst(), true
	case "ca_trust_unsupported":
		return locales.SettingsCaTrustErrorsCaTrustUnsupported(), true
	case "ca_trust_verification_failed":
		return locales.SettingsCaTrustErrorsCaTrustVerificationFailed(), true
	default:
		return "", false
	}
}
