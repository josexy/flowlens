package shell

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestMainWindowToastIsTopCenterWithoutProgress(t *testing.T) {
	f := newFixture(t)
	f.app.Notifier.Success("Main saved")
	f.tester.Frame()
	f.tester.Frame()
	rect, ok := f.tester.Find("Main saved")
	if !ok {
		t.Fatalf("%v", f.tester.Texts())
	}
	if center := rect.X + rect.W/2; center < 640-60 || center > 640+60 || rect.Y > 120 {
		t.Fatalf("toast at %+v, want top-center of 1280×800", rect)
	}
	if _, ok := f.tester.AccessByRole(ui.RoleProgress, ""); ok {
		t.Fatal("toast shows a progress bar")
	}
}
