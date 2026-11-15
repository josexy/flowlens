package logic

import (
	"regexp"
	"strconv"
	"strings"

	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

// TrafficEntry is one captured exchange.
type TrafficEntry = proxyservice.TrafficEntry

// ProcessCategoryUnavailableKey groups traffic with no identified process.
const ProcessCategoryUnavailableKey = "process:unavailable"

// IsHARExportableHistoryFormat reports whether a history version can be
// exported as HAR.
func IsHARExportableHistoryFormat(version int) bool { return version == 1 || version == 2 }

// AdvanceEvictionWatermark returns the highest evicted ID seen.
func AdvanceEvictionWatermark(watermark uint64, evicted ...uint64) uint64 {
	for _, id := range evicted {
		if id > watermark {
			watermark = id
		}
	}
	return watermark
}

// IsTrafficEntryEvicted reports whether id left the bounded window.
func IsTrafficEntryEvicted(id, watermark uint64) bool {
	return id > 0 && watermark > 0 && id <= watermark
}

// HostPort is an address split into host and port.
type HostPort struct{ Host, Port string }

var (
	bracketedIPv6Pattern = regexp.MustCompile(`^\[([^\]]+)\](?::([^:]+))?$`)
	digitsPattern        = regexp.MustCompile(`^\d+$`)
)

// ParseHostPort splits an address, supporting bracketed IPv6.
func ParseHostPort(address string) HostPort {
	normalized := strings.TrimSpace(address)
	if normalized == "" {
		return HostPort{}
	}
	if match := bracketedIPv6Pattern.FindStringSubmatch(normalized); match != nil {
		return HostPort{Host: match[1], Port: match[2]}
	}
	last := strings.LastIndexByte(normalized, ':')
	if last > 0 && strings.IndexByte(normalized, ':') == last && digitsPattern.MatchString(normalized[last+1:]) {
		return HostPort{Host: normalized[:last], Port: normalized[last+1:]}
	}
	return HostPort{Host: normalized}
}

// SplitHostPortToIP returns an address's host.
func SplitHostPortToIP(address string) string { return ParseHostPort(address).Host }

// ProtocolLabel shortens an HTTP version for the table.
func ProtocolLabel(proto string) string {
	if proto == "HTTP/2.0" {
		return "h2"
	}
	return proto
}

// IsRawTCP reports whether entry is a raw TCP tunnel.
func IsRawTCP(entry *TrafficEntry) bool { return entry != nil && entry.Type == "tcp" }

// IsWebSocket reports whether entry is a WebSocket exchange.
func IsWebSocket(entry *TrafficEntry) bool {
	return entry != nil && (entry.Type == "ws" || entry.Type == "wss")
}

// IsHTTP reports whether entry is an HTTP exchange.
func IsHTTP(entry *TrafficEntry) bool {
	return entry != nil && (entry.Type == "http" || entry.Type == "https")
}

// TrafficTarget returns an entry's host or tunnel address.
func TrafficTarget(entry *TrafficEntry) string {
	if IsRawTCP(entry) && entry.RawTCP != nil {
		if hostPort := strings.TrimSpace(entry.RawTCP.HostPort); hostPort != "" {
			return hostPort
		}
	}
	return strings.TrimSpace(entry.Host)
}

// TrafficCategoryHost returns the host an entry is grouped under.
func TrafficCategoryHost(entry *TrafficEntry) string {
	target := TrafficTarget(entry)
	if IsRawTCP(entry) {
		if host := ParseHostPort(target).Host; host != "" {
			return host
		}
	}
	return target
}

// TrafficTargetPort returns a raw tunnel's port.
func TrafficTargetPort(entry *TrafficEntry) string {
	if !IsRawTCP(entry) {
		return ""
	}
	return ParseHostPort(TrafficTarget(entry)).Port
}

// ProcessCategory identifies the process an entry belongs to.
type ProcessCategory struct {
	Resolved                                                             bool
	Key, Label, DisplayName, ProcessName, ExecutablePath, AppID, IconKey string
}

// TrafficProcessCategory groups an entry by stable process identity.
func TrafficProcessCategory(entry *TrafficEntry) ProcessCategory {
	unavailable := ProcessCategory{Key: ProcessCategoryUnavailableKey}
	if entry.Metadata == nil || entry.Metadata.Process == nil || entry.Metadata.Process.Status != proxyservice.ProcessStatusResolved {
		return unavailable
	}
	process := entry.Metadata.Process
	appID := strings.TrimSpace(process.AppID)
	executablePath := strings.TrimSpace(process.ExecutablePath)
	processName := strings.TrimSpace(process.ProcessName)
	displayName := strings.TrimSpace(process.DisplayName)
	key := ""
	switch {
	case appID != "":
		key = "app:" + appID
	case executablePath != "":
		key = "exe:" + executablePath
	case processName != "":
		key = "name:" + processName
	}
	label := displayName
	if label == "" {
		label = processName
	}
	if key == "" || label == "" {
		return unavailable
	}
	return ProcessCategory{
		Resolved: true, Key: key, Label: label, DisplayName: displayName, ProcessName: processName,
		ExecutablePath: executablePath, AppID: appID, IconKey: strings.TrimSpace(process.IconKey),
	}
}

// TrafficMatchesCategoryFilters applies the host and process selections.
func TrafficMatchesCategoryFilters(entry *TrafficEntry, hosts, processes map[string]bool) bool {
	if len(hosts) > 0 && !hosts[TrafficCategoryHost(entry)] {
		return false
	}
	return len(processes) == 0 || processes[TrafficProcessCategory(entry).Key]
}

// TrafficProtocol returns the protocol label of an entry.
func TrafficProtocol(entry *TrafficEntry) string {
	if IsRawTCP(entry) {
		if entry.Metadata != nil && entry.Metadata.TLS != nil {
			if alpn := strings.TrimSpace(entry.Metadata.TLS.SelectedALPN); alpn != "" {
				return alpn
			}
		}
		if entry.RawTCP != nil && entry.RawTCP.TLS {
			return "TLS"
		}
		return "TCP"
	}
	proto := ""
	if entry.Response != nil {
		proto = entry.Response.Proto
	}
	if proto == "" && entry.Request != nil {
		proto = entry.Request.Proto
	}
	return ProtocolLabel(proto)
}

// TrafficTypeLabel returns an entry's type as the table shows it.
func TrafficTypeLabel(entry *TrafficEntry) string {
	if IsRawTCP(entry) {
		if entry.RawTCP != nil && entry.RawTCP.TLS {
			return "TCP/TLS"
		}
		return "TCP"
	}
	return strings.ToUpper(entry.Type)
}

// TrafficMethodLabel returns an entry's method or a dash.
func TrafficMethodLabel(entry *TrafficEntry) string {
	if method := strings.TrimSpace(entry.Method); method != "" {
		return method
	}
	return UnknownValue
}

// TrafficPathLabel returns an entry's path or a dash.
func TrafficPathLabel(entry *TrafficEntry) string {
	if IsRawTCP(entry) {
		return UnknownValue
	}
	if path := strings.TrimSpace(entry.Path); path != "" {
		return path
	}
	return UnknownValue
}

// TrafficTotalDurationMicros returns the exchange duration, or false while it
// is unknown.
func TrafficTotalDurationMicros(entry *TrafficEntry) (int64, bool) {
	if entry.Request == nil || entry.Request.Metrics == nil || entry.Response == nil || entry.Response.Metrics == nil {
		return 0, false
	}
	started, ended := entry.Request.Metrics.StartedAtMicros, entry.Response.Metrics.EndedAtMicros
	if started < 0 || ended < started {
		return 0, false
	}
	return ended - started, true
}

func messageSize(message *proxyservice.HTTPMessage) int64 {
	if message == nil || message.Metrics == nil {
		return UnknownSize
	}
	return SummarizeHTTPMessageSize(message.Metrics.HeaderSize, message.Metrics.BodySize, message.HeadersTruncated).Total
}

// TrafficTotalSizeBytes returns the exchange size, or false while it is unknown.
func TrafficTotalSizeBytes(entry *TrafficEntry) (int64, bool) {
	total := SumKnownByteSizes(messageSize(entry.Request), messageSize(entry.Response))
	return total, total >= 0
}

// TrafficMatchesSearch reports whether entry matches a search query.
func TrafficMatchesSearch(entry *TrafficEntry, query string) bool {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return true
	}
	values := []string{entry.URL, entry.Host, entry.Path}
	if IsRawTCP(entry) {
		target := TrafficTarget(entry)
		parsed := ParseHostPort(target)
		values = append(values, target, parsed.Host, parsed.Port)
		if entry.RawTCP != nil {
			values = append(values, string(entry.RawTCP.Source))
		}
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), normalized) {
			return true
		}
	}
	return false
}

// TrafficCapabilities says which actions an entry supports.
type TrafficCapabilities struct {
	CanLoadBody, CanSubscribeLiveDetail, CanEditRequest, CanResend, CanCopyCurl, CanSaveToCollection bool
}

// CapabilitiesOf returns what an entry supports.
func CapabilitiesOf(entry *TrafficEntry) TrafficCapabilities {
	if entry == nil || IsRawTCP(entry) {
		return TrafficCapabilities{}
	}
	http, websocket := IsHTTP(entry), IsWebSocket(entry)
	return TrafficCapabilities{
		CanLoadBody:            http || websocket,
		CanSubscribeLiveDetail: http || websocket,
		CanEditRequest:         http || websocket,
		CanResend:              http,
		CanCopyCurl:            http,
		CanSaveToCollection:    http || websocket,
	}
}

// FormatStatusCode shows a status code or a dash.
func FormatStatusCode(code int) string {
	if code == 0 {
		return UnknownValue
	}
	return strconv.Itoa(code)
}
