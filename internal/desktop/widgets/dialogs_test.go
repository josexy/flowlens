package widgets

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func dialogView(dialog *ConfirmDialog, notifier *Notifier) func(*ui.Context) {
	config := nui.DefaultApp()
	return func(c *ui.Context) {
		theme := nui.App(c, config)
		notifier.Show(c, theme)
		nui.ToastViewport(c, theme, nui.ToastViewportOptions{})
		dialog.Build(c, theme)
	}
}

func deleteRequest(result *string) ConfirmRequest {
	return ConfirmRequest{
		Title:       "Delete",
		Message:     "Delete this?",
		Confirm:     ConfirmAction{Label: "Yes", Color: nui.Error, Run: func() { *result = "confirm" }},
		CancelLabel: "No",
		OnCancel:    func() { *result = "cancel" },
	}
}

func TestConfirmRunsConfirmAction(t *testing.T) {
	dialog, result := &ConfirmDialog{}, ""
	tester := ui.NewTester(dialogView(dialog, &Notifier{}), 800, 600)
	dialog.Ask(deleteRequest(&result))
	tester.Frame()
	if !tester.HasText("Delete this?") {
		t.Fatalf("texts %q", tester.Texts())
	}
	if err := tester.Click("Yes"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if result != "confirm" || dialog.Open() {
		t.Fatalf("result %q, open %v", result, dialog.Open())
	}
}

func TestCancelRunsCancelAction(t *testing.T) {
	dialog, result := &ConfirmDialog{}, ""
	tester := ui.NewTester(dialogView(dialog, &Notifier{}), 800, 600)
	dialog.Ask(deleteRequest(&result))
	tester.Frame()
	if err := tester.Click("No"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if result != "cancel" || dialog.Open() {
		t.Fatalf("result %q, open %v", result, dialog.Open())
	}
}

func TestDismissingCountsAsCancel(t *testing.T) {
	dialog, result := &ConfirmDialog{}, ""
	tester := ui.NewTester(dialogView(dialog, &Notifier{}), 800, 600)
	dialog.Ask(deleteRequest(&result))
	tester.Frame()
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
	if result != "cancel" || dialog.Open() {
		t.Fatalf("result %q, open %v", result, dialog.Open())
	}
}

func TestSecondaryAction(t *testing.T) {
	dialog, result := &ConfirmDialog{}, ""
	tester := ui.NewTester(dialogView(dialog, &Notifier{}), 800, 600)
	request := deleteRequest(&result)
	request.Secondary = &ConfirmAction{Label: "Discard", Run: func() { result = "discard" }}
	dialog.Ask(request)
	tester.Frame()
	if err := tester.Click("Discard"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if result != "discard" {
		t.Fatalf("result %q", result)
	}
}

func TestReplacingAnOpenDialogCancelsIt(t *testing.T) {
	dialog := &ConfirmDialog{}
	first, second := "", ""
	dialog.Ask(deleteRequest(&first))
	dialog.Ask(deleteRequest(&second))
	if first != "cancel" || second != "" || !dialog.Open() {
		t.Fatalf("first %q, second %q, open %v", first, second, dialog.Open())
	}
}

func TestNotificationsAreShownOnce(t *testing.T) {
	notifier := &Notifier{}
	tester := ui.NewTester(dialogView(&ConfirmDialog{}, notifier), 800, 600)
	notifier.Success("Saved")
	notifier.Titled(SeverityError, "Failed", "Could not save")
	tester.Frame()
	if notifier.Pending() != 0 {
		t.Fatalf("pending %d", notifier.Pending())
	}
	for _, text := range []string{"Saved", "Failed", "Could not save"} {
		if !tester.HasText(text) {
			t.Fatalf("missing %q in %q", text, tester.Texts())
		}
	}
}

func TestNotificationTimeoutsFollowSeverity(t *testing.T) {
	if severityTimeout(SeverityError) <= severityTimeout(SeverityWarning) || severityTimeout(SeverityWarning) <= severityTimeout(SeveritySuccess) {
		t.Fatal("more severe notifications should stay longer")
	}
}

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

func TestAutoFocusFocusesRevealedInput(t *testing.T) {
	config := nui.DefaultApp()
	shown, value := false, ""
	tester := ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, config)
		ui.Column(c).Fill().Children(func() {
			if nui.Button(c, theme, "Reveal", nui.ButtonOptions{}).Clicked() {
				shown = true
			}
			if shown {
				nui.Input(c, theme, &value, nui.InputOptions{Placeholder: "Name", Style: AutoFocus()})
			}
		})
	}, 400, 200)
	tester.Frame()
	if err := tester.Click("Reveal"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Frame()
	if active, _ := tester.TextInputActive(); !active {
		t.Fatal("revealed input does not have the focus")
	}
	tester.Type("abc")
	tester.Frame()
	if value != "abc" {
		t.Fatalf("value = %q", value)
	}
}
