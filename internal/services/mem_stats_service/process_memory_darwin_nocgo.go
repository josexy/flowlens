//go:build darwin && !cgo

package memstatsservice

// readProcessMemory needs cgo to read the physical footprint on macOS.
func readProcessMemory() ProcessMemory { return unavailableProcessMemory() }
