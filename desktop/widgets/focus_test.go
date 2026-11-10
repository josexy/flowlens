package widgets

import (
	"testing"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
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

type inlineProbe struct {
	open      bool
	value     string
	submitted []string
	canceled  int
}

func inlineView(probe *inlineProbe) func(*ui.Context) {
	config := nui.DefaultApp()
	return func(c *ui.Context) {
		theme := nui.App(c, config)
		ui.Column(c).Fill().Children(func() {
			if !probe.open {
				return
			}
			InlineInput(c, theme, &probe.value, InlineInputOptions{
				Placeholder: "Folder name",
				OnSubmit: func(text string) {
					probe.submitted = append(probe.submitted, text)
					probe.open = false
				},
				OnCancel: func() {
					probe.canceled++
					probe.open = false
				},
			})
		})
	}
}

func TestInlineInputEnterSubmits(t *testing.T) {
	probe := &inlineProbe{open: true}
	tester := ui.NewTester(inlineView(probe), 400, 200)
	tester.Frame()
	tester.Frame()
	if active, _ := tester.TextInputActive(); !active {
		t.Fatal("inline input is not focused on appearance")
	}
	tester.Type("Docs")
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if len(probe.submitted) != 1 || probe.submitted[0] != "Docs" || probe.canceled != 0 {
		t.Fatalf("submitted %q, canceled %d", probe.submitted, probe.canceled)
	}
}

func TestInlineInputEscapeCancels(t *testing.T) {
	probe := &inlineProbe{open: true}
	tester := ui.NewTester(inlineView(probe), 400, 200)
	tester.Frame()
	tester.Frame()
	tester.Type("Docs")
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	tester.Frame()
	if probe.canceled != 1 || len(probe.submitted) != 0 || probe.open {
		t.Fatalf("submitted %q, canceled %d, open %v", probe.submitted, probe.canceled, probe.open)
	}
	if tester.HasText("Docs") {
		t.Fatal("canceled input is still shown")
	}
}
