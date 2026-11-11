package widgets

import (
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

// Menu items show 1.0.5's icons where the platform's menus show item icons:
// Windows' and Linux's menu bars draw none, their context menus and macOS's
// menus do. Each icon is made once, on first use; one that cannot be made
// leaves its items without an icon.

// menuIcon makes an icon of an SVG once.
type menuIcon struct {
	once sync.Once
	icon ui.MenuIcon
	make func() *ui.SVG
	// template draws the icon in the color of the item's text.
	template bool
}

func (m *menuIcon) get() ui.MenuIcon {
	m.once.Do(func() {
		svg := m.make()
		if svg == nil {
			return
		}
		if icon, err := mygo.NewMenuIconSVG(svg, m.template); err == nil && icon != nil {
			m.icon = icon
		}
	})
	return m.icon
}

var (
	exportMenuIcon     = &menuIcon{make: lucide.FileDown, template: true}
	aliasMenuIcon      = &menuIcon{make: lucide.Tag, template: true}
	deleteMenuIcon     = &menuIcon{make: lucide.Trash, template: true}
	showAllMenuIcon    = &menuIcon{make: lucide.ListChecks, template: true}
	swatchMenuIconsMu  sync.Mutex
	swatchMenuIconsMap = map[string]*menuIcon{}
)

// ExportMenuIcon is the file-download icon of Export items.
func ExportMenuIcon() ui.MenuIcon { return exportMenuIcon.get() }

// AliasMenuIcon is the tag icon of Set Alias items.
func AliasMenuIcon() ui.MenuIcon { return aliasMenuIcon.get() }

// DeleteMenuIcon is the trash icon of Delete items.
func DeleteMenuIcon() ui.MenuIcon { return deleteMenuIcon.get() }

// ShowAllMenuIcon is the list-check icon of Show All Columns.
func ShowAllMenuIcon() ui.MenuIcon { return showAllMenuIcon.get() }

// SwatchSVG is a round swatch of color, a hex as #ef4444, as 1.0.5 drew a
// highlight color in its menu: 12 px across with a 1 px outline of 20%
// black, centered in a menu icon's 16 px box.
func SwatchSVG(color string) *ui.SVG {
	svg, err := ui.ParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">` +
		`<circle cx="8" cy="8" r="6" fill="` + color + `"/>` +
		`<circle cx="8" cy="8" r="5.5" fill="none" stroke="#000000" stroke-opacity="0.2" stroke-width="1"/></svg>`))
	if err != nil {
		return nil
	}
	return svg
}

// SwatchMenuIcon is the swatch of color, in its own colors.
func SwatchMenuIcon(color string) ui.MenuIcon {
	swatchMenuIconsMu.Lock()
	icon, ok := swatchMenuIconsMap[color]
	if !ok {
		icon = &menuIcon{make: func() *ui.SVG { return SwatchSVG(color) }}
		swatchMenuIconsMap[color] = icon
	}
	swatchMenuIconsMu.Unlock()
	return icon.get()
}
