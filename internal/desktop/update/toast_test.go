package update

import (
	"testing"

	"github.com/egoist/mygo/ui"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
)

func TestUpdateToastIsTopCenterWithoutProgress(t *testing.T) {
	f := newFixture(t, appservice.UpdatePhaseIdle, appservice.UpdateApplyModeSelf)
	f.view.Notifier.Info("Update note")
	f.tester.Frame()
	f.tester.Frame()
	rect, ok := f.tester.Find("Update note")
	if !ok {
		t.Fatalf("%v", f.tester.Texts())
	}
	if center := rect.X + rect.W/2; center < 350-60 || center > 350+60 || rect.Y > 120 {
		t.Fatalf("toast at %+v, want top-center of 700 wide", rect)
	}
	if _, ok := f.tester.AccessByRole(ui.RoleProgress, ""); ok {
		t.Fatal("toast shows a progress bar")
	}
}
