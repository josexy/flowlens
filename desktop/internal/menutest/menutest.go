// Package menutest drives the context menus drawn in the window
// (nui.ContextMenu) in tests, through what assistive technology sees of them.
package menutest

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// itemRoles are the roles of a menu's items.
var itemRoles = []ui.Role{ui.RoleMenuItem, ui.RoleMenuItemCheckBox, ui.RoleMenuItemRadio}

// Item returns the first item labeled label of the menus on show.
func Item(tester *ui.Tester, label string) (ui.AccessInfo, bool) {
	for _, role := range itemRoles {
		if item, ok := tester.AccessByRole(role, label); ok {
			return item, true
		}
	}
	return ui.AccessInfo{}, false
}

// Shown reports whether a menu is on show.
func Shown(tester *ui.Tester) bool {
	_, ok := tester.AccessByRole(ui.RoleMenu, "")
	return ok
}

// Open right-clicks the element showing text or labeled label, settles, and
// reports whether a menu is on show.
func Open(tester *ui.Tester, settle func(), label string) error {
	if err := tester.RightClick(label); err != nil {
		return err
	}
	settle()
	if !Shown(tester) {
		return fmt.Errorf("menutest: right-clicking %q shows no menu", label)
	}
	return nil
}

// Choose clicks the items of path in turn: the first in the menu on show,
// each other in the submenu the one before it opens. It fails, choosing
// nothing more, on an item missing or disabled.
func Choose(tester *ui.Tester, settle func(), path ...string) error {
	if len(path) == 0 {
		return fmt.Errorf("menutest: no item to choose")
	}
	for index, label := range path {
		item, ok := Item(tester, label)
		switch {
		case !ok:
			return fmt.Errorf("menutest: the menu has no item %q", strings.Join(path[:index+1], " > "))
		case item.Disabled:
			return fmt.Errorf("menutest: the item %q is disabled", strings.Join(path[:index+1], " > "))
		}
		tester.ClickAt(item.Bounds.X+item.Bounds.W/2, item.Bounds.Y+item.Bounds.H/2)
		settle()
	}
	return nil
}

// Close presses Escape until no menu is on show, one submenu at a time.
func Close(tester *ui.Tester, settle func()) {
	for range 8 {
		if !Shown(tester) {
			return
		}
		tester.Key(0, ui.KeyEscape)
		settle()
	}
}
