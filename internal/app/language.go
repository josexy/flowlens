package app

import settingservice "github.com/josexy/flowlens/internal/services/setting_service"

func currentLanguage(settingSvc *settingservice.SettingService) string {
	if settingSvc == nil {
		return ""
	}
	settings, err := settingSvc.Get()
	if err != nil || settings == nil || settings.CommonConfig == nil {
		return ""
	}
	return settings.CommonConfig.Language
}
