package widgets

import (
	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go"
)

// AutoFocus returns an InputOptions.Style that gives the input's editor the
// keyboard focus in the frame the input appears, so a field revealed by an
// action takes typing without a click.
func AutoFocus() func(root, editor, leading, trailing ui.Element) {
	return WithAutoFocus(nil)
}

// WithAutoFocus wraps style, which may be nil, to also focus the editor when
// the input appears.
func WithAutoFocus(style func(root, editor, leading, trailing ui.Element)) func(root, editor, leading, trailing ui.Element) {
	return func(root, editor, leading, trailing ui.Element) {
		editor.AutoFocus()
		if style != nil {
			style(root, editor, leading, trailing)
		}
	}
}

// InlineInputOptions configures an InlineInput.
type InlineInputOptions struct {
	// Label names the input for assistive technology.
	Label       string
	Placeholder string
	Size        nui.Size
	Variant     nui.Variant
	Invalid     bool
	MaxLength   int
	// OnSubmit receives the text when Enter is pressed.
	OnSubmit func(string)
	// OnCancel runs when Escape is pressed. The text is not submitted.
	OnCancel func()
	Style    func(root, editor, leading, trailing ui.Element)
}

// InlineInput is a single-field editor shown in place, such as a new folder's
// name in a tree. It takes the focus when it appears; Enter submits and
// Escape cancels. Escape is a control behavior, like Enter, and is not an
// application command.
func InlineInput(c *ui.Context, theme nui.Theme, value *string, o InlineInputOptions) ui.Element {
	cancel := false
	element := nui.Input(c, theme, value, nui.InputOptions{
		Label:       o.Label,
		Placeholder: o.Placeholder,
		Size:        o.Size,
		Variant:     o.Variant,
		Invalid:     o.Invalid,
		MaxLength:   o.MaxLength,
		OnSubmit:    o.OnSubmit,
		Style: WithAutoFocus(func(root, editor, leading, trailing ui.Element) {
			if editor.Shortcut(0, ui.KeyEscape) {
				cancel = true
			}
			if o.Style != nil {
				o.Style(root, editor, leading, trailing)
			}
		}),
	})
	if cancel && o.OnCancel != nil {
		o.OnCancel()
	}
	return element
}
