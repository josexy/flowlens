package logic

import (
	"cmp"
	"slices"
	"strings"
)

// SortOrder is a column's sort direction.
type SortOrder string

// Sort directions; the empty value is unsorted.
const (
	SortNone SortOrder = ""
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)

// TrafficSort is the table's sort state.
type TrafficSort struct {
	Key   TrafficColumnKey
	Order SortOrder
}

func (s TrafficSort) active() bool { return s.Key != "" && s.Order != SortNone }

// LatestEdge is where new requests appear in the table.
type LatestEdge int

// Edges.
const (
	EdgeUnknown LatestEdge = iota
	EdgeStart
	EdgeEnd
)

// TrafficLatestEdge reports where new requests land. With other sort columns
// they can land anywhere.
func TrafficLatestEdge(sort TrafficSort) LatestEdge {
	if !sort.active() {
		return EdgeEnd
	}
	if sort.Key == ColumnID {
		if sort.Order == SortDesc {
			return EdgeStart
		}
		return EdgeEnd
	}
	return EdgeUnknown
}

// IsAtLatestEdge reports whether the rows from first to last show the newest
// requests of a table of count rows.
func IsAtLatestEdge(sort TrafficSort, first, last, count int) bool {
	if count == 0 || last < first {
		return false
	}
	switch TrafficLatestEdge(sort) {
	case EdgeStart:
		return first == 0
	case EdgeEnd:
		return last >= count-1
	default:
		return false
	}
}

type sortValue struct {
	text    string
	number  int64
	missing bool
	numeric bool
}

func textValue(text string) sortValue { return sortValue{text: text} }

func numberValue(number int64, known bool) sortValue {
	return sortValue{number: number, numeric: true, missing: !known}
}

func trafficSortValue(entry *TrafficEntry, key TrafficColumnKey) sortValue {
	switch key {
	case ColumnID:
		return numberValue(int64(entry.ID), true)
	case ColumnStatusCode:
		return numberValue(int64(entry.StatusCode), true)
	case ColumnDestination:
		if entry.Metadata != nil {
			return textValue(entry.Metadata.RemoteDestinationAddr)
		}
		return textValue("")
	case ColumnProtocol:
		return textValue(TrafficProtocol(entry))
	case ColumnProcess:
		if entry.Metadata != nil && entry.Metadata.Process != nil {
			return textValue(entry.Metadata.Process.DisplayName)
		}
		return textValue("")
	case ColumnHost:
		return textValue(TrafficTarget(entry))
	case ColumnMethod:
		return textValue(TrafficMethodLabel(entry))
	case ColumnPath:
		return textValue(TrafficPathLabel(entry))
	case ColumnType:
		return textValue(TrafficTypeLabel(entry))
	case ColumnDuration:
		value, known := TrafficTotalDurationMicros(entry)
		return numberValue(value, known)
	case ColumnSize:
		value, known := TrafficTotalSizeBytes(entry)
		return numberValue(value, known)
	default:
		return textValue("")
	}
}

func compareSortValues(left, right sortValue) int {
	if left.numeric && right.numeric {
		return cmp.Compare(left.number, right.number)
	}
	if compared := strings.Compare(strings.ToLower(left.text), strings.ToLower(right.text)); compared != 0 {
		return compared
	}
	return strings.Compare(left.text, right.text)
}

// TrafficTableSorter sorts entries, computing each value once per entry
// version and sorting again only when membership or a value changes.
type TrafficTableSorter struct {
	lastKey        TrafficColumnKey
	lastOrder      SortOrder
	cached         map[*TrafficEntry]sortValue
	previousValues map[uint64]sortValue
	previousSorted []*TrafficEntry
	computed       int
}

// NewTrafficTableSorter returns an empty sorter.
func NewTrafficTableSorter() *TrafficTableSorter {
	return &TrafficTableSorter{cached: map[*TrafficEntry]sortValue{}, previousValues: map[uint64]sortValue{}}
}

// Sort returns entries in table order. Unsorted returns entries itself.
func (s *TrafficTableSorter) Sort(entries []*TrafficEntry, sort TrafficSort) []*TrafficEntry {
	if !sort.active() {
		s.lastKey, s.lastOrder = "", SortNone
		s.cached = map[*TrafficEntry]sortValue{}
		s.previousValues = map[uint64]sortValue{}
		s.previousSorted = nil
		return entries
	}
	if sort.Key != s.lastKey {
		s.cached = map[*TrafficEntry]sortValue{}
	}
	needsSort := sort.Key != s.lastKey || sort.Order != s.lastOrder || len(entries) != len(s.previousValues)
	values := make(map[uint64]sortValue, len(entries))
	byID := make(map[uint64]*TrafficEntry, len(entries))
	current := make(map[*TrafficEntry]sortValue, len(entries))
	for _, entry := range entries {
		value, ok := s.cached[entry]
		if !ok {
			value = trafficSortValue(entry, sort.Key)
			s.computed++
		}
		current[entry] = value
		values[entry.ID] = value
		byID[entry.ID] = entry
		if previous, seen := s.previousValues[entry.ID]; !seen || previous != value {
			needsSort = true
		}
	}
	s.cached = current

	var sorted []*TrafficEntry
	if needsSort {
		sorted = slices.Clone(entries)
		slices.SortStableFunc(sorted, func(left, right *TrafficEntry) int {
			a, b := values[left.ID], values[right.ID]
			switch {
			case a.missing && b.missing:
				return cmp.Compare(left.ID, right.ID)
			case a.missing:
				return 1
			case b.missing:
				return -1
			}
			compared := compareSortValues(a, b)
			if compared == 0 {
				return cmp.Compare(left.ID, right.ID)
			}
			if sort.Order == SortDesc {
				return -compared
			}
			return compared
		})
	} else {
		sorted = make([]*TrafficEntry, len(s.previousSorted))
		for index, entry := range s.previousSorted {
			sorted[index] = byID[entry.ID]
		}
	}
	s.lastKey, s.lastOrder = sort.Key, sort.Order
	s.previousValues = values
	s.previousSorted = sorted
	return sorted
}
