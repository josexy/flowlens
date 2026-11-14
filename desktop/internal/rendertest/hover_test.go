package rendertest

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestRequireTooltipHovers(t *testing.T) {
	config := nui.DefaultApp()
	tester := ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		ui.Column(c).Fill().Padding(40).Children(func() {
			button := nui.Button(c, theme, "", nui.ButtonOptions{Square: true, Leading: lucide.Copy(), AccessibleLabel: "Copy it"})
			nui.Tooltip(c, theme, button, "Copy the thing", nui.TooltipOptions{})
		})
	}, 400, 300)
	if tester.HasText("Copy the thing") {
		t.Fatal("tooltip shown before hovering")
	}
	RequireTooltip(t, tester, "Copy it", "Copy the thing")
}

func TestRequireTooltipNeedsATooltip(t *testing.T) {
	config := nui.DefaultApp()
	tester := ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		ui.Column(c).Fill().Padding(40).Children(func() {
			nui.Button(c, theme, "", nui.ButtonOptions{Square: true, Leading: lucide.Copy(), AccessibleLabel: "Copy it"})
		})
	}, 400, 300)
	probe := &failureProbe{TB: t}
	func() {
		defer func() { _ = recover() }()
		RequireTooltip(probe, tester, "Copy it", "Copy it")
	}()
	if !probe.failed {
		t.Fatal("a control without a tooltip passed")
	}
}

type failureProbe struct {
	testing.TB
	failed bool
}

func (p *failureProbe) Helper() {}
func (p *failureProbe) Fatalf(string, ...any) {
	p.failed = true
	panic("stop")
}
