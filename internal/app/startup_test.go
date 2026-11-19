package app

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/desktop/startupfailure"
	appdatabase "github.com/josexy/flowlens/internal/pkg/database"
	appfs "github.com/josexy/flowlens/internal/pkg/fs"
	"github.com/josexy/flowlens/internal/pkg/logger"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
)

type startupRecorder struct {
	calls       []string
	locked      bool
	dbErr       error
	servicesErr error
	db          *sql.DB
	failures    []startupfailure.Stage
	served      bool
	first       *launched
	failure     *startupError
	retry       startupAttempt
}

func (r *startupRecorder) steps() startupSteps {
	return startupSteps{
		enableBootstrapLogging: func() { r.calls = append(r.calls, "log") },
		acquireInstanceLock: func() bool {
			r.calls = append(r.calls, "lock")
			return r.locked
		},
		startProfiling: func() func() {
			r.calls = append(r.calls, "profiling")
			return func() { r.calls = append(r.calls, "profiling stopped") }
		},
		openDatabase: func() (*sql.DB, error) {
			r.calls = append(r.calls, "database")
			if r.dbErr != nil {
				return nil, r.dbErr
			}
			return r.db, nil
		},
		startServices: func(*sql.DB) (*launched, error) {
			r.calls = append(r.calls, "services")
			if r.servicesErr != nil {
				return nil, r.servicesErr
			}
			return &launched{}, nil
		},
		reportFailure: func(failure *startupError) { r.failures = append(r.failures, failure.stage) },
		serve: func(first *launched, failure *startupError, retry startupAttempt) {
			r.calls = append(r.calls, "serve")
			r.served, r.first, r.failure, r.retry = true, first, failure, retry
		},
	}
}

func TestStartupOrder(t *testing.T) {
	recorder := &startupRecorder{locked: true}
	recorder.steps().run()
	if want := []string{"log", "lock", "profiling", "database", "services", "serve", "profiling stopped"}; !slices.Equal(recorder.calls, want) {
		t.Fatalf("calls %v, want %v", recorder.calls, want)
	}
	if recorder.first == nil || recorder.failure != nil {
		t.Fatalf("first %v, failure %v", recorder.first, recorder.failure)
	}
}

func TestSecondInstanceNeverOpensTheDatabase(t *testing.T) {
	recorder := &startupRecorder{locked: false}
	recorder.steps().run()
	if slices.Contains(recorder.calls, "database") || slices.Contains(recorder.calls, "profiling") || recorder.served {
		t.Fatalf("calls %v", recorder.calls)
	}
}

func TestDatabaseFailureStaysOpen(t *testing.T) {
	recorder := &startupRecorder{locked: true, dbErr: errors.New("locked")}
	recorder.steps().run()
	if slices.Contains(recorder.calls, "services") || !recorder.served || recorder.first != nil {
		t.Fatalf("calls %v", recorder.calls)
	}
	if recorder.failure.stage != startupfailure.StageDatabase || recorder.failure.failure().Error != "locked" {
		t.Fatalf("failure %+v", recorder.failure.failure())
	}
	if want := "open application database: locked"; recorder.failure.Error() != want {
		t.Fatalf("log message %q, want %q", recorder.failure.Error(), want)
	}
}

func TestServiceFailureKeepsItsStep(t *testing.T) {
	for _, test := range []struct {
		err  error
		want startupfailure.Stage
	}{
		{errors.New("python"), startupfailure.StageServices},
		{startupFailed(startupfailure.StageSettings, errors.New("bad settings")), startupfailure.StageSettings},
	} {
		recorder := &startupRecorder{locked: true, servicesErr: test.err}
		recorder.steps().run()
		if recorder.failure == nil || recorder.failure.stage != test.want || recorder.first != nil {
			t.Fatalf("%v: failure %v", test.err, recorder.failure)
		}
	}
}

func TestRetryRunsEveryStepAgain(t *testing.T) {
	recorder := &startupRecorder{locked: true, dbErr: errors.New("locked")}
	recorder.steps().run()
	recorder.calls = nil

	started, failure := recorder.retry()
	if failure == nil || started != nil {
		t.Fatalf("still locked: %v %v", started, failure)
	}
	recorder.dbErr = nil
	recorder.calls = nil
	started, failure = recorder.retry()
	if started == nil || failure != nil {
		t.Fatalf("retry: %v %v", started, failure)
	}
	if want := []string{"log", "database", "services"}; !slices.Equal(recorder.calls, want) {
		t.Fatalf("calls %v, want %v", recorder.calls, want)
	}
}

// useTestStorage points the application's storage at a temporary directory
// and returns the FlowLens data directory inside it.
func useTestStorage(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	base, err := appfs.BaseStoragePath()
	if err != nil {
		t.Fatal(err)
	}
	return base
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("retained test content"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestServiceConstructionFailurePreservesSavedLanguage(t *testing.T) {
	for _, test := range []struct {
		name    string
		message string
		setup   func(*testing.T, *sql.DB, string)
	}{
		{
			name: "python plugins", message: "initialize Python plugin service",
			setup: func(t *testing.T, _ *sql.DB, base string) {
				writeTestFile(t, filepath.Join(base, "python_plugins"))
			},
		},
		{
			name: "API collection", message: "load API collection",
			setup: func(t *testing.T, db *sql.DB, _ string) {
				if _, err := db.Exec("DROP TABLE api_request_payloads"); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := useTestStorage(t)
			t.Cleanup(func() { _ = logger.Close() })
			db, err := appdatabase.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			settings := settingservice.New(db)
			if err := settings.Load(); err != nil {
				t.Fatal(err)
			}
			if err := settings.SetLanguage("en"); err != nil {
				t.Fatal(err)
			}
			if err := settings.Save(); err != nil {
				t.Fatal(err)
			}
			test.setup(t, db, base)
			started, err := startServices(db)
			var failure *startupError
			if started != nil || !errors.As(err, &failure) {
				t.Fatalf("expected startup error, got %v, %v", started, err)
			}
			if failure.stage != startupfailure.StageServices || failure.language != "en" || !strings.Contains(failure.failure().Error, test.message) {
				t.Fatalf("failure: stage=%q language=%q error=%v", failure.stage, failure.language, failure)
			}
		})
	}
}

func TestFailedAttemptClosesTheDatabase(t *testing.T) {
	db, err := appdatabase.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	recorder := &startupRecorder{locked: true, db: db, servicesErr: errors.New("boom")}
	recorder.steps().run()
	if err := db.Ping(); err == nil {
		t.Fatal("a failed attempt left the database open for the next one")
	}
}

type recoveryRecorder struct {
	recovery *startupRecovery
	shown    []*startupError
	opened   []*launched
	quits    int
}

func newRecoveryRecorder(first *launched, attempt startupAttempt) *recoveryRecorder {
	r := &recoveryRecorder{}
	r.recovery = &startupRecovery{
		attempt: attempt,
		started: first,
		post:    func(run func()) { run() },
		show:    func(failure *startupError) { r.shown = append(r.shown, failure) },
		open:    func(started *launched) { r.opened = append(r.opened, started) },
		quit:    func() { r.quits++ },
	}
	return r
}

func TestRecoveryOpensTheMainWindowAfterASuccessfulRetry(t *testing.T) {
	started := &launched{}
	r := newRecoveryRecorder(nil, func() (*launched, *startupError) { return started, nil })
	r.recovery.retry()
	if len(r.opened) != 1 || r.opened[0] != started || len(r.shown) != 0 {
		t.Fatalf("opened %v, shown %v", r.opened, r.shown)
	}
	if r.recovery.finish() != started {
		t.Fatal("the started application is not stopped at exit")
	}
}

func TestRecoveryShowsAFailedRetry(t *testing.T) {
	failure := startupFailed(startupfailure.StageDatabase, errors.New("locked"))
	r := newRecoveryRecorder(nil, func() (*launched, *startupError) { return nil, failure })
	r.recovery.retry()
	if len(r.shown) != 1 || r.shown[0] != failure || len(r.opened) != 0 || r.recovery.finish() != nil {
		t.Fatalf("shown %v, opened %v", r.shown, r.opened)
	}
}

func TestRecoveryStopsWhatARetryStartedAfterTheLoopEnded(t *testing.T) {
	started := &launched{}
	r := newRecoveryRecorder(nil, func() (*launched, *startupError) { return started, nil })
	r.recovery.post = func(func()) {} // the event loop has stopped
	r.recovery.retry()
	if len(r.opened) != 0 || r.recovery.finish() != started {
		t.Fatal("a retry that finished after the event loop was not stopped")
	}
}

func TestRecoveryFinishWaitsForARunningAttempt(t *testing.T) {
	release := make(chan struct{})
	started := &launched{}
	r := newRecoveryRecorder(nil, func() (*launched, *startupError) {
		<-release
		return started, nil
	})
	r.recovery.post = func(func()) {}
	r.recovery.spawn(r.recovery.retry)
	finished := make(chan *launched, 1)
	go func() { finished <- r.recovery.finish() }()
	select {
	case <-finished:
		t.Fatal("finish returned before the attempt ended")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if got := <-finished; got != started {
		t.Fatalf("finish returned %v", got)
	}
}

func TestFirstStartIsStoppedAtExit(t *testing.T) {
	first := &launched{}
	r := newRecoveryRecorder(first, nil)
	if r.recovery.finish() != first {
		t.Fatal("first start is not stopped at exit")
	}
}

// A build counts as production exactly when MyGo says so, as `mygo build` and
// MYGO_ENV=production do.
func TestApplicationNameFollowsMyGoProductionBuild(t *testing.T) {
	t.Setenv("MYGO_ENV", "")
	if got := applicationName(); got != "FlowLens Dev" {
		t.Fatalf("development name = %q", got)
	}
	t.Setenv("MYGO_ENV", "production")
	if got := applicationName(); got != "FlowLens" {
		t.Fatalf("production name = %q", got)
	}
}
