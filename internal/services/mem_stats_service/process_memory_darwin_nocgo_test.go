//go:build darwin && !cgo

package memstatsservice

import "testing"

func TestDarwinNoCGOProcessMemoryIsUnavailable(t *testing.T) {
	if read := readProcessMemory(); read.Available() || read.Bytes != -1 || read.Measure != MeasureUnavailable {
		t.Fatalf("process memory = %+v, want unavailable", read)
	}
}
