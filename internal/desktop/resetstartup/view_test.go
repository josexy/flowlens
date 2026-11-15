package resetstartup

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/theme"
	appservice "github.com/josexy/flowlens/internal/services/app_service"
	"github.com/josexy/flowlens/locales"
)

type fakeService struct {
	calls []string
	fail  map[string]error
}

func (f *fakeService) call(name string) error {
	f.calls = append(f.calls, name)
	return f.fail[name]
}
func (f *fakeService) Show() error  { return f.call("show") }
func (f *fakeService) Ready() error { return f.call("ready") }
func (f *fakeService) Exit() error  { return f.call("exit") }
func (f *fakeService) Retry() error { return f.call("retry") }

type fixture struct {
	t       *testing.T
	service *fakeService
	view    *View
	tester  *ui.Tester
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	service := &fakeService{fail: map[string]error{}}
	view := &View{Service: service, Config: theme.Resolve(theme.Appearance{}).Config, Spawn: func(run func()) { run() }, Post: func(run func()) { run() }}
	f := &fixture{t: t, service: service, view: view}
	f.view.Apply(appservice.ResetStartupState{Language: "en", Stage: "storage", Stages: []string{"storage", "initializing"}, Busy: true})
	f.tester = ui.NewTester(view.Build, 480, 380)
	f.tester.SetLocale("en")
	i18n.SetLocaleFunc(func() string { return "en" })
	f.tester.Frame()
	return f
}

func (f *fixture) frames() {
	for range 3 {
		f.tester.Frame()
	}
}

func (f *fixture) want(texts ...string) {
	f.t.Helper()
	for _, text := range texts {
		if !f.tester.HasText(text) {
			f.t.Fatalf("no %q in %v", text, f.tester.Texts())
		}
	}
}

func TestStagesInProgress(t *testing.T) {
	f := newFixture(t)
	f.want(locales.SettingsResetStartupTitle(), locales.SettingsResetStartupStorage(), locales.SettingsResetStartupInitializing())
	if f.tester.HasText(locales.SettingsResetRecoveryRetry()) {
		t.Fatal("retry only after a failure")
	}
}

func TestReadyWaitsForAVisibleFrame(t *testing.T) {
	f := newFixture(t)
	f.frames()
	if len(f.service.calls) != 0 {
		t.Fatal("nothing is authorised before the window shows", f.service.calls)
	}
	f.view.Show()
	f.tester.Frame()
	if len(f.service.calls) != 1 || f.service.calls[0] != "show" {
		t.Fatal(f.service.calls)
	}
	f.frames()
	if len(f.service.calls) != 2 || f.service.calls[1] != "ready" {
		t.Fatal(f.service.calls)
	}
	f.frames()
	if len(f.service.calls) != 2 {
		t.Fatal("once", f.service.calls)
	}
}

func TestFailedShowNeverAuthorises(t *testing.T) {
	f := newFixture(t)
	f.service.fail["show"] = errors.New("reset progress window did not become visible")
	f.view.Show()
	f.frames()
	f.want(locales.SettingsResetRecoveryTitle(), "reset progress window did not become visible")
	for _, call := range f.service.calls {
		if call == "ready" {
			t.Fatal(f.service.calls)
		}
	}
}

func TestRetryAfterFailure(t *testing.T) {
	f := newFixture(t)
	f.view.Apply(appservice.ResetStartupState{Language: "en", Stage: "storage", Stages: []string{"storage", "initializing"}, Error: "disk is busy"})
	f.frames()
	f.want(locales.SettingsResetRecoveryTitle(), locales.SettingsResetRecoveryDescription(), "disk is busy")
	if err := f.tester.Click(locales.SettingsResetRecoveryRetry()); err == nil {
		f.frames()
		for _, call := range f.service.calls {
			if call == "retry" {
				t.Fatal("retry waits for the handshake")
			}
		}
	}
	f.view.Show()
	f.frames()
	if err := f.tester.Click(locales.SettingsResetRecoveryRetry()); err != nil {
		t.Fatalf("%v %v", err, f.tester.Texts())
	}
	f.frames()
	if f.service.calls[len(f.service.calls)-1] != "retry" {
		t.Fatal(f.service.calls)
	}
}

func TestRetryIsDisabledWhileBusy(t *testing.T) {
	f := newFixture(t)
	f.view.Show()
	f.frames()
	f.view.Apply(appservice.ResetStartupState{Language: "en", Stage: "storage", Error: "busy", Busy: true})
	f.frames()
	_ = f.tester.Click(locales.SettingsResetRecoveryRetry())
	f.frames()
	for _, call := range f.service.calls {
		if call == "retry" {
			t.Fatal(f.service.calls)
		}
	}
}

func TestExit(t *testing.T) {
	f := newFixture(t)
	if err := f.tester.Click(locales.SettingsResetRecoveryExit()); err != nil {
		t.Fatal(err)
	}
	f.frames()
	if f.service.calls[0] != "exit" {
		t.Fatal(f.service.calls)
	}
}

func TestFailedExitIsShownAndCanRepeat(t *testing.T) {
	f := newFixture(t)
	f.service.fail["exit"] = errors.New("could not write")
	if err := f.tester.Click(locales.SettingsResetRecoveryExit()); err != nil {
		t.Fatal(err)
	}
	f.frames()
	f.want("could not write")
	if err := f.tester.Click(locales.SettingsResetRecoveryExit()); err != nil {
		t.Fatal(err)
	}
	f.frames()
	if len(f.service.calls) != 2 {
		t.Fatal(f.service.calls)
	}
}
