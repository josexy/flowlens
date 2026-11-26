package widgets

import "testing"

func TestReadOnlyReplacementKeepsOptionsAndCanRenderAgain(t *testing.T) {
	v := NewCodeView("json", true)
	v.SetWordWrap(true)
	v.SetText(`{"before":true}`)
	tester := codeViewTester(v)
	tester.Frame()
	v.State().Select(0, 5)
	old := v.State().Buffer
	v.SetText(`{"after":true}`)
	if v.State().Buffer == old || !v.WordWrap() || !v.State().ReadOnly || v.State().Buffer.CanUndo() {
		t.Fatal("replacement kept versions or lost options")
	}
	if v.State().SelectedText() != "" {
		t.Fatal("replacement retained the old selection")
	}
	tester.Frame()
	v.State().Select(0, 7)
	if got := v.State().SelectedText(); got != `{"after` {
		t.Fatalf("new selection %q", got)
	}
	if v.State().Buffer.Language() == nil {
		t.Fatal("replacement lost syntax language")
	}
	editable := NewCodeView("json", false)
	buffer := editable.State().Buffer
	editable.SetText(`{"editable":true}`)
	if editable.State().Buffer != buffer {
		t.Fatal("editable buffer identity changed")
	}
}
