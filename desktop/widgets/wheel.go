package widgets

import "github.com/egoist/mygo/ui"

// HorizontalWheel makes a horizontal scroll strip, such as a row of tabs,
// scroll sideways with a vertical wheel. It takes the wheel axis that moved
// more, as FlowLens 1.0.5 did. Ctrl+wheel, a strip that does not overflow,
// and a strip already at its end in that direction leave the event to the
// content around the strip. state must track the strip's scrolling
// (TrackScroll); HorizontalWheel installs it. onScroll, when given, runs
// after the wheel scrolls the strip.
func HorizontalWheel(c *ui.Context, strip ui.Element, state *ui.ScrollState, onScroll ...func()) ui.Element {
	held := HeldModifiers(c)
	return strip.TrackScroll(state).HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll || (ev.Mods|held)&ui.Ctrl != 0 {
			return false
		}
		if !ScrollStripBy(state, dominantAxis(ev.DX, ev.DY)) {
			return false
		}
		for _, fn := range onScroll {
			fn()
		}
		return true
	})
}

// ScrollStripBy scrolls a horizontal strip by delta and reports whether it
// moved.
func ScrollStripBy(state *ui.ScrollState, delta float32) bool {
	if delta == 0 || state.MaxX <= 0 {
		return false
	}
	if (delta < 0 && state.X <= 0) || (delta > 0 && state.X >= state.MaxX) {
		return false
	}
	x := min(max(state.X+delta, 0), state.MaxX)
	if state.Scrolling() {
		// A glide the app started would carry the strip on: turn it here.
		state.ScrollTo(x, state.Y, ui.Smooth)
		return true
	}
	state.X = x
	return true
}

func dominantAxis(dx, dy float32) float32 {
	if abs32(dy) > abs32(dx) {
		return dy
	}
	return dx
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// HeldModifiers returns the modifier keys held as the frame is built. Wheel
// handlers combine it with the event's own modifiers, since a wheel event
// may not report keys held on their own.
func HeldModifiers(c *ui.Context) ui.Modifiers { return c.Modifiers() }

// WheelScroll is HorizontalWheel for a strip whose scrolling no one else
// needs: the strip keeps its own ScrollState.
func WheelScroll(c *ui.Context, strip ui.Element) ui.Element {
	state := ui.Local(strip, "flowlens-wheel-scroll", func() ui.ScrollState { return ui.ScrollState{} })
	if state == nil {
		return strip
	}
	return HorizontalWheel(c, strip, state)
}
