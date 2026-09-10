//go:build windows

package pythonpluginservice

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const ioReparseTagAppExecLink = 0x8000001b

func validateInterpreterFile(path string) error {
	info, err := os.Stat(path)
	if err == nil && info.Mode().IsRegular() {
		return nil
	}
	// App Execution Aliases are executable reparse points, not regular files.
	// Keep normal symlink handling through os.Stat; do not accept other special files.
	if isWindowsAppExecutionAlias(path) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Python interpreter: %w", err)
	}
	return errors.New("Python interpreter path must reference a regular file or Windows app execution alias")
}

func isWindowsAppExecutionAlias(path string) bool {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	handle, err := windows.CreateFile(name, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var info struct {
		FileAttributes uint32
		ReparseTag     uint32
	}
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileAttributeTagInfo,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return false
	}
	return info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 &&
		info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 &&
		info.ReparseTag == ioReparseTagAppExecLink
}
