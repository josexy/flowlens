//go:build windows

package systemproxy

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const windowsInternetSettingsRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

const (
	internetOptionRefresh             = 37
	internetOptionSettingsChanged     = 39
	internetOptionPerConnectionOption = 75
	internetPerConnFlags              = 1
	internetPerConnProxyServer        = 2
	internetPerConnFlagsUI            = 10
	proxyTypeDirect                   = 0x00000001
	proxyTypeProxy                    = 0x00000002
)

var (
	wininetDLL               = windows.NewLazySystemDLL("wininet.dll")
	internetQueryOptionWProc = wininetDLL.NewProc("InternetQueryOptionW")
	internetSetOptionWProc   = wininetDLL.NewProc("InternetSetOptionW")
	kernel32DLL              = windows.NewLazySystemDLL("kernel32.dll")
	globalFreeProc           = kernel32DLL.NewProc("GlobalFree")
)

type internetPerConnOption struct {
	Option uint32
	Value  internetPerConnOptionValue
}

type internetPerConnOptionValue struct {
	_     [0]uintptr
	bytes [8]byte
}

func (v *internetPerConnOptionValue) setUint32(value uint32) {
	*(*uint32)(unsafe.Pointer(&v.bytes[0])) = value
}

func (v *internetPerConnOptionValue) uint32() uint32 {
	return *(*uint32)(unsafe.Pointer(&v.bytes[0]))
}

func (v *internetPerConnOptionValue) setPointer(value unsafe.Pointer) {
	*(*unsafe.Pointer)(unsafe.Pointer(&v.bytes[0])) = value
}

func (v *internetPerConnOptionValue) pointer() unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&v.bytes[0]))
}

type internetPerConnOptionList struct {
	Size        uint32
	Connection  *uint16
	OptionCount uint32
	OptionError uint32
	Options     *internetPerConnOption
}

type windowsProxySnapshot struct {
	Flags  uint32
	Server string
}

type windowsPlatformDriver struct{}

func newPlatformDriver() platformDriver {
	return windowsPlatformDriver{}
}

func (windowsPlatformDriver) Supported() bool {
	return true
}

func (windowsPlatformDriver) Supports(mode Mode) bool {
	return mode == ModeHTTP
}

func (windowsPlatformDriver) Snapshot() (any, error) {
	return queryWindowsProxySettings()
}

func (windowsPlatformDriver) Apply(endpoint Endpoint) error {
	return setWindowsProxySettings(windowsProxySnapshot{
		Flags:  proxyTypeDirect | proxyTypeProxy,
		Server: ProxyServerValue(endpoint),
	})
}

func (windowsPlatformDriver) Matches(endpoint Endpoint) (bool, error) {
	settings, err := queryWindowsProxySettings()
	if err != nil {
		return false, err
	}
	return settings.Flags == proxyTypeDirect|proxyTypeProxy && settings.Server == ProxyServerValue(endpoint), nil
}

func (windowsPlatformDriver) Restore(snapshot any) error {
	settings, ok := snapshot.(windowsProxySnapshot)
	if !ok {
		return errors.New("invalid Windows system proxy snapshot")
	}
	return setWindowsProxySettings(settings)
}

func fromPlatform() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsInternetSettingsRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return ""
	}

	proxyServer, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return ""
	}
	return proxyURLFromWindowsProxyServer(proxyServer)
}

func queryWindowsProxySettings() (windowsProxySnapshot, error) {
	settings, err := queryWindowsProxySettingsWithFlags(internetPerConnFlagsUI)
	if err == nil {
		return settings, nil
	}
	return queryWindowsProxySettingsWithFlags(internetPerConnFlags)
}

func queryWindowsProxySettingsWithFlags(flagsOption uint32) (windowsProxySnapshot, error) {
	options := []internetPerConnOption{
		{Option: flagsOption},
		{Option: internetPerConnProxyServer},
	}
	list := internetPerConnOptionList{
		Size:        uint32(unsafe.Sizeof(internetPerConnOptionList{})),
		OptionCount: uint32(len(options)),
		Options:     &options[0],
	}
	length := uint32(unsafe.Sizeof(list))
	result, _, callErr := internetQueryOptionWProc.Call(
		0,
		internetOptionPerConnectionOption,
		uintptr(unsafe.Pointer(&list)),
		uintptr(unsafe.Pointer(&length)),
	)
	runtime.KeepAlive(options)
	if result == 0 {
		return windowsProxySnapshot{}, windowsCallError("query system proxy", callErr)
	}

	serverPtr := options[1].Value.pointer()
	server := ""
	if serverPtr != nil {
		server = windows.UTF16PtrToString((*uint16)(serverPtr))
		_, _, _ = globalFreeProc.Call(uintptr(serverPtr))
	}
	return windowsProxySnapshot{
		Flags:  options[0].Value.uint32(),
		Server: server,
	}, nil
}

func setWindowsProxySettings(settings windowsProxySnapshot) error {
	serverPtr, err := windows.UTF16PtrFromString(settings.Server)
	if err != nil {
		return fmt.Errorf("encode Windows system proxy server: %w", err)
	}
	options := []internetPerConnOption{
		{Option: internetPerConnFlags},
		{Option: internetPerConnProxyServer},
	}
	options[0].Value.setUint32(settings.Flags)
	options[1].Value.setPointer(unsafe.Pointer(serverPtr))
	list := internetPerConnOptionList{
		Size:        uint32(unsafe.Sizeof(internetPerConnOptionList{})),
		OptionCount: uint32(len(options)),
		Options:     &options[0],
	}
	result, _, callErr := internetSetOptionWProc.Call(
		0,
		internetOptionPerConnectionOption,
		uintptr(unsafe.Pointer(&list)),
		unsafe.Sizeof(list),
	)
	runtime.KeepAlive(serverPtr)
	runtime.KeepAlive(options)
	if result == 0 {
		return windowsCallError("set system proxy", callErr)
	}
	if err := notifyWindowsProxyChanged(); err != nil {
		return err
	}
	return nil
}

func notifyWindowsProxyChanged() error {
	for _, option := range []uintptr{internetOptionSettingsChanged, internetOptionRefresh} {
		result, _, callErr := internetSetOptionWProc.Call(0, option, 0, 0)
		if result == 0 {
			return windowsCallError("notify system proxy change", callErr)
		}
	}
	return nil
}

func windowsCallError(operation string, callErr error) error {
	if callErr == nil || errors.Is(callErr, syscall.Errno(0)) {
		callErr = syscall.EINVAL
	}
	return fmt.Errorf("%s: %w", operation, callErr)
}
