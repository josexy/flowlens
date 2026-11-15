package capture

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// Both controls use Nuxt's MD single-line recipe: a 20-DIP line and 6-DIP
// vertical padding. Listbox also has a 4-DIP inset at each end.
func categoryContentHeight(theme nui.Theme, count int, inset float32) float32 {
	density, scale, font := theme.Density, theme.TextScale, theme.FontSize
	if density <= 0 {
		density = 1
	}
	if scale <= 0 {
		scale = 1
	}
	if font <= 0 {
		font = 14
	}
	line := 20 * font / 14 * scale
	if count == 0 {
		return line + 48*density
	}
	return float32(count)*(line+12*density) + inset*density
}

// categoryViewport gives a native Listbox or Tree its full place in the outer
// scroll area while building only a window of rows. The native list offset is
// the window's offset within that place, not an independent user scroll area.
type categoryViewport struct {
	anchor                                        ui.Handle
	list                                          ui.ScrollState
	offset, outerY, contentHeight, viewportHeight float32
	bounds                                        ui.Rect
}

func (v *categoryViewport) style(slot nui.Slot, element ui.Element) {
	if slot == nui.List {
		element.TrackScroll(&v.list).Scrollbars(false)
	}
}

func (v *categoryViewport) build(c *ui.Context, outer *ui.ScrollState, contentHeight float32, content func(float32)) {
	bounds, viewport := v.anchor.Bounds(c), outer.Bounds(c)
	height := min(contentHeight, max(float32(32), viewport.H))
	// Native keyboard navigation reveals rows in the list. Apply that movement
	// to the shared scroller; resizing and changed data only recalculate layout.
	if v.contentHeight == contentHeight && v.viewportHeight == height && v.list.FocusWithin(c) {
		outer.Y = min(outer.MaxY, max(float32(0), outer.Y+v.list.Y-v.offset))
	}
	top := min(max(float32(0), viewport.Y-bounds.Y+outer.Y-v.outerY), max(float32(0), contentHeight-height))
	if bounds.H == 0 {
		top = 0
	}
	if bounds != v.bounds || contentHeight != v.contentHeight || height != v.viewportHeight || outer.Y != v.outerY {
		// Read the committed positions again after changed groups have laid out.
		c.After(0)
	}
	v.list.Y, v.offset, v.outerY = top, top, outer.Y
	v.bounds, v.contentHeight, v.viewportHeight = bounds, contentHeight, height
	ui.Box(c).FillWidth().Height(contentHeight).Shrink(0).Bind(&v.anchor).HandleInput(func(event ui.InputEvent) bool {
		if event.Kind != ui.InputScroll || event.DY == 0 || event.Mods&(ui.Ctrl|ui.Super|ui.Shift) != 0 {
			return false
		}
		outer.Y = min(outer.MaxY, max(float32(0), outer.Y+event.DY))
		return true
	}).Children(func() {
		ui.Box(c.Key("viewport")).Absolute().Top(top).Left(0).Right(0).Height(height).Children(func() {
			content(height)
		})
	})
}
