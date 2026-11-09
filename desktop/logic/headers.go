package logic

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

// HeaderField is one ordered header line.
type HeaderField = proxyservice.HTTPHeaderField

// HeaderSortOrder is how a header table is sorted.
type HeaderSortOrder string

// Sort orders.
const (
	HeaderSortDefault HeaderSortOrder = "default"
	HeaderSortAsc     HeaderSortOrder = "asc"
	HeaderSortDesc    HeaderSortOrder = "desc"
)

// RequestProtocol is a request's chosen HTTP version.
type RequestProtocol string

// Request protocols.
const (
	ProtocolAuto  RequestProtocol = "auto"
	ProtocolHTTP1 RequestProtocol = "http1"
	ProtocolHTTP2 RequestProtocol = "http2"
)

// EditableRow is one row of an editable key-value table.
type EditableRow struct {
	Key     string
	Value   string
	Enabled bool
}

// HeaderRecordEntry is one name in a HeaderRecord.
type HeaderRecordEntry struct {
	Name   string
	Values []string
}

// HeaderRecord groups values by name in first-seen order.
type HeaderRecord []HeaderRecordEntry

func (r *HeaderRecord) add(name, value string) {
	for index := range *r {
		if (*r)[index].Name == name {
			(*r)[index].Values = append((*r)[index].Values, value)
			return
		}
	}
	*r = append(*r, HeaderRecordEntry{Name: name, Values: []string{value}})
}

var headerNamePattern = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")

var http2ConnectionHeaders = []string{"connection", "keep-alive", "proxy-connection", "transfer-encoding", "upgrade"}

// NormalizeRequestProtocol maps unknown values to automatic.
func NormalizeRequestProtocol(value string) RequestProtocol {
	if protocol := RequestProtocol(value); protocol == ProtocolHTTP1 || protocol == ProtocolHTTP2 {
		return protocol
	}
	return ProtocolAuto
}

// InferRequestProtocol picks a protocol from a captured message when its header
// block is complete.
func InferRequestProtocol(message *proxyservice.HTTPMessage) RequestProtocol {
	if message == nil || message.HeaderFields == nil || message.HeadersTruncated || message.HeaderOrderUnavailable {
		return ProtocolAuto
	}
	switch {
	case strings.HasPrefix(message.Proto, "HTTP/2"):
		return ProtocolHTTP2
	case strings.HasPrefix(message.Proto, "HTTP/1"):
		return ProtocolHTTP1
	default:
		return ProtocolAuto
	}
}

// HeadersRecordToFields expands a record into header lines. A name without
// values becomes one empty line.
func HeadersRecordToFields(record HeaderRecord) []HeaderField {
	fields := make([]HeaderField, 0, len(record))
	for _, entry := range record {
		if len(entry.Values) == 0 {
			fields = append(fields, HeaderField{Name: entry.Name})
			continue
		}
		for _, value := range entry.Values {
			fields = append(fields, HeaderField{Name: entry.Name, Value: value})
		}
	}
	return fields
}

// FirstHeaderValue returns the first value of a header, ignoring case.
func FirstHeaderValue(fields []HeaderField, name string) (string, bool) {
	expected := strings.ToLower(strings.TrimSpace(name))
	for _, field := range fields {
		if strings.ToLower(strings.TrimSpace(field.Name)) == expected {
			return field.Value, true
		}
	}
	return "", false
}

// HasWireOrder reports whether fields carry the captured order.
func HasWireOrder(fields []HeaderField, orderUnavailable bool) bool {
	return fields != nil && !orderUnavailable
}

// SortHeaderFields returns a sorted copy. Equal names keep their order.
func SortHeaderFields(fields []HeaderField, order HeaderSortOrder) []HeaderField {
	result := slices.Clone(fields)
	if order == HeaderSortDefault {
		return result
	}
	slices.SortStableFunc(result, func(left, right HeaderField) int {
		compared := strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
		if order == HeaderSortDesc {
			return -compared
		}
		return compared
	})
	return result
}

func jsonString(value string) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return strings.TrimSuffix(buffer.String(), "\n")
}

// FormatHeaderFieldsAsJSON renders fields as an indented JSON object, in
// first-seen name order. Repeated names become arrays.
func FormatHeaderFieldsAsJSON(fields []HeaderField) string {
	var record HeaderRecord
	for _, field := range fields {
		record.add(field.Name, field.Value)
	}
	if len(record) == 0 {
		return "{}"
	}
	var builder strings.Builder
	builder.WriteString("{\n")
	for index, entry := range record {
		builder.WriteString("  " + jsonString(entry.Name) + ": ")
		if len(entry.Values) == 1 {
			builder.WriteString(jsonString(entry.Values[0]))
		} else {
			builder.WriteString("[\n")
			for valueIndex, value := range entry.Values {
				builder.WriteString("    " + jsonString(value))
				if valueIndex < len(entry.Values)-1 {
					builder.WriteByte(',')
				}
				builder.WriteByte('\n')
			}
			builder.WriteString("  ]")
		}
		if index < len(record)-1 {
			builder.WriteByte(',')
		}
		builder.WriteByte('\n')
	}
	builder.WriteByte('}')
	return builder.String()
}

// FormatHeaderFieldsAsText renders fields one per line.
func FormatHeaderFieldsAsText(fields []HeaderField) string {
	lines := make([]string, len(fields))
	for index, field := range fields {
		lines[index] = field.Name + ": " + field.Value
	}
	return strings.Join(lines, "\n")
}

// EditableRowsToHeaderFields converts enabled, named rows to header lines.
func EditableRowsToHeaderFields(rows []EditableRow) []HeaderField {
	fields := make([]HeaderField, 0, len(rows))
	for _, row := range rows {
		if name := strings.TrimSpace(row.Key); row.Enabled && name != "" {
			fields = append(fields, HeaderField{Name: name, Value: row.Value})
		}
	}
	return fields
}

// HeaderFieldsToEditableRows converts header lines to enabled rows.
func HeaderFieldsToEditableRows(fields []HeaderField) []EditableRow {
	rows := make([]EditableRow, len(fields))
	for index, field := range fields {
		rows[index] = EditableRow{Key: field.Name, Value: field.Value, Enabled: true}
	}
	return rows
}

// EditableHeaderFieldsToRows is HeaderFieldsToEditableRows without pseudo-headers.
func EditableHeaderFieldsToRows(fields []HeaderField) []EditableRow {
	return slices.DeleteFunc(HeaderFieldsToEditableRows(fields), func(row EditableRow) bool {
		return strings.HasPrefix(strings.TrimSpace(row.Key), ":")
	})
}

// IsValidHTTPHeaderName reports whether name is an HTTP token.
func IsValidHTTPHeaderName(name string) bool {
	return headerNamePattern.MatchString(strings.TrimSpace(name))
}

// IsValidRequestHeaderName also rejects connection headers under HTTP/2.
func IsValidRequestHeaderName(name string, protocol RequestProtocol) bool {
	if !IsValidHTTPHeaderName(name) {
		return false
	}
	return protocol != ProtocolHTTP2 || !slices.Contains(http2ConnectionHeaders, strings.ToLower(strings.TrimSpace(name)))
}

// FindInvalidRequestHeaderName returns the first invalid enabled name.
func FindInvalidRequestHeaderName(rows []EditableRow, protocol RequestProtocol) (string, bool) {
	for _, row := range rows {
		if name := strings.TrimSpace(row.Key); row.Enabled && name != "" && !IsValidRequestHeaderName(name, protocol) {
			return name, true
		}
	}
	return "", false
}

// requestAuthority returns a URL's normalized host and port, as browsers
// normalize it, for http and https URLs.
func requestAuthority(rawURL string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := parsed.Port()
	if port == "" || (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		return host, true
	}
	if number, err := strconv.Atoi(port); err == nil {
		port = strconv.Itoa(number)
	}
	return host + ":" + port, true
}

func isRouteHeader(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return normalized == "host" || strings.HasPrefix(normalized, ":")
}

// ConvertRequestRouteHeaders removes backend-owned pseudo-headers and adjusts
// Host when a request's protocol changes.
func ConvertRequestRouteHeaders(rows []EditableRow, protocol RequestProtocol, rawURL string) []EditableRow {
	authority, ok := requestAuthority(rawURL)
	if !ok {
		for _, row := range rows {
			if name := strings.ToLower(strings.TrimSpace(row.Key)); name == ":authority" || name == "host" {
				authority = row.Value
				break
			}
		}
	}
	retained := make([]EditableRow, 0, len(rows)+1)
	insertAt := -1
	hostName := "Host"
	for _, row := range rows {
		if !isRouteHeader(row.Key) {
			retained = append(retained, row)
			continue
		}
		if insertAt < 0 {
			insertAt = len(retained)
		}
		if name := strings.TrimSpace(row.Key); strings.EqualFold(name, "host") && name != "" {
			hostName = name
		}
	}
	if protocol == ProtocolHTTP2 {
		return retained
	}
	return slices.Insert(retained, max(insertAt, 0), EditableRow{Key: hostName, Value: authority, Enabled: true})
}

// SynchronizeRequestRouteHeaders keeps the URL as the routing truth while all
// pseudo-headers stay backend-owned.
func SynchronizeRequestRouteHeaders(rows []EditableRow, protocol RequestProtocol, rawURL string) []EditableRow {
	authority, routed := requestAuthority(rawURL)
	sanitized := slices.DeleteFunc(slices.Clone(rows), func(row EditableRow) bool {
		return strings.HasPrefix(strings.TrimSpace(row.Key), ":")
	})
	isHost := func(row EditableRow) bool { return strings.EqualFold(strings.TrimSpace(row.Key), "host") }
	if protocol == ProtocolHTTP2 {
		return slices.DeleteFunc(sanitized, isHost)
	}
	var host *EditableRow
	insertAt := -1
	result := make([]EditableRow, 0, len(sanitized)+1)
	for _, row := range sanitized {
		if !isHost(row) {
			result = append(result, row)
			continue
		}
		if insertAt < 0 {
			insertAt = len(result)
		}
		if host == nil {
			copied := row
			if routed {
				copied.Value = authority
			}
			host = &copied
		}
	}
	if host == nil {
		host = &EditableRow{Key: "Host", Value: authority, Enabled: true}
	}
	return slices.Insert(result, max(insertAt, 0), *host)
}
