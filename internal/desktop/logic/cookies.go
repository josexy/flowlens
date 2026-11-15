package logic

import "strings"

// Header names that carry cookies.
const (
	RequestCookieHeader  = "cookie"
	ResponseCookieHeader = "set-cookie"
)

// CookieAttribute is one attribute of a Set-Cookie header.
type CookieAttribute struct {
	Name      string
	Value     string
	HasEquals bool
}

// ResponseCookie is one parsed Set-Cookie header.
type ResponseCookie struct {
	Name       string
	Value      string
	HasEquals  bool
	Attributes []CookieAttribute
	Header     HeaderField
}

func isCookieHeader(name, expected string) bool {
	return strings.ToLower(strings.TrimSpace(name)) == expected
}

func splitCookieField(raw string) (key, value string, hasEquals bool) {
	field := strings.TrimSpace(raw)
	key, value, hasEquals = strings.Cut(field, "=")
	if !hasEquals {
		return field, "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), true
}

func parseRequestCookieValue(value string, enabled bool) []EditableRow {
	var rows []EditableRow
	for raw := range strings.SplitSeq(value, ";") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		key, fieldValue, _ := splitCookieField(raw)
		rows = append(rows, EditableRow{Key: key, Value: fieldValue, Enabled: enabled})
	}
	return rows
}

// HasCookieHeader reports whether any field is the named cookie header.
func HasCookieHeader(fields []HeaderField, expected string) bool {
	for _, field := range fields {
		if isCookieHeader(field.Name, expected) {
			return true
		}
	}
	return false
}

// CookieHeaderFields returns the named cookie headers in order.
func CookieHeaderFields(fields []HeaderField, expected string) []HeaderField {
	var matched []HeaderField
	for _, field := range fields {
		if isCookieHeader(field.Name, expected) {
			matched = append(matched, field)
		}
	}
	return matched
}

// RequestCookieHeadersRecord groups the enabled Cookie rows by header name.
func RequestCookieHeadersRecord(rows []EditableRow) HeaderRecord {
	var record HeaderRecord
	for _, row := range rows {
		if !row.Enabled || !isCookieHeader(row.Key, RequestCookieHeader) {
			continue
		}
		name := strings.TrimSpace(row.Key)
		if name == "" {
			name = "Cookie"
		}
		record.add(name, row.Value)
	}
	return record
}

// RequestCookiesRecord groups the cookies of Cookie headers by name.
func RequestCookiesRecord(fields []HeaderField) HeaderRecord {
	var record HeaderRecord
	for _, field := range CookieHeaderFields(fields, RequestCookieHeader) {
		for _, cookie := range parseRequestCookieValue(field.Value, true) {
			record.add(cookie.Key, cookie.Value)
		}
	}
	return record
}

// ParseResponseCookies parses Set-Cookie headers, keeping malformed ones
// inspectable.
func ParseResponseCookies(fields []HeaderField) []ResponseCookie {
	headers := CookieHeaderFields(fields, ResponseCookieHeader)
	cookies := make([]ResponseCookie, 0, len(headers))
	for _, header := range headers {
		parts := strings.Split(header.Value, ";")
		name, value, hasEquals := splitCookieField(parts[0])
		cookie := ResponseCookie{Name: name, Value: value, HasEquals: hasEquals, Header: header}
		for _, raw := range parts[1:] {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			key, attribute, equals := splitCookieField(raw)
			cookie.Attributes = append(cookie.Attributes, CookieAttribute{Name: key, Value: attribute, HasEquals: equals})
		}
		cookies = append(cookies, cookie)
	}
	return cookies
}

// RequestCookieRows lists the cookies of Cookie header rows, or one empty row.
func RequestCookieRows(headers []EditableRow) []EditableRow {
	var rows []EditableRow
	for _, header := range headers {
		if isCookieHeader(header.Key, RequestCookieHeader) {
			rows = append(rows, parseRequestCookieValue(header.Value, header.Enabled)...)
		}
	}
	if len(rows) == 0 {
		return []EditableRow{{Enabled: true}}
	}
	return rows
}

// CountRequestCookieRows counts enabled, named cookies.
func CountRequestCookieRows(headers []EditableRow) int {
	count := 0
	for _, cookie := range RequestCookieRows(headers) {
		if cookie.Enabled && strings.TrimSpace(cookie.Key) != "" {
			count++
		}
	}
	return count
}

// ReplaceRequestCookieHeaders rewrites the Cookie header rows from cookies,
// keeping their position and the first header's spelling.
func ReplaceRequestCookieHeaders(headers, cookies []EditableRow) []EditableRow {
	firstIndex := -1
	headerName := "Cookie"
	for index, header := range headers {
		if isCookieHeader(header.Key, RequestCookieHeader) {
			firstIndex = index
			if name := strings.TrimSpace(header.Key); name != "" {
				headerName = name
			}
			break
		}
	}
	result := make([]EditableRow, 0, len(headers)+2)
	insertAt := -1
	for index, header := range headers {
		if index == firstIndex {
			insertAt = len(result)
		}
		if !isCookieHeader(header.Key, RequestCookieHeader) {
			result = append(result, header)
		}
	}
	if insertAt < 0 {
		insertAt = len(result)
	}
	var enabled, disabled []string
	for _, cookie := range cookies {
		name := strings.TrimSpace(cookie.Key)
		if name == "" {
			continue
		}
		if cookie.Enabled {
			enabled = append(enabled, name+"="+cookie.Value)
		} else {
			disabled = append(disabled, name+"="+cookie.Value)
		}
	}
	var replacements []EditableRow
	if len(enabled) > 0 {
		replacements = append(replacements, EditableRow{Key: headerName, Value: strings.Join(enabled, "; "), Enabled: true})
	}
	if len(disabled) > 0 {
		replacements = append(replacements, EditableRow{Key: headerName, Value: strings.Join(disabled, "; ")})
	}
	combined := make([]EditableRow, 0, len(result)+len(replacements))
	combined = append(combined, result[:insertAt]...)
	combined = append(combined, replacements...)
	return append(combined, result[insertAt:]...)
}
