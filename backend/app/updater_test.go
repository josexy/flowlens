package app

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

func TestMatchUpdaterAssetUsesUniversalMacOSArchive(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "flowlens_v1.0.2_darwin_arm64.dmg"},
		{Name: "flowlens_v1.0.2_darwin_universal.zip"},
		{Name: "flowlens_v1.0.2_macos_universal.dmg"},
	}

	for _, arch := range []string{"amd64", "arm64"} {
		if got := matchUpdaterAsset(updater.CheckRequest{Platform: "darwin", Arch: arch}, assets); got != 1 {
			t.Fatalf("arch %s: expected universal archive at index 1, got %d", arch, got)
		}
	}
	if got := matchUpdaterAsset(updater.CheckRequest{Platform: "darwin", Arch: "amd64"}, assets[:1]); got != -1 {
		t.Fatalf("missing universal archive: expected no updater asset, got %d", got)
	}
}

func TestMatchUpdaterAssetPrefersWindowsExecutableAndRejectsLinux(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "flowlens_v1.0.2_windows_x64_setup.exe"},
		{Name: "flowlens_v1.0.2_windows_amd64.exe"},
		{Name: "flowlens_v1.0.2_linux_x64.AppImage"},
	}

	if got := matchUpdaterAsset(updater.CheckRequest{Platform: "windows", Arch: "amd64"}, assets); got != 1 {
		t.Fatalf("windows: expected updater executable at index 1, got %d", got)
	}
	if got := matchUpdaterAsset(updater.CheckRequest{Platform: "linux", Arch: "amd64"}, assets); got != -1 {
		t.Fatalf("linux: must not offer an AppImage for executable replacement, got %d", got)
	}
}

func TestUpdaterHostDoesNotRegisterFrameworkRestart(t *testing.T) {
	// A nil app would panic if this delegated to the Wails event bus. The
	// framework must never get a listener that spawns a helper before approval.
	host := &updaterHost{}
	off := host.OnEvent(updater.EventUserRestart, func(any) { t.Fatal("unguarded restart") })
	off()
}

func TestUpdaterRestartWaitsForSettingsApproval(t *testing.T) {
	dirty, prompted, restarts := true, 0, 0
	handler := &updaterRestartHandler{
		allowRestart: func() bool {
			if dirty {
				prompted++
				return false
			}
			return true
		},
		restart: func() error { restarts++; return nil },
	}
	if err := handler.request(); err != nil || prompted != 1 || restarts != 0 {
		t.Fatalf("dirty request must only prompt: err=%v prompts=%d restarts=%d", err, prompted, restarts)
	}
	// Continuing to edit leaves the helper unstarted. A subsequent request must
	// prompt again, without retaining an earlier approval or restart intent.
	_ = handler.request()
	if prompted != 2 || restarts != 0 {
		t.Fatal("cancelled confirmation must not start a helper")
	}
	dirty = false // Settings were saved or their discard was confirmed.
	_ = handler.request()
	_ = handler.request()
	if restarts != 1 {
		t.Fatalf("approved restart must spawn exactly one helper, got %d", restarts)
	}
}

func TestUpdaterRestartRetriesSpawnFailure(t *testing.T) {
	spawnErr := errors.New("cannot spawn")
	calls := 0
	handler := &updaterRestartHandler{
		allowRestart: func() bool { return true },
		restart: func() error {
			calls++
			if calls == 1 {
				return spawnErr
			}
			return nil
		},
	}
	if err := handler.request(); !errors.Is(err, spawnErr) {
		t.Fatalf("expected spawn error, got %v", err)
	}
	if err := handler.request(); err != nil || calls != 2 {
		t.Fatalf("failed restart must be retryable: err=%v calls=%d", err, calls)
	}
}

func TestUpdaterRestartCoalescesConcurrentRequests(t *testing.T) {
	var calls atomic.Int32
	handler := &updaterRestartHandler{
		allowRestart: func() bool { return true },
		restart:      func() error { calls.Add(1); return nil },
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { _ = handler.request() })
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent restart requests spawned %d helpers", calls.Load())
	}
}
