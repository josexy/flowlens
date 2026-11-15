package capture

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAliasDialogTakesTypingWithoutClick(t *testing.T) {
	f := newFixture(t)
	f.workspace.ShowAliasDialog()
	f.settle()
	f.tester.Key(ui.Ctrl, ui.KeyA)
	f.tester.Type("session")
	f.settle()
	if f.workspace.alias.draft != "session" {
		t.Fatalf("draft = %q", f.workspace.alias.draft)
	}
}

func TestDomainFilterAddedRowTakesTyping(t *testing.T) {
	f := newFixture(t)
	f.click("Domain filters: 0")
	dialog := &f.workspace.toolbar.hosts
	dialog.add(&dialog.include)
	f.settle()
	f.tester.Type("a.test")
	f.settle()
	if len(dialog.include) != 1 || dialog.include[0].value != "a.test" {
		t.Fatalf("%+v", dialog.include)
	}
	dialog.add(&dialog.exclude)
	f.settle()
	f.tester.Type("b.test")
	f.settle()
	if dialog.include[0].value != "a.test" || dialog.exclude[0].value != "b.test" {
		t.Fatalf("%+v %+v", dialog.include, dialog.exclude)
	}
}
