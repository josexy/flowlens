//go:build windows

package processattribution

import (
	"context"
	"errors"
	"image"
	"os"
	"sync/atomic"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsIconResourcesAreReleased(t *testing.T) {
	var destroys atomic.Int32
	functions := windowsIconFunctions{
		extract: func(string) (windows.Handle, error) { return windows.Handle(123), nil },
		convert: func(windows.Handle) (image.Image, error) { return nil, errors.New("convert failed") },
		destroy: func(windows.Handle) error {
			destroys.Add(1)
			return nil
		},
	}
	if _, err := loadWindowsIconWithFunctions("C:/test/app.exe", functions); err == nil {
		t.Fatal("loadWindowsIconWithFunctions succeeded, want conversion error")
	}
	if got := destroys.Load(); got != 1 {
		t.Fatalf("DestroyIcon calls = %d, want 1", got)
	}
}

func TestWindowsProviderLoadsCurrentExecutableIcon(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}
	icon, err := newWindowsProvider().LoadIcon(context.Background(), Result{ExecutablePath: executable})
	if err != nil {
		t.Fatalf("LoadIcon: %v", err)
	}
	if bounds := icon.Bounds(); bounds.Dx() != windowsRenderedIconSize || bounds.Dy() != windowsRenderedIconSize {
		t.Fatalf("icon bounds = %v, want %dx%d", bounds, windowsRenderedIconSize, windowsRenderedIconSize)
	}
}
