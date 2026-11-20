package memstatsservice

import (
	"math"
	"runtime"
)

// Apple's osfmk/mach/task_info.h uses #pragma pack(4). TASK_VM_INFO_REV1
// ends after the uint64_t phys_footprint at byte offset 144. Mach counts
// are uint32_t words: request exactly 38 words, avoiding Go struct padding.
// https://github.com/apple-oss-distributions/xnu/blob/main/osfmk/mach/task_info.h
const (
	machTaskVMInfo      = 22
	machVMInfoRev1Count = 38
	machFootprintWord   = 36
)

type machMemoryAPI struct {
	task uint32
	// kern_return_t task_info(mach_port_name_t, task_flavor_t,
	//                        integer_t *, mach_msg_type_number_t *).
	// Only synchronous calls borrow the Go buffer and count pointer.
	taskInfo func(uint32, uint32, *uint32, *uint32) int32
}

func (api *machMemoryAPI) read() ProcessMemory {
	if api == nil || api.task == 0 || api.taskInfo == nil {
		return unavailableProcessMemory()
	}
	var words [machVMInfoRev1Count]uint32
	count := uint32(len(words))
	code := api.taskInfo(api.task, machTaskVMInfo, &words[0], &count)
	runtime.KeepAlive(&words)
	if code != 0 || count != machVMInfoRev1Count {
		return unavailableProcessMemory()
	}
	// Both supported Darwin architectures are little endian.
	bytes := uint64(words[machFootprintWord]) | uint64(words[machFootprintWord+1])<<32
	if bytes == 0 || bytes > math.MaxInt64 {
		return unavailableProcessMemory()
	}
	// Activity Monitor's Memory measure for this task; excludes Python Workers.
	return ProcessMemory{Bytes: int64(bytes), Measure: MeasurePhysFootprint}
}
