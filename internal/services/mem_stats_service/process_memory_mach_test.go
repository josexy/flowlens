package memstatsservice

import (
	"math"
	"testing"
	"unsafe"
)

func TestMachFootprintResponses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      int32
		count     uint32
		bytes     uint64
		available bool
	}{
		{"positive", 0, 38, 123456789, true},
		{"above32bits", 0, 38, 1 << 34, true},
		{"machError", 5, 38, 123, false},
		{"truncated", 0, 37, 123, false},
		{"olderRevision", 0, 36, 123, false},
		{"oversizedCount", 0, 39, 123, false},
		{"zero", 0, 38, 0, false},
		{"overflow", 0, 38, math.MaxUint64, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := machMemoryAPI{task: 42, taskInfo: func(task, flavor uint32, output, count *uint32) int32 {
				if task != 42 || flavor != 22 || *count != 38 {
					t.Fatalf("task_info arguments = %d, %d, %d", task, flavor, *count)
				}
				words := unsafe.Slice(output, 38)
				words[36], words[37] = uint32(tc.bytes), uint32(tc.bytes>>32)
				*count = tc.count
				return tc.code
			}}
			got := api.read()
			if tc.available {
				if got.Bytes != int64(tc.bytes) || got.Measure != MeasurePhysFootprint {
					t.Fatalf("sample = %+v", got)
				}
			} else if got != unavailableProcessMemory() {
				t.Fatalf("sample = %+v, want unavailable", got)
			}
		})
	}
}
