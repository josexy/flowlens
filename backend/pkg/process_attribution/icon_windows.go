//go:build windows

package processattribution

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsShellFileInfoIcon      = 0x000000100
	windowsShellFileInfoLargeIcon = 0x000000000
	windowsDIBRGBColors           = 0
	windowsBitmapRGB              = 0
	windowsDrawIconNormal         = 0x0003
	windowsRenderedIconSize       = 64
)

var (
	windowsShell32            = windows.NewLazySystemDLL("shell32.dll")
	windowsSHGetFileInfo      = windowsShell32.NewProc("SHGetFileInfoW")
	windowsUser32             = windows.NewLazySystemDLL("user32.dll")
	windowsDestroyIcon        = windowsUser32.NewProc("DestroyIcon")
	windowsDrawIconEx         = windowsUser32.NewProc("DrawIconEx")
	windowsGetDC              = windowsUser32.NewProc("GetDC")
	windowsReleaseDC          = windowsUser32.NewProc("ReleaseDC")
	windowsGDI32              = windows.NewLazySystemDLL("gdi32.dll")
	windowsCreateCompatibleDC = windowsGDI32.NewProc("CreateCompatibleDC")
	windowsDeleteDC           = windowsGDI32.NewProc("DeleteDC")
	windowsCreateDIBSection   = windowsGDI32.NewProc("CreateDIBSection")
	windowsSelectObject       = windowsGDI32.NewProc("SelectObject")
	windowsDeleteObject       = windowsGDI32.NewProc("DeleteObject")
	windowsGDIFlush           = windowsGDI32.NewProc("GdiFlush")
)

type windowsShellFileInfo struct {
	icon        windows.Handle
	iconIndex   int32
	attributes  uint32
	displayName [260]uint16
	typeName    [80]uint16
}

type windowsBitmapInfoHeader struct {
	size            uint32
	width           int32
	height          int32
	planes          uint16
	bitCount        uint16
	compression     uint32
	sizeImage       uint32
	xPixelsPerM     int32
	yPixelsPerM     int32
	colorsUsed      uint32
	colorsImportant uint32
}

type windowsRGBQuad struct {
	blue     byte
	green    byte
	red      byte
	reserved byte
}

type windowsBitmapInfo struct {
	header windowsBitmapInfoHeader
	colors [1]windowsRGBQuad
}

type windowsIconFunctions struct {
	extract func(string) (windows.Handle, error)
	convert func(windows.Handle) (image.Image, error)
	destroy func(windows.Handle) error
}

func loadWindowsIcon(path string) (image.Image, error) {
	return loadWindowsIconWithFunctions(path, windowsIconFunctions{
		extract: extractWindowsShellIcon,
		convert: convertWindowsIcon,
		destroy: destroyWindowsIcon,
	})
}

func loadWindowsIconWithFunctions(path string, functions windowsIconFunctions) (image.Image, error) {
	if path == "" {
		return nil, &IconUnavailableError{Reason: "executable_path_unavailable"}
	}
	icon, err := functions.extract(path)
	if err != nil {
		return nil, err
	}
	defer functions.destroy(icon)
	return functions.convert(icon)
}

func extractWindowsShellIcon(path string) (windows.Handle, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var info windowsShellFileInfo
	result, _, callErr := windowsSHGetFileInfo.Call(
		uintptr(unsafe.Pointer(pathPointer)),
		0,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
		windowsShellFileInfoIcon|windowsShellFileInfoLargeIcon,
	)
	if result == 0 || info.icon == 0 {
		return 0, fmt.Errorf("extract Windows shell icon: %w", normalizeWindowsCallError(callErr))
	}
	return info.icon, nil
}

func destroyWindowsIcon(icon windows.Handle) error {
	if icon == 0 {
		return nil
	}
	result, _, callErr := windowsDestroyIcon.Call(uintptr(icon))
	if result == 0 {
		return fmt.Errorf("destroy Windows icon: %w", normalizeWindowsCallError(callErr))
	}
	return nil
}

func convertWindowsIcon(icon windows.Handle) (image.Image, error) {
	black, err := renderWindowsIcon(icon, 0)
	if err != nil {
		return nil, err
	}
	white, err := renderWindowsIcon(icon, 255)
	if err != nil {
		return nil, err
	}
	result := image.NewNRGBA(image.Rect(0, 0, windowsRenderedIconSize, windowsRenderedIconSize))
	for index := range windowsRenderedIconSize * windowsRenderedIconSize {
		offset := index * 4
		blackBlue, blackGreen, blackRed := int(black[offset]), int(black[offset+1]), int(black[offset+2])
		whiteBlue, whiteGreen, whiteRed := int(white[offset]), int(white[offset+1]), int(white[offset+2])
		alphaRed := 255 - max(0, whiteRed-blackRed)
		alphaGreen := 255 - max(0, whiteGreen-blackGreen)
		alphaBlue := 255 - max(0, whiteBlue-blackBlue)
		alpha := min(255, max(0, (alphaRed+alphaGreen+alphaBlue)/3))
		pixel := color.NRGBA{A: uint8(alpha)}
		if alpha > 0 {
			pixel.R = uint8(min(255, blackRed*255/alpha))
			pixel.G = uint8(min(255, blackGreen*255/alpha))
			pixel.B = uint8(min(255, blackBlue*255/alpha))
		}
		result.SetNRGBA(index%windowsRenderedIconSize, index/windowsRenderedIconSize, pixel)
	}
	return result, nil
}

func renderWindowsIcon(icon windows.Handle, background byte) ([]byte, error) {
	screenDC, _, callErr := windowsGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("get Windows screen DC: %w", normalizeWindowsCallError(callErr))
	}
	defer windowsReleaseDC.Call(0, screenDC)

	memoryDC, _, callErr := windowsCreateCompatibleDC.Call(screenDC)
	if memoryDC == 0 {
		return nil, fmt.Errorf("create Windows memory DC: %w", normalizeWindowsCallError(callErr))
	}
	defer windowsDeleteDC.Call(memoryDC)

	bitmapInfo := windowsBitmapInfo{header: windowsBitmapInfoHeader{
		size:        uint32(unsafe.Sizeof(windowsBitmapInfoHeader{})),
		width:       windowsRenderedIconSize,
		height:      -windowsRenderedIconSize,
		planes:      1,
		bitCount:    32,
		compression: windowsBitmapRGB,
	}}
	var bits unsafe.Pointer
	bitmap, _, callErr := windowsCreateDIBSection.Call(
		screenDC,
		uintptr(unsafe.Pointer(&bitmapInfo)),
		windowsDIBRGBColors,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if bitmap == 0 || bits == nil {
		return nil, fmt.Errorf("create Windows icon DIB: %w", normalizeWindowsCallError(callErr))
	}
	defer windowsDeleteObject.Call(bitmap)

	previous, _, callErr := windowsSelectObject.Call(memoryDC, bitmap)
	if previous == 0 || previous == ^uintptr(0) {
		return nil, fmt.Errorf("select Windows icon DIB: %w", normalizeWindowsCallError(callErr))
	}
	defer windowsSelectObject.Call(memoryDC, previous)

	byteCount := windowsRenderedIconSize * windowsRenderedIconSize * 4
	pixels := unsafe.Slice((*byte)(bits), byteCount)
	for offset := 0; offset < byteCount; offset += 4 {
		pixels[offset] = background
		pixels[offset+1] = background
		pixels[offset+2] = background
		pixels[offset+3] = 255
	}
	result, _, callErr := windowsDrawIconEx.Call(
		memoryDC,
		0,
		0,
		uintptr(icon),
		windowsRenderedIconSize,
		windowsRenderedIconSize,
		0,
		0,
		windowsDrawIconNormal,
	)
	if result == 0 {
		return nil, fmt.Errorf("draw Windows icon: %w", normalizeWindowsCallError(callErr))
	}
	result, _, callErr = windowsGDIFlush.Call()
	if result == 0 {
		return nil, fmt.Errorf("flush Windows icon drawing: %w", normalizeWindowsCallError(callErr))
	}
	return append([]byte(nil), pixels...), nil
}

func normalizeWindowsCallError(err error) error {
	if err == nil || errors.Is(err, windows.ERROR_SUCCESS) {
		return errors.New("Windows API call failed")
	}
	return err
}
