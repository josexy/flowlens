// Package keytest exercises keyboard entry paths without assigning focus
// directly, so tests catch controls that only respond to pointer clicks.
package keytest

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TabTo reaches label through the window's focus order. Shift traverses it
// backwards, without crossing a code editor that keeps Tab for indentation.
func TabTo(t testing.TB, tester *ui.Tester, label string, modifiers ...ui.Modifiers) {
	t.Helper()
	mods := ui.Modifiers(0)
	if len(modifiers) > 0 {
		mods = modifiers[0]
	}
	for range 256 {
		// A group or virtual-list wrapper can share its child's name.
		// Match the actual control, never a focused ancestor of a switch.
		for _, role := range []ui.Role{ui.RoleButton, ui.RoleCheckBox, ui.RoleSwitch, ui.RoleListItem} {
			if control, ok := tester.AccessByRole(role, label); ok && control.Focused {
				return
			}
		}
		tester.Key(mods, ui.KeyTab)
	}
	if control, ok := tester.Access(label); ok {
		t.Logf("first accessible %q: %+v", label, control)
	}
	t.Fatalf("keyboard focus cannot reach %q", label)
}
