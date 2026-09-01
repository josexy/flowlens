package pythonpluginservice

import (
	"reflect"
	"testing"
)

func TestURLWildcardMatchesNormalizedFullURLAndOnlyExpandsStarQuestion(t *testing.T) {
	tests := []struct {
		pattern   string
		candidate string
		want      bool
	}{
		{"https://example.com/api/*", "HTTPS://EXAMPLE.COM/api/One?Value=Two", true},
		{"https://example.com/api/?", "https://example.com/api/x", true},
		{"https://example.com/api/?", "https://example.com/api/xy", false},
		{"https://example.com/a.b", "https://example.com/a.b", true},
		{"https://example.com/a.b", "https://example.com/axb", false},
		{"*example.com*", "https://example.com/path", true},
		{"example.com", "https://example.com/path", false},
		{"https://example.com/Path", "https://example.com/path", false},
		{"https://example.com/path?Key=*", "https://example.com/path?Key=Value", true},
		{"https://example.com/path?Key=*", "https://example.com/path?key=Value", false},
	}
	for _, test := range tests {
		matcher, err := compileURLWildcard(test.pattern)
		if err != nil {
			t.Fatalf("compile %q: %v", test.pattern, err)
		}
		if got := matcher.MatchString(normalizeMatchURL(test.candidate)); got != test.want {
			t.Errorf("pattern %q candidate %q = %v, want %v", test.pattern, test.candidate, got, test.want)
		}
	}
}

func TestURLWildcardNormalizesOnlySchemeAndAuthorityWithoutExplicitPath(t *testing.T) {
	matcher, err := compileURLWildcard("HTTPS://EXAMPLE.COM?Token=AbC*")
	if err != nil {
		t.Fatalf("compileURLWildcard: %v", err)
	}
	if !matcher.MatchString(normalizeMatchURL("https://example.com?Token=AbC123")) {
		t.Fatal("pattern did not preserve query-string case after authority normalization")
	}
	if matcher.MatchString(normalizeMatchURL("https://example.com?token=abc123")) {
		t.Fatal("query-string matching unexpectedly became case-insensitive")
	}
}

func TestMatchPluginsUsesOriginalMethodStableOrderAndDeduplicatesRules(t *testing.T) {
	plugins := []*Plugin{
		{
			ID: "first", Name: "First", Enabled: true, SortOrder: 0,
			ActiveRevision: "rev-1", ValidationStatus: ValidationStatusValid,
			Rules: []*Rule{
				{ID: "first-a", Enabled: true, Method: "GET", URLPattern: "https://example.com/*", SortOrder: 0},
				{ID: "first-b", Enabled: true, Method: "*", URLPattern: "*", SortOrder: 1},
			},
		},
		{
			ID: "second", Name: "Second", Enabled: true, SortOrder: 1,
			ActiveRevision: "rev-2", ValidationStatus: ValidationStatusValid,
			Rules: []*Rule{{ID: "second-a", Enabled: true, Method: "POST", URLPattern: "*"}},
		},
		{
			ID: "third", Name: "Third", Enabled: true, SortOrder: 2,
			ActiveRevision: "rev-3", ValidationStatus: ValidationStatusValid,
			Rules: []*Rule{{ID: "third-a", Enabled: true, Method: "*", URLPattern: "https://example.com/api"}},
		},
		{
			ID: "disabled", Enabled: false, ActiveRevision: "rev", ValidationStatus: ValidationStatusValid,
			Rules: []*Rule{{Enabled: true, Method: "*", URLPattern: "*"}},
		},
		{
			ID: "invalid", Enabled: true, ActiveRevision: "", ValidationStatus: ValidationStatusInvalid,
			Rules: []*Rule{{Enabled: true, Method: "*", URLPattern: "*"}},
		},
	}
	matched, err := matchPlugins(plugins, "GET", "https://example.com/api")
	if err != nil {
		t.Fatalf("matchPlugins: %v", err)
	}
	got := make([]string, 0, len(matched))
	for _, plugin := range matched {
		got = append(got, plugin.ID)
	}
	if want := []string{"first", "third"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("matched plugins = %#v, want %#v", got, want)
	}
}

func TestMatchPluginsRejectsInvalidStoredWildcard(t *testing.T) {
	plugins := []*Plugin{{
		ID: "plugin", Enabled: true, ActiveRevision: "rev", ValidationStatus: ValidationStatusValid,
		Rules: []*Rule{{Enabled: true, Method: "*", URLPattern: string([]byte{0xff})}},
	}}
	if _, err := matchPlugins(plugins, "GET", "https://example.com/"); err == nil {
		t.Fatal("invalid UTF-8 wildcard was accepted")
	}
}
