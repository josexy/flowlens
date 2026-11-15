package inspect

import (
	"testing"

	"github.com/josexy/flowlens/internal/desktop/widgets"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

func TestLongHeaderOpensReadOnlyEditor(t *testing.T) {
	entry := renderEntry()
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{})
	f.panel.requestTab = tabRequestHeaders
	f.settle()
	agent := entry.Request.HeaderFields[2].Value
	accept, ok := f.tester.Find("accept")
	if !ok {
		t.Fatalf("%v", f.tester.Texts())
	}
	value, _ := f.tester.Find(agent)
	if value.H > widgets.FieldGridRowHeight {
		t.Fatalf("user-agent row is %v tall", value.H)
	}
	f.tester.ClickAt(value.X+4, value.Y+value.H/2)
	f.settle()
	editor, ok := f.tester.Access("cell-editor")
	if !ok || editor.Value != agent {
		t.Fatalf("editor %+v", editor)
	}
	if !f.tester.Focused("cell-editor") {
		t.Fatal("editor not focused")
	}
	if after, _ := f.tester.Find("accept"); after.Y != accept.Y {
		t.Fatalf("rows moved from %v to %v", accept.Y, after.Y)
	}
}
