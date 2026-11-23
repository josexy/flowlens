package widgets

import (
	"testing"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestAutoFocusFocusesRevealedInput(t *testing.T) {
	config := nui.DefaultApp()
	shown, value := false, ""
	tester := ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		ui.Column(c).Fill().Children(func() {
			if nui.Button(c, theme, "Reveal", nui.ButtonOptions{}).Clicked() {
				shown = true
			}
			if shown {
				nui.Input(c, theme, &value, nui.InputOptions{Placeholder: "Name", Style: AutoFocus()})
			}
		})
	}, 400, 200)
	tester.Frame()
	if err := tester.Click("Reveal"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Frame()
	if active, _ := tester.TextInputActive(); !active {
		t.Fatal("revealed input does not have the focus")
	}
	tester.Type("abc")
	tester.Frame()
	if value != "abc" {
		t.Fatalf("value = %q", value)
	}
}
