package rewriteservice

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func waitForRuleDatabaseRead(t *testing.T, db *sql.DB, previous int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for db.Stats().WaitCount == previous {
		if time.Now().After(deadline) {
			t.Fatal("loader did not request the held database connection")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestStartupLoadsOnceWithoutBlockingAndGuardsConsumers(t *testing.T) {
	seed := testService(t)
	state, err := seed.SaveRule(testRule(), 0)
	if err != nil {
		t.Fatal(err)
	}
	state, err = seed.SetRuleEnabled(state.Rules[0].ID, true, state.Revision)
	if err != nil {
		t.Fatal(err)
	}
	state, err = seed.SetEnabled(true, state.Revision)
	if err != nil {
		t.Fatal(err)
	}
	s := New(seed.db)
	t.Cleanup(func() { s.Shutdown() })
	held, err := seed.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	previousWaits := seed.db.Stats().WaitCount
	startup := make(chan error, 1)
	go func() { startup <- s.ServiceStartup(t.Context(), application.ServiceOptions{}) }()
	select {
	case err := <-startup:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("service startup waited for SQLite")
	}
	for range 8 {
		if err := s.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	waitForRuleDatabaseRead(t, seed.db, previousWaits)
	if s.Snapshot() != nil {
		t.Fatal("published an empty snapshot before loading rules")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	if _, err := s.GetState(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pending read did not honor caller cancellation: %v", err)
	}
	mutations := []func() (State, error){
		func() (State, error) { return s.SaveRule(testRule(), state.Revision) },
		func() (State, error) { return s.DeleteRule(state.Rules[0].ID, state.Revision) },
		func() (State, error) { return s.SetRuleEnabled(state.Rules[0].ID, false, state.Revision) },
		func() (State, error) { return s.SetEnabled(false, state.Revision) },
		func() (State, error) { return s.ReorderRules([]string{state.Rules[0].ID}, state.Revision) },
	}
	for _, mutate := range mutations {
		if _, err := mutate(); err == nil || !strings.Contains(err.Error(), "still loading") {
			t.Fatalf("mutation bypassed startup readiness: %v", err)
		}
	}
	if got := seed.db.Stats().WaitCount - previousWaits; got != 1 {
		t.Fatalf("startup launched %d database readers, want one", got)
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancelReady := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancelReady()
	got, err := s.GetState(ctx)
	if err != nil || got.Revision != state.Revision || !got.Enabled || len(got.Rules) != 1 || !got.Rules[0].Enabled {
		t.Fatalf("initial state did not preserve saved rules: %+v, %v", got, err)
	}
	if matches := s.Snapshot().Match("GET", "https://api.example.com/users/a?token=b"); len(matches) != 1 {
		t.Fatalf("published state without its compiled matcher: %v", matches)
	}
	// Neither repeat startup nor a canceled API caller may start another load.
	if _, err := seed.db.Exec(`UPDATE rewrite_state SET revision=999 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(); err != nil || mustGetState(t, s).Revision != state.Revision {
		t.Fatalf("initial load was not coalesced: %v", err)
	}
}

func TestStartupFailureDoesNotPublishPartialRules(t *testing.T) {
	seed := testService(t)
	state, err := seed.SaveRule(testRule(), 0)
	if err != nil {
		t.Fatal(err)
	}
	// The first rule decodes successfully; reading the second fails at Scan.
	_, err = seed.db.Exec(`INSERT INTO rewrite_rules SELECT 'broken',name,enabled,1,method,url_pattern,action_json,'invalid timestamp',updated_at FROM rewrite_rules WHERE id=?`, state.Rules[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	s := New(seed.db)
	t.Cleanup(func() { s.Shutdown() })
	if err := s.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatalf("load failure terminated application startup: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, loadErr := s.GetState(ctx)
	if loadErr == nil || !strings.Contains(loadErr.Error(), "invalid timestamp") {
		t.Fatalf("read hid the load failure: %v", loadErr)
	}
	if s.Snapshot() != nil {
		t.Fatal("failed load published a partial snapshot")
	}
	if _, err := s.SetEnabled(true, state.Revision); err != loadErr {
		t.Fatalf("mutation did not preserve the load failure: %v", err)
	}
	var revision int64
	if err := seed.db.QueryRow(`SELECT revision FROM rewrite_state WHERE id=1`).Scan(&revision); err != nil || revision != state.Revision {
		t.Fatalf("failed load changed persisted rules: %d, %v", revision, err)
	}
}

func TestStartupShutdownCancelsAndJoinsDatabaseRead(t *testing.T) {
	seed := testService(t)
	held, err := seed.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	s := New(seed.db)
	t.Cleanup(func() { s.Shutdown() })
	previousWaits := seed.db.Stats().WaitCount
	if err := s.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	waitForRuleDatabaseRead(t, seed.db, previousWaits)
	done := make(chan error, 1)
	go func() { done <- s.Shutdown() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel the pending database read")
	}
	select {
	case <-s.loadDone:
	default:
		t.Fatal("shutdown returned before the loader exited")
	}
	if _, err := s.GetState(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed service remained available: %v", err)
	}
	if _, err := s.SaveRule(testRule(), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed service accepted a mutation: %v", err)
	}
	if s.Snapshot() != nil {
		t.Fatal("canceled load published a snapshot")
	}
	if err := s.Shutdown(); err != nil {
		t.Fatalf("repeated shutdown failed: %v", err)
	}
}

func TestShutdownBeforeStartupDoesNotAccessDatabase(t *testing.T) {
	s := New(nil)
	if err := s.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := s.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitReady(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown service restarted its loader: %v", err)
	}
}

func TestShutdownWaitsForMutationNotification(t *testing.T) {
	s := testService(t)
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	SetChangedHandler(s, func(Changed) { close(entered); <-release })
	mutationDone := make(chan error, 1)
	go func() { _, err := s.SaveRule(testRule(), 0); mutationDone <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("mutation did not reach its notification")
	}
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- s.Shutdown() }()
	<-s.loadCtx.Done()
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before mutation notification completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	for _, done := range []<-chan error{mutationDone, shutdownDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("mutation/shutdown did not finish")
		}
	}
	if s.changed != nil {
		t.Fatal("shutdown retained its application event callback")
	}
}
