package proxyservice

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// HeaderSize is the UTF-8 size of the logical head shown by httpRaw.ts:
// start line + CRLF, displayed field lines + CRLF, and the final CRLF.
// HTTP/2 uses a synthesized start line and converts its pseudo-headers to the
// same text representation as the raw viewer. This is not a wire-size metric.
func logicalHTTPRequestHeaderSize(entry *TrafficEntry) int64 {
	if entry == nil || entry.Request == nil || entry.Request.HeadersTruncated {
		return -1
	}
	message := entry.Request
	protocol := logicalHeaderProtocol(message.Proto)
	method := strings.TrimSpace(entry.Method)
	target := logicalRequestTarget(method, entry.URL, entry.Host)
	if protocol == "HTTP/2.0" {
		if value, ok := logicalPseudoHeaderValue(message.HeaderFields, ":method"); ok {
			method = strings.TrimSpace(value)
		}
		authority := entry.Host
		if value, ok := logicalPseudoHeaderValue(message.HeaderFields, ":authority"); ok {
			authority = value
		}
		target = logicalRequestTarget(method, entry.URL, authority)
		if value, ok := logicalPseudoHeaderValue(message.HeaderFields, ":path"); ok {
			target = value
		}
	}
	if protocol == "" || method == "" || target == "" || message.HeaderFields == nil {
		return -1
	}
	return logicalHTTPHeadSize(method+" "+target+" "+protocol, message.HeaderFields, protocol, true)
}

func logicalHTTPResponseHeaderSize(entry *TrafficEntry) int64 {
	if entry == nil || entry.Response == nil || entry.Response.HeadersTruncated {
		return -1
	}
	message := entry.Response
	protocol := logicalHeaderProtocol(message.Proto)
	status := strings.TrimSpace(entry.Status)
	if status == "" && entry.StatusCode > 0 {
		status = strconv.Itoa(entry.StatusCode)
	}
	if protocol == "HTTP/2.0" {
		pseudoStatus, _ := logicalPseudoHeaderValue(message.HeaderFields, ":status")
		if pseudoStatus = strings.TrimSpace(pseudoStatus); pseudoStatus != "" {
			status = pseudoStatus
		} else if entry.StatusCode > 0 {
			status = strconv.Itoa(entry.StatusCode)
		} else if parts := strings.Fields(status); len(parts) > 0 {
			status = parts[0]
		}
	}
	if protocol == "" || status == "" || message.HeaderFields == nil {
		return -1
	}
	return logicalHTTPHeadSize(protocol+" "+status, message.HeaderFields, protocol, false)
}

func logicalHTTPHeadSize(startLine string, fields []HTTPHeaderField, protocol string, request bool) int64 {
	size := logicalUTF8Size(startLine) + 4 // start-line CRLF and final empty line
	for _, field := range fields {
		name := field.Name
		if protocol == "HTTP/2.0" {
			switch strings.ToLower(name) {
			case ":authority":
				name = "host"
			case ":scheme":
				continue
			case ":method", ":path":
				if request {
					continue
				}
			case ":status":
				if !request {
					continue
				}
			}
			name = strings.TrimPrefix(name, ":")
		}
		size += logicalUTF8Size(name) + logicalUTF8Size(field.Value) + 4 // ': ' and CRLF
	}
	return size
}

func logicalUTF8Size(value string) int64 {
	if utf8.ValidString(value) {
		return int64(len(value))
	}
	// Match JSON/Wails replacement of each invalid UTF-8 byte before display.
	var size int64
	for _, r := range value {
		size += int64(utf8.RuneLen(r))
	}
	return size
}

func logicalHeaderProtocol(protocol string) string {
	protocol = strings.TrimSpace(protocol)
	if strings.EqualFold(protocol, "HTTP/2") || strings.EqualFold(protocol, "HTTP/2.0") {
		return "HTTP/2.0"
	}
	return protocol
}

func logicalPseudoHeaderValue(fields []HTTPHeaderField, name string) (string, bool) {
	for _, field := range fields {
		if strings.EqualFold(field.Name, name) {
			return field.Value, true
		}
	}
	return "", false
}

// Preserve escaped paths, trailing '?' and dot segments just like the raw
// viewer. Parsing and rebuilding a URL here could change its display length.
func logicalRequestTarget(method, rawURL, fallbackHost string) string {
	rawURL = strings.TrimSpace(rawURL)
	rawURL, _, _ = strings.Cut(rawURL, "#")
	if strings.EqualFold(method, "CONNECT") {
		authority := rawURL
		if index := strings.Index(authority, "://"); index >= 0 {
			authority = authority[index+3:]
		} else {
			authority = strings.TrimPrefix(authority, "//")
		}
		if index := strings.IndexAny(authority, "/?"); index >= 0 {
			authority = authority[:index]
		}
		if authority == "" {
			authority = strings.TrimSpace(fallbackHost)
		}
		return authority
	}
	if rawURL == "*" {
		return rawURL
	}
	if index := strings.Index(rawURL, "://"); index >= 0 {
		afterScheme := rawURL[index+3:]
		if index := strings.IndexAny(afterScheme, "/?"); index >= 0 {
			target := afterScheme[index:]
			if target[0] == '?' {
				return "/" + target
			}
			return target
		}
		return "/"
	}
	if strings.HasPrefix(rawURL, "/") {
		return rawURL
	}
	return ""
}
