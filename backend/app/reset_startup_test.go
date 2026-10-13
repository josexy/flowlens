package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
)

func TestResetStartupSessionCanReportCorruptMarker(t *testing.T) {
	resetTestStorage(t)
	root, _ := resetDirectory()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte("broken marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "startup", uuid.NewString())
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	snapshot := resetStartupSnapshot{
		ResetStartupState: appservice.ResetStartupState{Language: "zh", Stage: "starting", Error: "invalid reset marker"},
		Token:             filepath.Base(directory), UpdatedAt: time.Now().UnixMilli(),
	}
	if err := writeResetStartupJSON(directory, "state.json", snapshot); err != nil {
		t.Fatal(err)
	}
	state, err := readResetStartupState(directory)
	if err != nil || state.Error != snapshot.Error || state.Language != "zh" {
		t.Fatalf("corrupt marker prevented its recovery UI: %+v %v", state, err)
	}
	for _, token := range []string{resetTestManifest().Token, ""} {
		snapshot.Token = token
		if err := writeResetStartupJSON(directory, "state.json", snapshot); err != nil {
			t.Fatal(err)
		}
		if _, err := readResetStartupState(directory); err == nil {
			t.Fatal("different UI session token was accepted")
		}
	}
	snapshot.Token, snapshot.UpdatedAt = filepath.Base(directory), time.Now().Add(-time.Minute).UnixMilli()
	if err := writeResetStartupJSON(directory, "state.json", snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := readResetStartupState(directory); err == nil {
		t.Fatal("expired UI session was accepted")
	}
}

func TestResetRecoveryRetryReloadsMarkerWithoutDeletingData(t *testing.T) {
	base := resetTestStorage(t)
	old := filepath.Join(base, "histories", "old.hbin")
	writeResetTestFile(t, old)
	manifest := resetTestManifest()
	if err := saveResetManifest(manifest); err != nil {
		t.Fatal(err)
	}
	root, _ := resetDirectory()
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte("broken marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, failure := loadResetManifest()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &resetStartupClient{directory: t.TempDir(), ctx: ctx, cancel: cancel, actions: make(chan string, 1)}
	type result struct {
		manifest *resetManifest
		err      error
	}
	completed := make(chan result, 1)
	go func() {
		manifest, err := client.waitForManifest(nil, failure)
		completed <- result{manifest, err}
	}()
	waitFailure := func(expected string) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			client.mu.Lock()
			failure := client.state.Error
			client.mu.Unlock()
			if failure == expected {
				return
			}
			select {
			case <-ticker.C:
			case <-deadline:
				t.Fatalf("recovery did not report %q: %q", expected, failure)
			}
		}
	}
	waitFailure(failure.Error())
	if err := os.Remove(filepath.Join(root, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	client.actions <- "retry"
	waitFailure("application reset marker is unavailable")
	if _, err := os.Stat(old); err != nil {
		t.Fatal("invalid marker retry deleted application data")
	}
	manifest.Language = "en"
	if err := saveResetManifest(manifest); err != nil {
		t.Fatal(err)
	}
	client.actions <- "retry"
	select {
	case got := <-completed:
		if got.err != nil || got.manifest.Token != manifest.Token || got.manifest.Language != "en" {
			t.Fatalf("repaired marker was not revalidated: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("repaired marker retry did not complete")
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("marker validation started deleting data")
	}
}

func TestResetStartupUncommittedMarkerPreservesData(t *testing.T) {
	base := resetTestStorage(t)
	old := filepath.Join(base, "histories", "old.hbin")
	writeResetTestFile(t, old)
	manifest := resetTestManifest()
	manifest.Phase = resetPrepared
	if err := saveResetManifest(manifest); err != nil {
		t.Fatal(err)
	}
	got, client, err := prepareResetStartup()
	if err != nil || got != nil || client != nil {
		t.Fatalf("uncommitted reset started cleanup: %+v %v", got, err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("uncommitted reset deleted data")
	}
	if got, err := loadResetManifest(); got != nil || err != nil {
		t.Fatal("uncommitted marker remains")
	}
}

func TestResetStartupRevalidatesMarkerAfterUIStartup(t *testing.T) {
	resetTestStorage(t)
	manifest := resetTestManifest()
	if err := saveResetManifest(manifest); err != nil {
		t.Fatal(err)
	}
	root, _ := resetDirectory()
	if err := os.Remove(filepath.Join(root, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &resetStartupClient{directory: t.TempDir(), ctx: ctx, cancel: cancel}
	got, err := client.waitForManifest(manifest, nil)
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("removed marker authorised cleanup from a stale snapshot: %+v %v", got, err)
	}
	if client.state.Error != "application reset marker is unavailable" {
		t.Fatal("missing marker was not reported by the recovery UI")
	}
}

func TestResetStartupStagesIncludeOnlyApplicableCleanup(t *testing.T) {
	manifest := resetTestManifest()
	expected := []string{"storage"}
	if got := resetStartupStages(manifest); !reflect.DeepEqual(got, append(append([]string{}, expected...), "initializing")) {
		t.Fatalf("stages without temporary files: %v", got)
	}
	manifest.UpdateStaging = []string{"first", "second"}
	if got := resetStartupStages(manifest); !reflect.DeepEqual(got, append(expected, "temporary", "initializing")) {
		t.Fatalf("temporary directories must share a single progress step: %v", got)
	}
}

func TestResetStartupSnapshotRetainsStagesAcrossFastUpdatesAndFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stages := resetStartupStages(resetTestManifest())
	client := &resetStartupClient{
		directory: t.TempDir(), ctx: ctx, cancel: cancel,
		state: resetStartupSnapshot{ResetStartupState: appservice.ResetStartupState{Stages: stages}},
	}
	for _, stage := range stages {
		client.update(stage, "", true)
	}
	// A slow polling reader must still be able to display every prior step.
	var snapshot resetStartupSnapshot
	if err := readResetStartupJSON(client.directory, "state.json", &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Stage != "initializing" || !reflect.DeepEqual(snapshot.Stages, stages) {
		t.Fatalf("fast updates lost completed stages: %+v", snapshot)
	}
	client.update("", "initialization failed", false)
	if err := readResetStartupJSON(client.directory, "state.json", &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Stage != "initializing" || snapshot.Error == "" || snapshot.Busy {
		t.Fatalf("failure lost its active stage: %+v", snapshot)
	}
	client.update("starting", "", true)
	if err := readResetStartupJSON(client.directory, "state.json", &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Stage != "starting" || snapshot.Error != "" || !snapshot.Busy {
		t.Fatalf("retry did not restart progress: %+v", snapshot)
	}
}

func TestResetPermanentErrorDoesNotRetry(t *testing.T) {
	base := resetTestStorage(t)
	manifest := resetTestManifest()
	if err := saveResetManifest(manifest); err != nil {
		t.Fatal(err)
	}
	failure := &os.PathError{Op: "remove", Path: base, Err: os.ErrPermission}
	calls := 0
	err := cleanResetData(manifest, func(string) error { calls++; return failure }, 50, time.Hour)
	if !errors.Is(err, failure) || calls != 1 {
		t.Fatalf("permanent error retried: %d %v", calls, err)
	}
	pending, err := loadResetManifest()
	if err != nil || pending.Phase != resetPending {
		t.Fatalf("lost pending reset: %+v %v", pending, err)
	}
}

func TestResetCancellationInterruptsBackoff(t *testing.T) {
	resetTestStorage(t)
	manifest := resetTestManifest()
	if err := saveResetManifest(manifest); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	err := cleanResetDataWithProgress(ctx, manifest, func(string) error {
		calls++
		cancel()
		return syscall.EBUSY
	}, 50, time.Hour, nil)
	if !errors.Is(err, context.Canceled) || calls != 1 || manifest.Phase != resetPending {
		t.Fatalf("cancelled cleanup continued: %d %s %v", calls, manifest.Phase, err)
	}
}

func TestResetProgressStagesBeforeRemoval(t *testing.T) {
	resetTestStorage(t)
	manifest := resetTestManifest()
	manifest.UpdateStaging = []string{filepath.Join(os.TempDir(), "wails-update-progress")}
	paths, err := resetCleanupPaths(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var removed []string
	var stages []string
	err = cleanResetDataWithProgress(context.Background(), manifest, func(path string) error {
		if len(stages) != len(removed)+1 {
			t.Fatal("directory deletion started before reporting progress")
		}
		removed = append(removed, path)
		return nil
	}, 1, 0, func(stage string) { stages = append(stages, stage) })
	if err != nil || !reflect.DeepEqual(removed, paths) || !reflect.DeepEqual(stages, []string{"storage", "temporary"}) {
		t.Fatalf("progress cleanup: %v %v %v", removed, stages, err)
	}
}

func TestResetStartupSessionRejectsOutsideAndLinkedPaths(t *testing.T) {
	resetTestStorage(t)
	t.Setenv(resetStartupEnvironment, t.TempDir())
	if _, err := resetStartupSessionDirectory(); err == nil {
		t.Fatal("external startup directory accepted")
	}
	root, _ := resetDirectory()
	directory := filepath.Join(root, "startup", resetTestManifest().Token)
	if err := os.MkdirAll(filepath.Dir(directory), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(resetStartupEnvironment, directory)
	if _, err := resetStartupSessionDirectory(); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), directory); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := resetStartupSessionDirectory(); err == nil {
		t.Fatal("linked startup directory accepted")
	}
}

func TestResetStartupMonitorGuardsRetryAndHonoursExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &resetStartupClient{
		directory: t.TempDir(), ctx: ctx, cancel: cancel,
		done: make(chan struct{}), ready: make(chan struct{}), actions: make(chan string, 1),
		state: resetStartupSnapshot{Token: resetTestManifest().Token},
	}
	client.state.Error = "sharing violation"
	if err := client.publish(); err != nil {
		t.Fatal(err)
	}
	go client.monitor()
	send := func(sequence uint64, action string) {
		t.Helper()
		if err := writeResetStartupJSON(client.directory, "action.json", resetStartupAction{Sequence: sequence, Action: action}); err != nil {
			t.Fatal(err)
		}
	}
	send(1, "ready")
	select {
	case <-client.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("startup ready handshake was not delivered")
	}
	send(2, "retry")
	select {
	case <-client.actions:
	case <-time.After(3 * time.Second):
		t.Fatal("retry was not delivered")
	}
	send(3, "retry")
	select {
	case <-client.actions:
		t.Fatal("duplicate retry started while busy")
	case <-time.After(250 * time.Millisecond):
	}
	send(4, "exit")
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("exit did not cancel cleanup")
	}
	if !client.exitRequested.Load() {
		t.Fatal("user exit was not distinguished from a startup failure")
	}
}

func TestResetStartupExitBeforeReadyDoesNotAcceptCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &resetStartupClient{
		directory: t.TempDir(), ctx: ctx, cancel: cancel,
		done: make(chan struct{}), ready: make(chan struct{}), actions: make(chan string, 1),
	}
	// The window can be closed between Show and its rendered-frame handshake.
	if err := writeResetStartupJSON(client.directory, "action.json", resetStartupAction{Sequence: 1, Action: "exit"}); err != nil {
		t.Fatal(err)
	}
	go client.monitor()
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("early exit did not cancel startup")
	}
	if !client.exitRequested.Load() {
		t.Fatal("early exit would be reported as a startup failure")
	}
	select {
	case <-client.ready:
		t.Fatal("early exit authorised cleanup")
	default:
	}
}

func TestResetStartupQuickRetryRetainsReadyHandshake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &resetStartupClient{
		directory: t.TempDir(), ctx: ctx, cancel: cancel,
		done: make(chan struct{}), ready: make(chan struct{}), actions: make(chan string, 1),
		state: resetStartupSnapshot{ResetStartupState: appservice.ResetStartupState{Error: "invalid marker"}},
	}
	if err := writeResetStartupJSON(client.directory, "action.json", resetStartupAction{Sequence: 2, Action: "retry", Ready: true}); err != nil {
		t.Fatal(err)
	}
	go client.monitor()
	select {
	case <-client.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("quick retry lost the rendered-frame handshake")
	}
	select {
	case <-client.actions:
	case <-time.After(3 * time.Second):
		t.Fatal("quick retry was not delivered")
	}
}
