//go:build windows

package systemproxy

import (
	"testing"
	"unsafe"
)

func TestInternetPerConnOptionLayout(t *testing.T) {
	pointerSize := unsafe.Sizeof(uintptr(0))
	wantValueOffset := pointerSize
	if got := unsafe.Offsetof(internetPerConnOption{}.Value); got != wantValueOffset {
		t.Fatalf("Value offset = %d, want %d", got, wantValueOffset)
	}
	if got, want := unsafe.Sizeof(internetPerConnOptionValue{}), uintptr(8); got != want {
		t.Fatalf("union size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(internetPerConnOption{}), wantValueOffset+8; got != want {
		t.Fatalf("option size = %d, want %d", got, want)
	}
}

func TestWindowsSystemProxySupportsHTTPOnly(t *testing.T) {
	driver := windowsPlatformDriver{}
	if !driver.Supports(ModeHTTP) {
		t.Fatal("expected Windows system proxy to support HTTP mode")
	}
	if driver.Supports(ModeSOCKS5) {
		t.Fatal("Windows system proxy must reject SOCKS5 mode")
	}
}
