//go:build !windows

package pythonpluginservice

import (
	"os/exec"
	"syscall"
)

type workerProcessTree struct{}

func configureWorkerCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachWorkerProcessTree(_ *exec.Cmd) workerProcessTree {
	return workerProcessTree{}
}

func terminateWorkerProcessTree(command *exec.Cmd, _ workerProcessTree) {
	if command == nil || command.Process == nil {
		return
	}
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	_ = command.Process.Kill()
}
