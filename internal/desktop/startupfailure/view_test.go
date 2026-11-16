package startupfailure

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/locales"
)

type fixture struct {
	t       *testing.T
	view    *View
	tester  *ui.Tester
	calls   []string
	logsErr error
	// spawned holds work the view started off the main thread.
	spawned []func()
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	f := &fixture{t: t}
	f.view = &View{
		Config: theme.Resolve(theme.Appearance{}).Config,
		Retry:  func() { f.calls = append(f.calls, "retry") },
		OpenLogs: func() error {
			f.calls = append(f.calls, "logs")
			return f.logsErr
		},
		Quit:  func() { f.calls = append(f.calls, "quit") },
		Spawn: func(run func()) { f.spawned = append(f.spawned, run) },
		Post:  func(run func()) { run() },
	}
	f.view.Fail(Failure{Stage: StageDatabase, Error: "database is locked"})
	f.tester = ui.NewTester(f.view.Build, 560, 400)
	f.tester.SetLocale("en")
	i18n.SetLocaleFunc(func() string { return "en" })
	f.tester.Frame()
	return f
}

func (f *fixture) runSpawned() {
	spawned := f.spawned
	f.spawned = nil
	for _, run := range spawned {
		run()
	}
	f.tester.Frame()
}

func (f *fixture) want(texts ...string) {
	f.t.Helper()
	for _, text := range texts {
		if !f.tester.HasText(text) {
			f.t.Fatalf("no %q in %v", text, f.tester.Texts())
		}
	}
}

func (f *fixture) click(label string) {
	f.t.Helper()
	if err := f.tester.Click(label); err != nil {
		f.t.Fatalf("click %q: %v in %v", label, err, f.tester.Texts())
	}
	f.tester.Frame()
}

func TestShowsTheFailedStepAndError(t *testing.T) {
	f := newFixture(t)
	f.want(locales.StartupFailureTitle(), locales.StartupFailureDescription(), locales.StartupFailureStageDatabase(), "database is locked",
		locales.StartupFailureRetry(), locales.StartupFailureQuit(), locales.StartupFailureOpenLogs())
}

func TestStageLabels(t *testing.T) {
	f := newFixture(t)
	for stage, label := range map[Stage]string{
		StageReset:    locales.StartupFailureStageReset(),
		StageSettings: locales.StartupFailureStageSettings(),
		StageServices: locales.StartupFailureStageServices(),
	} {
		f.view.Fail(Failure{Stage: stage, Error: "x"})
		f.tester.Frame()
		f.want(label)
	}
}

func TestRetryRunsOffTheMainThreadOnce(t *testing.T) {
	f := newFixture(t)
	f.click(locales.StartupFailureRetry())
	if len(f.calls) != 0 || len(f.spawned) != 1 || !f.view.Retrying() {
		t.Fatalf("calls %v, spawned %d", f.calls, len(f.spawned))
	}
	_ = f.tester.Click(locales.StartupFailureRetry())
	_ = f.tester.Click(locales.StartupFailureQuit())
	f.tester.Frame()
	if len(f.spawned) != 1 {
		t.Fatal("a second action started while retrying")
	}
	f.runSpawned()
	if len(f.calls) != 1 || f.calls[0] != "retry" {
		t.Fatal(f.calls)
	}
	f.view.Fail(Failure{Stage: StageServices, Error: "port in use"})
	f.tester.Frame()
	f.want(locales.StartupFailureStageServices(), "port in use")
	if f.view.Retrying() {
		t.Fatal("a failure ends the attempt")
	}
	f.click(locales.StartupFailureRetry())
	if len(f.spawned) != 1 {
		t.Fatal("cannot retry again after a failure")
	}
}

func TestQuit(t *testing.T) {
	f := newFixture(t)
	f.click(locales.StartupFailureQuit())
	if len(f.calls) != 1 || f.calls[0] != "quit" {
		t.Fatal(f.calls)
	}
}

func TestOpenLogsShowsItsFailure(t *testing.T) {
	f := newFixture(t)
	f.logsErr = errors.New("no file manager")
	f.click(locales.StartupFailureOpenLogs())
	f.runSpawned()
	f.want(locales.StartupFailureOpenLogsFailed("no file manager"))
	f.logsErr = nil
	f.click(locales.StartupFailureOpenLogs())
	f.runSpawned()
	if f.tester.HasText(locales.StartupFailureOpenLogsFailed("no file manager")) {
		t.Fatal("a later success keeps the old failure")
	}
	if len(f.calls) != 2 || f.calls[1] != "logs" {
		t.Fatal(f.calls)
	}
}
