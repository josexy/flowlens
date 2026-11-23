package widgets

import "github.com/egoist/mygo/ui"

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
