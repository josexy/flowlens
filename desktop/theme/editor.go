package theme

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// Editor keeps the original VS code palette on the native editor while its
// surfaces, focus, selection, and diagnostics follow the application theme.
func Editor(base *ui.Theme) *editor.Theme {
	palette := editor.DefaultTheme(base)
	set := func(kind editor.TokenKind, color string) { palette.Tokens[kind].Color = ui.Hex(color) }
	if base.Dark {
		set(editor.TokenKeyword, "#569cd6")
		set(editor.TokenType, "#4ec9b0")
		set(editor.TokenFunction, "#dcdcaa")
		set(editor.TokenProperty, "#9cdcfe")
		set(editor.TokenConstant, "#569cd6")
		set(editor.TokenNumber, "#b5cea8")
		set(editor.TokenString, "#ce9178")
		set(editor.TokenComment, "#6a9955")
		set(editor.TokenTag, "#569cd6")
		set(editor.TokenAttribute, "#9cdcfe")
		palette.ActiveLine = ui.RGBA(255, 255, 255, 0.035)
	} else {
		set(editor.TokenKeyword, "#0000ff")
		set(editor.TokenType, "#267f99")
		palette.Tokens[editor.TokenFunction].Color = base.Text
		set(editor.TokenProperty, "#a31515")
		set(editor.TokenConstant, "#0000ff")
		set(editor.TokenNumber, "#098658")
		set(editor.TokenString, "#a31515")
		set(editor.TokenComment, "#008000")
		set(editor.TokenTag, "#800000")
		set(editor.TokenAttribute, "#ff0000")
	}
	palette.Tokens[editor.TokenOperator].Color = base.Text
	return palette
}
