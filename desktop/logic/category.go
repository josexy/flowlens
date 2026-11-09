package logic

import (
	"cmp"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

func localeCompare(left, right string) int {
	if compared := cmp.Compare(strings.ToLower(left), strings.ToLower(right)); compared != 0 {
		return compared
	}
	return cmp.Compare(left, right)
}

// HostSummaryItem counts the requests of a host.
type HostSummaryItem struct {
	Host  string
	Count int
}

// ProcessKinds of a summary item.
const (
	ProcessResolved    = "resolved"
	ProcessUnavailable = "unavailable"
)

// ProcessSummaryItem counts the requests of a process.
type ProcessSummaryItem struct {
	Kind, ProcessKey, Label, DisplayName, ProcessName, IconKey string
	Count                                                      int
}

// BuildHostSummary counts entries per host, sorted by host.
func BuildHostSummary(entries []*TrafficEntry) []HostSummaryItem {
	counts := map[string]int{}
	for _, entry := range entries {
		if host := TrafficCategoryHost(entry); host != "" {
			counts[host]++
		}
	}
	items := make([]HostSummaryItem, 0, len(counts))
	for host, count := range counts {
		items = append(items, HostSummaryItem{Host: host, Count: count})
	}
	slices.SortFunc(items, func(a, b HostSummaryItem) int { return localeCompare(a.Host, b.Host) })
	return items
}

func compareProcessItems(left, right ProcessSummaryItem) int {
	if left.Kind != right.Kind {
		if left.Kind == ProcessUnavailable {
			return 1
		}
		return -1
	}
	if compared := localeCompare(left.Label, right.Label); compared != 0 {
		return compared
	}
	return localeCompare(left.ProcessKey, right.ProcessKey)
}

// BuildProcessSummary counts entries per process, sorted by name, with
// unidentified processes last.
func BuildProcessSummary(entries []*TrafficEntry, unavailableLabel string, keepUnavailable bool) []ProcessSummaryItem {
	summaries := map[string]*ProcessSummaryItem{}
	unavailable := 0
	for _, entry := range entries {
		process := TrafficProcessCategory(entry)
		if !process.Resolved {
			unavailable++
			continue
		}
		item, ok := summaries[process.Key]
		if !ok {
			summaries[process.Key] = &ProcessSummaryItem{
				Kind: ProcessResolved, ProcessKey: process.Key, Label: process.Label,
				DisplayName: process.DisplayName, ProcessName: process.ProcessName, IconKey: process.IconKey, Count: 1,
			}
			continue
		}
		item.Count++
		if item.DisplayName == "" && process.DisplayName != "" {
			item.DisplayName, item.Label = process.DisplayName, process.DisplayName
		}
		if item.ProcessName == "" && process.ProcessName != "" {
			item.ProcessName = process.ProcessName
			if item.DisplayName == "" {
				item.Label = process.ProcessName
			}
		}
		if item.IconKey == "" {
			item.IconKey = process.IconKey
		}
	}
	result := make([]ProcessSummaryItem, 0, len(summaries)+1)
	for _, item := range summaries {
		result = append(result, *item)
	}
	slices.SortFunc(result, compareProcessItems)
	if unavailable > 0 || keepUnavailable {
		result = append(result, ProcessSummaryItem{Kind: ProcessUnavailable, ProcessKey: ProcessCategoryUnavailableKey, Label: strings.TrimSpace(unavailableLabel), Count: unavailable})
	}
	return result
}

func retainSelectedHosts(visible, all []HostSummaryItem, selected map[string]bool) []HostSummaryItem {
	result := slices.Clone(visible)
	shown := map[string]bool{}
	for _, item := range visible {
		shown[item.Host] = true
	}
	for host := range selected {
		if !shown[host] {
			result = append(result, HostSummaryItem{Host: host})
		}
	}
	_ = all
	slices.SortFunc(result, func(a, b HostSummaryItem) int { return localeCompare(a.Host, b.Host) })
	return result
}

func retainSelectedProcesses(visible, all []ProcessSummaryItem, selected map[string]bool) []ProcessSummaryItem {
	result := slices.Clone(visible)
	shown := map[string]bool{}
	for _, item := range visible {
		shown[item.ProcessKey] = true
	}
	for _, item := range all {
		if selected[item.ProcessKey] && !shown[item.ProcessKey] {
			item.Count = 0
			result = append(result, item)
		}
	}
	slices.SortFunc(result, compareProcessItems)
	return result
}

// BuildCategoryFacets summarizes hosts narrowed by the selected processes and
// processes narrowed by the selected hosts. With selections on both sides,
// selected items with no matches stay listed.
func BuildCategoryFacets(entries []*TrafficEntry, hosts, processes map[string]bool, unavailableLabel string) ([]HostSummaryItem, []ProcessSummaryItem) {
	narrow := func(hostSet, processSet map[string]bool) []*TrafficEntry {
		if len(hostSet) == 0 && len(processSet) == 0 {
			return entries
		}
		matched := make([]*TrafficEntry, 0, len(entries))
		for _, entry := range entries {
			if TrafficMatchesCategoryFilters(entry, hostSet, processSet) {
				matched = append(matched, entry)
			}
		}
		return matched
	}
	keepUnavailable := processes[ProcessCategoryUnavailableKey]
	hostItems := BuildHostSummary(narrow(nil, processes))
	processItems := BuildProcessSummary(narrow(hosts, nil), unavailableLabel, keepUnavailable)
	if len(hosts) > 0 && len(processes) > 0 {
		hostItems = retainSelectedHosts(hostItems, BuildHostSummary(entries), hosts)
		processItems = retainSelectedProcesses(processItems, BuildProcessSummary(entries, unavailableLabel, keepUnavailable), processes)
	}
	return hostItems, processItems
}

// StructureNodeType is what a structure tree node represents.
type StructureNodeType string

// Structure node types.
const (
	StructureHost    StructureNodeType = "host"
	StructureSegment StructureNodeType = "segment"
	StructureLeaf    StructureNodeType = "leaf"
)

// StructureNode is one node of a host's URL tree.
type StructureNode struct {
	Key, Label, Host, ContentType, TrafficKind string
	Type                                       StructureNodeType
	Depth                                      int
	EntryIDs                                   []uint64
	Children                                   []*StructureNode

	childByIdentity map[string]*StructureNode
}

func pathSegments(entry *TrafficEntry) []string {
	if IsRawTCP(entry) {
		kind := "TCP"
		if entry.RawTCP != nil && entry.RawTCP.TLS {
			kind = "TCP-TLS"
		}
		target := TrafficTargetPort(entry)
		if target == "" {
			target = TrafficTarget(entry)
		}
		if target == "" {
			target = UnknownValue
		}
		return []string{kind, target}
	}
	path := entry.Path
	if parsed, err := url.Parse(entry.URL); err == nil && parsed.Scheme != "" && parsed.EscapedPath() != "" {
		path = parsed.EscapedPath()
	}
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return []string{"/"}
	}
	var segments []string
	for _, segment := range strings.Split(path, "/") {
		if segment = strings.TrimSpace(segment); segment != "" {
			segments = append(segments, segment)
		}
	}
	if len(segments) == 0 {
		return []string{"/"}
	}
	return segments
}

func entryContentType(entry *TrafficEntry) string {
	if entry.Response == nil {
		return ""
	}
	value, _ := FirstHeaderValue(entry.Response.HeaderFields, "Content-Type")
	return value
}

func sortStructure(nodes []*StructureNode) {
	slices.SortFunc(nodes, func(a, b *StructureNode) int { return localeCompare(a.Label, b.Label) })
	for _, node := range nodes {
		sortStructure(node.Children)
	}
}

// BuildStructureTree groups entries by host and URL path.
func BuildStructureTree(entries []*TrafficEntry) []*StructureNode {
	hosts := map[string]*StructureNode{}
	for _, entry := range entries {
		host := TrafficCategoryHost(entry)
		if host == "" {
			continue
		}
		kind := "http"
		if IsRawTCP(entry) {
			kind = "raw-tcp"
		}
		current, ok := hosts[host]
		if !ok {
			current = &StructureNode{Key: "host:" + host, Label: host, Type: StructureHost, Host: host, childByIdentity: map[string]*StructureNode{}}
			hosts[host] = current
		}
		current.EntryIDs = append(current.EntryIDs, entry.ID)
		segments := pathSegments(entry)
		for index, segment := range segments {
			leaf := index == len(segments)-1
			identity := kind + ":" + segment
			var next *StructureNode
			if !leaf {
				next = current.childByIdentity[identity]
			}
			if next == nil {
				next = &StructureNode{
					Key: fmt.Sprintf("%s/%s:%d:%d", current.Key, identity, entry.ID, index), Label: segment, Host: host,
					Depth: current.Depth + 1, TrafficKind: kind, Type: StructureSegment, childByIdentity: map[string]*StructureNode{},
				}
				if leaf {
					next.Type, next.ContentType = StructureLeaf, entryContentType(entry)
				}
				current.Children = append(current.Children, next)
				if !leaf {
					current.childByIdentity[identity] = next
				}
			}
			next.EntryIDs = append(next.EntryIDs, entry.ID)
			current = next
		}
	}
	roots := make([]*StructureNode, 0, len(hosts))
	for _, node := range hosts {
		roots = append(roots, node)
	}
	sortStructure(roots)
	return roots
}

// FilterHostSummary keeps hosts containing query.
func FilterHostSummary(items []HostSummaryItem, query string) []HostSummaryItem {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return items
	}
	return slices.DeleteFunc(slices.Clone(items), func(item HostSummaryItem) bool {
		return !strings.Contains(strings.ToLower(item.Host), normalized)
	})
}

// FilterProcessSummary keeps processes whose visible names contain query.
func FilterProcessSummary(items []ProcessSummaryItem, query string) []ProcessSummaryItem {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return items
	}
	return slices.DeleteFunc(slices.Clone(items), func(item ProcessSummaryItem) bool {
		for _, value := range []string{item.Label, item.DisplayName, item.ProcessName} {
			if strings.Contains(strings.ToLower(value), normalized) {
				return false
			}
		}
		return true
	})
}

// FilterStructureByHost keeps the roots whose host contains query.
func FilterStructureByHost(roots []*StructureNode, query string) []*StructureNode {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return roots
	}
	return slices.DeleteFunc(slices.Clone(roots), func(node *StructureNode) bool {
		return !strings.Contains(strings.ToLower(node.Host), normalized)
	})
}
