package settings

import (
	"strings"
	"testing"

	"github.com/josexy/flowlens/locales"
)

func TestGeneralSettingsOfferNoCursorOption(t *testing.T) {
	f := newFixture(t)
	f.open(locales.SettingsTabGeneral())
	if !f.has(locales.SettingsAppFontLabel()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	for _, text := range f.tester.Texts() {
		lower := strings.ToLower(text)
		if strings.Contains(lower, "pointer cursor") || strings.Contains(text, "指针光标") {
			t.Fatalf("General settings show %q", text)
		}
	}
}
