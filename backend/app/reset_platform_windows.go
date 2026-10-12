package app

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureResetStartupCommand(command *exec.Cmd) {
	// HideWindow supplies SW_HIDE through STARTUPINFO, overriding the helper's
	// first ShowWindow call. Suppress only the console, not the progress WebView.
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}

func isResetSharingViolation(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

func isResetStartupReplaceBlocked(err error) bool {
	return isResetSharingViolation(err) || errors.Is(err, windows.ERROR_ACCESS_DENIED)
}

// Readers share delete access to minimise Windows IPC sharing conflicts.
func openResetStartupFile(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}
