package logic

import (
	"cmp"
	"slices"
	"strings"
)

// TrafficProcessingPolicy distinguishes mutable capture updates from published
// immutable history snapshots. Source kind, not the presence of live controls,
// chooses the policy.
type TrafficProcessingPolicy uint8

const (
	TrafficLive TrafficProcessingPolicy = iota
	TrafficSnapshot
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
	folded  string
	number  int64
	missing bool
	numeric bool
}

func textValue(text string) sortValue { return sortValue{text: text, folded: strings.ToLower(text)} }

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
	if compared := strings.Compare(left.folded, right.folded); compared != 0 {
		return compared
	}
	return strings.Compare(left.text, right.text)
}

// TrafficTableSorter sorts entries, computing each value once per entry
// version and sorting again only when membership or a value changes.
type TrafficTableSorter struct {
	policy          TrafficProcessingPolicy
	lastKey         TrafficColumnKey
	lastOrder       SortOrder
	cached          map[uint64]trafficSortRecord
	capacity        int
	generation      uint64
	previousSorted  []*TrafficEntry
	computed        int
	snapshotVersion uint64
	snapshotReady   bool
}

type trafficSortRecord struct {
	entry *TrafficEntry
	value sortValue
	seen  uint64
}

// NewTrafficTableSorter returns an empty sorter.
func NewTrafficTableSorter(policy ...TrafficProcessingPolicy) *TrafficTableSorter {
	s := &TrafficTableSorter{}
	if len(policy) != 0 {
		s.policy = policy[0]
	}
	return s
}

// Sort returns a borrowed main-thread view until the next Sort. Unsorted
// returns entries itself. Callers needing a persistent snapshot must clone it.
func (s *TrafficTableSorter) Sort(entries []*TrafficEntry, sort TrafficSort, revision ...uint64) []*TrafficEntry {
	if !sort.active() || len(entries) == 0 {
		s.snapshotReady = false
		s.lastKey, s.lastOrder = "", SortNone
		s.cached, s.capacity = nil, 0
		clear(s.previousSorted)
		s.previousSorted = nil
		return entries
	}
	if s.policy == TrafficSnapshot {
		return s.sortSnapshot(entries, sort, revision)
	}
	n := len(entries)
	limit := max(64, 4*n)
	if s.cached == nil || s.capacity < n || s.capacity > limit {
		capacity := n
		if s.capacity < n {
			capacity = max(n, 2*s.capacity)
		}
		s.capacity = capacity
		s.cached = make(map[uint64]trafficSortRecord, capacity)
	}
	keyChanged := sort.Key != s.lastKey
	needsSort := keyChanged || sort.Order != s.lastOrder || n != len(s.cached)
	s.generation++
	for _, entry := range entries {
		record, ok := s.cached[entry.ID]
		value := record.value
		if !ok || keyChanged || record.entry != entry {
			value = trafficSortValue(entry, sort.Key)
			s.computed++
		}
		if !ok || record.value != value {
			needsSort = true
		}
		s.cached[entry.ID] = trafficSortRecord{entry: entry, value: value, seen: s.generation}
	}
	for id, record := range s.cached {
		if record.seen != s.generation {
			delete(s.cached, id)
		}
	}

	sorted := s.previousSorted
	if cap(sorted) > limit {
		sorted = nil
	}
	clear(sorted[min(n, len(sorted)):])
	sorted = slices.Grow(sorted[:0], n)[:n]
	if needsSort {
		copy(sorted, entries)
		slices.SortStableFunc(sorted, func(left, right *TrafficEntry) int {
			a, b := s.cached[left.ID].value, s.cached[right.ID].value
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
		for index, entry := range sorted {
			sorted[index] = s.cached[entry.ID].entry
		}
	}
	s.lastKey, s.lastOrder = sort.Key, sort.Order
	s.previousSorted = sorted
	return sorted
}

type snapshotSortRecord struct {
	entry *TrafficEntry
	value sortValue
}

func (s *TrafficTableSorter) sortSnapshot(entries []*TrafficEntry, sort TrafficSort, revision []uint64) []*TrafficEntry {
	if len(revision) > 0 && s.snapshotReady && s.snapshotVersion == revision[0] &&
		s.lastKey == sort.Key && s.lastOrder == sort.Order {
		return s.previousSorted
	}
	// Scratch and full folded strings die with this call. Only the ordered
	// pointers survive; never pool or cache the comparison records.
	scratch := make([]snapshotSortRecord, len(entries))
	for i, entry := range entries {
		scratch[i] = snapshotSortRecord{entry, trafficSortValue(entry, sort.Key)}
		s.computed++
	}
	slices.SortFunc(scratch, func(left, right snapshotSortRecord) int {
		a, b := left.value, right.value
		switch {
		case a.missing && b.missing:
			return cmp.Compare(left.entry.ID, right.entry.ID)
		case a.missing:
			return 1
		case b.missing:
			return -1
		}
		compared := compareSortValues(a, b)
		if compared == 0 {
			return cmp.Compare(left.entry.ID, right.entry.ID)
		}
		if sort.Order == SortDesc {
			return -compared
		}
		return compared
	})
	n := len(entries)
	result := s.previousSorted
	clear(result[min(n, len(result)):])
	if cap(result) > max(64, 4*n) {
		result = nil
	}
	result = slices.Grow(result[:0], n)[:n]
	for i := range scratch {
		result[i] = scratch[i].entry
	}
	s.previousSorted, s.lastKey, s.lastOrder = result, sort.Key, sort.Order
	s.snapshotReady = len(revision) > 0
	if s.snapshotReady {
		s.snapshotVersion = revision[0]
	}
	return result
}
