package logic

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// RawHTTPBinaryBody stands in for a body that cannot be shown as text.
const RawHTTPBinaryBody = "<binary body>"

// RawHTTPRequestInput describes a request to show as raw HTTP. A nil Body shows
// only the head.
type RawHTTPRequestInput struct {
	Method, URL, Host, Protocol string
	HeaderFields                []HeaderField
	Body                        *string
	BodyEncoding                string
}

// RawHTTPResponseInput describes a response to show as raw HTTP.
type RawHTTPResponseInput struct {
	Status       string
	StatusCode   int
	Protocol     string
	HeaderFields []HeaderField
	Body         *string
	BodyEncoding string
}

var (
	http2ProtocolPattern = regexp.MustCompile(`(?i)^HTTP/2(?:\.0)?$`)
	statusCodePattern    = regexp.MustCompile(`^\d{3}\b`)
)

func normalizeHTTPProtocol(protocol string) string {
	normalized := strings.TrimSpace(protocol)
	if http2ProtocolPattern.MatchString(normalized) {
		return "HTTP/2.0"
	}
	return normalized
}

func firstPseudoHeader(fields []HeaderField, name string) (string, bool) {
	for _, field := range fields {
		if strings.EqualFold(field.Name, name) {
			return field.Value, true
		}
	}
	return "", false
}

func authorityFormTarget(rawURL, fallbackHost string) string {
	trimmed := strings.TrimSpace(rawURL)
	withoutFragment := trimmed
	if before, _, ok := strings.Cut(trimmed, "#"); ok {
		withoutFragment = before
	}
	authorityOf := func(value string) string {
		if end := strings.IndexAny(value, "/?"); end >= 0 {
			return value[:end]
		}
		return value
	}
	if separator := strings.Index(withoutFragment, "://"); separator >= 0 {
		return authorityOf(withoutFragment[separator+3:])
	}
	if strings.HasPrefix(withoutFragment, "//") {
		return authorityOf(withoutFragment[2:])
	}
	if withoutFragment != "" && !strings.HasPrefix(withoutFragment, "/") {
		return authorityOf(withoutFragment)
	}
	return strings.TrimSpace(fallbackHost)
}

func rawRequestTarget(method, rawURL, host string) string {
	if strings.EqualFold(method, "CONNECT") {
		if target := authorityFormTarget(rawURL, host); target != "" {
			return target
		}
		if trimmed := strings.TrimSpace(host); trimmed != "" {
			return trimmed
		}
		return "/"
	}
	if target, ok := RequestTargetFromURL(rawURL); ok {
		return target
	}
	return "/"
}

func transformHTTP2Headers(fields []HeaderField, consumed ...string) []HeaderField {
	isConsumed := func(name string) bool {
		return slices.Contains(consumed, name)
	}
	var authority, remaining []HeaderField
	for _, field := range fields {
		name := strings.ToLower(field.Name)
		switch {
		case name == ":authority":
			authority = append(authority, HeaderField{Name: "host", Value: field.Value})
		case name == ":scheme" || isConsumed(name):
		case strings.HasPrefix(field.Name, ":"):
			remaining = append(remaining, HeaderField{Name: field.Name[1:], Value: field.Value})
		default:
			remaining = append(remaining, field)
		}
	}
	return append(authority, remaining...)
}

func formatRawMessage(startLine string, fields []HeaderField, body *string, encoding string) string {
	var builder strings.Builder
	builder.WriteString(startLine)
	builder.WriteString("\r\n")
	for _, field := range fields {
		builder.WriteString(field.Name)
		builder.WriteString(": ")
		builder.WriteString(field.Value)
		builder.WriteString("\r\n")
	}
	builder.WriteString("\r\n")
	if body != nil {
		if encoding == "base64" {
			builder.WriteString(RawHTTPBinaryBody)
		} else {
			builder.WriteString(*body)
		}
	}
	return builder.String()
}

// FormatRawHTTPRequest renders a request as the Raw view shows it.
func FormatRawHTTPRequest(input RawHTTPRequestInput) string {
	protocol := normalizeHTTPProtocol(input.Protocol)
	method := strings.TrimSpace(input.Method)
	target := rawRequestTarget(method, input.URL, input.Host)
	fields := input.HeaderFields
	if protocol == "HTTP/2.0" {
		if pseudo, ok := firstPseudoHeader(fields, ":method"); ok {
			method = strings.TrimSpace(pseudo)
		}
		authority := input.Host
		if pseudo, ok := firstPseudoHeader(fields, ":authority"); ok {
			authority = pseudo
		}
		if path, ok := firstPseudoHeader(fields, ":path"); ok {
			target = path
		} else {
			target = rawRequestTarget(method, input.URL, authority)
		}
		fields = transformHTTP2Headers(fields, ":method", ":path")
	}
	return formatRawMessage(strings.TrimSpace(method+" "+target+" "+protocol), fields, input.Body, input.BodyEncoding)
}

// FormatRawHTTPResponse renders a response as the Raw view shows it.
func FormatRawHTTPResponse(input RawHTTPResponseInput) string {
	protocol := normalizeHTTPProtocol(input.Protocol)
	status := strings.TrimSpace(input.Status)
	code := ""
	if input.StatusCode != 0 {
		code = strconv.Itoa(input.StatusCode)
	}
	if status == "" {
		status = code
	}
	fields := input.HeaderFields
	if protocol == "HTTP/2.0" {
		pseudo, _ := firstPseudoHeader(fields, ":status")
		switch pseudo = strings.TrimSpace(pseudo); {
		case pseudo != "":
			status = pseudo
		case code != "":
			status = code
		default:
			if match := statusCodePattern.FindString(status); match != "" {
				status = match
			}
		}
		fields = transformHTTP2Headers(fields, ":status")
	}
	return formatRawMessage(strings.TrimSpace(protocol+" "+status), fields, input.Body, input.BodyEncoding)
}
