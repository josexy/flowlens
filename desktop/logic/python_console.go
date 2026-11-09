package logic

import (
	"regexp"
	"strings"

	pythonpluginservice "github.com/josexy/flowlens/backend/services/python_plugin_service"
)

// PluginLogEntry is one line a Python plugin wrote.
type PluginLogEntry = pythonpluginservice.PluginLogEntry

// Log tones.
const (
	PythonToneNeutral = "neutral"
	PythonToneInfo    = "info"
	PythonToneWarning = "warning"
	PythonToneError   = "error"
)

var (
	pythonNewlinePattern    = regexp.MustCompile(`\r?\n|\r`)
	pythonWhitespacePattern = regexp.MustCompile(`\s+`)
)

// PythonLogStreamKey names an entry's stream.
func PythonLogStreamKey(stream string) string {
	if strings.EqualFold(strings.TrimSpace(stream), "stderr") {
		return "stderr"
	}
	return "stdout"
}

// PythonLogLevelKey names an entry's level.
func PythonLogLevelKey(level string) string {
	normalized := strings.ToLower(strings.TrimSpace(level))
	switch normalized {
	case "warn":
		return "warning"
	case "debug", "info", "warning", "error":
		return normalized
	default:
		return "info"
	}
}

// PythonLogTone returns how an entry is colored.
func PythonLogTone(stream, level string) string {
	key := PythonLogLevelKey(level)
	switch {
	case PythonLogStreamKey(stream) == "stderr" || key == "error":
		return PythonToneError
	case key == "warning":
		return PythonToneWarning
	case key == "debug":
		return PythonToneNeutral
	default:
		return PythonToneInfo
	}
}

// PythonLogPreview returns a message on one bounded line.
func PythonLogPreview(message string, maxLength int) string {
	normalized := strings.TrimSpace(pythonWhitespacePattern.ReplaceAllString(pythonNewlinePattern.ReplaceAllString(message, " "), " "))
	characters := []rune(normalized)
	if len(characters) <= maxLength {
		return normalized
	}
	return strings.TrimRight(string(characters[:max(0, maxLength-1)]), " ") + "…"
}

// PythonLogLineCount counts a message's lines.
func PythonLogLineCount(message string) int {
	if message == "" {
		return 0
	}
	return len(pythonNewlinePattern.Split(message, -1))
}

// FilterPythonLogBatch keeps the entries of one execution.
func FilterPythonLogBatch(entries []PluginLogEntry, executionID string) []PluginLogEntry {
	var matched []PluginLogEntry
	for _, entry := range entries {
		if entry.ExecutionID == executionID {
			matched = append(matched, entry)
		}
	}
	return matched
}

// AppendPythonLogBatch adds batch to target, keeping the newest maxEntries.
func AppendPythonLogBatch(target, batch []PluginLogEntry, maxEntries int) []PluginLogEntry {
	target = append(target, batch...)
	if overflow := len(target) - maxEntries; overflow > 0 {
		target = append([]PluginLogEntry(nil), target[overflow:]...)
	}
	return target
}
