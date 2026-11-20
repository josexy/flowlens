package processattribution

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

// proc_bsdinfo from Apple's bsd/sys/proc_info.h: 136 bytes on arm64 and
// amd64, name at 64, start seconds at 120, microseconds at 128. There are
// no native pointers. libproc borrows this buffer only for the call.
// https://github.com/apple-oss-distributions/xnu/blob/main/bsd/sys/proc_info.h
type darwinBSDInfo struct {
	Flags, Status, ExitStatus, PID, PPID               uint32
	UID, GID, RUID, RGID, SavedUID, SavedGID, Reserved uint32
	Comm                                               [16]byte
	Name                                               [32]byte
	Files, Group, JobCount, Device, TerminalGroup      uint32
	Nice                                               int32
	StartSeconds, StartMicroseconds                    uint64
}

type darwinProcAPI struct {
	// int proc_pidinfo(int, int, uint64_t, void *, int)
	pidInfo func(int32, int32, uint64, *darwinBSDInfo, int32) int32
	// int proc_pidpath(int, void *, uint32_t)
	pidPath func(int32, *byte, uint32) int32
	errno   func() *int32 // int *__error(void), thread local and borrowed
}

var loadDarwinProc = sync.OnceValues(func() (*darwinProcAPI, error) {
	return bindDarwinProc(purego.Dlopen, purego.Dlsym)
})

func bindDarwinProc(open func(string, int) (uintptr, error), symbol func(uintptr, string) (uintptr, error)) (*darwinProcAPI, error) {
	handle, err := open("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	// Handles stay loaded for the lifetime of registered calls.
	api := &darwinProcAPI{}
	for _, binding := range []struct {
		name   string
		target any
	}{
		{"proc_pidinfo", &api.pidInfo}, {"proc_pidpath", &api.pidPath}, {"__error", &api.errno},
	} {
		address, err := symbol(handle, binding.name)
		if err != nil || address == 0 {
			return nil, fmt.Errorf("resolve %s: %v", binding.name, err)
		}
		purego.RegisterFunc(binding.target, address)
	}
	return api, nil
}

func lookupDarwinProcessIdentity(pid uint32) (darwinProcessIdentity, error) {
	api, err := loadDarwinProc()
	if err != nil {
		return darwinProcessIdentity{}, err
	}
	return api.identity(pid)
}

func (api *darwinProcAPI) identity(pid uint32) (darwinProcessIdentity, error) {
	if pid == 0 || pid > 1<<31-1 {
		return darwinProcessIdentity{}, syscall.EINVAL
	}
	// errno must be cleared/read on the same OS thread as proc_pidinfo.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	errno := api.errno()
	*errno = 0
	var info darwinBSDInfo
	count := api.pidInfo(int32(pid), 3 /* PROC_PIDTBSDINFO */, 0, &info, int32(unsafe.Sizeof(info)))
	if count != int32(unsafe.Sizeof(info)) {
		if *errno != 0 {
			return darwinProcessIdentity{}, syscall.Errno(*errno)
		}
		return darwinProcessIdentity{}, syscall.ESRCH
	}
	if info.StartSeconds == 0 && info.StartMicroseconds == 0 {
		return darwinProcessIdentity{}, syscall.ESRCH
	}
	var path [4096]byte // PROC_PIDPATHINFO_MAXSIZE
	*errno = 0
	length := api.pidPath(int32(pid), &path[0], uint32(len(path)))
	identity := darwinProcessIdentity{
		startToken:  strconv.FormatUint(info.StartSeconds, 10) + ":" + strconv.FormatUint(info.StartMicroseconds, 10),
		processName: darwinBufferString(info.Name[:]), identityConfidence: "none",
		metadataDenied: length <= 0 || length > int32(len(path)),
	}
	if !identity.metadataDenied {
		identity.executablePath = darwinBufferString(path[:length])
	}
	if identity.processName == "" {
		identity.processName = darwinBufferString(info.Comm[:])
	}
	if identity.processName == "" && identity.executablePath != "" {
		identity.processName = filepath.Base(identity.executablePath)
	}
	identity.displayName = identity.processName
	runtime.KeepAlive(&info)
	runtime.KeepAlive(&path)
	return identity, nil
}

func darwinBufferString(buffer []byte) string {
	for i, b := range buffer {
		if b == 0 {
			return string(buffer[:i])
		}
	}
	return string(buffer)
}
