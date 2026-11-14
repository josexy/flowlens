package widgets

import (
	"math"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// Splitter keeps Nuxt's resize controls and draws its indicator one device pixel wide.
func Splitter(c *ui.Context, theme nui.Theme, value *float32, options nui.SplitterOptions, first, second func()) ui.Element {
	stacked := options.Vertical || options.Orientation == nui.Vertical
	color := theme.Border
	style := options.Style
	options.Style = func(slot nui.Slot, element ui.Element) {
		switch slot {
		case nui.Root:
			// The reserved layout gap can cover more than one device pixel.
			// Fill it with the same surface as the adjoining panes.
			element.Background(theme.Background)
		case nui.SeparatorSlot:
			button, pressed := element.PressedButton()
			if !element.IsDisabled() && (element.Hovered() || pressed && button == 0) {
				color = theme.Primary
			}
		case nui.Indicator:
			element.Material(splitterLine{color: color, stacked: stacked})
		}
		if style != nil {
			style(slot, element)
		}
	}
	return nui.Splitter(c, theme, value, options, first, second)
}

type splitterLine struct {
	color   ui.Color
	stacked bool
}

func (line splitterLine) PaintMaterial(p *ui.Painter, bounds ui.Rect, _ [4]float32) {
	scale := p.Scale()
	center := func(start, size float32) float32 {
		return float32(math.Round(float64((start+size/2)*scale-.5))) / scale
	}
	if line.stacked {
		bounds.Y, bounds.H = center(bounds.Y, bounds.H), 1/scale
	} else {
		bounds.X, bounds.W = center(bounds.X, bounds.W), 1/scale
	}
	p.Fill(bounds, line.color, 0)
}
