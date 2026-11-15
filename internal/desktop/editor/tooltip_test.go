package editor

import (
	"testing"

	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/locales"
)

func TestURLEncodedActionsUseTheirOwnCopy(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	tab.HTTP.ActiveRequestTab = logic.RequestTabBody
	tab.HTTP.RequestBodyType = logic.BodyTypeURLEncoded
	f.settle()
	for _, text := range []string{locales.WorkspaceHttpRequestUrlencodedNamePlaceholder(), locales.WorkspaceHttpRequestUrlencodedValuePlaceholder()} {
		if !f.tester.HasText(text) {
			t.Fatalf("missing %q in %v", text, f.tester.Texts())
		}
	}
	rendertest.RequireTooltip(t, f.tester, locales.WorkspaceHttpRequestUrlencodedAddRow(), locales.WorkspaceHttpRequestUrlencodedAddRow())
	rendertest.RequireTooltip(t, f.tester, locales.WorkspaceHttpRequestUrlencodedClearAll(), locales.WorkspaceHttpRequestUrlencodedClearAll())
}

func TestFileChooseShowsTooltip(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	tab.HTTP.ActiveRequestTab = logic.RequestTabBody
	tab.HTTP.RequestBodyType = logic.BodyTypeFile
	f.settle()
	rendertest.RequireTooltip(t, f.tester, locales.WorkspaceHttpRequestFileChoose(), locales.WorkspaceHttpRequestFileChoose())
}
