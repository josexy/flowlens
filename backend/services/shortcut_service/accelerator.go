package shortcutservice

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

func resolveAccelerator(binding settingservice.ShortcutBinding, osName string) (string, error) {
	modifiers := make(map[string]struct{}, len(binding.Modifiers))
	for _, modifier := range binding.Modifiers {
		resolved, ok := resolveModifier(modifier, osName)
		if !ok {
			return "", fmt.Errorf("unsupported modifier %q", modifier)
		}
		modifiers[resolved] = struct{}{}
	}
	if len(modifiers) == 0 {
		return "", fmt.Errorf("at least one modifier is required")
	}

	key, ok := resolveGlobalKey(binding.Key)
	if !ok {
		return "", fmt.Errorf("unsupported key %q", binding.Key)
	}
	orderedModifiers := make([]string, 0, len(modifiers))
	for modifier := range modifiers {
		orderedModifiers = append(orderedModifiers, modifier)
	}
	sort.Slice(orderedModifiers, func(i, j int) bool {
		return modifierOrder(orderedModifiers[i]) < modifierOrder(orderedModifiers[j])
	})
	return strings.Join(append(orderedModifiers, key), "+"), nil
}

func resolveModifier(modifier settingservice.ShortcutModifier, osName string) (string, bool) {
	switch modifier {
	case settingservice.ShortcutModifierPrimary:
		if osName == "darwin" {
			return "Cmd", true
		}
		return "Ctrl", true
	case settingservice.ShortcutModifierControl:
		return "Ctrl", true
	case settingservice.ShortcutModifierAlt:
		if osName == "darwin" {
			return "Option", true
		}
		return "Alt", true
	case settingservice.ShortcutModifierShift:
		return "Shift", true
	case settingservice.ShortcutModifierSuper:
		if osName == "darwin" {
			return "Cmd", true
		}
		return "Super", true
	default:
		return "", false
	}
}

func modifierOrder(modifier string) int {
	switch modifier {
	case "Cmd":
		return 0
	case "Ctrl":
		return 1
	case "Option":
		return 2
	case "Alt":
		return 3
	case "Shift":
		return 4
	case "Super":
		return 5
	default:
		return 6
	}
}

func resolveGlobalKey(key string) (string, bool) {
	if key == " " {
		return "Space", true
	}
	key = strings.TrimSpace(key)
	if len(key) == 1 {
		value := key[0]
		if value >= 'a' && value <= 'z' {
			return strings.ToUpper(key), true
		}
		if value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' {
			return strings.ToUpper(key), true
		}
		return "", false
	}

	if len(key) >= 2 && (key[0] == 'f' || key[0] == 'F') {
		functionKey, err := strconv.Atoi(key[1:])
		if err == nil && functionKey >= 1 && functionKey <= 20 {
			return fmt.Sprintf("F%d", functionKey), true
		}
	}

	resolved, ok := globalNamedKeys[strings.ToLower(key)]
	return resolved, ok
}

var globalNamedKeys = map[string]string{
	"backspace":  "Backspace",
	"tab":        "Tab",
	"enter":      "Enter",
	"return":     "Return",
	"escape":     "Escape",
	"arrowleft":  "Left",
	"left":       "Left",
	"arrowright": "Right",
	"right":      "Right",
	"arrowup":    "Up",
	"up":         "Up",
	"arrowdown":  "Down",
	"down":       "Down",
	"space":      "Space",
	"delete":     "Delete",
	"home":       "Home",
	"end":        "End",
	"pageup":     "Page Up",
	"page up":    "Page Up",
	"pagedown":   "Page Down",
	"page down":  "Page Down",
}
