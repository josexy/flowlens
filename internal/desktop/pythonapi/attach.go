package pythonapi

import (
	"github.com/egoist/mygo/ui/editor"
	"github.com/josexy/flowlens/internal/desktop/widgets"
)

func completionKind(kind string) editor.CompletionKind {
	switch kind {
	case "method":
		return editor.CompletionMethod
	case "property":
		return editor.CompletionField
	case "class":
		return editor.CompletionType
	case "snippet":
		return editor.CompletionSnippet
	case "variable":
		return editor.CompletionVariable
	default:
		return editor.CompletionText
	}
}

func editorCompletions(items []Completion) []editor.Completion {
	result := make([]editor.Completion, 0, len(items))
	for _, item := range items {
		completion := editor.Completion{Label: item.Name, Detail: item.Detail, Kind: completionKind(item.Kind), Insert: item.InsertText, Documentation: item.Documentation}
		if item.Snippet {
			if snippet, err := editor.ParseSnippet(item.InsertText); err == nil {
				completion.Snippet = snippet
			}
		}
		result = append(result, completion)
	}
	return result
}

// Attach gives a Python editor completions, hovers, and signature help.
func Attach(view *widgets.CodeView) {
	state := view.State()
	state.OnCompletion = func(request editor.CompletionRequest) {
		result := Completions(view.Text(), request.Offset)
		state.ShowCompletions(request.Offset-result.ReplaceLength, editorCompletions(result.Items))
	}
	state.OnHover = func(request editor.HoverRequest) {
		text := view.Text()
		if hover := HoverAt(text, request.Offset); hover != nil {
			state.ShowHover(request.Offset, request.Offset, hover.Signature+"\n\n"+hover.Documentation, nil)
			return
		}
		if signature := SignatureAt(text, request.Offset); signature != nil {
			state.ShowHover(request.Offset, request.Offset, signature.Label+"\n\n"+signature.Documentation, nil)
		}
	}
	previous := view.OnChange
	view.OnChange = func(text string) {
		if previous != nil {
			previous(text)
		}
		ShowSignatureHelp(state, text)
	}
}

// ShowSignatureHelp shows the signature of the call being typed.
func ShowSignatureHelp(state *editor.State, text string) {
	caret := state.Selection().Head
	if caret <= 0 || caret > len(text) || (text[caret-1] != '(' && text[caret-1] != ',') {
		return
	}
	if signature := SignatureAt(text, caret); signature != nil {
		state.ShowHover(caret, caret, signature.Label+"\n\n"+signature.Documentation, nil)
	}
}
