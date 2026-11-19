//go:build linux

package memstatsservice

import (
	"bytes"
	"os"
	"strconv"
)

// readProcessMemory reads resident memory from /proc/self/statm, as top and
// htop show it in RES.
func readProcessMemory() ProcessMemory {
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return unavailableProcessMemory()
	}
	fields := bytes.Fields(data)
	if len(fields) < 2 {
		return unavailableProcessMemory()
	}
	pages, err := strconv.ParseInt(string(fields[1]), 10, 64)
	if err != nil || pages < 0 {
		return unavailableProcessMemory()
	}
	return ProcessMemory{Bytes: pages * int64(os.Getpagesize()), Measure: MeasureResident}
}
