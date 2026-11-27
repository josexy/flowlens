package logic

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTrafficCellPreview(t *testing.T) {
	for _, original := range []string{"", UnknownValue, "OPTIONS", strings.Repeat("x", 256)} {
		if got := TrafficCellPreview(original); got != original {
			t.Fatalf("short value changed: %q", got)
		}
	}
	for _, original := range []string{
		strings.Repeat("x", 257), strings.Repeat("界", 300), strings.Repeat("🙂", 200),
		strings.Repeat("x", 252) + "界界", strings.Repeat("x", 251) + "🙂🙂",
	} {
		got := TrafficCellPreview(original)
		if len(got) > TrafficCellPreviewBytes || !utf8.ValidString(got) || !strings.HasSuffix(got, "…") ||
			!strings.HasPrefix(original, strings.TrimSuffix(got, "…")) {
			t.Fatalf("invalid bounded preview: %q", got)
		}
	}
}

func TestTrafficPreviewDoesNotChangeFullPathOperations(t *testing.T) {
	prefix := "/" + strings.Repeat("x", 600)
	a, b := tableEntry(1, 200), tableEntry(2, 200)
	a.Path, b.Path = prefix+"z-last", prefix+"a-first"
	a.URL, b.URL = "https://example.test"+a.Path, "https://example.test"+b.Path
	if TrafficCellPreview(a.Path) != TrafficCellPreview(b.Path) {
		t.Fatal("test paths must share a preview")
	}
	if !TrafficMatchesSearch(a, "z-last") || TrafficMatchesSearch(b, "z-last") {
		t.Fatal("search lost the undisplayed suffix")
	}
	rows := NewTrafficTableSorter().Sort([]*TrafficEntry{a, b}, TrafficSort{ColumnPath, SortAsc})
	if rows[0] != b || TrafficPathLabel(a) != a.Path {
		t.Fatal("full path order or value was changed by previewing")
	}
	if TrafficCopyValue(a, CopyPath) != a.Path || TrafficCopyValue(a, CopyURL) != a.URL {
		t.Fatal("copy lost the undisplayed suffix")
	}
}
