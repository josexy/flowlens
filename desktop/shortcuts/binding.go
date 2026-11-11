// Package shortcuts holds the application's keyboard commands: their default
// bindings, how saved bindings resolve on each platform, and how key presses
// reach the handlers of the current page.
package shortcuts

import (
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

// Binding is a key with its modifiers.
type Binding = settingservice.ShortcutBinding

const (
	modifierPrimary = settingservice.ShortcutModifierPrimary
	modifierControl = settingservice.ShortcutModifierControl
	modifierAlt     = settingservice.ShortcutModifierAlt
	modifierShift   = settingservice.ShortcutModifierShift
	modifierSuper   = settingservice.ShortcutModifierSuper
)

var modifierOrder = []settingservice.ShortcutModifier{modifierPrimary, modifierControl, modifierAlt, modifierShift, modifierSuper}

// IsMacOS reports whether Primary means Command here.
func IsMacOS() bool { return runtime.GOOS == "darwin" }

// NormalizeKey returns a key name in canonical form: a lowercase letter, or
// the name as written. A single space stays a space.
func NormalizeKey(key string) string {
	if key == " " {
		return " "
	}
	value := strings.TrimSpace(key)
	switch value {
	case "":
		return ""
	case "Esc":
		return "Escape"
	case "Spacebar":
		return " "
	}
	if len(value) == 1 && value[0] >= 'A' && value[0] <= 'Z' {
		return strings.ToLower(value)
	}
	return value
}

// NormalizeBinding returns a canonical copy, or nil for an empty binding.
func NormalizeBinding(binding *Binding) *Binding {
	if binding == nil {
		return nil
	}
	key := NormalizeKey(binding.Key)
	if key == "" {
		return nil
	}
	normalized := &Binding{Key: key}
	for _, modifier := range modifierOrder {
		if slices.Contains(binding.Modifiers, modifier) {
			normalized.Modifiers = append(normalized.Modifiers, modifier)
		}
	}
	return normalized
}

// Resolved is a binding with Primary replaced by the platform's modifier.
type Resolved struct {
	Key                        string
	Control, Alt, Shift, Super bool
}

// Resolve applies the platform to a binding. It reports false when there is no
// binding.
func Resolve(binding *Binding, macOS bool) (Resolved, bool) {
	normalized := NormalizeBinding(binding)
	if normalized == nil {
		return Resolved{}, false
	}
	resolved := Resolved{Key: normalized.Key}
	for _, modifier := range normalized.Modifiers {
		switch modifier {
		case modifierPrimary:
			if macOS {
				resolved.Super = true
			} else {
				resolved.Control = true
			}
		case modifierControl:
			resolved.Control = true
		case modifierAlt:
			resolved.Alt = true
		case modifierShift:
			resolved.Shift = true
		case modifierSuper:
			resolved.Super = true
		}
	}
	return resolved, true
}

// BindingsConflict reports whether two bindings press the same keys here.
func BindingsConflict(left, right *Binding, macOS bool) bool {
	resolvedLeft, okLeft := Resolve(left, macOS)
	resolvedRight, okRight := Resolve(right, macOS)
	return okLeft && okRight && resolvedLeft == resolvedRight
}

// Display returns a binding as the platform writes it.
func Display(binding *Binding, macOS bool) string {
	resolved, ok := Resolve(binding, macOS)
	if !ok {
		return ""
	}
	var parts []string
	add := func(on bool, mac, other string) {
		if on {
			if macOS {
				parts = append(parts, mac)
			} else {
				parts = append(parts, other)
			}
		}
	}
	add(resolved.Control, "⌃", "Ctrl")
	add(resolved.Alt, "⌥", "Alt")
	add(resolved.Shift, "⇧", "Shift")
	add(resolved.Super, "⌘", "Super")
	key := resolved.Key
	switch {
	case key == " ":
		key = "Space"
	case len(key) == 1:
		key = strings.ToUpper(key)
	}
	parts = append(parts, key)
	if macOS {
		return strings.Join(parts, "")
	}
	return strings.Join(parts, "+")
}

// KbdKeys returns a binding's keys for showing as key caps.
func KbdKeys(binding *Binding, macOS bool) []string {
	resolved, ok := Resolve(binding, macOS)
	if !ok {
		return nil
	}
	var keys []string
	if resolved.Control {
		keys = append(keys, "ctrl")
	}
	if resolved.Alt {
		keys = append(keys, "alt")
	}
	if resolved.Shift {
		keys = append(keys, "shift")
	}
	if resolved.Super {
		if macOS {
			keys = append(keys, "meta")
		} else {
			keys = append(keys, "win")
		}
	}
	if resolved.Key == " " {
		return append(keys, "space")
	}
	return append(keys, strings.ToLower(resolved.Key))
}

// keyNames maps saved key names to keys. Shifted punctuation names the key
// that types it, because the saved name follows what the key typed.
var keyNames = map[string]ui.Key{
	"Enter": ui.KeyEnter, "Escape": ui.KeyEscape, "Backspace": ui.KeyBackspace, "Tab": ui.KeyTab, " ": ui.KeySpace,
	"Delete": ui.KeyDelete, "Insert": ui.KeyInsert, "Home": ui.KeyHome, "End": ui.KeyEnd, "PageUp": ui.KeyPageUp, "PageDown": ui.KeyPageDown,
	"ArrowLeft": ui.KeyLeft, "ArrowRight": ui.KeyRight, "ArrowUp": ui.KeyUp, "ArrowDown": ui.KeyDown,
	"F1": ui.KeyF1, "F2": ui.KeyF2, "F3": ui.KeyF3, "F4": ui.KeyF4, "F5": ui.KeyF5, "F6": ui.KeyF6,
	"F7": ui.KeyF7, "F8": ui.KeyF8, "F9": ui.KeyF9, "F10": ui.KeyF10, "F11": ui.KeyF11, "F12": ui.KeyF12,
	"-": ui.KeyMinus, "=": ui.KeyEqual, ",": ui.KeyComma, ".": ui.KeyPeriod, "/": ui.KeySlash, ";": ui.KeySemicolon,
	"'": ui.KeyQuote, "[": ui.KeyBracketLeft, "]": ui.KeyBracketRight, "\\": ui.KeyBackslash, "`": ui.KeyBackquote,
	"_": ui.KeyMinus, "+": ui.KeyEqual, "<": ui.KeyComma, ">": ui.KeyPeriod, "?": ui.KeySlash, ":": ui.KeySemicolon,
	"\"": ui.KeyQuote, "{": ui.KeyBracketLeft, "}": ui.KeyBracketRight, "|": ui.KeyBackslash, "~": ui.KeyBackquote,
	"!": ui.Key1, "@": ui.Key2, "#": ui.Key3, "$": ui.Key4, "%": ui.Key5, "^": ui.Key6, "&": ui.Key7, "*": ui.Key8, "(": ui.Key9, ")": ui.Key0,
}

func uiKey(name string) (ui.Key, bool) {
	if key, ok := keyNames[name]; ok {
		return key, true
	}
	if len(name) == 1 {
		switch character := name[0]; {
		case character >= 'a' && character <= 'z':
			return ui.KeyA + ui.Key(character-'a'), true
		case character >= '0' && character <= '9':
			return ui.Key0 + ui.Key(character-'0'), true
		}
	}
	return ui.KeyUnknown, false
}

func uiModifiers(resolved Resolved) ui.Modifiers {
	var modifiers ui.Modifiers
	if resolved.Control {
		modifiers |= ui.Ctrl
	}
	if resolved.Alt {
		modifiers |= ui.Alt
	}
	if resolved.Shift {
		modifiers |= ui.Shift
	}
	if resolved.Super {
		modifiers |= ui.Super
	}
	return modifiers
}

// BindingFromKey returns the binding of a key pressed with modifiers, or nil
// for a key a binding cannot name.
func BindingFromKey(key ui.Key, modifiers ui.Modifiers, macOS bool) *Binding {
	name := keyName(key)
	if name == "" {
		return nil
	}
	var held []settingservice.ShortcutModifier
	if macOS {
		if modifiers&ui.Super != 0 {
			held = append(held, modifierPrimary)
		}
		if modifiers&ui.Ctrl != 0 {
			held = append(held, modifierControl)
		}
	} else {
		if modifiers&ui.Ctrl != 0 {
			held = append(held, modifierPrimary)
		}
		if modifiers&ui.Super != 0 {
			held = append(held, modifierSuper)
		}
	}
	if modifiers&ui.Alt != 0 {
		held = append(held, modifierAlt)
	}
	if modifiers&ui.Shift != 0 {
		held = append(held, modifierShift)
	}
	return NormalizeBinding(&Binding{Key: name, Modifiers: held})
}

func keyName(key ui.Key) string {
	if key >= ui.KeyA && key <= ui.KeyZ {
		return string(rune('a' + (key - ui.KeyA)))
	}
	if key >= ui.Key0 && key <= ui.Key9 {
		return string(rune('0' + (key - ui.Key0)))
	}
	found := ""
	for name, candidate := range keyNames {
		if candidate != key {
			continue
		}
		// Shifted punctuation shares a key with the plain mark, which wins.
		if found == "" || !strings.ContainsAny(name, "_+<>?:\"{}|~!@#$%^&*()") {
			if found == "" || name < found || strings.ContainsAny(found, "_+<>?:\"{}|~!@#$%^&*()") {
				found = name
			}
		}
	}
	return found
}

// RecordOutcome is what a key press does to a recording.
type RecordOutcome int

// Recording outcomes.
const (
	RecordContinue RecordOutcome = iota
	RecordCancel
	RecordClear
	RecordBinding
)

// Record decides what a key pressed during recording does.
func Record(key ui.Key, modifiers ui.Modifiers, macOS bool) (RecordOutcome, *Binding) {
	if key == ui.KeyEscape {
		return RecordCancel, nil
	}
	if modifiers&(ui.Ctrl|ui.Alt|ui.Shift|ui.Super) == 0 && (key == ui.KeyBackspace || key == ui.KeyDelete) {
		return RecordClear, nil
	}
	if binding := BindingFromKey(key, modifiers, macOS); binding != nil {
		return RecordBinding, binding
	}
	return RecordContinue, nil
}

// Conflicts lists the commands other than id bound to binding.
func Conflicts(id string, binding *Binding, config *settingservice.ShortcutConfig, macOS bool) []Command {
	var conflicts []Command
	for _, command := range Catalog {
		if command.ID != id && BindingsConflict(ResolveCommand(command, config).Binding, binding, macOS) {
			conflicts = append(conflicts, command)
		}
	}
	return conflicts
}

// MenuShortcut returns the keys of a command's binding in use, with the
// user's override, for a menu item to show (ui.MenuItem.Shortcut). It
// reports false for a command without a binding, as one an override
// disables.
func MenuShortcut(commandID string, config *settingservice.ShortcutConfig) (ui.Modifiers, ui.Key, bool) {
	return menuShortcut(commandID, config, IsMacOS())
}

func menuShortcut(commandID string, config *settingservice.ShortcutConfig, macOS bool) (ui.Modifiers, ui.Key, bool) {
	command, ok := CommandByID(commandID)
	if !ok {
		return 0, ui.KeyUnknown, false
	}
	binding, ok := Resolve(ResolveCommand(command, config).Binding, macOS)
	if !ok {
		return 0, ui.KeyUnknown, false
	}
	key, ok := uiKey(binding.Key)
	if !ok {
		return 0, ui.KeyUnknown, false
	}
	return uiModifiers(binding), key, true
}
