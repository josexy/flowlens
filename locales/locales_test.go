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

// minimumMessages guards against a conversion that loses messages.
const minimumMessages = 1374

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
	if len(english) != len(chinese) || len(english) < minimumMessages {
		t.Fatalf("messages: en %d, zh-Hans %d, want the same count and at least %d", len(english), len(chinese), minimumMessages)
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

func TestMessagesFormat(t *testing.T) {
	for _, test := range []struct{ got, want string }{
		{In("en").UpdaterUpToDateTitle(), "You're up to date"},
		{In("en").StatusTotal("7"), "Total 7 items"},
		{In("en").StatusEnvironmentMygo(), "MyGo"},
		{In("zh-Hans").TrayClose(), "关闭"},
		{In("en").SettingsRestartReasonProxyModeChanged(), readCatalog(t, "en.json")["settingsRestartReasonProxyModeChanged"]},
	} {
		if test.got != test.want {
			t.Errorf("got %q, want %q", test.got, test.want)
		}
	}
}
