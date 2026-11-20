package processattribution

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
)

func init() {
	// TestMain must retain the process's initial thread when pumping AppKit.
	if os.Getenv("FLOWLENS_APPKIT_TEST") == "1" {
		runtime.LockOSThread()
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("FLOWLENS_APPKIT_TEST") != "1" {
		os.Exit(m.Run())
	}
	api, err := loadDarwinAppKit()
	if err != nil {
		panic(err)
	}
	pool := api.poolPush()
	api.msg0(api.class("NSApplication"), api.selector("sharedApplication"))
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		panic(err)
	}
	var run func(uintptr, float64, uint8) int32
	purego.RegisterLibFunc(&run, cf, "CFRunLoopRunInMode")
	modeAddress, err := purego.Dlsym(cf, "kCFRunLoopDefaultMode")
	if err != nil {
		panic(err)
	}
	mode := **(**uintptr)(unsafe.Pointer(&modeAddress))
	finished := make(chan int, 1)
	go func() { finished <- m.Run() }()
	for {
		select {
		case code := <-finished:
			api.poolPop(pool)
			os.Exit(code)
		default:
			run(mode, 0.01, 0)
		}
	}
}

func TestDarwinAppKitMainLoop(t *testing.T) {
	if os.Getenv("FLOWLENS_APPKIT_TEST") != "1" {
		t.Skip("run task test:macos:appkit in a macOS GUI session")
	}
	api, err := loadDarwinAppKit()
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "FlowLensFixture.app", "Contents")
	executable := filepath.Join(bundle, "MacOS", "fixture")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.flowlens.bindingfixture</string><key>CFBundleName</key><string>Fixture fallback</string><key>CFBundleDisplayName</key><string>FlowLens Native Fixture</string><key>CFBundleExecutable</key><string>fixture</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>`
	if err := os.WriteFile(filepath.Join(bundle, "Info.plist"), []byte(plist), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		value, err := api.queue.invoke(context.Background(), func() (any, error) {
			if api.isMain() == 0 {
				return nil, os.ErrInvalid
			}
			return api.metadata(0, darwinProcessIdentity{executablePath: executable, processName: "fixture"}), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		identity := value.(darwinProcessIdentity)
		if identity.appID != "com.flowlens.bindingfixture" || identity.displayName != "FlowLens Native Fixture" {
			t.Fatalf("bundle metadata: %+v", identity)
		}
		for _, path := range []string{executable, "/bin/sh"} {
			value, err := api.queue.invoke(context.Background(), func() (any, error) { return api.iconPNG(0, path) })
			if err != nil {
				t.Fatal(err)
			}
			icon, err := png.Decode(bytes.NewReader(value.([]byte)))
			if err != nil || icon.Bounds().Empty() {
				t.Fatalf("invalid AppKit PNG for %s: %v", path, err)
			}
		}
	}
}

func TestDarwinAppKitRepresentationOwnership(t *testing.T) {
	for _, empty := range []bool{false, true} {
		selectors := make(map[string]uintptr)
		for i, name := range []string{"runningApplicationWithProcessIdentifier:", "icon", "executableURL", "path", "alloc", "initWithCGImage:", "dictionary", "representationUsingType:properties:", "length", "bytes", "release", "CGImageForProposedRect:context:hints:"} {
			selectors[name] = uintptr(i + 1)
		}
		released := 0
		buffer := []byte{1, 2, 3}
		api := &darwinAppKitAPI{classes: map[string]uintptr{"NSRunningApplication": 1, "NSBitmapImageRep": 2, "NSDictionary": 3}, selectors: selectors}
		api.msg0 = func(object, selector uintptr) uintptr {
			switch selector {
			case selectors["icon"]:
				return 10
			case selectors["alloc"]:
				return 20
			case selectors["dictionary"]:
				return 30
			case selectors["length"]:
				if empty {
					return 0
				}
				return uintptr(len(buffer))
			case selectors["release"]:
				if object != 21 {
					t.Fatal("released borrowed object")
				}
				released++
			}
			return 0
		}
		api.msg1 = func(_, selector, _ uintptr) uintptr {
			if selector == selectors["initWithCGImage:"] {
				return 21
			}
			return 5
		}
		api.msg2 = func(uintptr, uintptr, uintptr, uintptr) uintptr { return 40 }
		api.msg3 = func(uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 50 }
		api.msgBytes = func(uintptr, uintptr) *byte { return &buffer[0] }
		data, err := api.iconPNG(42, "")
		if released != 1 || (err != nil) != empty {
			t.Fatalf("empty=%v release=%d err=%v", empty, released, err)
		}
		if !empty {
			buffer[0] = 9
			if data[0] != 1 {
				t.Fatal("returned native bytes without copying")
			}
		}
	}
}
