package inspect

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/logic"
	desktoptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

type overviewState struct {
	timing, size, process, tls, cert bool
}

const labelColumnWidth = 120

// overviewRow is a label and its value, tinted with the accent under the
// pointer.
func overviewRow(c *ui.Context, theme nui.Theme, key string) ui.Element {
	row := ui.Row(c.Key(key)).FillWidth().Gap(10).PaddingX(8).PaddingY(4).Radius(6)
	if row.Hovered() {
		row.Background(theme.Primary.Alpha(0.12)).Border(1, theme.Primary.Alpha(0.22))
	} else {
		row.Border(1, ui.Transparent)
	}
	return row
}

// overviewLabel is the label column of an overview row.
func overviewLabel(c *ui.Context, theme nui.Theme, label string) ui.Element {
	return ui.Text(c, strings.ToUpper(label)).Width(labelColumnWidth).Shrink(0).FontSize(14).FontWeight(600).LetterSpacing(0.5).TextColor(theme.Toned).Selectable()
}

// overviewValue is the value column of an overview row.
func overviewValue(c *ui.Context, theme nui.Theme, value string) ui.Element {
	return ui.Text(c, value).Grow(1).Basis(0).MinWidth(0).FontSize(14).TextColor(theme.Text).Selectable()
}

func detailRow(c *ui.Context, theme nui.Theme, key, label, value string) {
	overviewRow(c, theme, key).Children(func() {
		overviewLabel(c, theme, label)
		overviewValue(c, theme, value)
	})
}

// urlRow shows a URL with its parts in their own colors.
func urlRow(c *ui.Context, theme nui.Theme, key, label, raw string) {
	colors := desktoptheme.URLPalette(theme.Dark)
	parsed := logic.ParseHighlightedURL(raw)
	span := func(text string, color ui.Color, weight int) ui.Span {
		return ui.Span{Text: text, Color: color, Weight: weight, Size: 14}
	}
	spans := []ui.Span{span(parsed.Scheme, theme.Muted, 0), span(parsed.Host, colors.Host, 600), span(parsed.Path, colors.Path, 0)}
	if parsed.HasQuery {
		spans = append(spans, span("?", colors.QueryPunct, 0))
		for index, item := range parsed.QueryItems {
			if index > 0 {
				spans = append(spans, span("&", colors.QueryPunct, 0))
			}
			spans = append(spans, span(item.Key, colors.QueryKey, 0))
			if item.HasEquals {
				spans = append(spans, span("=", colors.QueryPunct, 0), span(item.Value, colors.QueryValue, 0))
			}
		}
	}
	spans = append(spans, span(parsed.Hash, colors.Hash, 0))
	overviewRow(c, theme, key).Children(func() {
		overviewLabel(c, theme, label)
		ui.RichText(c, spans...).Grow(1).Basis(0).MinWidth(0).Selectable()
	})
}

func optionalRow(c *ui.Context, theme nui.Theme, key, label, value string) {
	if value != "" {
		detailRow(c, theme, key, label, value)
	}
}

// listRow shows values one per line, as 1.0.5 listed supported TLS values.
func listRow(c *ui.Context, theme nui.Theme, key, label string, values []string) {
	if len(values) == 0 {
		return
	}
	overviewRow(c, theme, key).Children(func() {
		overviewLabel(c, theme, label)
		ui.Column(c.Key("values")).Grow(1).Basis(0).MinWidth(0).Gap(3).Children(func() {
			for index, value := range values {
				overviewValue(c.Key(index), theme, value).LineHeight(1.45)
			}
		})
	})
}

func timeRow(c *ui.Context, theme nui.Theme, key, label string, value time.Time) {
	if !value.IsZero() {
		detailRow(c, theme, key, label, logic.FormatTimeLocal(value))
	}
}

func section(c *ui.Context, theme nui.Theme, key, title string, open *bool, body func()) {
	ui.Column(c.Key(key)).FillWidth().Margin(4, 0, 0, 0).Children(func() {
		header := ui.ButtonBase(c.Key("header")).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(12).PaddingX(8).PaddingY(7).Radius(6).Label(title)
		color := theme.Toned
		switch {
		case *open:
			color = theme.Primary
			header.Background(theme.Primary.Alpha(0.14)).Border(1, theme.Primary.Alpha(0.22))
		case header.Hovered() || header.FocusVisible():
			color = theme.Text
			header.Background(theme.Primary.Alpha(0.12)).Border(1, ui.Transparent)
		default:
			header.Border(1, ui.Transparent)
		}
		header.OnClick(func() { *open = !*open })
		header.Children(func() {
			if *open {
				ui.Box(c.Key("bar")).Absolute().Left(0).Top(0).Bottom(0).Width(3).Background(theme.Primary).IgnoreInput(true)
			}
			ui.Text(c, title).SingleLine().Shrink(1).MinWidth(0).FontSize(14).FontWeight(600).TextColor(color)
			icon := lucide.ChevronRight()
			if *open {
				icon = lucide.ChevronDown()
			}
			ui.Icon(c, icon).Size(15, 15).TextColor(color)
		})
		if *open {
			ui.Column(c.Key("body")).FillWidth().Gap(2).Children(body)
		}
	})
}

func (p *Panel) overviewPane(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(10, 10, 14, 10).Gap(2).Children(func() {
			p.basicRows(c, theme, entry)
			p.processSection(c, theme, entry)
			p.tlsSection(c, theme, entry)
			p.certificateSection(c, theme, entry)
			p.metricsSections(c, theme, entry)
		})
	})
}

func proto(message *proxyservice.HTTPMessage) string {
	if message == nil {
		return "-"
	}
	return message.Proto
}

func (p *Panel) basicRows(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	overviewRow(c, theme, "method").Children(func() {
		overviewLabel(c, theme, locales.DetailMethod())
		color := desktoptheme.MethodColor(entry.Method)
		ui.Box(c).MinHeight(20).PaddingX(8).Radius(6).Center().Background(color.Alpha(0.13).Over(theme.Background)).Children(func() {
			ui.Text(c, entry.Method).SingleLine().FontSize(14).Bold().TextColor(color)
		})
	})
	urlRow(c, theme, "url", locales.DetailUrl(), entry.URL)
	detailRow(c, theme, "path", locales.DetailPath(), entry.Path)
	status := "-"
	if entry.StatusCode != 0 {
		status = strconv.Itoa(entry.StatusCode)
	}
	detailRow(c, theme, "status", locales.DetailStatusCode(), status)
	detailRow(c, theme, "proto-req", locales.DetailProtocol()+" (Req)", proto(entry.Request))
	detailRow(c, theme, "proto-res", locales.DetailProtocol()+" (Res)", proto(entry.Response))
	detailRow(c, theme, "server", locales.DetailServerAddress(), entry.Host)
	if metadata := entry.Metadata; metadata != nil {
		optionalRow(c, theme, "local-src", locales.DetailLocalSourceAddress(), metadata.LocalSourceAddr)
		optionalRow(c, theme, "local-dst", locales.DetailLocalDestinationAddress(), metadata.LocalDestinationAddr)
		optionalRow(c, theme, "remote-src", locales.DetailRemoteSourceAddress(), metadata.RemoteSourceAddr)
		optionalRow(c, theme, "remote-dst", locales.DetailRemoteDestinationAddress(), metadata.RemoteDestinationAddr)
		timeRow(c, theme, "local-time", locales.DetailLocalConnectionTime(), metadata.LocalConnectionEstablishedAt)
		timeRow(c, theme, "remote-time", locales.DetailRemoteConnectionTime(), metadata.RemoteConnectionEstablishedAt)
		timeRow(c, theme, "request-time", locales.DetailRequestTime(), metadata.RequestProcessedAt)
		timeRow(c, theme, "tls-time", locales.DetailSslHandshake(), metadata.SSLHandshakeCompletedAt)
	}
}

func processStatusMessage(process *proxyservice.ProcessInfo) string {
	reasons := map[string]func() string{
		"remote_client": locales.DetailProcessReasonRemoteClient, "queue_full": locales.DetailProcessReasonQueueFull,
		"lookup_timeout": locales.DetailProcessReasonLookupTimeout, "socket_owner_not_found": locales.DetailProcessReasonSocketOwnerNotFound,
		"socket_scan_restricted": locales.DetailProcessReasonPermissionDenied, "process_scan_restricted": locales.DetailProcessReasonPermissionDenied,
		"multiple_socket_owners": locales.DetailProcessReasonMultipleSocketOwners, "metadata_denied": locales.DetailProcessReasonMetadataDenied,
	}
	if reason, ok := reasons[process.UnavailableReason]; ok {
		return reason()
	}
	switch process.Status {
	case proxyservice.ProcessStatusPending:
		return locales.DetailProcessStatusPending()
	case proxyservice.ProcessStatusResolved:
		if process.UnavailableReason != "" {
			return locales.DetailProcessReasonUnavailable()
		}
		return ""
	case proxyservice.ProcessStatusRemote:
		return locales.DetailProcessStatusRemote()
	case proxyservice.ProcessStatusNotFound:
		return locales.DetailProcessStatusNotFound()
	case proxyservice.ProcessStatusPermissionDenied:
		return locales.DetailProcessStatusPermissionDenied()
	case proxyservice.ProcessStatusUnsupported:
		return locales.DetailProcessStatusUnsupported()
	case proxyservice.ProcessStatusAmbiguous:
		return locales.DetailProcessStatusAmbiguous()
	default:
		return locales.DetailProcessStatusUnavailable()
	}
}

func processDisplayName(process *proxyservice.ProcessInfo) string {
	switch {
	case process.DisplayName != "":
		return process.DisplayName
	case process.ProcessName != "":
		return process.ProcessName
	case process.PID != 0:
		return locales.TrafficProcessPid(strconv.FormatUint(uint64(process.PID), 10))
	default:
		return locales.DetailProcessUnknown()
	}
}

func orDash(value string) string {
	if value == "" {
		return logic.UnknownValue
	}
	return value
}

func (p *Panel) processSection(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	if entry.Metadata == nil || entry.Metadata.Process == nil {
		return
	}
	process := entry.Metadata.Process
	section(c, theme, "process", locales.DetailProcess(), &p.overview.process, func() {
		overviewRow(c, theme, "app").AlignItems(ui.Center).Children(func() {
			overviewLabel(c, theme, locales.DetailProcessApplicationName())
			if process.Status == proxyservice.ProcessStatusResolved && p.deps.Icons != nil {
				widgets.ProcessIcon(c, p.deps.Icons, process.IconKey, 24, theme.Muted).Shrink(0)
			}
			overviewValue(c, theme, processDisplayName(process))
		})
		if message := processStatusMessage(process); message != "" {
			ui.Text(c.Key("status"), message).FontSize(14).TextColor(theme.Muted).Padding(4, 8, 4, 8)
		}
		detailRow(c, theme, "name", locales.DetailProcessName(), orDash(process.ProcessName))
		pid := logic.UnknownValue
		if process.PID != 0 {
			pid = strconv.FormatUint(uint64(process.PID), 10)
		}
		detailRow(c, theme, "pid", locales.DetailProcessPid(), pid)
		optionalRow(c, theme, "app-id", locales.DetailProcessAppId(), process.AppID)
		overviewRow(c, theme, "path").AlignItems(ui.Center).Children(func() {
			overviewLabel(c, theme, locales.DetailProcessExecutablePath())
			overviewValue(c, theme, orDash(process.ExecutablePath))
			if process.ExecutablePath != "" {
				iconButton(c, theme, "copy-path", lucide.Copy(), locales.DetailCopyProcessPath(), false, func() { p.copyText(process.ExecutablePath, locales.DetailProcessPathCopied()) })
			}
		})
	})
}

func (p *Panel) tlsSection(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	if entry.Metadata == nil || entry.Metadata.TLS == nil {
		return
	}
	tls := entry.Metadata.TLS
	section(c, theme, "tls", locales.DetailTlsInfo(), &p.overview.tls, func() {
		optionalRow(c, theme, "sni", locales.DetailTlsServerName(), tls.ServerName)
		optionalRow(c, theme, "version", locales.DetailTlsVersion(), tls.SelectedVersion)
		optionalRow(c, theme, "alpn", locales.DetailAlpn(), tls.SelectedALPN)
		optionalRow(c, theme, "cipher", locales.DetailCipherSuite(), tls.SelectedCipherSuite)
		listRow(c, theme, "supported-versions", locales.DetailSupportedTlsVersions(), tls.SupportedVersion)
		listRow(c, theme, "supported-alpn", locales.DetailSupportedAlpns(), tls.SupportedALPN)
		listRow(c, theme, "supported-ciphers", locales.DetailSupportedCipherSuites(), tls.SupportedCipherSuites)
	})
}

func pkixRows(c *ui.Context, theme nui.Theme, prefix string, name *proxyservice.PkixName, labels pkixLabels) {
	if name == nil {
		return
	}
	optionalRow(c, theme, prefix+"cn", labels.common, name.CommonName)
	optionalRow(c, theme, prefix+"org", labels.org, strings.Join(name.Organization, ", "))
	optionalRow(c, theme, prefix+"ou", labels.unit, strings.Join(name.OrganizationalUnit, ", "))
	optionalRow(c, theme, prefix+"country", labels.country, strings.Join(name.Country, ", "))
	optionalRow(c, theme, prefix+"locality", labels.locality, strings.Join(name.Locality, ", "))
	optionalRow(c, theme, prefix+"province", labels.province, strings.Join(name.Province, ", "))
	optionalRow(c, theme, prefix+"street", labels.street, strings.Join(name.StreetAddress, ", "))
	optionalRow(c, theme, prefix+"postal", labels.postal, strings.Join(name.PostalCode, ", "))
	optionalRow(c, theme, prefix+"serial", labels.serial, name.SerialNumber)
}

type pkixLabels struct {
	common, org, unit, country, locality, province, street, postal, serial string
}

func (p *Panel) certificateSection(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	if entry.Metadata == nil || entry.Metadata.Certificate == nil {
		return
	}
	cert := entry.Metadata.Certificate
	section(c, theme, "cert", locales.DetailCertInfo(), &p.overview.cert, func() {
		if cert.Version != 0 {
			detailRow(c, theme, "version", locales.DetailCertVersion(), strconv.Itoa(cert.Version))
		}
		if cert.NotBeforeMicros != 0 {
			detailRow(c, theme, "before", locales.DetailCertNotBefore(), logic.FormatUnixMicrosLocal(cert.NotBeforeMicros))
		}
		if cert.NotAfterMicros != 0 {
			detailRow(c, theme, "after", locales.DetailCertNotAfter(), logic.FormatUnixMicrosLocal(cert.NotAfterMicros))
		}
		pkixRows(c, theme, "subject-", cert.Subject, pkixLabels{
			locales.DetailCertSubject(), locales.DetailCertSubjectOrg(), locales.DetailCertSubjectOu(), locales.DetailCertSubjectCountry(),
			locales.DetailCertSubjectLocality(), locales.DetailCertSubjectProvince(), locales.DetailCertSubjectStreet(), locales.DetailCertSubjectPostal(), locales.DetailCertSubjectSerial(),
		})
		pkixRows(c, theme, "issuer-", cert.Issuer, pkixLabels{
			locales.DetailCertIssuer(), locales.DetailCertIssuerOrg(), locales.DetailCertIssuerOu(), locales.DetailCertIssuerCountry(),
			locales.DetailCertIssuerLocality(), locales.DetailCertIssuerProvince(), locales.DetailCertIssuerStreet(), locales.DetailCertIssuerPostal(), locales.DetailCertIssuerSerial(),
		})
		optionalRow(c, theme, "dns", locales.DetailCertDns(), strings.Join(cert.DNSNames, ", "))
		optionalRow(c, theme, "ips", locales.DetailCertIps(), strings.Join(cert.IPAddresses, ", "))
		optionalRow(c, theme, "serial", locales.DetailCertSerialNumber(), cert.SerialNumber)
		optionalRow(c, theme, "algorithm", locales.DetailCertSignatureAlgorithm(), cert.SignatureAlgorithm)
		optionalRow(c, theme, "sha1", locales.DetailCertSha1Fingerprint(), cert.Sha1Fingerprint)
		optionalRow(c, theme, "sha256", locales.DetailCertSha256Fingerprint(), cert.Sha256Fingerprint)
	})
}

func metricsOf(message *proxyservice.HTTPMessage) *proxyservice.HTTPMessageMetrics {
	if message == nil || message.Metrics == nil {
		return &proxyservice.HTTPMessageMetrics{StartedAtMicros: -1, EndedAtMicros: -1, HeaderSize: -1, BodySize: -1}
	}
	return message.Metrics
}

func hasMetrics(message *proxyservice.HTTPMessage) bool {
	return message != nil && message.Metrics != nil
}

func stateBadge(c *ui.Context, theme nui.Theme, metrics *proxyservice.HTTPMessageMetrics) {
	label, color := "", nui.Neutral
	switch metrics.State {
	case proxyservice.HTTPMessageStatePending:
		label, color = locales.DetailMetricStatePending(), nui.Warning
	case proxyservice.HTTPMessageStateFailed:
		label, color = locales.DetailMetricStateFailed(), nui.Error
	case proxyservice.HTTPMessageStateCanceled:
		label, color = locales.DetailMetricStateCanceled(), nui.Neutral
	}
	if label != "" {
		nui.Badge(c, theme, label, nui.BadgeOptions{Color: color, Variant: nui.Subtle, Size: nui.SM})
	}
}

func metricRow(c *ui.Context, theme nui.Theme, key, label, value string, badge func()) {
	overviewRow(c, theme, key).AlignItems(ui.Center).Children(func() {
		overviewLabel(c, theme, label)
		if value != logic.UnknownValue || badge == nil {
			ui.Text(c, value).FontSize(14).TextColor(theme.Text).Selectable()
		}
		if badge != nil {
			badge()
		}
	})
}

func (p *Panel) metricsSections(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	if !hasMetrics(entry.Request) && !hasMetrics(entry.Response) {
		if entry.Type != "tcp" {
			ui.Row(c.Key("no-metrics")).FillWidth().Gap(8).Padding(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, lucide.Info()).Size(14, 14).TextColor(theme.Muted)
				ui.Text(c, locales.DetailLegacyMetricsUnavailable()).FontSize(14).TextColor(theme.Muted)
			})
		}
		return
	}
	request, response := metricsOf(entry.Request), metricsOf(entry.Response)
	timings := proxyservice.HTTPConnectionTimings{DNSStartedAtMicros: -1, DNSEndedAtMicros: -1, ConnectStartedAtMicros: -1, ConnectEndedAtMicros: -1, TLSStartedAtMicros: -1, TLSEndedAtMicros: -1}
	if entry.Metadata != nil && entry.Metadata.ConnectionTimings != nil {
		timings = *entry.Metadata.ConnectionTimings
	}
	section(c, theme, "timing", locales.DetailTiming(), &p.overview.timing, func() {
		metricRow(c, theme, "dns", locales.DetailUpstreamDns(), logic.FormatDurationMicros(timings.DNSStartedAtMicros, timings.DNSEndedAtMicros), nil)
		metricRow(c, theme, "connect", locales.DetailUpstreamConnect(), logic.FormatDurationMicros(timings.ConnectStartedAtMicros, timings.ConnectEndedAtMicros), nil)
		metricRow(c, theme, "tls", locales.DetailUpstreamTls(), logic.FormatDurationMicros(timings.TLSStartedAtMicros, timings.TLSEndedAtMicros), nil)
		metricRow(c, theme, "req-start", locales.DetailRequestStart(), logic.FormatUnixMicrosLocal(request.StartedAtMicros), nil)
		metricRow(c, theme, "req-end", locales.DetailRequestEnd(), logic.FormatUnixMicrosLocal(request.EndedAtMicros), nil)
		metricRow(c, theme, "req-duration", locales.DetailRequestDuration(), logic.FormatDurationMicros(request.StartedAtMicros, request.EndedAtMicros), func() { stateBadge(c, theme, request) })
		metricRow(c, theme, "res-start", locales.DetailResponseStart(), logic.FormatUnixMicrosLocal(response.StartedAtMicros), nil)
		metricRow(c, theme, "res-end", locales.DetailResponseEnd(), logic.FormatUnixMicrosLocal(response.EndedAtMicros), nil)
		metricRow(c, theme, "res-duration", locales.DetailResponseDuration(), logic.FormatDurationMicros(response.StartedAtMicros, response.EndedAtMicros), func() { stateBadge(c, theme, response) })
		metricRow(c, theme, "total", locales.DetailTotalDuration(), logic.FormatDurationMicros(request.StartedAtMicros, response.EndedAtMicros), nil)
	})
	requestSize := logic.SummarizeHTTPMessageSize(request.HeaderSize, request.BodySize, entry.Request != nil && entry.Request.HeadersTruncated)
	responseSize := logic.SummarizeHTTPMessageSize(response.HeaderSize, response.BodySize, entry.Response != nil && entry.Response.HeadersTruncated)
	section(c, theme, "size", locales.DetailMessageSize(), &p.overview.size, func() {
		size := func(key, label string, value int64) {
			metricRow(c, theme, key, label, logic.FormatFileSize(value), nil)
		}
		size("req-total", locales.DetailRequestSize(), requestSize.Total)
		size("req-header", locales.DetailRequestHeaderSize(), requestSize.Header)
		size("req-body", locales.DetailRequestBodySize(), requestSize.Body)
		size("res-total", locales.DetailResponseSize(), responseSize.Total)
		size("res-header", locales.DetailResponseHeaderSize(), responseSize.Header)
		size("res-body", locales.DetailResponseBodySize(), responseSize.Body)
		size("total", locales.DetailTotalSize(), logic.SumKnownByteSizes(requestSize.Total, responseSize.Total))
	})
}
