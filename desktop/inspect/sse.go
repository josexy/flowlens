package inspect

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/runtime"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// asyncParseThreshold is how much new text is parsed off the main thread.
const asyncParseThreshold = 256 * 1024

// sseRowHeight matches the console's row height, including cell padding.
const sseRowHeight = 40

// sseColumnIDs are the message list's columns in the order columns gives
// them.
var sseColumnIDs = []string{"id", "event", "preview"}

// columns use the same native resize grips as the console. The preview
// takes the remaining width and the table scrolls if the columns overflow.
func (s *SSEView) columns() []nui.TableColumn[*logic.SSEEvent] {
	columns := []nui.TableColumn[*logic.SSEEvent]{
		{ID: sseColumnIDs[0], Title: locales.DetailSseId(), Width: 68, MinWidth: 48, MaxWidth: 360},
		{ID: sseColumnIDs[1], Title: locales.DetailSseEventType(), Width: 116, MinWidth: 96, MaxWidth: 420},
		{ID: sseColumnIDs[2], Title: locales.DetailSsePreview(), MinWidth: 296},
	}
	columns[0].Value = func(event *logic.SSEEvent) string { return locales.DetailSseOpenMessage(strconv.Itoa(event.Sequence)) }
	for index := range columns {
		columns[index].Cell = func(c *ui.Context, theme nui.Theme, event *logic.SSEEvent, _ bool) {
			s.cell(c, theme, event, index)
		}
	}
	return columns
}

// SSEView lists the events of a stream.
type SSEView struct {
	deps Dependencies

	identity uint64
	body     string
	encoding string
	parsed   int
	parser   *logic.SSEParser
	events   []*logic.SSEEvent
	parsing  bool
	sequence uint64

	table nui.TableState[*logic.SSEEvent]
	// rows opens a message on a click or Enter, as 1.0.5's row buttons did.
	rows *widgets.RowActivator

	open       bool
	selected   *logic.SSEMessageDetail
	detailTab  string
	formatted  string
	detailView *widgets.CodeView
	wrap       widgets.DetailWrap
	exporting  bool
	clipboard  *string
}

// NewSSEView returns an empty view.
func NewSSEView(deps Dependencies) *SSEView {
	view := &SSEView{deps: deps, parser: &logic.SSEParser{}, detailTab: "data", detailView: widgets.NewCodeView("plaintext", true)}
	view.resetList()
	return view
}

func (s *SSEView) reset() {
	s.parser, s.events, s.parsed, s.parsing = &logic.SSEParser{}, nil, 0, false
	s.sequence++
	s.closeDetail()
	s.resetList()
}

// resetList starts following a new stream with the current column layout.
func (s *SSEView) resetList() {
	s.table = nui.TableState[*logic.SSEEvent]{List: ui.ListState{FollowEnd: true, Columns: s.table.List.Columns}}
	s.rows = widgets.NewRowActivator(&s.table.List, &s.table.Selection)
}

func (s *SSEView) closeDetail() { s.open, s.selected = false, nil }

// update parses what arrived since the last call.
func (s *SSEView) update(content BodyContent) {
	identityChanged := content.Identity != s.identity || content.Encoding != s.encoding
	grew := !identityChanged && len(content.Body) >= s.parsed && strings.HasPrefix(content.Body, s.body)
	if !grew {
		s.reset()
	}
	s.identity, s.body, s.encoding = content.Identity, content.Body, content.Encoding
	if content.Encoding == "base64" || s.parsing || len(content.Body) == s.parsed {
		return
	}
	chunk := content.Body[s.parsed:]
	end := len(content.Body)
	if len(chunk) < asyncParseThreshold {
		s.events = append(s.events, s.parser.Append(chunk)...)
		s.parsed = end
		return
	}
	s.parsing = true
	parser, sequence := s.parser, s.sequence
	runtime.Run(s.deps.Runtime.Jobs, fmt.Sprintf("inspect:sse:%p", s), func(context.Context) ([]*logic.SSEEvent, error) {
		return parser.Append(chunk), nil
	}, func(events []*logic.SSEEvent, _ error) {
		if sequence != s.sequence {
			return
		}
		s.parsing, s.parsed = false, end
		s.events = append(s.events, events...)
	})
}

// Build shows the stream.
func (s *SSEView) Build(c *ui.Context, theme nui.Theme, content BodyContent) {
	if s.clipboard != nil {
		c.WriteClipboard(*s.clipboard)
		s.clipboard = nil
	}
	s.update(content)
	if len(s.events) != s.table.Len() {
		s.table.SetRows(s.events, func(event *logic.SSEEvent) string { return strconv.Itoa(event.Sequence) })
	}
	s.detail(c, theme)
	if content.Encoding == "base64" {
		emptyState(c, theme, lucide.Binary(), locales.DetailSseUnavailable())
		return
	}
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(8).PaddingX(10).PaddingY(4).Children(func() {
			ui.Text(c, locales.DetailSseMessageCount(strconv.Itoa(len(s.events)))).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
			if s.parsing {
				ui.Text(c, locales.DetailSseParsing()).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
			}
			ui.Box(c).Grow(1)
			iconButton(c, theme, "latest", lucide.ArrowDownToLine(), locales.DetailSseJumpToLatest(), len(s.events) == 0, s.table.List.ScrollToEnd)
		})
		ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0).PaddingX(10).Children(func() {
			// As 1.0.5: the empty state only once parsing found nothing.
			if len(s.events) == 0 && !s.parsing {
				emptyState(c, theme, lucide.Radio(), locales.DetailSseEmpty())
				return
			}
			table := widgets.Table(c.Key("sse-table"), theme, &s.table, s.columns(), nui.TableOptions{
				Label:      locales.DetailSseTab(),
				BuildEmpty: func(*ui.Context, nui.Theme) {},
			}).Border(1, theme.Border)
			s.rows.Attach(table)
			if row, ok := s.rows.Activated(table, len(s.events)); ok {
				s.openDetail(s.events[row])
			}
		})
		// 1.0.5 noted an incomplete last event under the list once parsed;
		// the parser is the background job's while it parses.
		if !s.parsing && s.parser.HasPendingEvent() {
			ui.Text(c, locales.DetailSseIncomplete()).FillWidth().Shrink(0).Padding(6, 10, 0, 10).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
		}
		ui.Box(c).Shrink(0).Height(10)
	})
}

// cell builds a message row's column on one line, as 1.0.5 did: the ID in
// muted figures, the event type, and the data preview.
func (s *SSEView) cell(c *ui.Context, theme nui.Theme, event *logic.SSEEvent, col int) {
	ui.Row(c).FillWidth().MinWidth(0).Height(sseRowHeight - 12).AlignItems(ui.Center).Children(func() {
		switch sseColumnIDs[col] {
		case "id":
			ui.Text(c, orDash(event.LastEventID)).SingleLine().MinWidth(0).FontSize(apptheme.TextSize(c, 14)).FontFeatures("tnum").TextColor(theme.Muted)
		case "event":
			ui.Text(c, event.EventType).SingleLine().MinWidth(0).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Text)
		default:
			preview := event.Preview
			if preview == "" {
				preview = locales.DetailSseEmptyData()
			}
			ui.Text(c, preview).SingleLine().MinWidth(0).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Text)
		}
	})
}

func (s *SSEView) openDetail(event *logic.SSEEvent) {
	detail := logic.SSEEventDetail(s.body, *event)
	s.selected, s.open, s.detailTab, s.formatted = &detail, true, "data", formatSSEData(detail.Data)
}

// formatSSEData returns indented JSON for small valid JSON data, else "".
func formatSSEData(data string) string {
	if data == "" || len(data) >= logic.LargeTextThreshold {
		return ""
	}
	formatted := logic.FormatBody(data, logic.BodyJSON)
	if !json.Valid([]byte(data)) || formatted == "" {
		return ""
	}
	return formatted
}

func (s *SSEView) detailContent() string {
	if s.selected == nil {
		return ""
	}
	switch s.detailTab {
	case "raw":
		return s.selected.Raw
	case "formatted":
		return s.formatted
	default:
		return s.selected.Data
	}
}

// detail shows the open message as 1.0.5 did: its sequence and event type
// as badges and its ID, then the Data, Formatted and Raw Event views.
func (s *SSEView) detail(c *ui.Context, theme nui.Theme) {
	if s.selected == nil {
		s.open = false
	}
	widgets.DetailDialog(c.Key("sse-detail"), theme, &s.open, widgets.DetailDialogOptions{
		Title: locales.DetailSseMessageDetail(),
		Lead: func(c *ui.Context, palette nui.Theme) {
			nui.Badge(c, palette, "#"+strconv.Itoa(s.selected.Sequence), nui.BadgeOptions{Color: nui.Neutral}).Shrink(0)
			nui.Badge(c, palette, s.selected.EventType, nui.BadgeOptions{Color: nui.Info}).MinWidth(0)
			ui.Text(c, locales.DetailSseId()+": "+orDash(s.selected.LastEventID)).FontSize(apptheme.TextSize(c, 14)).MinWidth(0).TextColor(palette.Muted)
		},
		Header: func(c *ui.Context, palette nui.Theme) {
			items := []widgets.PillTab{{ID: "data", Label: locales.DetailSseData()}}
			if s.formatted != "" {
				items = append(items, widgets.PillTab{ID: "formatted", Label: locales.DetailFormatted()})
			}
			items = append(items, widgets.PillTab{ID: "raw", Label: locales.DetailSseRawEvent()})
			widgets.PillTabs(c.Key("sse-tabs"), palette, &s.detailTab, items, locales.DetailSseMessageDetail()).Shrink(0)
		},
		Actions: []widgets.DetailAction{
			s.wrap.Action(locales.DetailWrapBody()),
			{Key: "copy", Icon: lucide.Copy(), Label: locales.DetailSseCopyMessage(), Run: func() {
				text := s.detailContent()
				s.clipboard = &text
				s.deps.Notifier.Success(locales.DetailBodyCopied())
			}},
			{Key: "save", Icon: lucide.Download(), Label: locales.DetailSseSaveMessage(), Disabled: s.exporting, Run: s.saveMessage},
		},
		Wrap:          &s.wrap,
		SquareContent: true,
		Content: func(c *ui.Context, palette nui.Theme) {
			language := "plaintext"
			if s.detailTab == "formatted" {
				language = "json"
			}
			s.detailView.SetLanguage(language)
			if text := s.detailContent(); s.detailView.Text() != text {
				s.detailView.SetText(text)
			}
			s.detailView.SetWordWrap(s.wrap.On())
			s.detailView.BuildUnframed(c, s.deps.Settings.CodeFont())
		},
	})
	if !s.open {
		s.closeDetail()
	}
}

func (s *SSEView) saveMessage() {
	if s.selected == nil {
		return
	}
	name, contentType := "sse-message.txt", "text/plain"
	switch s.detailTab {
	case "raw":
		name = "sse-event.txt"
	case "formatted":
		name, contentType = "sse-message.json", "application/json"
	}
	saveToFile(s.deps, &s.exporting, name, proxyservice.SaveBodyToFileRequest{Body: s.detailContent(), ContentType: contentType})
}
