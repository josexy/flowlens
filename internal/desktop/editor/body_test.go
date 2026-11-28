package editor

import (
	"slices"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/locales"
)

func TestBodyTypeMenuOrder(t *testing.T) {
	f := newFake(t)
	tab := f.newHTTP()
	tab.HTTP.ActiveRequestTab = logic.RequestTabBody
	f.settle()
	if err := f.tester.Click(locales.WorkspaceHttpRequestBodyTypeNone()); err != nil {
		t.Fatalf("%v: %v", err, f.tester.Texts())
	}
	f.tester.Advance(time.Second)
	f.settle()
	want := []string{
		locales.WorkspaceHttpRequestBodyTypeNone(), locales.WorkspaceHttpRequestBodyTypeJson(), locales.WorkspaceHttpRequestBodyTypeText(),
		locales.WorkspaceHttpRequestBodyTypeXml(), locales.WorkspaceHttpRequestBodyTypeFile(), locales.WorkspaceHttpRequestBodyTypeFormData(),
		locales.WorkspaceHttpRequestBodyTypeUrlencoded(),
	}
	// The options follow the trigger's own label in the texts.
	texts := f.tester.Texts()
	var shown []string
	for _, text := range texts {
		if slices.Contains(want, text) && (len(shown) == 0 || shown[len(shown)-1] != text) {
			shown = append(shown, text)
		}
	}
	if len(shown) < len(want) || !slices.Equal(shown[len(shown)-len(want):], want) {
		t.Fatalf("options %v, want %v (texts %v)", shown, want, texts)
	}
}

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
