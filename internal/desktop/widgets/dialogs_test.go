package widgets

import (
	"testing"

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
