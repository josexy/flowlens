//go:build darwin && cgo

package memstatsservice

/*
#include <mach/mach.h>
#include <stdint.h>

// phys_footprint is part of TASK_VM_INFO on every macOS release Go supports.
static int64_t flowlens_phys_footprint(void) {
	task_vm_info_data_t info;
	mach_msg_type_number_t count = TASK_VM_INFO_COUNT;
	if (task_info(mach_task_self(), TASK_VM_INFO, (task_info_t)&info, &count) != KERN_SUCCESS) {
		return -1;
	}
	return (int64_t)info.phys_footprint;
}
*/
import "C"

// readProcessMemory reads the physical footprint, as Activity Monitor's Memory
// column shows it.
func readProcessMemory() ProcessMemory {
	bytes := int64(C.flowlens_phys_footprint())
	if bytes <= 0 {
		return unavailableProcessMemory()
	}
	return ProcessMemory{Bytes: bytes, Measure: MeasurePhysFootprint}
}
