package logic

import (
	"slices"
	"strconv"
	"strings"
)

// The filter tabs, in display order.
const (
	TabAll       = "all"
	TabHTTP      = "http"
	TabHTTPS     = "https"
	TabWebSocket = "websocket"
	TabSSE       = "sse"
	TabTCP       = "tcp"
	TabHTTP1     = "http1"
	TabHTTP2     = "http2"
	TabJSON      = "json"
	TabXML       = "xml"
	TabText      = "text"
	TabHTML      = "html"
	TabJS        = "js"
	TabImage     = "image"
	TabMedia     = "media"
	TabBinary    = "binary"
	Tab1xx       = "1xx"
	Tab2xx       = "2xx"
	Tab3xx       = "3xx"
	Tab4xx       = "4xx"
	Tab5xx       = "5xx"
)

// TrafficFilterTabs lists the tabs in display order.
var TrafficFilterTabs = []string{
	TabAll, TabHTTP, TabHTTPS, TabWebSocket, TabSSE, TabTCP, TabHTTP1, TabHTTP2, TabJSON, TabXML,
	TabText, TabHTML, TabJS, TabImage, TabMedia, TabBinary, Tab1xx, Tab2xx, Tab3xx, Tab4xx, Tab5xx,
}

func responseContentType(entry *TrafficEntry) string {
	if entry.Response == nil {
		return ""
	}
	value, _ := FirstHeaderValue(entry.Response.HeaderFields, "Content-Type")
	return strings.ToLower(value)
}

func matchesContentType(entry *TrafficEntry, tab string) bool {
	contentType := responseContentType(entry)
	switch tab {
	case TabSSE:
		mediaType, _, _ := strings.Cut(contentType, ";")
		return IsHTTP(entry) && strings.TrimSpace(mediaType) == "text/event-stream"
	case TabJSON:
		return strings.Contains(contentType, "json")
	case TabXML:
		return strings.Contains(contentType, "xml")
	case TabText:
		return strings.Contains(contentType, "text/plain")
	case TabHTML:
		return strings.Contains(contentType, "html")
	case TabJS:
		return strings.Contains(contentType, "javascript") || strings.Contains(contentType, "ecmascript")
	case TabImage:
		return strings.Contains(contentType, "image/")
	case TabMedia:
		return strings.Contains(contentType, "video/") || strings.Contains(contentType, "audio/")
	case TabBinary:
		return strings.Contains(contentType, "octet-stream") || strings.Contains(contentType, "application/pdf")
	default:
		return false
	}
}

func matchesProtocol(entry *TrafficEntry, tab string) bool {
	switch tab {
	case TabHTTP:
		return entry.Type == "http"
	case TabHTTPS:
		return entry.Type == "https"
	case TabWebSocket:
		return IsWebSocket(entry)
	case TabTCP:
		return IsRawTCP(entry)
	case TabHTTP1:
		return entry.Response != nil && strings.HasPrefix(entry.Response.Proto, "HTTP/1.")
	case TabHTTP2:
		return entry.Response != nil && strings.HasPrefix(entry.Response.Proto, "HTTP/2.")
	default:
		return false
	}
}

// TrafficMatchesTab reports whether entry belongs to a filter tab.
func TrafficMatchesTab(entry *TrafficEntry, tab string) bool {
	switch tab {
	case "", TabAll:
		return true
	case TabHTTP, TabHTTPS, TabWebSocket, TabTCP, TabHTTP1, TabHTTP2:
		return matchesProtocol(entry, tab)
	case TabSSE, TabJSON, TabXML, TabText, TabHTML, TabJS, TabImage, TabMedia, TabBinary:
		return matchesContentType(entry, tab)
	case Tab1xx, Tab2xx, Tab3xx, Tab4xx, Tab5xx:
		return entry.StatusCode > 0 && strconv.Itoa(entry.StatusCode/100)+"xx" == tab
	default:
		return true
	}
}

// MergeVisibleColumnOrder applies a new order of the visible columns to the
// full order without moving hidden columns from their slots.
func MergeVisibleColumnOrder(full, visible []string) []string {
	merged := slices.Clone(full)
	shown := make(map[string]bool, len(visible))
	for _, key := range visible {
		shown[key] = true
	}
	next := 0
	for index, key := range merged {
		if shown[key] && next < len(visible) {
			merged[index] = visible[next]
			next++
		}
	}
	return merged
}

// CompleteColumnOrder returns saved followed by any columns it does not name,
// in their default positions.
func CompleteColumnOrder(saved []string) []string {
	order := make([]string, 0, len(TrafficColumnKeys))
	seen := make(map[string]bool, len(TrafficColumnKeys))
	known := make(map[string]bool, len(TrafficColumnKeys))
	for _, key := range TrafficColumnKeys {
		known[string(key)] = true
	}
	for _, key := range saved {
		if known[key] && !seen[key] {
			seen[key] = true
			order = append(order, key)
		}
	}
	for _, key := range TrafficColumnKeys {
		if !seen[string(key)] {
			order = append(order, string(key))
		}
	}
	return order
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// BuildCurl returns a shell command that repeats the request. body is the
// entry's request body and encoding is empty or "base64".
func BuildCurl(entry *TrafficEntry, body, encoding string) string {
	var command strings.Builder
	command.WriteString("curl")
	if entry.Method != "GET" {
		command.WriteString(" -X " + entry.Method)
	}
	command.WriteString(" " + shellQuote(entry.URL))
	if entry.Request != nil {
		for _, field := range entry.Request.HeaderFields {
			if strings.HasPrefix(strings.TrimSpace(field.Name), ":") {
				continue
			}
			command.WriteString(" \\\n  -H " + shellQuote(field.Name+": "+field.Value))
		}
	}
	if body != "" && encoding == "base64" {
		return "printf %s " + shellQuote(body) + " | base64 -d | " + command.String() + " --data-binary @-"
	}
	if body != "" {
		command.WriteString(" \\\n  --data-raw " + shellQuote(body))
	}
	return command.String()
}

// CopyField names a value the row menu can copy.
type CopyField string

// Copyable values.
const (
	CopyTarget     CopyField = "target"
	CopyURL        CopyField = "url"
	CopyDomain     CopyField = "domain"
	CopyPath       CopyField = "path"
	CopyServerIP   CopyField = "server-ip"
	CopyClientIP   CopyField = "client-ip"
	CopyServerAddr CopyField = "server-addr"
	CopyClientAddr CopyField = "client-addr"
)

func remoteAddress(entry *TrafficEntry) string {
	if entry.Metadata == nil {
		return ""
	}
	return entry.Metadata.RemoteDestinationAddr
}

func clientAddress(entry *TrafficEntry) string {
	if entry.Metadata == nil {
		return ""
	}
	return entry.Metadata.LocalSourceAddr
}

// TrafficCopyValue returns the text of one copyable value.
func TrafficCopyValue(entry *TrafficEntry, field CopyField) string {
	switch field {
	case CopyTarget:
		return TrafficTarget(entry)
	case CopyURL:
		return entry.URL
	case CopyDomain:
		return entry.Host
	case CopyPath:
		return entry.Path
	case CopyServerIP:
		return SplitHostPortToIP(remoteAddress(entry))
	case CopyClientIP:
		return SplitHostPortToIP(clientAddress(entry))
	case CopyServerAddr:
		return remoteAddress(entry)
	case CopyClientAddr:
		return clientAddress(entry)
	default:
		return ""
	}
}

// TrafficDestinationIP returns the server IP shown in the table.
func TrafficDestinationIP(entry *TrafficEntry) string {
	return SplitHostPortToIP(remoteAddress(entry))
}
