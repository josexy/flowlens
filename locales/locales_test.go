package locales

import (
	"encoding/json"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var placeholderPattern = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func readCatalog(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	messages := map[string]string{}
	for key, value := range raw {
		if strings.HasPrefix(key, "@") {
			continue
		}
		messages[key] = value.(string)
	}
	return messages
}

func placeholders(message string) []string {
	set := map[string]bool{}
	for _, match := range placeholderPattern.FindAllStringSubmatch(message, -1) {
		set[match[1]] = true
	}
	return slices.Sorted(maps.Keys(set))
}

func TestCatalogsHaveEveryMessageWithMatchingPlaceholders(t *testing.T) {
	english := readCatalog(t, "en.json")
	chinese := readCatalog(t, "zh-Hans.json")
	if len(english) != len(chinese) {
		t.Fatalf("messages: en %d, zh-Hans %d, want the same count", len(english), len(chinese))
	}
	for key, message := range english {
		translated, ok := chinese[key]
		if !ok {
			t.Errorf("%s: missing from zh-Hans", key)
			continue
		}
		if !slices.Equal(placeholders(message), placeholders(translated)) {
			t.Errorf("%s: placeholders differ: %q and %q", key, message, translated)
		}
	}
}
