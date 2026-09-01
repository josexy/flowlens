//go:build windows

package pythonpluginservice

import (
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type workerProcessTree struct {
	job windows.Handle
}

func configureWorkerCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
}

func attachWorkerProcessTree(command *exec.Cmd) workerProcessTree {
	if command == nil || command.Process == nil {
		return workerProcessTree{}
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return workerProcessTree{}
	}
	information := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	information.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&information)),
		uint32(unsafe.Sizeof(information)),
	); err != nil {
		windows.CloseHandle(job)
		return workerProcessTree{}
	}
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(command.Process.Pid),
	)
	if err != nil {
		windows.CloseHandle(job)
		return workerProcessTree{}
	}
	err = windows.AssignProcessToJobObject(job, process)
	windows.CloseHandle(process)
	if err != nil {
		windows.CloseHandle(job)
		return workerProcessTree{}
	}
	return workerProcessTree{job: job}
}

func terminateWorkerProcessTree(command *exec.Cmd, tree workerProcessTree) {
	if tree.job != 0 {
		_ = windows.TerminateJobObject(tree.job, 1)
		_ = windows.CloseHandle(tree.job)
		return
	}
	if command == nil || command.Process == nil {
		return
	}
	helper := exec.Command("taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F")
	helper.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW, HideWindow: true}
	_ = helper.Run()
	_ = command.Process.Kill()
}
