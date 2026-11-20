package processattribution

import (
	"errors"
	"syscall"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
)

func TestDarwinProcLayout(t *testing.T) {
	var info darwinBSDInfo
	if unsafe.Sizeof(info) != 136 || unsafe.Offsetof(info.Comm) != 48 || unsafe.Offsetof(info.Name) != 64 || unsafe.Offsetof(info.StartSeconds) != 120 || unsafe.Offsetof(info.StartMicroseconds) != 128 {
		t.Fatal("proc_bsdinfo ABI mismatch")
	}
}

func TestDarwinProcResponses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		count   int32
		errno   int32
		path    int32
		wantErr error
	}{
		{"success", 136, 0, 8, nil}, {"pathDenied", 136, 0, 0, nil},
		{"short", 135, 0, 0, syscall.ESRCH}, {"exited", 0, 3, 0, syscall.ESRCH},
		{"denied", 0, 1, 0, syscall.EPERM}, {"access", 0, 13, 0, syscall.EACCES},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var errno int32
			api := darwinProcAPI{errno: func() *int32 { return &errno }, pidInfo: func(pid, flavor int32, arg uint64, out *darwinBSDInfo, size int32) int32 {
				if pid != 42 || flavor != 3 || arg != 0 || size != 136 {
					t.Fatal("wrong libproc arguments")
				}
				out.StartSeconds, out.StartMicroseconds = 123, 456
				copy(out.Name[:], "fixture")
				errno = tc.errno
				return tc.count
			}, pidPath: func(_ int32, out *byte, size uint32) int32 { copy(unsafe.Slice(out, size), "/fixture"); return tc.path }}
			identity, err := api.identity(42)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("identity error: %v", err)
			}
			if err != nil {
				if identity.startToken != "" {
					t.Fatal("fabricated identity")
				}
				return
			}
			if identity.startToken != "123:456" || identity.processName != "fixture" || identity.metadataDenied != (tc.path == 0) {
				t.Fatalf("identity: %+v", identity)
			}
		})
	}
}

func TestDarwinNativeBindingFailures(t *testing.T) {
	failure := errors.New("missing framework")
	open := func(string, int) (uintptr, error) { return 0, failure }
	if _, err := bindDarwinProc(open, nil); err == nil {
		t.Fatal("missing libproc accepted")
	}
	if _, err := bindDarwinAppKit(open, nil); err == nil {
		t.Fatal("missing AppKit accepted")
	}
	for _, missing := range []string{"proc_pidinfo", "proc_pidpath", "__error"} {
		_, err := bindDarwinProc(purego.Dlopen, func(h uintptr, name string) (uintptr, error) {
			if name == missing {
				return 0, failure
			}
			return purego.Dlsym(h, name)
		})
		if err == nil {
			t.Fatalf("missing %s accepted", missing)
		}
	}
	if _, err := bindDarwinAppKit(purego.Dlopen, func(uintptr, string) (uintptr, error) { return 0, failure }); err == nil {
		t.Fatal("missing objc symbol accepted")
	}
}
