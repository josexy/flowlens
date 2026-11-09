package locale

import (
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
)

func TestTagMapping(t *testing.T) {
	for _, test := range []struct{ language, want string }{
		{"", ChineseTag}, {"zh", ChineseTag}, {"fr", ChineseTag}, {"en", EnglishTag},
	} {
		if got := Tag(test.language); got != test.want {
			t.Errorf("Tag(%q) = %q, want %q", test.language, got, test.want)
		}
	}
	if Normalize("") != Chinese || Normalize("en") != English {
		t.Fatal("Normalize")
	}
}

func TestMessagesAndComponentsSwitchAtRuntime(t *testing.T) {
	current := Tag("")
	i18n.SetLocaleFunc(func() string { return current })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })

	value := "text"
	view := func(c *ui.Context) {
		theme := nui.App(c, nui.DefaultApp())
		ui.Column(c).Fill().Children(func() {
			ui.Text(c, locales.TrayClose())
			nui.Input(c.Key("field"), theme, &value, nui.InputOptions{Clearable: true})
		})
	}
	tester := ui.NewTester(view, 400, 200)
	tester.SetLocale(current)
	if !tester.HasText("关闭") {
		t.Fatalf("default language: texts %q", tester.Texts())
	}
	if _, found := tester.Find("清空"); !found {
		t.Fatalf("component text is not Chinese: %q", tester.Texts())
	}

	current = Tag("en")
	tester.SetLocale(current)
	if !tester.HasText("Close") {
		t.Fatalf("English: texts %q", tester.Texts())
	}
	if _, found := tester.Find("Clear"); !found {
		t.Fatalf("component text is not English: %q", tester.Texts())
	}
}
