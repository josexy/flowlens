package widgets

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

// The detail dialogs of SSE messages, WebSocket messages and Python console
// entries share 1.0.5's frame: min(960px, 92vw) wide, with a header row and
// a bordered content area min(70vh, 720px) tall.
const (
	detailDialogMaxWidth     = 960
	detailDialogWidthShare   = 0.92
	detailContentMaxHeight   = 720
	detailContentHeightShare = 0.7
)

// DetailDialogSize returns the width of a detail dialog and the height of
// its content area in a window of width × height.
func DetailDialogSize(width, height float32) (dialog, content float32) {
	return min(detailDialogMaxWidth, width*detailDialogWidthShare), min(height*detailContentHeightShare, detailContentMaxHeight)
}

// DetailAction is an icon action of a detail dialog: a ghost button with a
// tooltip, drawn in the primary color while Pressed.
type DetailAction struct {
	Key   string
	Icon  *ui.SVG
	Label string
	// Toggle marks an action that is on or off, as Wrap: Pressed tells
	// assistive technology which.
	Toggle   bool
	Pressed  bool
	Disabled bool
	Run      func()
}

// DetailWrap is the Wrap of a detail dialog. It is off each time the dialog
// opens, as each 1.0.5 dialog's wrap flag was.
type DetailWrap struct {
	on, shown bool
}

// On reports whether the dialog wraps its text.
func (w *DetailWrap) On() bool { return w.on }

// Action returns the Wrap action, named label.
func (w *DetailWrap) Action(label string) DetailAction {
	return DetailAction{Key: "wrap", Icon: lucide.TextWrap(), Label: label, Toggle: true, Pressed: w.on, Run: func() { w.on = !w.on }}
}

// track turns wrap off when the dialog opens.
func (w *DetailWrap) track(open bool) {
	if open && !w.shown {
		w.on = false
	}
	w.shown = open
}

// DetailDialogOptions describe a detail dialog.
type DetailDialogOptions struct {
	Title string
	// Lead, when set, builds a row above the header row, as the badges of an
	// SSE message.
	Lead func(c *ui.Context, theme nui.Theme)
	// Header builds the start of the header row, before the actions.
	Header func(c *ui.Context, theme nui.Theme)
	// Actions end the header row.
	Actions []DetailAction
	// Wrap, when set, is turned off as the dialog opens.
	Wrap *DetailWrap
	// SquareContent leaves the content area's corners square, as 1.0.5's
	// SSE message detail had them.
	SquareContent bool
	// Content fills the content area.
	Content func(c *ui.Context, theme nui.Theme)
}

// DetailDialog builds a detail dialog. Call it on every build, open or not,
// so that Wrap starts off each time it opens.
func DetailDialog(c *ui.Context, theme nui.Theme, open *bool, o DetailDialogOptions) ui.Element {
	if o.Wrap != nil {
		o.Wrap.track(*open)
	}
	windowWidth, windowHeight := c.Size()
	width, height := DetailDialogSize(windowWidth, windowHeight)
	return nui.ModalWithOptions(c, theme, open, nui.ModalOptions{Title: o.Title, Width: nui.Set(width)}, func(palette nui.Theme) {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			if o.Lead != nil {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() { o.Lead(c, palette) })
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12).Children(func() {
				ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(8).Children(func() {
					if o.Header != nil {
						o.Header(c, palette)
					}
				})
				ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(4).Children(func() {
					for _, action := range o.Actions {
						detailActionButton(c, palette, action)
					}
				})
			})
			box := ui.Column(c.Key("detail-content")).FillWidth().Height(height).Shrink(0).MinHeight(0).Clip()
			box.Border(1, palette.Border).Background(palette.Elevated)
			if !o.SquareContent {
				box.Radius(8)
			}
			box.Children(func() {
				if o.Content != nil {
					o.Content(c, palette)
				}
			})
		})
	})
}

func detailActionButton(c *ui.Context, theme nui.Theme, action DetailAction) {
	color := nui.Neutral
	if action.Pressed {
		color = nui.Primary
	}
	button := nui.Button(c.Key(action.Key), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: color, Size: nui.SM, Square: true, Leading: action.Icon, AccessibleLabel: action.Label, Disabled: action.Disabled})
	if action.Toggle {
		button.Checked(action.Pressed)
	}
	if !action.Disabled && action.Run != nil {
		button.OnClick(action.Run)
	}
	nui.Tooltip(c, theme, button, action.Label, nui.TooltipOptions{})
}
