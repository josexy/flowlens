package memstatsservice

// ProcessMeasure names how the platform measured the process's memory.
type ProcessMeasure string

const (
	// MeasureUnavailable means the platform value could not be read.
	MeasureUnavailable ProcessMeasure = ""
	// MeasurePrivateWorkingSet is the Windows private working set, as in Task
	// Manager's Memory column.
	MeasurePrivateWorkingSet ProcessMeasure = "private_working_set"
	// MeasurePhysFootprint is the macOS physical footprint, as in Activity
	// Monitor's Memory column.
	MeasurePhysFootprint ProcessMeasure = "phys_footprint"
	// MeasureResident is resident memory, as RES in top.
	MeasureResident ProcessMeasure = "resident"
)

// ProcessMemory is FlowLens's own process memory, measured the way the
// platform's process tool measures it. Child processes are not included.
type ProcessMemory struct {
	Bytes   int64          `json:"bytes"` // -1 when unavailable
	Measure ProcessMeasure `json:"measure"`
}

// Available reports whether the platform value was read.
func (m ProcessMemory) Available() bool {
	return m.Bytes >= 0 && m.Measure != MeasureUnavailable
}

func unavailableProcessMemory() ProcessMemory {
	return ProcessMemory{Bytes: -1, Measure: MeasureUnavailable}
}
