package app

import (
	"encoding/json"
	"strings"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type trayLabels struct {
	OpenMainWindow string `json:"openMainWindow"`
	Close          string `json:"close"`
}

func updateTrayMenu(tray *application.SystemTray, labels trayLabels, showMainWindow func(), quitApplication func()) {
	if tray == nil {
		return
	}
	menu := application.NewMenu()
	menu.Add(labels.OpenMainWindow).OnClick(func(ctx *application.Context) {
		showMainWindow()
	})
	menu.AddSeparator()
	menu.Add(labels.Close).OnClick(func(ctx *application.Context) {
		quitApplication()
	})
	tray.SetMenu(menu)
}

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

func fallbackTrayLabels(language string) trayLabels {
	if language == "en" {
		return trayLabels{
			OpenMainWindow: "Open Main Window",
			Close:          "Close",
		}
	}
	return trayLabels{
		OpenMainWindow: "打开主窗口",
		Close:          "关闭",
	}
}

func parseTrayLabels(data any) (trayLabels, bool) {
	var labels trayLabels
	switch value := data.(type) {
	case trayLabels:
		labels = value
	case *trayLabels:
		if value == nil {
			return trayLabels{}, false
		}
		labels = *value
	case map[string]any:
		labels = trayLabels{
			OpenMainWindow: stringFromMap(value, "openMainWindow"),
			Close:          stringFromMap(value, "close"),
		}
	case map[string]string:
		labels = trayLabels{
			OpenMainWindow: value["openMainWindow"],
			Close:          value["close"],
		}
	case json.RawMessage:
		if err := json.Unmarshal(value, &labels); err != nil {
			return trayLabels{}, false
		}
	case []byte:
		if err := json.Unmarshal(value, &labels); err != nil {
			return trayLabels{}, false
		}
	case string:
		if err := json.Unmarshal([]byte(value), &labels); err != nil {
			return trayLabels{}, false
		}
	default:
		payload, err := json.Marshal(value)
		if err != nil {
			return trayLabels{}, false
		}
		if err := json.Unmarshal(payload, &labels); err != nil {
			return trayLabels{}, false
		}
	}

	if strings.TrimSpace(labels.OpenMainWindow) == "" || strings.TrimSpace(labels.Close) == "" {
		return trayLabels{}, false
	}
	return labels, true
}

func stringFromMap(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func boolFromEventData(data any) (bool, bool) {
	switch value := data.(type) {
	case bool:
		return value, true
	case map[string]any:
		dirty, ok := value["dirty"].(bool)
		return dirty, ok
	case map[string]bool:
		dirty, ok := value["dirty"]
		return dirty, ok
	case json.RawMessage:
		var dirty bool
		if err := json.Unmarshal(value, &dirty); err == nil {
			return dirty, true
		}
		var payload map[string]bool
		if err := json.Unmarshal(value, &payload); err != nil {
			return false, false
		}
		dirty, ok := payload["dirty"]
		return dirty, ok
	case []byte:
		return boolFromEventData(json.RawMessage(value))
	case string:
		return boolFromEventData(json.RawMessage(value))
	default:
		payload, err := json.Marshal(value)
		if err != nil {
			return false, false
		}
		return boolFromEventData(json.RawMessage(payload))
	}
}
