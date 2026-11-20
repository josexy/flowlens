package processattribution

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// All Objective-C messages here have pointer/integer arguments and returns;
// none return structs or floats (objc_msgSend_stret is therefore unnecessary).
// Getters return borrowed/autoreleased objects. Only alloc/init and the pool
// create ownership; they are released on the calling main OS thread.
type darwinAppKitAPI struct {
	class     func(string) uintptr
	selector  func(string) uintptr
	msg0      func(uintptr, uintptr) uintptr
	msgBytes  func(uintptr, uintptr) *byte
	msg1      func(uintptr, uintptr, uintptr) uintptr
	msg2      func(uintptr, uintptr, uintptr, uintptr) uintptr
	msg3      func(uintptr, uintptr, uintptr, uintptr, uintptr) uintptr
	poolPush  func() uintptr
	poolPop   func(uintptr)
	isMain    func() int32
	async     func(uintptr, uintptr, uintptr)
	queue     *appKitQueue
	classes   map[string]uintptr
	selectors map[string]uintptr
}

var loadDarwinAppKit = sync.OnceValues(func() (*darwinAppKitAPI, error) {
	return bindDarwinAppKit(purego.Dlopen, purego.Dlsym)
})

func bindDarwinAppKit(open func(string, int) (uintptr, error), symbol func(uintptr, string) (uintptr, error)) (*darwinAppKitAPI, error) {
	// No Dlclose: callbacks, classes and message addresses have process lifetime.
	for _, library := range []string{"/System/Library/Frameworks/AppKit.framework/AppKit", "/usr/lib/libobjc.A.dylib", "/usr/lib/libSystem.B.dylib"} {
		if _, err := open(library, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
			return nil, err
		}
	}
	api := &darwinAppKitAPI{classes: make(map[string]uintptr), selectors: make(map[string]uintptr)}
	for _, binding := range []struct {
		name   string
		target any
	}{
		{"objc_getClass", &api.class}, {"sel_registerName", &api.selector},
		{"objc_msgSend", &api.msg0}, {"objc_msgSend", &api.msgBytes}, {"objc_msgSend", &api.msg1}, {"objc_msgSend", &api.msg2}, {"objc_msgSend", &api.msg3},
		{"objc_autoreleasePoolPush", &api.poolPush}, {"objc_autoreleasePoolPop", &api.poolPop},
		{"pthread_main_np", &api.isMain}, {"dispatch_async_f", &api.async},
	} {
		address, err := symbol(purego.RTLD_DEFAULT, binding.name)
		if err != nil || address == 0 {
			return nil, fmt.Errorf("resolve %s: %v", binding.name, err)
		}
		purego.RegisterFunc(binding.target, address)
	}
	// dispatch_get_main_queue is a macro: the queue IS the exported structure's
	// address, unlike a CFString pointer constant. Do not dereference this symbol.
	mainQueue, err := symbol(purego.RTLD_DEFAULT, "_dispatch_main_q")
	if err != nil || mainQueue == 0 {
		return nil, fmt.Errorf("resolve main dispatch queue: %v", err)
	}
	for _, name := range []string{"NSRunningApplication", "NSBundle", "NSWorkspace", "NSBitmapImageRep", "NSString", "NSDictionary"} {
		api.classes[name] = api.class(name)
		if api.classes[name] == 0 {
			return nil, fmt.Errorf("AppKit class %s unavailable", name)
		}
	}
	for _, name := range []string{"runningApplicationWithProcessIdentifier:", "executableURL", "path", "bundleIdentifier", "localizedName", "bundleWithPath:", "localizedInfoDictionary", "infoDictionary", "objectForKey:", "stringWithUTF8String:", "UTF8String", "lengthOfBytesUsingEncoding:", "icon", "bundlePath", "sharedWorkspace", "iconForFile:", "CGImageForProposedRect:context:hints:", "alloc", "initWithCGImage:", "representationUsingType:properties:", "dictionary", "release", "length", "bytes", "isKindOfClass:"} {
		api.selectors[name] = api.selector(name)
		if api.selectors[name] == 0 {
			return nil, fmt.Errorf("AppKit selector %s unavailable", name)
		}
	}
	api.queue = &appKitQueue{isMain: func() bool { return api.isMain() != 0 }, scope: func(call func() (any, error)) (any, error) {
		pool := api.poolPush()
		defer api.poolPop(pool)
		return call()
	}}
	callback := purego.NewCallback(func(id uintptr) { api.queue.run(id) })
	api.queue.dispatch = func(id uintptr) { api.async(mainQueue, id, callback) }
	return api, nil
}

func (a *darwinAppKitAPI) get(object uintptr, name string) uintptr {
	return a.msg0(object, a.selectors[name])
}
func (a *darwinAppKitAPI) send(object uintptr, name string, arg uintptr) uintptr {
	return a.msg1(object, a.selectors[name], arg)
}

func (a *darwinAppKitAPI) nsString(value string) uintptr {
	buffer := append([]byte(value), 0)
	result := a.send(a.classes["NSString"], "stringWithUTF8String:", uintptr(unsafe.Pointer(&buffer[0])))
	runtime.KeepAlive(buffer)
	return result
}

func (a *darwinAppKitAPI) goString(object uintptr) string {
	if object == 0 || a.send(object, "isKindOfClass:", a.classes["NSString"])&0xff == 0 {
		return ""
	}
	length := a.send(object, "lengthOfBytesUsingEncoding:", 4 /* NSUTF8StringEncoding */)
	if length == 0 || length > 1<<20 {
		return ""
	}
	address := a.msgBytes(object, a.selectors["UTF8String"])
	if address == nil {
		return ""
	}
	return string(unsafe.Slice(address, int(length)))
}

func (a *darwinAppKitAPI) bundle(path string) uintptr {
	var bundle uintptr
	for candidate := filepath.Dir(path); len(candidate) > 1; candidate = filepath.Dir(candidate) {
		if strings.EqualFold(filepath.Ext(candidate), ".app") {
			if next := a.send(a.classes["NSBundle"], "bundleWithPath:", a.nsString(candidate)); next != 0 {
				bundle = next
			}
		}
		if filepath.Dir(candidate) == candidate {
			break
		}
	}
	return bundle
}

func (a *darwinAppKitAPI) metadata(pid uint32, identity darwinProcessIdentity) darwinProcessIdentity {
	running := a.send(a.classes["NSRunningApplication"], "runningApplicationWithProcessIdentifier:", uintptr(pid))
	if path := a.goString(a.get(a.get(running, "executableURL"), "path")); path != "" {
		identity.executablePath = path
	}
	bundle := a.bundle(identity.executablePath)
	identity.appID = a.goString(a.get(running, "bundleIdentifier"))
	if identity.appID == "" {
		identity.appID = a.goString(a.get(bundle, "bundleIdentifier"))
	}
	identity.displayName = a.goString(a.get(running, "localizedName"))
	for _, source := range []string{"localizedInfoDictionary", "infoDictionary"} {
		if identity.displayName != "" {
			break
		}
		dictionary := a.get(bundle, source)
		for _, key := range []string{"CFBundleDisplayName", "CFBundleName"} {
			identity.displayName = a.goString(a.send(dictionary, "objectForKey:", a.nsString(key)))
			if identity.displayName != "" {
				break
			}
		}
	}
	if identity.displayName == "" {
		identity.displayName = identity.processName
	}
	if identity.displayName != "" && (identity.appID != "" || identity.displayName != identity.processName) {
		identity.identityConfidence = "exact"
	}
	return identity
}

func enrichDarwinProcessIdentity(ctx context.Context, pid uint32, identity darwinProcessIdentity) darwinProcessIdentity {
	api, err := loadDarwinAppKit()
	if err != nil {
		return identity
	}
	value, err := api.queue.invoke(ctx, func() (any, error) { return api.metadata(pid, identity), nil })
	if err != nil {
		return identity
	}
	return value.(darwinProcessIdentity)
}

func (a *darwinAppKitAPI) iconPNG(pid uint32, path string) ([]byte, error) {
	running := a.send(a.classes["NSRunningApplication"], "runningApplicationWithProcessIdentifier:", uintptr(pid))
	icon := a.get(running, "icon")
	if runningPath := a.goString(a.get(a.get(running, "executableURL"), "path")); runningPath != "" {
		path = runningPath
	}
	if icon == 0 {
		bundlePath := a.get(a.bundle(path), "bundlePath")
		if a.goString(bundlePath) != "" {
			icon = a.send(a.get(a.classes["NSWorkspace"], "sharedWorkspace"), "iconForFile:", bundlePath)
		}
	}
	if icon == 0 && path != "" {
		icon = a.send(a.get(a.classes["NSWorkspace"], "sharedWorkspace"), "iconForFile:", a.nsString(path))
	}
	if icon == 0 {
		return nil, &IconUnavailableError{Reason: "darwin_icon_missing"}
	}
	cgImage := a.msg3(icon, a.selectors["CGImageForProposedRect:context:hints:"], 0, 0, 0)
	if cgImage == 0 {
		return nil, &IconUnavailableError{Reason: "darwin_icon_missing"}
	}
	rep := a.send(a.get(a.classes["NSBitmapImageRep"], "alloc"), "initWithCGImage:", cgImage)
	if rep == 0 {
		return nil, &IconUnavailableError{Reason: "darwin_icon_representation"}
	}
	defer a.get(rep, "release")
	data := a.msg2(rep, a.selectors["representationUsingType:properties:"], 4 /* NSBitmapImageFileTypePNG */, a.get(a.classes["NSDictionary"], "dictionary"))
	length := a.get(data, "length")
	address := a.msgBytes(data, a.selectors["bytes"])
	if length == 0 || address == nil {
		return nil, &IconUnavailableError{Reason: "darwin_icon_empty"}
	}
	if length > 32<<20 {
		return nil, &IconUnavailableError{Reason: "darwin_icon_too_large"}
	}
	return bytes.Clone(unsafe.Slice(address, int(length))), nil
}

func loadDarwinProcessIcon(ctx context.Context, pid uint32, executablePath string) (image.Image, error) {
	api, err := loadDarwinAppKit()
	if err != nil {
		return nil, err
	}
	value, err := api.queue.invoke(ctx, func() (any, error) { return api.iconPNG(pid, executablePath) })
	if err != nil {
		return nil, err
	}
	decoded, err := png.Decode(bytes.NewReader(value.([]byte)))
	if err != nil {
		return nil, fmt.Errorf("decode Darwin process icon: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return decoded, nil
}
