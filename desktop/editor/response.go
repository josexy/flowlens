package editor

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/inspect"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

// responseView is the response half of an HTTP tab.
type responseView struct {
	editor   *Editor
	key      string
	state    *logic.HTTPEditorState
	body     *inspect.BodyViewer
	cookies  *inspect.CookieList
	console  *consoleView
	tab      string
	seen     *logic.HTTPResponseView
	identity uint64
}

func newResponseView(e *Editor, key string, state *logic.HTTPEditorState) *responseView {
	return &responseView{
		editor: e, key: key, state: state, body: inspect.NewBodyViewer(e.Host.Inspect, true),
		cookies: inspect.NewCookieList(e.Host.Inspect), console: newConsoleView(e, key, state),
	}
}

func (r *responseView) fields() []logic.HeaderField {
	if r.state.Response == nil {
		return nil
	}
	return logic.EditableRowsToHeaderFields(r.state.Response.Headers)
}

func formatElapsed(micros int64) string {
	if micros <= 0 {
		return ""
	}
	return time.Duration(micros * int64(time.Microsecond)).Round(time.Millisecond).String()
}

func statusColor(code int) nui.Color {
	switch {
	case code >= 500 || code >= 400:
		return nui.Error
	case code >= 300:
		return nui.Warning
	case code >= 200:
		return nui.Success
	default:
		return nui.Neutral
	}
}

func (r *responseView) build(c *ui.Context, theme nui.Theme) {
	state := r.state
	if state.Response != r.seen {
		r.seen = state.Response
		r.identity++
	}
	hasResponse, hasConsole := logic.HasResponse(state), logic.HasRequestConsole(state)
	fields := r.fields()
	hasCookies := logic.HasCookieHeader(fields, "set-cookie")
	visible := logic.VisibleResponseTab(state, hasCookies)
	cancelled := state.Response != nil && state.Response.Kind == logic.ResponseKindCancelled
	ui.Column(c.Key("response-panel")).Fill().MinHeight(0).Children(func() {
		switch {
		case state.IsSending && visible != logic.ResponseTabConsole:
			ui.Column(c).Fill().Center().Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.WorkspaceHttpRequestResponseSendingTitle()).FontSize(14).TextColor(theme.Muted)
			})
		case !hasResponse && !hasConsole && !cancelled:
			emptyPane(c, theme, lucide.Send(), locales.WorkspaceHttpRequestRequestNotStarted(), "")
		case cancelled && visible != logic.ResponseTabConsole:
			emptyPane(c, theme, lucide.CircleStop(), locales.WorkspaceHttpRequestRequestCancelledTitle(), locales.WorkspaceHttpRequestRequestCancelledDescription())
		default:
			r.tabs(c, theme, visible, hasCookies)
			ui.Column(c.Key(visible)).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() { r.content(c, theme, visible, fields) })
		}
	})
}

func emptyPane(c *ui.Context, theme nui.Theme, icon *ui.SVG, title, description string) {
	ui.Column(c).Fill().Center().Children(func() {
		widgets.EmptyState(c, theme, nui.EmptyOptions{Icon: icon, Title: title, Description: description})
	})
}

func (r *responseView) tabItems(hasCookies bool) []nui.TabItem {
	state := r.state
	consoleItem := nui.TabItem{ID: logic.ResponseTabConsole, Label: locales.WorkspaceHttpRequestConsole()}
	errorItem := nui.TabItem{ID: logic.ResponseTabError, Label: locales.WorkspaceHttpRequestResponseError()}
	if logic.HasErrorResponse(state) {
		return []nui.TabItem{errorItem, consoleItem}
	}
	if !logic.HasResponse(state) {
		return []nui.TabItem{consoleItem}
	}
	var items []nui.TabItem
	if logic.HasResponseErrorDetails(state) {
		items = append(items, errorItem)
	}
	items = append(items, nui.TabItem{ID: logic.ResponseTabHeaders, Label: locales.WorkspaceHttpRequestResponseHeaders() + "(" + strconv.Itoa(len(state.Response.Headers)) + ")"})
	if hasCookies {
		items = append(items, nui.TabItem{ID: logic.ResponseTabCookies, Label: locales.WorkspaceHttpRequestCookies() + "(" + strconv.Itoa(len(logic.ParseResponseCookies(r.fields()))) + ")"})
	}
	if count := logic.ResponseTrailerCount(state.Response); count > 0 {
		items = append(items, nui.TabItem{ID: logic.ResponseTabTrailers, Label: locales.WorkspaceHttpRequestResponseTrailers() + "(" + strconv.Itoa(len(state.Response.Trailers)) + ")"})
	}
	return append(items, nui.TabItem{ID: logic.ResponseTabBody, Label: locales.WorkspaceHttpRequestResponseBody()}, consoleItem)
}

func (r *responseView) tabs(c *ui.Context, theme nui.Theme, visible string, hasCookies bool) {
	r.tab = visible
	widgets.TabStrip(c.Key("response-head"), theme, &r.tab, r.tabItems(hasCookies), widgets.TabStripOptions{
		Label: locales.WorkspaceHttpRequestResponseBody(), PaddingX: 10, Trailing: func() { r.chips(c, theme) },
	})
	if r.tab != visible {
		r.state.ActiveResponseTab = r.tab
	}
}

func (r *responseView) chips(c *ui.Context, theme nui.Theme) {
	state := r.state
	if !logic.HasResponse(state) || logic.HasErrorResponse(state) {
		return
	}
	response := state.Response
	ui.Row(c.Key("chips")).Shrink(0).Gap(4).AlignItems(ui.Center).Children(func() {
		status := "-"
		if response.StatusCode != 0 {
			status = strconv.Itoa(response.StatusCode)
		}
		badge := nui.Badge(c.Key("status"), theme, status, nui.BadgeOptions{Color: statusColor(response.StatusCode), Variant: nui.Soft})
		if text := strings.TrimSpace(response.StatusText); text != "" {
			nui.Tooltip(c, theme, badge, text, nui.TooltipOptions{})
		}
		protocol := response.Protocol
		if protocol == "" {
			protocol = "-"
		}
		nui.Badge(c.Key("protocol"), theme, protocol, nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Soft})
		if elapsed := formatElapsed(response.ElapsedMicros); elapsed != "" {
			nui.Badge(c.Key("elapsed"), theme, elapsed, nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Soft})
		}
		switch response.StreamState {
		case logic.StreamStateStreaming:
			nui.Badge(c.Key("stream"), theme, locales.WorkspaceHttpRequestStreaming(), nui.BadgeOptions{Color: nui.Info, Variant: nui.Soft})
		case logic.StreamStateStopped:
			nui.Badge(c.Key("stream"), theme, locales.WorkspaceHttpRequestStreamStopped(), nui.BadgeOptions{Color: nui.Neutral, Variant: nui.Soft})
		case logic.StreamStateCompleted:
			nui.Badge(c.Key("stream"), theme, locales.WorkspaceHttpRequestStreamCompleted(), nui.BadgeOptions{Color: nui.Success, Variant: nui.Soft})
		case logic.StreamStateError:
			nui.Badge(c.Key("stream"), theme, locales.WorkspaceHttpRequestStreamError(), nui.BadgeOptions{Color: nui.Error, Variant: nui.Soft})
		}
	})
}

func (r *responseView) content(c *ui.Context, theme nui.Theme, visible string, fields []logic.HeaderField) {
	state := r.state
	switch visible {
	case logic.ResponseTabError:
		r.errorPane(c, theme)
	case logic.ResponseTabConsole:
		r.console.build(c, theme)
	case logic.ResponseTabCookies:
		warning := ""
		if state.Response != nil && state.Response.HeadersTruncated {
			warning = locales.WorkspaceHttpRequestHeaderFieldsTruncated()
		}
		r.cookies.Build(c, theme, fields, warning)
	case logic.ResponseTabTrailers:
		trailers := logic.EditableRowsToHeaderFields(state.Response.Trailers)
		warning := ""
		if state.Response.TrailersTruncated {
			warning = locales.WorkspaceHttpRequestTrailerFieldsTruncated()
		}
		r.fieldsPane(c, theme, locales.WorkspaceHttpRequestResponseTrailerList(), trailers, warning, locales.WorkspaceHttpRequestCopyResponseTrailersJson(), locales.WorkspaceHttpRequestCopyResponseTrailersText(), locales.WorkspaceHttpRequestResponseTrailersCopiedJson(), locales.WorkspaceHttpRequestResponseTrailersCopiedText())
	case logic.ResponseTabBody:
		content := inspect.BodyContent{Identity: r.identity, Body: state.Response.Body, Encoding: state.Response.BodyEncoding, SourcePath: state.URL}
		content.ContentType, _ = logic.FirstHeaderValue(fields, "content-type")
		r.body.Build(c, theme, content)
	default:
		warning := ""
		if state.Response != nil && state.Response.HeadersTruncated {
			warning = locales.WorkspaceHttpRequestHeaderFieldsTruncated()
		}
		r.fieldsPane(c, theme, locales.WorkspaceHttpRequestResponseHeaderList(), fields, warning, locales.WorkspaceHttpRequestCopyResponseHeadersJson(), locales.WorkspaceHttpRequestCopyResponseHeadersText(), locales.WorkspaceHttpRequestResponseHeadersCopiedJson(), locales.WorkspaceHttpRequestResponseHeadersCopiedText())
	}
}

func (r *responseView) fieldsPane(c *ui.Context, theme nui.Theme, title string, fields []logic.HeaderField, warning, copyJSON, copyText, copiedJSON, copiedText string) {
	fieldsPane(c, theme, r.editor, title, fields, warning, copyJSON, copyText, copiedJSON, copiedText)
}

func fieldsPane(c *ui.Context, theme nui.Theme, e *Editor, title string, fields []logic.HeaderField, warning, copyJSON, copyText, copiedJSON, copiedText string) {
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		paneHeader(c, theme, title, func() {
			iconAction(c, theme, "copy-json", lucide.Code(), copyJSON, false, func() {
				e.copy(logic.FormatHeaderFieldsAsJSON(fields), copiedJSON)
			})
			iconAction(c, theme, "copy-text", lucide.Copy(), copyText, false, func() {
				e.copy(logic.FormatHeaderFieldsAsText(fields), copiedText)
			})
		})
		if warning != "" {
			ui.Box(c.Key("warning")).FillWidth().Shrink(0).Padding(0, 10, 8, 10).Children(func() {
				nui.Alert(c, theme, nui.AlertOptions{Description: warning, Color: nui.Warning, Variant: nui.Soft, Icon: lucide.TriangleAlert()})
			})
		}
		if len(fields) == 0 {
			emptyPane(c, theme, lucide.Rows3(), locales.DetailNoHeaders(), "")
			return
		}
		ui.Column(c.Key("fields")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			rows := make([]widgets.KeyValue, len(fields))
			for index, field := range fields {
				rows[index] = widgets.KeyValue{Key: field.Name, Value: field.Value}
			}
			widgets.FieldGrid(c, theme, rows, widgets.FieldGridOptions{Label: title})
		})
	})
}

func errorResponseTitle(state *logic.HTTPEditorState) string {
	switch {
	case state.Response != nil && state.Response.Kind == logic.ResponseKindPlugin:
		return locales.WorkspaceHttpRequestPluginTerminalTitle()
	case state.Response != nil && state.Response.Outcome == logic.PluginOutcomeCompletedWithError:
		return locales.WorkspaceHttpRequestPluginOutcomeCompletedWithPluginError()
	default:
		return locales.WorkspaceHttpRequestResponseErrorTitle()
	}
}

func (r *responseView) errorPane(c *ui.Context, theme nui.Theme) {
	details := ""
	if r.state.Response != nil {
		details = r.state.Response.ErrorMessage
	}
	errorCard(c, theme, r.editor, errorResponseTitle(r.state), "", locales.WorkspaceHttpRequestResponseErrorDetails(), details, locales.WorkspaceHttpRequestCopyResponseErrorDetails(), locales.WorkspaceHttpRequestResponseErrorDetailsCopied())
}

func errorCard(c *ui.Context, theme nui.Theme, e *Editor, title, description, heading, details, copyLabel, copied string) {
	ui.Scroll(c.Key("error")).Fill().MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Padding(16).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, lucide.CircleAlert()).Size(18, 18).Shrink(0).TextColor(theme.Error)
				ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, title).FontSize(14).Bold().TextColor(theme.Highlighted)
					if description != "" {
						ui.Text(c, description).FontSize(12).TextColor(theme.Muted)
					}
				})
			})
			ui.Column(c).FillWidth().Gap(6).Border(1, theme.Border).Radius(theme.Radius).Padding(10).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
					ui.Text(c, heading).FontSize(12).TextColor(theme.Muted)
					iconAction(c, theme, "copy-error", lucide.Copy(), copyLabel, false, func() { e.copy(details, copied) })
				})
				ui.Text(c, details).FontSize(14).Font(theme.MonoFont).TextColor(theme.Text).Selectable().FillWidth()
			})
		})
	})
}
