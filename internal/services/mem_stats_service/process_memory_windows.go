//go:build windows

package memstatsservice

import (
	"errors"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	windowsKernel32               = windows.NewLazySystemDLL("kernel32.dll")
	windowsGetProcessMemoryInfo   = windowsKernel32.NewProc("K32GetProcessMemoryInfo")
	windowsQueryWorkingSet        = windowsKernel32.NewProc("K32QueryWorkingSet")
	windowsWorkingSetReadAttempts = 4
)

// windowsWorkingSetShared is the Shared bit of a PSAPI_WORKING_SET_BLOCK.
const windowsWorkingSetShared = 1 << 8

// processMemoryCountersEx2 mirrors PROCESS_MEMORY_COUNTERS_EX2. Microsoft lists
// its minimum as Windows 10 22H2 or Windows 11 22H2 with the September 2023
// cumulative update. Older systems reject it or leave PrivateWorkingSetSize at
// 0, and the reader falls back to counting private working-set pages.
type processMemoryCountersEx2 struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
	PrivateWorkingSetSize      uintptr
	SharedCommitUsage          uint64
}

// privateWorkingSetFromCounters is a variable so tests can force the fallback.
var privateWorkingSetFromCounters = readPrivateWorkingSetFromCounters

// workingSetBuffer is reused by the fallback, which samples may call at once.
var workingSetBuffer struct {
	sync.Mutex
	entries []uintptr
}

// readProcessMemory reads the private working set, as Task Manager's Memory
// column shows it.
func readProcessMemory() ProcessMemory {
	if bytes, ok := privateWorkingSetFromCounters(); ok {
		return ProcessMemory{Bytes: bytes, Measure: MeasurePrivateWorkingSet}
	}
	if bytes, ok := readPrivateWorkingSetFromPages(); ok {
		return ProcessMemory{Bytes: bytes, Measure: MeasurePrivateWorkingSet}
	}
	return unavailableProcessMemory()
}

func readPrivateWorkingSetFromCounters() (int64, bool) {
	if windowsGetProcessMemoryInfo.Find() != nil {
		return 0, false
	}
	var counters processMemoryCountersEx2
	counters.CB = uint32(unsafe.Sizeof(counters))
	ok, _, _ := windowsGetProcessMemoryInfo.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&counters)), uintptr(counters.CB))
	if ok == 0 || counters.PrivateWorkingSetSize == 0 {
		return 0, false
	}
	return int64(counters.PrivateWorkingSetSize), true
}

// readPrivateWorkingSetFromPages counts the working-set pages that are not
// shared. A short buffer fails with ERROR_BAD_LENGTH and reports the number of
// entries it needs.
func readPrivateWorkingSetFromPages() (int64, bool) {
	if windowsQueryWorkingSet.Find() != nil {
		return 0, false
	}
	workingSetBuffer.Lock()
	defer workingSetBuffer.Unlock()
	if len(workingSetBuffer.entries) == 0 {
		workingSetBuffer.entries = make([]uintptr, 1+16384)
	}
	for range windowsWorkingSetReadAttempts {
		buffer := workingSetBuffer.entries
		ok, _, err := windowsQueryWorkingSet.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer))*unsafe.Sizeof(buffer[0]))
		if ok != 0 {
			count := min(int(buffer[0]), len(buffer)-1)
			var private int64
			for _, block := range buffer[1 : 1+count] {
				if block&windowsWorkingSetShared == 0 {
					private++
				}
			}
			return private * int64(os.Getpagesize()), private > 0
		}
		if !errors.Is(err, windows.ERROR_BAD_LENGTH) {
			return 0, false
		}
		// Leave room for pages that join the working set before the next call.
		needed := int(buffer[0])
		workingSetBuffer.entries = make([]uintptr, 1+needed+needed/8+1024)
	}
	return 0, false
}
