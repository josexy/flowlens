package widgets

import (
	"testing"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestGroupedNumberPreservesNativeEditing(t *testing.T) {
	value, state := nui.Set[float64](5000), nui.NumberState{}
	tester := ui.NewTester(func(c *ui.Context) {
		palette := nui.App(c, nui.AppConfig{})
		ui.Column(c).Children(func() {
			InputNumber(c.Key("timeout"), palette, &value, &state, nui.InputNumberOptions{InputOptions: nui.InputOptions{Label: "Timeout"}, Orientation: nui.Vertical, Min: nui.Set[float64](100), Max: nui.Set[float64](60000), Step: 100})
			nui.Button(c, palette, "Done", nui.ButtonOptions{})
		})
	}, 400, 200)
	tester.Frame()
	if !tester.HasText("5,000") || state.Text != "5000" || value.Value != 5000 {
		t.Fatalf("grouping changed native value: %v %q %v", tester.Texts(), state.Text, value)
	}
	if err := tester.Click("Timeout"); err != nil {
		t.Fatal(err)
	}
	if tester.HasText("5,000") {
		t.Fatal("grouped overlay obscures editing")
	}
	tester.Key(ui.Ctrl, ui.KeyA)
	tester.Type("5100")
	if value.Value != 5100 {
		t.Fatalf("edited value %v", value)
	}
	if err := tester.Click("Done"); err != nil {
		t.Fatal(err)
	}
	if !tester.HasText("5,100") {
		t.Fatalf("no grouped result: %v", tester.Texts())
	}
	if err := tester.Click("Timeout"); err != nil {
		t.Fatal(err)
	}
	tester.Key(ui.Ctrl, ui.KeyA)
	tester.Type("-")
	if err := tester.Click("Done"); err != nil {
		t.Fatal(err)
	}
	if tester.HasText("5,100") || state.Text != "-" || state.Error == "" {
		t.Fatalf("invalid draft was hidden: %q %q %v", state.Text, state.Error, tester.Texts())
	}
}
