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
