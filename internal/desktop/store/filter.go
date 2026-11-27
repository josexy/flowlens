package store

import (
	"slices"
	"strings"

	"github.com/josexy/flowlens/internal/desktop/logic"
)

// TrafficSource supplies the entries a filter reads.
type TrafficSource func() (entries []*logic.TrafficEntry, version uint64)

type trafficFilterMatch struct {
	base, category bool
	seen           bool
}

// FilterStore narrows a traffic list by a search query, a tab, and category
// selections. An entry's results are remembered until it or the filter
// changes, so a live update costs only the entries it touched.
type FilterStore struct {
	source TrafficSource

	SearchText          string
	ActiveTab           string
	SelectedHosts       []string
	SelectedProcessKeys []string

	sourceVersion uint64
	computed      bool
	baseKey       string
	categoryKey   string
	matches       map[*logic.TrafficEntry]trafficFilterMatch
	matchCapacity int
	generation    bool
	base          []*logic.TrafficEntry
	filtered      []*logic.TrafficEntry
	version       uint64
}

// NewFilterStore returns a filter showing everything from source.
func NewFilterStore(source TrafficSource) *FilterStore {
	return &FilterStore{source: source, ActiveTab: logic.TabAll}
}

func filterKey(values ...[]string) string {
	var key strings.Builder
	for _, list := range values {
		for _, value := range list {
			key.WriteString(value + "\x00")
		}
		key.WriteString("\x01")
	}
	return key.String()
}

// Refresh recomputes the lists if the source or the filter changed.
func (f *FilterStore) Refresh() {
	entries, version := f.source()
	baseKey := filterKey([]string{f.SearchText, f.ActiveTab})
	categoryKey := filterKey(f.SelectedHosts, f.SelectedProcessKeys)
	if f.computed && version == f.sourceVersion && baseKey == f.baseKey && categoryKey == f.categoryKey {
		return
	}
	baseChanged, categoryChanged := baseKey != f.baseKey, categoryKey != f.categoryKey
	f.computed, f.sourceVersion, f.baseKey, f.categoryKey = true, version, baseKey, categoryKey
	f.generation = !f.generation
	n := len(entries)
	limit := max(64, 4*n)
	if n == 0 {
		if len(f.filtered) > 0 {
			f.version++
		}
		clear(f.base)
		clear(f.filtered)
		f.base, f.filtered, f.matches, f.matchCapacity = nil, nil, nil, 0
		return
	}
	unfiltered := f.SearchText == "" && f.ActiveTab == logic.TabAll && len(f.SelectedHosts) == 0 && len(f.SelectedProcessKeys) == 0
	if unfiltered {
		// The common All view needs no per-entry predicate cache.
		f.matches, f.matchCapacity = nil, 0
	} else if f.matches == nil || f.matchCapacity < n || f.matchCapacity > limit {
		capacity := n
		if f.matchCapacity < n {
			capacity = max(n, 2*f.matchCapacity)
		}
		f.matchCapacity = capacity
		f.matches = make(map[*logic.TrafficEntry]trafficFilterMatch, capacity)
	}
	base, next := f.base[:0], f.filtered[:0]
	if cap(base) > limit {
		base = nil
	}
	if cap(next) > limit {
		next = nil
	}
	base = slices.Grow(base, n)
	next = slices.Grow(next, n)
	hosts, processes := toSet(f.SelectedHosts), toSet(f.SelectedProcessKeys)
	hasCategories := len(hosts) > 0 || len(processes) > 0
	changed := false
	for _, entry := range entries {
		match := trafficFilterMatch{base: true, category: true}
		if !unfiltered {
			var known bool
			match, known = f.matches[entry]
			if !known || baseChanged {
				match.base = logic.TrafficMatchesSearch(entry, f.SearchText) && logic.TrafficMatchesTab(entry, f.ActiveTab)
			}
			if !known || categoryChanged {
				match.category = !hasCategories || logic.TrafficMatchesCategoryFilters(entry, hosts, processes)
			}
			match.seen = f.generation
			f.matches[entry] = match
		}
		if match.base {
			base = append(base, entry)
			if match.category {
				index := len(next)
				if index >= len(f.filtered) || f.filtered[index] != entry {
					changed = true
				}
				next = append(next, entry)
			}
		}
	}
	for entry, match := range f.matches {
		if match.seen != f.generation {
			delete(f.matches, entry)
		}
	}
	changed = changed || len(next) != len(f.filtered)
	clear(f.base[min(len(base), len(f.base)):])
	clear(f.filtered[min(len(next), len(f.filtered)):])
	f.base, f.filtered = base, next
	if changed {
		f.version++
	}
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

// Filtered returns a borrowed main-thread view, valid until the next Refresh.
func (f *FilterStore) Filtered() []*logic.TrafficEntry {
	f.Refresh()
	return f.filtered
}

// Base returns the search/tab matches as a borrowed view until the next Refresh.
func (f *FilterStore) Base() []*logic.TrafficEntry {
	f.Refresh()
	return f.base
}

// Version changes whenever Filtered does.
func (f *FilterStore) Version() uint64 {
	f.Refresh()
	return f.version
}

// SetActiveTab selects a tab.
func (f *FilterStore) SetActiveTab(tab string) { f.ActiveTab = tab }

func toggled(values []string, value string) []string {
	if slices.Contains(values, value) {
		return slices.DeleteFunc(slices.Clone(values), func(candidate string) bool { return candidate == value })
	}
	return append(slices.Clone(values), value)
}

// ToggleHost adds or removes a host selection.
func (f *FilterStore) ToggleHost(host string) { f.SelectedHosts = toggled(f.SelectedHosts, host) }

// SetSelectedHosts replaces the host selections.
func (f *FilterStore) SetSelectedHosts(hosts []string) { f.SelectedHosts = slices.Clone(hosts) }

// ClearSelectedHosts removes the host selections.
func (f *FilterStore) ClearSelectedHosts() { f.SelectedHosts = nil }

// ToggleProcess adds or removes a process selection.
func (f *FilterStore) ToggleProcess(key string) {
	f.SelectedProcessKeys = toggled(f.SelectedProcessKeys, key)
}

// ClearSelectedProcesses removes the process selections.
func (f *FilterStore) ClearSelectedProcesses() { f.SelectedProcessKeys = nil }

// Release drops the computed lists and remembered results, so the filter keeps
// no entry of a source that was closed. The next read recomputes them.
func (f *FilterStore) Release() {
	f.computed = false
	clear(f.base)
	clear(f.filtered)
	f.base, f.filtered = nil, nil
	f.matches, f.matchCapacity = nil, 0
	f.version++
}

// ClearFilters returns every filter to its default.
func (f *FilterStore) ClearFilters() {
	f.SearchText, f.ActiveTab = "", logic.TabAll
	f.SelectedHosts, f.SelectedProcessKeys = nil, nil
}
