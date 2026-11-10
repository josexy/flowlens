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
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

// asyncParseThreshold is how much new text is parsed off the main thread.
const asyncParseThreshold = 256 * 1024

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

	list ui.ListState

	open       bool
	selected   *logic.SSEMessageDetail
	detailTab  string
	formatted  string
	detailView *widgets.CodeView
	exporting  bool
	clipboard  *string
}

// NewSSEView returns an empty view.
func NewSSEView(deps Dependencies) *SSEView {
	view := &SSEView{deps: deps, parser: &logic.SSEParser{}, detailTab: "data", detailView: widgets.NewCodeView("plaintext", true)}
	view.list.FollowEnd = true
	return view
}

func (s *SSEView) reset() {
	s.parser, s.events, s.parsed, s.parsing = &logic.SSEParser{}, nil, 0, false
	s.sequence++
	s.closeDetail()
	s.list = ui.ListState{FollowEnd: true}
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
	if content.Encoding == "base64" {
		emptyState(c, theme, lucide.Binary(), locales.DetailSseUnavailable())
		return
	}
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(8).PaddingX(10).PaddingY(4).Children(func() {
			ui.Text(c, locales.DetailSseMessageCount(strconv.Itoa(len(s.events)))).FontSize(12).TextColor(theme.Muted)
			if s.parsing {
				ui.Text(c, locales.DetailSseParsing()).FontSize(12).TextColor(theme.Muted)
			}
			ui.Box(c).Grow(1)
			if !s.atLatest() {
				nui.Button(c.Key("latest"), theme, locales.DetailSseJumpToLatest(), nui.ButtonOptions{Variant: nui.Soft, Size: nui.XS, Leading: lucide.ArrowDownToLine()}).
					OnClick(s.list.ScrollToEnd)
			}
		})
		if s.parser.HasPendingEvent() {
			warningAlert(c, theme, locales.DetailSseIncomplete())
		}
		if len(s.events) == 0 {
			emptyState(c, theme, lucide.Radio(), locales.DetailSseEmpty())
			return
		}
		ui.List(c.Key("sse-list"), &s.list, len(s.events), func(row int) { s.row(c, theme, row) }).Grow(1).Basis(0).MinHeight(0).FillWidth()
	})
	s.detail(c, theme)
}

func (s *SSEView) atLatest() bool { return len(s.events) == 0 || s.list.AtEnd() }

func (s *SSEView) row(c *ui.Context, theme nui.Theme, index int) {
	event := s.events[index]
	row := ui.Row(c.Key(event.Sequence)).FillWidth().Gap(8).PaddingX(10).PaddingY(6).AlignItems(ui.Center).Label(locales.DetailSseOpenMessage(strconv.Itoa(event.Sequence)))
	row.OnClick(func() { s.openDetail(event) })
	row.Children(func() {
		ui.Text(c, strconv.Itoa(event.Sequence)).Width(48).Shrink(0).FontSize(12).TextColor(theme.Muted)
		ui.Text(c, event.EventType).Width(120).Shrink(0).FontSize(13).SingleLine().TextColor(theme.Primary)
		ui.Text(c, orDash(event.LastEventID)).Width(100).Shrink(0).FontSize(13).SingleLine().TextColor(theme.Toned)
		ui.Text(c, event.Preview).Grow(1).Basis(0).MinWidth(0).FontSize(13).SingleLine().TextColor(theme.Text)
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

func (s *SSEView) detail(c *ui.Context, theme nui.Theme) {
	if !s.open || s.selected == nil {
		return
	}
	nui.ModalWithOptions(c.Key("sse-detail"), theme, &s.open, nui.ModalOptions{Title: locales.DetailSseMessageDetail(), Width: nui.Set[float32](720), Footer: func(c *ui.Context, palette nui.Theme) {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			nui.Button(c.Key("sse-copy"), palette, locales.DetailSseCopyMessage(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral}).OnClick(func() {
				text := s.detailContent()
				s.clipboard = &text
				s.deps.Notifier.Success(locales.DetailBodyCopied())
			})
			nui.Button(c.Key("sse-save"), palette, locales.DetailSseSaveMessage(), nui.ButtonOptions{Disabled: s.exporting}).OnClick(s.saveMessage)
		})
	}}, func(palette nui.Theme) {
		detailRow(c, palette, "event", locales.DetailSseEventType(), s.selected.EventType)
		detailRow(c, palette, "id", locales.DetailSseId(), orDash(s.selected.LastEventID))
		items := []nui.TabItem{{ID: "data", Label: locales.DetailSseData()}}
		if s.formatted != "" {
			items = append(items, nui.TabItem{ID: "formatted", Label: locales.DetailFormatted()})
		}
		items = append(items, nui.TabItem{ID: "raw", Label: locales.DetailSseRawEvent()})
		nui.Tabs(c.Key("sse-tabs"), palette, &s.detailTab, items, nui.TabsOptions{Label: locales.DetailSseMessageDetail(), Size: nui.XS, Content: nui.Set(false)})
		text := s.detailContent()
		if text == "" {
			ui.Text(c, locales.DetailSseEmptyData()).FontSize(14).TextColor(palette.Muted)
			return
		}
		if s.detailView.Text() != text {
			s.detailView.SetText(text)
		}
		ui.Box(c).FillWidth().Height(320).Children(func() {
			s.detailView.Build(c, ui.Font{Family: palette.MonoFont, Size: 13}).Fill()
		})
	})
	if !s.open {
		s.closeDetail()
	}
}

func (s *SSEView) saveMessage() {
	if s.selected == nil {
		return
	}
	name, contentType := fmt.Sprintf("sse-message-%d.txt", s.selected.Sequence), "text/plain"
	if s.detailTab == "formatted" {
		name, contentType = fmt.Sprintf("sse-message-%d.json", s.selected.Sequence), "application/json"
	}
	saveToFile(s.deps, &s.exporting, name, proxyservice.SaveBodyToFileRequest{Body: s.detailContent(), ContentType: contentType})
}
