package systemproxy

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const cfUTF8 = 0x08000100
const cfInt32 = 3 // kCFNumberSInt32Type
const maxProxyPropertyBytes = 16 << 20

// The snapshotted service no longer exists in preferences. FlowLens therefore
// no longer owns its configuration; callers report an external change.
var errDarwinServiceMissing = errors.New("macOS network service disappeared")

// CF/SC references are opaque uintptrs. CFIndex/CFOptionFlags/CFTypeID are
// 64-bit on both targets; Boolean is uint8, OSStatus is int32. Create/Copy
// return owned references; Get returns a borrowed value. Callers release owned
// values, keep borrowed values inside their owner's lifetime, and copy bytes
// into Go before releasing CFData/CFString. Go pointer parameters are borrowed
// only during synchronous calls. Libraries and constant objects live forever.
type darwinProxyAPI struct {
	release                                       func(uintptr)
	retain                                        func(uintptr) uintptr
	typeID                                        func(uintptr) uint64
	equal                                         func(uintptr, uintptr) uint8
	dictType, stringType, numberType, booleanType func() uint64
	dictGet                                       func(uintptr, uintptr) uintptr
	dictSet                                       func(uintptr, uintptr, uintptr)
	dictCreate                                    func(uintptr, int64, uintptr, uintptr) uintptr
	dictCopy                                      func(uintptr, int64, uintptr) uintptr
	stringCreate                                  func(uintptr, *byte, int64, uint32, uint8) uintptr
	stringLength                                  func(uintptr) int64
	stringMax                                     func(int64, uint32) int64
	stringCopy                                    func(uintptr, *byte, int64, uint32) uint8
	boolValue                                     func(uintptr) uint8
	numberValue                                   func(uintptr, int64, *int32) uint8
	numberCreate                                  func(uintptr, int64, *int32) uintptr
	dataCreate                                    func(uintptr, *byte, int64) uintptr
	dataLength                                    func(uintptr) int64
	dataBytes                                     func(uintptr) *byte
	decode                                        func(uintptr, uintptr, uint64, *int64, *uintptr) uintptr
	encode                                        func(uintptr, uintptr, int64, uint64, *uintptr) uintptr
	arrayCount                                    func(uintptr) int64
	arrayGet                                      func(uintptr, int64) uintptr
	copyProxies                                   func(uintptr) uintptr
	copyValue                                     func(uintptr, uintptr) uintptr
	prefsCreate                                   func(uintptr, uintptr, uintptr) uintptr
	prefsAuthorized                               func(uintptr, uintptr, uintptr, uintptr) uintptr
	lock                                          func(uintptr, uint8) uint8
	unlock, commit, apply                         func(uintptr) uint8
	synchronize                                   func(uintptr)
	services                                      func(uintptr) uintptr
	serviceID                                     func(uintptr) uintptr
	protocol                                      func(uintptr, uintptr) uintptr
	addProtocol                                   func(uintptr, uintptr) uint8
	configuration                                 func(uintptr) uintptr
	setConfiguration                              func(uintptr, uintptr) uint8
	authorize                                     func(uintptr, uintptr, uint32, *uintptr) int32
	freeAuthorization                             func(uintptr, uint32) int32
	keys                                          map[string]uintptr
	keyCallbacks, valueCallbacks                  uintptr
}

var loadDarwinProxy = sync.OnceValues(func() (*darwinProxyAPI, error) {
	return bindDarwinProxy(purego.Dlopen, purego.Dlsym)
})

func bindDarwinProxy(open func(string, int) (uintptr, error), symbol func(uintptr, string) (uintptr, error)) (*darwinProxyAPI, error) {
	api := &darwinProxyAPI{keys: make(map[string]uintptr)}
	cf, err := open("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	sc, err := open("/System/Library/Frameworks/SystemConfiguration.framework/SystemConfiguration", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	security, err := open("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	// Check Dlsym explicitly instead of RegisterLibFunc's panic-on-missing path.
	for _, b := range []struct {
		library uintptr
		name    string
		target  any
	}{
		{cf, "CFRelease", &api.release}, {cf, "CFRetain", &api.retain}, {cf, "CFGetTypeID", &api.typeID}, {cf, "CFEqual", &api.equal},
		{cf, "CFDictionaryGetTypeID", &api.dictType}, {cf, "CFStringGetTypeID", &api.stringType}, {cf, "CFNumberGetTypeID", &api.numberType}, {cf, "CFBooleanGetTypeID", &api.booleanType},
		{cf, "CFDictionaryGetValue", &api.dictGet}, {cf, "CFDictionarySetValue", &api.dictSet}, {cf, "CFDictionaryCreateMutable", &api.dictCreate}, {cf, "CFDictionaryCreateMutableCopy", &api.dictCopy},
		{cf, "CFStringCreateWithBytes", &api.stringCreate}, {cf, "CFStringGetLength", &api.stringLength}, {cf, "CFStringGetMaximumSizeForEncoding", &api.stringMax}, {cf, "CFStringGetCString", &api.stringCopy},
		{cf, "CFBooleanGetValue", &api.boolValue}, {cf, "CFNumberGetValue", &api.numberValue}, {cf, "CFNumberCreate", &api.numberCreate},
		{cf, "CFDataCreate", &api.dataCreate}, {cf, "CFDataGetLength", &api.dataLength}, {cf, "CFDataGetBytePtr", &api.dataBytes},
		{cf, "CFPropertyListCreateWithData", &api.decode}, {cf, "CFPropertyListCreateData", &api.encode},
		{cf, "CFArrayGetCount", &api.arrayCount}, {cf, "CFArrayGetValueAtIndex", &api.arrayGet},
		{sc, "SCDynamicStoreCopyProxies", &api.copyProxies}, {sc, "SCDynamicStoreCopyValue", &api.copyValue},
		{sc, "SCPreferencesCreate", &api.prefsCreate}, {sc, "SCPreferencesCreateWithAuthorization", &api.prefsAuthorized},
		{sc, "SCPreferencesLock", &api.lock}, {sc, "SCPreferencesUnlock", &api.unlock}, {sc, "SCPreferencesCommitChanges", &api.commit}, {sc, "SCPreferencesApplyChanges", &api.apply}, {sc, "SCPreferencesSynchronize", &api.synchronize},
		{sc, "SCNetworkServiceCopyAll", &api.services}, {sc, "SCNetworkServiceGetServiceID", &api.serviceID}, {sc, "SCNetworkServiceCopyProtocol", &api.protocol}, {sc, "SCNetworkServiceAddProtocolType", &api.addProtocol},
		{sc, "SCNetworkProtocolGetConfiguration", &api.configuration}, {sc, "SCNetworkProtocolSetConfiguration", &api.setConfiguration},
		{security, "AuthorizationCreate", &api.authorize}, {security, "AuthorizationFree", &api.freeAuthorization},
	} {
		address, err := symbol(b.library, b.name)
		if err != nil || address == 0 {
			return nil, fmt.Errorf("resolve %s: %v", b.name, err)
		}
		purego.RegisterFunc(b.target, address)
	}
	for _, name := range []string{"kCFTypeDictionaryKeyCallBacks", "kCFTypeDictionaryValueCallBacks"} {
		address, err := symbol(cf, name)
		if err != nil || address == 0 {
			return nil, fmt.Errorf("resolve %s: %v", name, err)
		}
		// These exported callback STRUCTURES are passed by address, not loaded
		// as pointer variables (their first word is a zero version number).
		if name == "kCFTypeDictionaryKeyCallBacks" {
			api.keyCallbacks = address
		} else {
			api.valueCallbacks = address
		}
	}
	for _, name := range []string{"kSCDynamicStorePropNetPrimaryService", "kSCNetworkProtocolTypeProxies", "kSCPropNetProxiesHTTPEnable", "kSCPropNetProxiesHTTPProxy", "kSCPropNetProxiesHTTPPort", "kSCPropNetProxiesHTTPSEnable", "kSCPropNetProxiesHTTPSProxy", "kSCPropNetProxiesHTTPSPort", "kSCPropNetProxiesSOCKSEnable", "kSCPropNetProxiesSOCKSProxy", "kSCPropNetProxiesSOCKSPort", "kSCPropNetProxiesProxyAutoConfigEnable", "kSCPropNetProxiesProxyAutoDiscoveryEnable"} {
		address, err := symbol(sc, name)
		if err != nil || address == 0 {
			return nil, fmt.Errorf("resolve %s: %v", name, err)
		}
		// Exported CFStringRef VARIABLES contain a pointer: dereference once.
		api.keys[name] = **(**uintptr)(unsafe.Pointer(&address))
		if api.keys[name] == 0 {
			return nil, fmt.Errorf("null CFString constant %s", name)
		}
	}
	return api, nil
}

func (a *darwinProxyAPI) string(value string) uintptr {
	buffer := []byte(value)
	ref := a.stringCreate(0, unsafe.SliceData(buffer), int64(len(buffer)), cfUTF8, 0)
	runtime.KeepAlive(buffer)
	return ref
}

func (a *darwinProxyAPI) text(ref uintptr) string {
	if ref == 0 || a.typeID(ref) != a.stringType() {
		return ""
	}
	length := a.stringLength(ref)
	if length < 0 || length > maxProxyPropertyBytes {
		return ""
	}
	size := a.stringMax(length, cfUTF8)
	if size < 0 || size >= maxProxyPropertyBytes {
		return ""
	}
	buffer := make([]byte, size+1)
	if a.stringCopy(ref, &buffer[0], int64(len(buffer)), cfUTF8) == 0 {
		return ""
	}
	if end := bytes.IndexByte(buffer, 0); end >= 0 {
		return string(buffer[:end])
	}
	return ""
}

func (a *darwinProxyAPI) emptyDictionary() uintptr {
	return a.dictCreate(0, 0, a.keyCallbacks, a.valueCallbacks)
}

func (a *darwinProxyAPI) serialize(dictionary uintptr) ([]byte, error) {
	if dictionary == 0 || a.typeID(dictionary) != a.dictType() {
		return nil, errors.New("invalid macOS proxy dictionary")
	}
	var failure uintptr
	data := a.encode(0, dictionary, 200 /* kCFPropertyListBinaryFormat_v1_0 */, 0, &failure)
	if failure != 0 {
		a.release(failure)
	}
	if data == 0 {
		return nil, errors.New("serialize macOS proxy dictionary")
	}
	defer a.release(data)
	length, address := a.dataLength(data), a.dataBytes(data)
	if length <= 0 || length > maxProxyPropertyBytes || address == nil {
		return nil, errors.New("invalid macOS proxy data length")
	}
	return bytes.Clone(unsafe.Slice(address, int(length))), nil
}

func (a *darwinProxyAPI) dictionary(buffer []byte) (uintptr, error) {
	if len(buffer) == 0 || len(buffer) > maxProxyPropertyBytes {
		return 0, errors.New("invalid macOS proxy snapshot length")
	}
	data := a.dataCreate(0, &buffer[0], int64(len(buffer)))
	runtime.KeepAlive(buffer)
	if data == 0 {
		return 0, errors.New("create macOS proxy data")
	}
	defer a.release(data)
	var failure uintptr
	property := a.decode(0, data, 0 /* immutable */, nil, &failure)
	if failure != 0 {
		a.release(failure)
	}
	if property == 0 {
		return 0, errors.New("decode macOS proxy snapshot")
	}
	if a.typeID(property) != a.dictType() {
		a.release(property)
		return 0, errors.New("macOS proxy snapshot is not a dictionary")
	}
	return property, nil
}

func (a *darwinProxyAPI) primaryService() string {
	for _, key := range []string{"State:/Network/Global/IPv4", "State:/Network/Global/IPv6"} {
		name := a.string(key)
		if name == 0 {
			continue
		}
		state := a.copyValue(0, name)
		a.release(name)
		if state == 0 {
			continue
		}
		var service string
		if a.typeID(state) == a.dictType() {
			service = a.text(a.dictGet(state, a.keys["kSCDynamicStorePropNetPrimaryService"]))
		}
		a.release(state)
		if service != "" {
			return service
		}
	}
	return ""
}

func (a *darwinProxyAPI) copyProtocol(prefs uintptr, id string, create bool) (uintptr, error) {
	services := a.services(prefs)
	if services == 0 {
		return 0, errors.New("macOS network services unavailable")
	}
	defer a.release(services)
	count := a.arrayCount(services)
	if count < 0 || count > 1<<20 {
		return 0, errors.New("invalid macOS network service count")
	}
	for i := int64(0); i < count; i++ {
		service := a.arrayGet(services, i)
		if a.text(a.serviceID(service)) != id {
			continue
		}
		kind := a.keys["kSCNetworkProtocolTypeProxies"]
		protocol := a.protocol(service, kind)
		if protocol == 0 && create {
			if a.addProtocol(service, kind) == 0 {
				return 0, errors.New("add macOS proxy protocol")
			}
			protocol = a.protocol(service, kind)
			if protocol == 0 {
				return 0, errors.New("copy added macOS proxy protocol")
			}
		}
		return protocol, nil
	}
	return 0, errDarwinServiceMissing
}

// Returns an OWNED dictionary, including the historical nil -> empty convention.
func (a *darwinProxyAPI) copyConfiguration(protocol uintptr) (uintptr, error) {
	var config uintptr
	if protocol != 0 {
		config = a.configuration(protocol)
	}
	if config != 0 {
		if a.typeID(config) != a.dictType() {
			return 0, errors.New("invalid macOS proxy configuration")
		}
		return a.retain(config), nil
	}
	config = a.emptyDictionary()
	if config == 0 {
		return 0, errors.New("create empty macOS proxy dictionary")
	}
	return config, nil
}

func (a *darwinProxyAPI) readConfiguration(id string) (uintptr, error) {
	name := a.string("FlowLens")
	if name == 0 {
		return 0, errors.New("create preferences name")
	}
	defer a.release(name)
	prefs := a.prefsCreate(0, name, 0)
	if prefs == 0 {
		return 0, errors.New("read macOS preferences")
	}
	defer a.release(prefs)
	protocol, err := a.copyProtocol(prefs, id, false)
	if err != nil {
		return 0, err
	}
	if protocol != 0 {
		defer a.release(protocol)
	}
	return a.copyConfiguration(protocol)
}
