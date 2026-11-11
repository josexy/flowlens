package logic

import (
	"slices"
	"testing"
)

func TestPythonConsoleMetadata(t *testing.T) {
	if PythonLogStreamKey("stderr") != "stderr" || PythonLogLevelKey("warning") != "warning" || PythonLogTone("stderr", "warning") != PythonToneError || PythonLogTone("stdout", "debug") != PythonToneNeutral {
		t.Fatal("metadata")
	}
}

func TestPythonConsolePreview(t *testing.T) {
	if PythonLogLineCount("first\r\nsecond\nthird") != 3 || PythonLogLineCount("") != 0 {
		t.Fatal("lines")
	}
	if PythonLogPreview("  first\n  second  ", 240) != "first second" || PythonLogPreview("abcdefghij", 6) != "abcde…" || PythonLogPreview("", 240) != "" {
		t.Fatal("preview")
	}
}

func TestPythonConsoleBatches(t *testing.T) {
	entries := make([]PluginLogEntry, 4)
	for index := range entries {
		entries[index] = PluginLogEntry{EventID: uint64(index + 1), ExecutionID: map[bool]string{true: "current", false: "stale"}[index%2 == 0]}
	}
	matched := FilterPythonLogBatch(entries, "current")
	if len(matched) != 2 || matched[0].EventID != 1 || matched[1].EventID != 3 {
		t.Fatalf("%+v", matched)
	}
	target := AppendPythonLogBatch(slices.Clone(entries[:2]), entries[2:], 3)
	if len(target) != 3 || target[0].EventID != 2 || target[2].EventID != 4 {
		t.Fatalf("%+v", target)
	}
}

func TestPythonConsoleText(t *testing.T) {
	const micros = int64(1_700_000_000_123_456)
	stamp := FormatUnixMicrosLocal(micros)
	source := func(entry *PluginLogEntry) string { return "src-" + entry.PluginID }
	for _, tc := range []struct {
		name    string
		entries []PluginLogEntry
		want    string
	}{
		{"stdout", []PluginLogEntry{{PluginID: "a", Stream: "stdout", Level: "info", Message: "hello", Timestamp: micros}}, "[" + stamp + "] [src-a] [stdout] hello"},
		{"stderr", []PluginLogEntry{{PluginID: "a", Stream: "stderr", Message: "boom", Timestamp: micros}}, "[" + stamp + "] [src-a] [stderr] boom"},
		{"level only", []PluginLogEntry{{PluginID: "b", Level: "warning", Message: "careful", Timestamp: micros}}, "[" + stamp + "] [src-b] [warning] careful"},
		{"no stream or level", []PluginLogEntry{{PluginID: "c", Message: "plain", Timestamp: micros}}, "[" + stamp + "] [src-c] [log] plain"},
		{"multi-line", []PluginLogEntry{{PluginID: "a", Stream: "stdout", Message: "one\ntwo", Timestamp: micros}, {PluginID: "b", Stream: "STDERR", Message: "three", Timestamp: micros}}, "[" + stamp + "] [src-a] [stdout] one\ntwo\n[" + stamp + "] [src-b] [STDERR] three"},
		{"empty", nil, ""},
	} {
		if got := PythonConsoleText(tc.entries, source); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
