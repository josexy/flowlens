package plugins

import "github.com/egoist/mygo/ui/editor"

func selectionAt(offset int) editor.Selection { return editor.Selection{Anchor: offset, Head: offset} }

func editorRequest(offset int) editor.CompletionRequest {
	return editor.CompletionRequest{Offset: offset}
}
