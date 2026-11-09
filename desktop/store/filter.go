package store

import (
	"slices"

	"github.com/josexy/flowlens/desktop/logic"
)

// TrafficSource supplies the entries a filter reads.
type TrafficSource func() (entries []*logic.TrafficEntry, version uint64)

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
	baseMatches   map[*logic.TrafficEntry]bool
	categoryKey   string
	categoryMatch map[*logic.TrafficEntry]bool
	base          []*logic.TrafficEntry
	filtered      []*logic.TrafficEntry
	version       uint64
}

// NewFilterStore returns a filter showing everything from source.
func NewFilterStore(source TrafficSource) *FilterStore {
	return &FilterStore{
		source: source, ActiveTab: logic.TabAll,
		baseMatches: map[*logic.TrafficEntry]bool{}, categoryMatch: map[*logic.TrafficEntry]bool{},
	}
}

func filterKey(values ...[]string) string {
	key := ""
	for _, list := range values {
		for _, value := range list {
			key += value + "\x00"
		}
		key += "\x01"
	}
	return key
}

func reuseIfStable(current, next []*logic.TrafficEntry) ([]*logic.TrafficEntry, bool) {
	if slices.Equal(current, next) {
		return current, false
	}
	return next, true
}

// Refresh recomputes the lists if the source or the filter changed.
func (f *FilterStore) Refresh() {
	entries, version := f.source()
	baseKey := filterKey([]string{f.SearchText, f.ActiveTab})
	categoryKey := filterKey(f.SelectedHosts, f.SelectedProcessKeys)
	if f.computed && version == f.sourceVersion && baseKey == f.baseKey && categoryKey == f.categoryKey {
		return
	}
	f.computed, f.sourceVersion = true, version
	if baseKey != f.baseKey {
		f.baseKey = baseKey
		f.baseMatches = map[*logic.TrafficEntry]bool{}
	}
	if categoryKey != f.categoryKey {
		f.categoryKey = categoryKey
		f.categoryMatch = map[*logic.TrafficEntry]bool{}
	}

	nextMatches := make(map[*logic.TrafficEntry]bool, len(entries))
	base := make([]*logic.TrafficEntry, 0, len(entries))
	for _, entry := range entries {
		matched, known := f.baseMatches[entry]
		if !known {
			matched = logic.TrafficMatchesSearch(entry, f.SearchText) && logic.TrafficMatchesTab(entry, f.ActiveTab)
		}
		nextMatches[entry] = matched
		if matched {
			base = append(base, entry)
		}
	}
	f.baseMatches = nextMatches
	f.base, _ = reuseIfStable(f.base, base)

	next := f.base
	if len(f.SelectedHosts) > 0 || len(f.SelectedProcessKeys) > 0 {
		hosts := toSet(f.SelectedHosts)
		processes := toSet(f.SelectedProcessKeys)
		nextCategory := make(map[*logic.TrafficEntry]bool, len(f.base))
		next = make([]*logic.TrafficEntry, 0, len(f.base))
		for _, entry := range f.base {
			matched, known := f.categoryMatch[entry]
			if !known {
				matched = logic.TrafficMatchesCategoryFilters(entry, hosts, processes)
			}
			nextCategory[entry] = matched
			if matched {
				next = append(next, entry)
			}
		}
		f.categoryMatch = nextCategory
	}
	var changed bool
	if f.filtered, changed = reuseIfStable(f.filtered, next); changed {
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

// Filtered returns the entries that pass every filter.
func (f *FilterStore) Filtered() []*logic.TrafficEntry {
	f.Refresh()
	return f.filtered
}

// Base returns the entries that pass the search and the tab.
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

// ClearFilters returns every filter to its default.
func (f *FilterStore) ClearFilters() {
	f.SearchText, f.ActiveTab = "", logic.TabAll
	f.SelectedHosts, f.SelectedProcessKeys = nil, nil
}
