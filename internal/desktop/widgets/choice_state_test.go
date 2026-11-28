package widgets

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/menutest"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func TestSelectSubmissionSurvivesFramesAndLocaleChanges(t *testing.T) {
	var state SelectState[string]
	value, builds := "a", 0
	tester := ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, nui.DefaultApp())
		locale := c.Locale()
		options := state.Prepare(locale, func() []nui.Option[string] {
			builds++
			return []nui.Option[string]{{Value: "a", Label: "Alpha " + locale}, {Value: "b", Label: "Beta " + locale}}
		})
		nui.Select(c, theme, &value, options, nui.SelectOptions{Label: "Choice"}).Width(220)
	}, 400, 300)
	tester.SetLocale("en")
	before := builds
	if err := tester.Click("Choice"); err != nil {
		t.Fatal(err)
	}
	for range 6 {
		tester.Frame()
	}
	if builds != before || !state.state.Open {
		t.Fatal("opening or idle frames resubmitted options")
	}
	tester.SetLocale("zh-Hans")
	tester.Advance(time.Second)
	if builds != before+1 || !state.state.Open || value != "a" {
		t.Fatal("locale update lost popup/selection or resubmitted repeatedly")
	}
	if err := tester.Click("Beta zh-Hans"); err != nil {
		t.Fatal(err)
	}
	if value != "b" {
		t.Fatal("refreshed options did not update the bound value")
	}
}

func TestContextMenuSubmissionTracksActionsAndTargetChanges(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			var state nui.MenuState
			target, chosen, builds, checked := 1, 0, 0, false
			tester := ui.NewTester(func(c *ui.Context) {
				theme := nui.App(c, nui.DefaultApp())
				anchor := ui.ButtonBase(c.Key("anchor")).Label("Actions").Size(140, 36)
				ContextMenu(c, theme, anchor, &state, func() []nui.MenuItem {
					builds++
					current := target
					return []nui.MenuItem{
						{ID: "check", Label: "Check", Checked: nui.Set(checked), Close: nui.Set(false), OnCheckedChange: func(on bool) { checked = on }},
						{ID: "apply", Label: "Apply", OnSelect: func() { chosen = current }},
					}
				}, nui.MenuOptions{}, target)
			}, 400, 300)
			tester.SetDark(dark)
			settle := func() { tester.Advance(time.Second) }
			if builds != 0 {
				t.Fatal("closed context menu built its items")
			}
			if err := menutest.Open(tester, settle, "Actions"); err != nil {
				t.Fatal(err)
			}
			before := builds
			for range 6 {
				tester.Frame()
			}
			if builds != before {
				t.Fatal("idle open menu resubmitted items")
			}
			if err := menutest.Choose(tester, settle, "Check"); err != nil {
				t.Fatal(err)
			}
			if item, ok := menutest.Item(tester, "Check"); !ok || !item.Checked || !checked || !state.Open {
				t.Fatal("keep-open checkbox did not refresh")
			}
			target = 2 // Same item labels, different callback target.
			settle()
			if err := menutest.Choose(tester, settle, "Apply"); err != nil {
				t.Fatal(err)
			}
			if chosen != 2 || state.Open {
				t.Fatal("action retained an obsolete target or failed to close")
			}
		})
	}
}

func TestDropdownFirstArrowUpOpensAtLastItem(t *testing.T) {
	var state nui.MenuState
	chosen, builds := "", 0
	tester := ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, nui.DefaultApp())
		anchor := nui.Button(c.Key("anchor"), theme, "Actions", nui.ButtonOptions{})
		DropdownMenu(c, theme, anchor, &state, func() []nui.MenuItem {
			builds++
			return []nui.MenuItem{
				{ID: "first", Label: "First", OnSelect: func() { chosen = "first" }},
				{ID: "last", Label: "Last", OnSelect: func() { chosen = "last" }},
			}
		}, nui.MenuOptions{})
	}, 400, 300)
	tester.Key(0, ui.KeyTab)
	tester.Key(0, ui.KeyUp)
	tester.Advance(time.Second)
	if builds != 1 {
		t.Fatalf("opening resubmitted unchanged items %d times", builds)
	}
	tester.Key(0, ui.KeyEnter)
	tester.Advance(time.Second)
	if chosen != "last" {
		t.Fatalf("first ArrowUp chose %q", chosen)
	}
}
