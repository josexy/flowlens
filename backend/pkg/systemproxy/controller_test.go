package systemproxy

import (
	"errors"
	"testing"
)

type fakePlatformDriver struct {
	supported      bool
	supportedModes map[Mode]bool
	snapshot       any
	snapshotCalls  int
	applyCalls     []Endpoint
	applyErr       error
	matches        bool
	restoreCalls   int
	restoreErr     error
}

func (f *fakePlatformDriver) Supported() bool { return f.supported }

func (f *fakePlatformDriver) Supports(mode Mode) bool {
	if f.supportedModes == nil {
		return f.supported
	}
	return f.supportedModes[mode]
}

func (f *fakePlatformDriver) Snapshot() (any, error) {
	f.snapshotCalls++
	return f.snapshot, nil
}

func (f *fakePlatformDriver) Apply(endpoint Endpoint) error {
	f.applyCalls = append(f.applyCalls, endpoint)
	return f.applyErr
}

func (f *fakePlatformDriver) Matches(Endpoint) (bool, error) {
	return f.matches, nil
}

func (f *fakePlatformDriver) Restore(snapshot any) error {
	if snapshot != f.snapshot {
		return errors.New("unexpected snapshot")
	}
	f.restoreCalls++
	return f.restoreErr
}

func TestControllerSnapshotsOnceUpdatesAndRestores(t *testing.T) {
	driver := &fakePlatformDriver{supported: true, snapshot: "original", matches: true}
	controller := newController(driver)
	first := Endpoint{Mode: ModeHTTP, Host: "127.0.0.1", Port: 8080}
	second := Endpoint{Mode: ModeSOCKS5, Host: "127.0.0.1", Port: 1080}

	if err := controller.Apply(first, "http://proxy.example:3128"); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if err := controller.Apply(second, "http://ignored.example:8080"); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if driver.snapshotCalls != 1 {
		t.Fatalf("snapshot calls = %d, want 1", driver.snapshotCalls)
	}
	if len(driver.applyCalls) != 2 || driver.applyCalls[1] != second {
		t.Fatalf("unexpected apply calls: %#v", driver.applyCalls)
	}
	state := controller.State()
	if !state.Active || state.Endpoint != second || state.OriginalUpstreamProxy != "http://proxy.example:3128" {
		t.Fatalf("unexpected controller state: %#v", state)
	}

	if err := controller.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if driver.restoreCalls != 1 || controller.State().Active {
		t.Fatalf("restore did not clear controller: calls=%d state=%#v", driver.restoreCalls, controller.State())
	}
}

func TestControllerDoesNotOverwriteExternalChanges(t *testing.T) {
	driver := &fakePlatformDriver{supported: true, snapshot: "original", matches: true}
	controller := newController(driver)
	endpoint := Endpoint{Mode: ModeHTTP, Host: "127.0.0.1", Port: 8080}
	if err := controller.Apply(endpoint, ""); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	driver.matches = false
	if err := controller.Restore(); !errors.Is(err, ErrChangedExternally) {
		t.Fatalf("Restore error = %v, want ErrChangedExternally", err)
	}
	if driver.restoreCalls != 0 || controller.State().Active {
		t.Fatalf("external change should be preserved: calls=%d state=%#v", driver.restoreCalls, controller.State())
	}
}

func TestControllerDoesNotRestoreAfterAtomicInitialConflict(t *testing.T) {
	driver := &fakePlatformDriver{
		supported: true,
		snapshot:  "original",
		matches:   true,
		applyErr:  ErrChangedExternally,
	}
	controller := newController(driver)
	err := controller.Apply(Endpoint{Mode: ModeHTTP, Host: "127.0.0.1", Port: 8080}, "")
	if !errors.Is(err, ErrChangedExternally) {
		t.Fatalf("Apply error = %v, want ErrChangedExternally", err)
	}
	if driver.restoreCalls != 0 || controller.State().Active {
		t.Fatalf("initial conflict must not restore stale state: calls=%d state=%#v", driver.restoreCalls, controller.State())
	}
}

func TestControllerClearsOwnershipAfterAtomicRestoreConflict(t *testing.T) {
	driver := &fakePlatformDriver{supported: true, snapshot: "original", matches: true}
	controller := newController(driver)
	endpoint := Endpoint{Mode: ModeHTTP, Host: "127.0.0.1", Port: 8080}
	if err := controller.Apply(endpoint, ""); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	driver.restoreErr = ErrChangedExternally
	if err := controller.Restore(); !errors.Is(err, ErrChangedExternally) {
		t.Fatalf("Restore error = %v, want ErrChangedExternally", err)
	}
	if controller.State().Active {
		t.Fatalf("atomic restore conflict should release ownership: %#v", controller.State())
	}
}

func TestControllerUnsupported(t *testing.T) {
	driver := &fakePlatformDriver{supported: false}
	controller := newController(driver)
	err := controller.Apply(Endpoint{Mode: ModeHTTP, Host: "127.0.0.1", Port: 8080}, "")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Apply error = %v, want ErrUnsupported", err)
	}
	if controller.State().Supported {
		t.Fatal("unsupported driver reported supported")
	}
}

func TestControllerRejectsUnsupportedModeBeforeSnapshot(t *testing.T) {
	driver := &fakePlatformDriver{
		supported:      true,
		supportedModes: map[Mode]bool{ModeHTTP: true},
	}
	controller := newController(driver)
	err := controller.Apply(Endpoint{Mode: ModeSOCKS5, Host: "127.0.0.1", Port: 1080}, "")
	if !errors.Is(err, ErrUnsupportedMode) {
		t.Fatalf("Apply error = %v, want ErrUnsupportedMode", err)
	}
	if driver.snapshotCalls != 0 || len(driver.applyCalls) != 0 {
		t.Fatalf("unsupported mode reached platform driver: snapshots=%d applies=%d", driver.snapshotCalls, len(driver.applyCalls))
	}
}

func TestProxyServerValue(t *testing.T) {
	if got := ProxyServerValue(Endpoint{Mode: ModeHTTP, Host: "127.0.0.1", Port: 8080}); got != "http=127.0.0.1:8080;https=127.0.0.1:8080" {
		t.Fatalf("HTTP proxy server = %q", got)
	}
	if got := ProxyServerValue(Endpoint{Mode: ModeSOCKS5, Host: "127.0.0.1", Port: 1080}); got != "socks=127.0.0.1:1080" {
		t.Fatalf("SOCKS proxy server = %q", got)
	}
}
