package widgets

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/josexy/flowlens/internal/desktop/theme"
)

// Above these sizes, read-only text is shown plain: wrapped capture payloads
// are otherwise costly to highlight and lay out.
const (
	LargeTextThreshold = 512 * 1024
	LongLineThreshold  = 128 * 1024
)

// RequiresPlainText reports whether text is too large to highlight.
func RequiresPlainText(text string) bool {
	if ui.UTF16Len(text) >= LargeTextThreshold {
		return true
	}
	if len(text) < LongLineThreshold {
		return false
	}
	for line := range strings.SplitSeq(text, "\n") {
		if ui.UTF16Len(strings.TrimSuffix(line, "\r")) >= LongLineThreshold {
			return true
		}
	}
	return false
}

// CodeView shows or edits text with syntax highlighting. It belongs to one
// window, because it keeps scroll position and selections.
type CodeView struct {
	state    editor.State
	language string
	version  uint64
	large    bool

	// OnChange receives the text after the user edits it.
	OnChange func(text string)
}

// NewCodeView returns an empty view of a language, named as the old editor
// named it: json, xml, html, javascript, css, python, and so on.
func NewCodeView(language string, readOnly bool) *CodeView {
	view := &CodeView{language: language}
	view.state.Buffer = editor.NewBuffer("")
	view.state.ReadOnly = readOnly
	// As 1.0.5's Monaco: scrolling stops at the last line.
	view.state.NoScrollPastEnd = true
	view.applyLanguage()
	return view
}

func languageByID(id string) *editor.Language {
	names := map[string]string{
		"json": "JSON", "xml": "XML", "html": "HTML", "javascript": "JavaScript", "typescript": "TypeScript",
		"css": "CSS", "python": "Python", "yaml": "YAML", "markdown": "Markdown", "shell": "Shell",
	}
	if name, ok := names[strings.ToLower(id)]; ok {
		return editor.LanguageNamed(name)
	}
	return nil
}

func (v *CodeView) applyLanguage() {
	// Read-only text shows no whitespace markers, even when selected.
	if v.state.ReadOnly {
		v.state.Whitespace = editor.WhitespaceNone
	} else {
		v.state.Whitespace = editor.WhitespaceSelection
	}
	if v.state.ReadOnly && v.large {
		v.state.Buffer.SetLanguage(nil)
		v.state.NoFolding = true
		return
	}
	v.state.NoFolding = false
	v.state.Buffer.SetLanguage(languageByID(v.language))
}

// SetText replaces the text, without an undo step and without OnChange.
func (v *CodeView) SetText(text string) {
	v.large = RequiresPlainText(text)
	v.state.Buffer.Reset(text)
	v.version = v.state.Buffer.Version()
	v.applyLanguage()
}

// AppendText adds text at the end, as a stream delivers it.
func (v *CodeView) AppendText(text string) {
	if text == "" {
		return
	}
	buffer := v.state.Buffer
	buffer.Edit(editor.Edit{Start: buffer.Len(), End: buffer.Len(), Text: text})
	v.version = buffer.Version()
	if !v.large && buffer.Len() >= LongLineThreshold && RequiresPlainText(buffer.String()) {
		v.large = true
		v.applyLanguage()
	}
}

// Text returns the current text.
func (v *CodeView) Text() string { return v.state.Buffer.String() }

// SetLanguage changes the highlighting language.
func (v *CodeView) SetLanguage(language string) {
	if v.language != language {
		v.language = language
		v.applyLanguage()
	}
}

// SetReadOnly changes whether the text can be edited.
func (v *CodeView) SetReadOnly(readOnly bool) {
	if v.state.ReadOnly != readOnly {
		v.state.ReadOnly = readOnly
		v.applyLanguage()
	}
}

// SetWordWrap changes whether long lines wrap.
func (v *CodeView) SetWordWrap(wrap bool) { v.state.SoftWrap = wrap }

// WordWrap reports whether long lines wrap.
func (v *CodeView) WordWrap() bool { return v.state.SoftWrap }

// PlainText reports whether the text is shown without highlighting because of
// its size.
func (v *CodeView) PlainText() bool { return v.state.ReadOnly && v.large }

// State returns the editor state, for options such as completions.
func (v *CodeView) State() *editor.State { return &v.state }

// Build fills its parent with the framed view and reports edits to OnChange.
// It returns the editor itself so focus and accessibility stay on the input.
func (v *CodeView) Build(c *ui.Context, font ui.Font) ui.Element {
	var element ui.Element
	// The native editor paints its background over its own box.
	ui.Box(c).Fill().MinWidth(0).MinHeight(0).Border(1, c.Theme().Border).Clip().Children(func() {
		element = v.BuildUnframed(c, font)
	})
	return element
}

// BuildUnframed fills a container that supplies its own border.
func (v *CodeView) BuildUnframed(c *ui.Context, font ui.Font) ui.Element {
	v.state.Font = font
	v.state.LineHeight = 18.0 / 13.0
	v.state.Theme = theme.Editor(c.Theme())
	element := editor.View(c, &v.state).Fill().MinWidth(0).MinHeight(0)
	if version := v.state.Buffer.Version(); version != v.version {
		v.version = version
		if v.OnChange != nil && !v.state.ReadOnly {
			v.OnChange(v.state.Buffer.String())
		}
	}
	return element
}
