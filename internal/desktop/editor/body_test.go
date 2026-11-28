package editor

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/keytest"
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

func (f *fixture) keyboardCell(label, value string, finish ui.Key) {
	f.t.Helper()
	keytest.TabTo(f.t, f.tester, label)
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if !f.tester.Focused("cell-editor") {
		f.t.Fatal("keyboard did not enter cell editor")
	}
	f.tester.Type(value)
	f.tester.Key(0, finish)
	f.settle()
}

func TestHTTPGridsKeyboardEditingPreservesOrderedFields(t *testing.T) {
	f := newFake(t)
	state := f.newHTTP().HTTP
	state.Headers = []logic.EditableRow{
		{Key: "X-Duplicate", Value: "one", Enabled: true},
		{Key: "x-duplicate", Value: "", Enabled: true},
		{Key: "Disabled", Value: "keep"},
	}
	f.settle()
	f.keyboardCell(locales.WorkspaceHttpRequestHeaderValuePlaceholder()+" 2", "two", ui.KeyEnter)
	if state.Headers[0].Key != "X-Duplicate" || state.Headers[1].Key != "x-duplicate" || state.Headers[1].Value != "two" || state.Headers[2].Enabled || state.Headers[2].Value != "keep" {
		t.Fatalf("ordered headers changed: %+v", state.Headers)
	}
	keytest.TabTo(t, f.tester, locales.WorkspaceHttpRequestHeaderKeyPlaceholder()+" 3 "+locales.WorkspaceHttpRequestFormDataColEnabled())
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if !state.Headers[2].Enabled || f.tester.Focused("cell-editor") {
		t.Fatal("checkbox did not operate independently")
	}
	state.URL = "https://example.test/?a=1&a=2"
	state.ActiveRequestTab = logic.RequestTabQuery
	f.settle()
	f.keyboardCell(locales.WorkspaceHttpRequestQueryValuePlaceholder()+" 1", "changed", ui.KeyEscape)
	if state.URL != "https://example.test/?a=1&a=2" {
		t.Fatal("cancel did not restore URL", state.URL)
	}
	f.keyboardCell(locales.WorkspaceHttpRequestQueryValuePlaceholder()+" 2", "3", ui.KeyTab)
	if state.URL != "https://example.test/?a=1&a=3" {
		t.Fatal(state.URL)
	}
	state.Headers = append(state.Headers, logic.EditableRow{Key: "Cookie", Value: "a=1; b=2", Enabled: true})
	state.ActiveRequestTab = logic.RequestTabCookies
	f.settle()
	f.keyboardCell(locales.WorkspaceHttpRequestCookieValuePlaceholder()+" 2", "9", ui.KeyEnter)
	if state.Headers[len(state.Headers)-1].Value != "a=1; b=9" {
		t.Fatalf("cookie edit lost: %+v", state.Headers)
	}
	state.ActiveRequestTab, state.RequestBodyType = logic.RequestTabBody, logic.BodyTypeURLEncoded
	state.RequestBodyURLEncoded = []logic.EditableRow{{Key: "a", Value: "1", Enabled: true}, {Key: "a", Value: ""}}
	f.settle()
	f.keyboardCell(locales.WorkspaceHttpRequestUrlencodedValuePlaceholder()+" 2", "disabled draft", ui.KeyEnter)
	if state.RequestBodyURLEncoded[1].Value != "disabled draft" || state.RequestBodyURLEncoded[1].Enabled || state.RequestBodyURLEncoded[0].Value != "1" {
		t.Fatal(state.RequestBodyURLEncoded)
	}
}

func TestFormDataAndWebSocketGridKeyboardControls(t *testing.T) {
	f := newFake(t)
	state := f.newHTTP().HTTP
	state.ActiveRequestTab, state.RequestBodyType = logic.RequestTabBody, logic.BodyTypeFormData
	state.RequestBodyFormData = []logic.FormDataItem{
		{ID: "text", Enabled: true, Name: "name", ItemType: logic.FormDataItemText, Value: "one"},
		{ID: "file", Enabled: true, Name: "upload", ItemType: logic.FormDataItemFile},
	}
	f.settle()
	f.keyboardCell(locales.WorkspaceHttpRequestFormDataValuePlaceholder()+" 1", "two", ui.KeyEnter)
	keytest.TabTo(t, f.tester, locales.WorkspaceHttpRequestFormDataRowMenuUpload()+" 2")
	f.tester.Key(0, ui.KeySpace)
	f.settle()
	if state.RequestBodyFormData[1].File == nil || state.RequestBodyFormData[0].Value != "two" {
		t.Fatal("file control did not remain independent", state.RequestBodyFormData)
	}
	key := f.data.Workspace.CreateWebSocketClientTab(logic.DraftNew, nil)
	ws := f.data.Workspace.FindTab(key).WebSocket
	ws.Headers = []logic.EditableRow{{Key: "X-Test", Value: "1", Enabled: true}, {Key: "x-test", Value: "2", Enabled: true}}
	ws.ActiveLeftTab = socketTabHeaders
	f.settle()
	f.keyboardCell(locales.WorkspaceHttpRequestHeaderValuePlaceholder()+" 2", "3", ui.KeyEnter)
	if ws.Headers[0].Key != "X-Test" || ws.Headers[1].Key != "x-test" || ws.Headers[1].Value != "3" {
		t.Fatal(ws.Headers)
	}
}
