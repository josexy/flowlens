package settings

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSettingsToastIsTopCenterWithoutProgress(t *testing.T) {
	f := newFixture(t)
	f.notifier.Success("Settings saved here")
	f.tester.Frame()
	f.tester.Frame()
	rect, ok := f.tester.Find("Settings saved here")
	if !ok {
		t.Fatalf("%v", f.tester.Texts())
	}
	if center := rect.X + rect.W/2; center < 650-60 || center > 650+60 || rect.Y > 120 {
		t.Fatalf("toast at %+v, want top-center of 1300 wide", rect)
	}
	if _, ok := f.tester.AccessByRole(ui.RoleProgress, ""); ok {
		t.Fatal("toast shows a progress bar")
	}
}
