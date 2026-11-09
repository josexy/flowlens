package logic

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// UnknownValue is shown for values that were never measured.
const UnknownValue = "—"

// UnknownSize marks a byte size that is not known.
const UnknownSize int64 = -1

func formatLocalTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04:05.000000")
}

// FormatTimeLocal formats a timestamp in local time with microseconds.
func FormatTimeLocal(t time.Time) string {
	if t.IsZero() {
		return UnknownValue
	}
	return formatLocalTime(t)
}

// FormatTimeRFC3339 formats a timestamp in local time as RFC 3339 with
// milliseconds, as exported text shows it.
func FormatTimeRFC3339(t time.Time) string {
	local := t.Local()
	_, offset := local.Zone()
	sign := '+'
	if offset < 0 {
		sign = '-'
		offset = -offset
	}
	return fmt.Sprintf("%s.%d%c%02d:%02d", local.Format("2006-01-02T15:04:05"),
		local.Nanosecond()/int(time.Millisecond), sign, offset/3600, offset%3600/60)
}

// FormatUnixMicrosLocal formats a Unix microsecond timestamp in local time.
func FormatUnixMicrosLocal(micros int64) string {
	if micros < 0 {
		return UnknownValue
	}
	return formatLocalTime(time.UnixMicro(micros))
}

// FormatDurationMicros formats the time between two Unix microsecond stamps.
func FormatDurationMicros(startedAt, endedAt int64) string {
	if startedAt < 0 || endedAt < startedAt {
		return UnknownValue
	}
	duration := endedAt - startedAt
	if duration < 1000 {
		return strconv.FormatInt(duration, 10) + " μs"
	}
	return strconv.FormatInt(int64(math.Round(float64(duration)/1000)), 10) + " ms"
}

// HTTPMessageSizeSummary holds a message's sizes; unknown ones are UnknownSize.
type HTTPMessageSizeSummary struct {
	Header, Body, Total int64
}

func knownByteSize(value int64) int64 {
	if value >= 0 {
		return value
	}
	return UnknownSize
}

// SumKnownByteSizes adds sizes; the total is unknown if any size is.
func SumKnownByteSizes(sizes ...int64) int64 {
	var total int64
	for _, size := range sizes {
		if size < 0 {
			return UnknownSize
		}
		total += size
	}
	return total
}

// SummarizeHTTPMessageSize combines a message's header and body sizes.
func SummarizeHTTPMessageSize(headerSize, bodySize int64, headersTruncated bool) HTTPMessageSizeSummary {
	header := knownByteSize(headerSize)
	if headersTruncated {
		header = UnknownSize
	}
	body := knownByteSize(bodySize)
	return HTTPMessageSizeSummary{Header: header, Body: body, Total: SumKnownByteSizes(header, body)}
}

// FileSizeOptions adjusts FormatFileSize. Use DefaultFileSizeOptions to start.
type FileSizeOptions struct {
	Precision         int
	TrimTrailingZeros bool
	UnknownValue      string
}

// DefaultFileSizeOptions returns the usual formatting.
func DefaultFileSizeOptions() FileSizeOptions {
	return FileSizeOptions{Precision: 2, TrimTrailingZeros: true, UnknownValue: UnknownValue}
}

// FormatFileSize formats a byte count in binary units. Negative sizes are
// unknown.
func FormatFileSize(bytes int64) string {
	return FormatFileSizeWith(bytes, DefaultFileSizeOptions())
}

// FormatFileSizeWith is FormatFileSize with options.
func FormatFileSizeWith(bytes int64, options FileSizeOptions) string {
	if bytes < 0 {
		return options.UnknownValue
	}
	if bytes == 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	exponent := min(max(int(math.Floor(math.Log(float64(bytes))/math.Log(1024))), 0), len(units)-1)
	if exponent == 0 {
		return fmt.Sprintf("%d %s", bytes, units[0])
	}
	precision := min(max(options.Precision, 0), 6)
	value := strconv.FormatFloat(float64(bytes)/math.Pow(1024, float64(exponent)), 'f', precision, 64)
	if options.TrimTrailingZeros && strings.Contains(value, ".") {
		value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	}
	return value + " " + units[exponent]
}

// RequestTargetFromURL returns the origin-form target of a URL. It reports
// false for text that is neither an absolute URL, a path, nor "*".
func RequestTargetFromURL(rawURL string) (string, bool) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "*" {
		return "*", true
	}
	withoutFragment := trimmed
	if index := strings.IndexByte(trimmed, '#'); index >= 0 {
		withoutFragment = trimmed[:index]
	}
	if separator := strings.Index(withoutFragment, "://"); separator >= 0 {
		rest := withoutFragment[separator+3:]
		pathStart := strings.IndexByte(rest, '/')
		queryStart := strings.IndexByte(rest, '?')
		switch {
		case pathStart >= 0 && (queryStart < 0 || pathStart < queryStart):
			return rest[pathStart:], true
		case queryStart >= 0:
			return "/" + rest[queryStart:], true
		default:
			return "/", true
		}
	}
	if strings.HasPrefix(withoutFragment, "/") {
		return withoutFragment, true
	}
	return "", false
}
