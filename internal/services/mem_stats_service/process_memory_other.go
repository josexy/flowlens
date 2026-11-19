//go:build !windows && !darwin && !linux

package memstatsservice

// readProcessMemory has no platform reader here.
func readProcessMemory() ProcessMemory { return unavailableProcessMemory() }
