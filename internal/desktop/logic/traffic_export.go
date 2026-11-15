package logic

import (
	"regexp"
	"strings"
	"time"

	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

// TrafficExportFormat is a way to write entries to files.
type TrafficExportFormat string

// Export formats.
const (
	ExportRequest         TrafficExportFormat = "request"
	ExportRequestHeaders  TrafficExportFormat = "request-headers"
	ExportRequestBody     TrafficExportFormat = "request-body"
	ExportResponse        TrafficExportFormat = "response"
	ExportResponseHeaders TrafficExportFormat = "response-headers"
	ExportResponseBody    TrafficExportFormat = "response-body"
	ExportRequestResponse TrafficExportFormat = "request-response"
	ExportCSV             TrafficExportFormat = "csv"
	ExportHAR             TrafficExportFormat = "har"
)

// TrafficExportGroups lists the formats in menu groups.
var TrafficExportGroups = [][]TrafficExportFormat{
	{ExportRequest, ExportRequestHeaders, ExportRequestBody},
	{ExportResponse, ExportResponseHeaders, ExportResponseBody},
	{ExportRequestResponse},
	{ExportCSV, ExportHAR},
}

// CanExportTraffic reports whether an entry can be written in format.
func CanExportTraffic(entry *TrafficEntry, format TrafficExportFormat) bool {
	if format == ExportCSV {
		return true
	}
	if !IsHTTP(entry) && !IsWebSocket(entry) {
		return false
	}
	if format == ExportHAR {
		return true
	}
	value := string(format)
	request := strings.HasPrefix(value, "request")
	response := strings.HasPrefix(value, "response") || format == ExportRequestResponse
	body := !strings.HasSuffix(value, "-headers")
	if request && (entry.Request == nil || strings.TrimSpace(entry.Request.Proto) == "" || strings.TrimSpace(entry.Method) == "") {
		return false
	}
	if response && (entry.Response == nil || strings.TrimSpace(entry.Response.Proto) == "" || (entry.StatusCode <= 0 && strings.TrimSpace(entry.Status) == "")) {
		return false
	}
	completed := func(message *proxyservice.HTTPMessage) bool {
		return message != nil && message.Metrics != nil && message.Metrics.State == proxyservice.HTTPMessageStateCompleted
	}
	if body && request && !completed(entry.Request) {
		return false
	}
	if body && response && !completed(entry.Response) {
		return false
	}
	return true
}

// TrafficExportExtension returns the file extension of a format.
func TrafficExportExtension(format TrafficExportFormat, entry *TrafficEntry) string {
	value := string(format)
	switch {
	case format == ExportCSV || format == ExportHAR:
		return "." + value
	case strings.HasSuffix(value, "-headers"):
		return ".txt"
	case !strings.HasSuffix(value, "-body"):
		return ".http"
	}
	contentType := ""
	if entry != nil {
		message := entry.Response
		if format == ExportRequestBody {
			message = entry.Request
		}
		if message != nil {
			contentType, _ = FirstHeaderValue(message.HeaderFields, "Content-Type")
		}
	}
	media := mediaType(contentType)
	switch {
	case media == "application/json" || strings.HasSuffix(media, "+json"):
		return ".json"
	case media == "image/svg+xml":
		return ".svg"
	case media == "text/xml" || media == "application/xml" || strings.HasSuffix(media, "+xml"):
		return ".xml"
	}
	extensions := map[string]string{
		"text/plain": ".txt", "text/html": ".html", "image/jpeg": ".jpg", "image/png": ".png",
		"image/gif": ".gif", "image/webp": ".webp", "application/pdf": ".pdf", "application/zip": ".zip",
	}
	if extension, ok := extensions[media]; ok {
		return extension
	}
	return ".bin"
}

var trailingDotsPattern = regexp.MustCompile(`[. ]+$`)

// MakeHARFilename returns a safe file name for an export.
func MakeHARFilename(hint string, now time.Time) string {
	safe := trailingDotsPattern.ReplaceAllString(unsafeFilenamePattern.ReplaceAllString(strings.TrimSpace(hint), "-"), "")
	if len(safe) > 80 {
		safe = safe[:80]
	}
	if safe == "" {
		safe = "flowlens-" + now.Format("20060102-150405")
	}
	return safe + ".har"
}

// EnsureFileExtension appends extension to a path without one.
func EnsureFileExtension(path, extension string) string {
	base := path[strings.LastIndexAny(path, `/\`)+1:]
	if strings.Contains(base, ".") {
		return path
	}
	return path + extension
}

// EnsureHARExtension appends .har unless path already ends with it.
func EnsureHARExtension(path string) string {
	if strings.HasSuffix(strings.ToLower(path), ".har") {
		return path
	}
	return path + ".har"
}

// UniqueHARImportPaths drops repeats while keeping file order. Windows paths
// compare without regard to case or slash direction.
func UniqueHARImportPaths(paths []string, windows bool) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		key := path
		if windows {
			key = strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
		}
		if !seen[key] {
			seen[key] = true
			unique = append(unique, path)
		}
	}
	return unique
}

// HARImportFileName returns the last part of a path.
func HARImportFileName(path string) string {
	if base := path[strings.LastIndexAny(path, `/\`)+1:]; base != "" {
		return base
	}
	return path
}
