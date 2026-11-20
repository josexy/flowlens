package memstatsservice

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var loadDarwinMach = sync.OnceValues(func() (*machMemoryAPI, error) {
	return bindDarwinMach(purego.Dlopen, purego.Dlsym)
})

func bindDarwinMach(open func(string, int) (uintptr, error), symbol func(uintptr, string) (uintptr, error)) (*machMemoryAPI, error) {
	// Registered addresses and the libSystem handle live for the whole process.
	handle, err := open("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("load Mach memory API: %w", err)
	}
	info, err := symbol(handle, "task_info")
	if err != nil || info == 0 {
		return nil, fmt.Errorf("resolve task_info: %v", err)
	}
	self, err := symbol(handle, "mach_task_self_")
	if err != nil || self == 0 {
		return nil, fmt.Errorf("resolve mach_task_self_: %v", err)
	}
	// mach_task_self() expands to this exported uint32_t port variable, not
	// a function. This borrowed task port must not be deallocated.
	api := &machMemoryAPI{task: **(**uint32)(unsafe.Pointer(&self))}
	purego.RegisterFunc(&api.taskInfo, info)
	return api, nil
}

func readProcessMemory() ProcessMemory {
	api, err := loadDarwinMach()
	if err != nil {
		return unavailableProcessMemory()
	}
	return api.read()
}
