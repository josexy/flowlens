package app

import (
	"database/sql"
	"errors"
	"slices"
	"testing"
)

type startupRecorder struct {
	calls    []string
	locked   bool
	resetErr error
	dbErr    error
	failures []string
}

func (r *startupRecorder) steps() startupSteps {
	return startupSteps{
		enableBootstrapLogging: func() { r.calls = append(r.calls, "log") },
		acquireInstanceLock: func() bool {
			r.calls = append(r.calls, "lock")
			return r.locked
		},
		prepareReset: func() (*resetManifest, *resetStartupClient, error) {
			r.calls = append(r.calls, "reset")
			return nil, nil, r.resetErr
		},
		openDatabase: func() (*sql.DB, error) {
			r.calls = append(r.calls, "database")
			return nil, r.dbErr
		},
		launch:        func(*sql.DB, *resetManifest, *resetStartupClient) { r.calls = append(r.calls, "launch") },
		reportFailure: func(stage string, _ error) { r.failures = append(r.failures, stage) },
	}
}

func TestStartupOrder(t *testing.T) {
	recorder := &startupRecorder{locked: true}
	recorder.steps().run()
	if want := []string{"log", "lock", "reset", "database", "launch"}; !slices.Equal(recorder.calls, want) {
		t.Fatalf("calls %v, want %v", recorder.calls, want)
	}
}

func TestSecondInstanceNeverOpensTheDatabase(t *testing.T) {
	recorder := &startupRecorder{locked: false}
	recorder.steps().run()
	if slices.Contains(recorder.calls, "database") || slices.Contains(recorder.calls, "launch") {
		t.Fatalf("calls %v", recorder.calls)
	}
}

func TestResetFailureStopsBeforeTheDatabase(t *testing.T) {
	recorder := &startupRecorder{locked: true, resetErr: errors.New("boom")}
	recorder.steps().run()
	if slices.Contains(recorder.calls, "database") || len(recorder.failures) != 1 {
		t.Fatalf("calls %v, failures %v", recorder.calls, recorder.failures)
	}
}

func TestDatabaseFailureIsReportedAndStopsStartup(t *testing.T) {
	recorder := &startupRecorder{locked: true, dbErr: errors.New("locked")}
	recorder.steps().run()
	if slices.Contains(recorder.calls, "launch") || len(recorder.failures) != 1 {
		t.Fatalf("calls %v, failures %v", recorder.calls, recorder.failures)
	}
}
