// Package logic holds the interface's rules that do not depend on widgets.
package logic

import "slices"

// TrafficColumnKey identifies a traffic table column.
type TrafficColumnKey string

// The traffic table's columns, in their default order.
const (
	ColumnID          TrafficColumnKey = "id"
	ColumnMethod      TrafficColumnKey = "method"
	ColumnHost        TrafficColumnKey = "host"
	ColumnPath        TrafficColumnKey = "path"
	ColumnProcess     TrafficColumnKey = "process"
	ColumnStatusCode  TrafficColumnKey = "statusCode"
	ColumnType        TrafficColumnKey = "type"
	ColumnDestination TrafficColumnKey = "destination"
	ColumnProtocol    TrafficColumnKey = "protocol"
	ColumnDuration    TrafficColumnKey = "duration"
	ColumnSize        TrafficColumnKey = "size"
)

// TrafficColumnKeys lists every column in default order.
var TrafficColumnKeys = []TrafficColumnKey{
	ColumnID, ColumnMethod, ColumnHost, ColumnPath, ColumnProcess, ColumnStatusCode,
	ColumnType, ColumnDestination, ColumnProtocol, ColumnDuration, ColumnSize,
}

// NormalizeHiddenTrafficColumns keeps known column keys in default order. At
// least one column stays visible: if every column is hidden, ID is shown.
func NormalizeHiddenTrafficColumns(values []string) []string {
	hidden := make(map[TrafficColumnKey]bool, len(values))
	for _, value := range values {
		key := TrafficColumnKey(trimSpace(value))
		if slices.Contains(TrafficColumnKeys, key) {
			hidden[key] = true
		}
	}
	if len(hidden) == len(TrafficColumnKeys) {
		delete(hidden, ColumnID)
	}
	normalized := make([]string, 0, len(hidden))
	for _, key := range TrafficColumnKeys {
		if hidden[key] {
			normalized = append(normalized, string(key))
		}
	}
	return normalized
}
