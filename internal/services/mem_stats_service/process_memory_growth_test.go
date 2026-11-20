//go:build linux || darwin

package memstatsservice

import (
	"os"
	"runtime"
	"testing"
)

func TestProcessMemoryGrowsWithTouchedMemory(t *testing.T) {
	want := MeasureResident
	if runtime.GOOS == "darwin" {
		want = MeasurePhysFootprint
	}
	before := readProcessMemory()
	if before.Measure != want || before.Bytes <= 0 || !before.Available() {
		t.Fatalf("process memory = %+v, want a positive %q value", before, want)
	}

	const size = 64 << 20
	block := make([]byte, size)
	for index := 0; index < size; index += os.Getpagesize() {
		block[index] = 1
	}
	after := readProcessMemory()
	runtime.KeepAlive(block)

	if growth := after.Bytes - before.Bytes; growth < 48<<20 {
		t.Fatalf("process memory grew by %d bytes after touching 64 MiB (%+v -> %+v)", growth, before, after)
	}
}
