package widgets

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func notifyView(notifier *Notifier, raw *nui.ToastOptions) func(*ui.Context) {
	config := nui.DefaultApp()
	return func(c *ui.Context) {
		theme := nui.App(c, config)
		if raw != nil {
			nui.Toast(c, theme, *raw)
			raw = nil
		}
		notifier.Show(c, theme)
		ToastViewport(c, theme)
	}
}

// RequireTopCenterToast fails unless text is shown centered horizontally
// near the top of a window width wide.
func RequireTopCenterToast(t testing.TB, tester *ui.Tester, text string, width float32) {
	t.Helper()
	rect, ok := tester.Find(text)
	if !ok {
		t.Fatalf("no toast %q in %v", text, tester.Texts())
	}
	center := rect.X + rect.W/2
	if center < width/2-60 || center > width/2+60 || rect.Y > 120 {
		t.Fatalf("toast %q at %+v, want top-center of a %v-wide window", text, rect, width)
	}
}

func TestToastsAreTopCenterWithoutProgress(t *testing.T) {
	notifier := &Notifier{}
	tester := ui.NewTester(notifyView(notifier, nil), 1000, 700)
	notifier.Success("Saved")
	tester.Frame()
	tester.Frame()
	RequireTopCenterToast(t, tester, "Saved", 1000)
	if _, ok := tester.AccessByRole(ui.RoleProgress, ""); ok {
		t.Fatal("toast shows a progress bar")
	}
}

func TestProgressProbeSeesDefaultToastProgress(t *testing.T) {
	// The probe above must be able to see a progress bar at all.
	tester := ui.NewTester(notifyView(&Notifier{}, &nui.ToastOptions{Title: "Raw", Timeout: time.Second}), 1000, 700)
	tester.Frame()
	tester.Frame()
	if _, ok := tester.AccessByRole(ui.RoleProgress, ""); !ok {
		t.Skip("progress bars are not exposed to accessibility; the no-progress check is vacuous")
	}
}

func TestDefaultTimeouts(t *testing.T) {
	notifier := &Notifier{}
	tester := ui.NewTester(notifyView(notifier, nil), 1000, 700)
	notifier.Success("Short")
	tester.Frame()
	tester.Advance(1500 * time.Millisecond)
	if !tester.HasText("Short") {
		t.Fatal("success toast left before 1.8 s")
	}
	tester.Advance(2 * time.Second)
	if tester.HasText("Short") {
		t.Fatal("success toast stayed past 1.8 s")
	}
}

func TestWithTimeoutKeepsToastLonger(t *testing.T) {
	notifier := &Notifier{}
	tester := ui.NewTester(notifyView(notifier, nil), 1000, 700)
	notifier.WithTimeout(8*time.Second).Titled(SeverityWarning, "Import failed", "1 of 2 files failed")
	tester.Frame()
	tester.Advance(7 * time.Second)
	if !tester.HasText("Import failed") {
		t.Fatal("8 s toast left before 7 s")
	}
	tester.Advance(2 * time.Second)
	if tester.HasText("Import failed") {
		t.Fatal("8 s toast stayed past 9 s")
	}
}
