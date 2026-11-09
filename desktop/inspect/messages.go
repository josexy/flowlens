package inspect

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

// Direction filters of a message stream.
const (
	DirectionAll     = "all"
	DirectionSend    = "send"
	DirectionReceive = "receive"
)

// MessageStream lists WebSocket messages.
type MessageStream struct {
	// OnClear, when set, offers clearing the messages.
	OnClear func()
	deps    Dependencies

	Filter       string
	Conversation bool

	source   []logic.WebSocketDisplayMessage
	filtered []logic.WebSocketDisplayMessage

	list ui.ListState

	open      bool
	active    *logic.WebSocketDisplayMessage
	view      *widgets.CodeView
	exporting bool
	clipboard *string
	hex       *HexView
	showHex   bool
}

// NewMessageStream returns an empty stream.
func NewMessageStream(deps Dependencies) *MessageStream {
	stream := &MessageStream{deps: deps, Filter: DirectionAll, view: widgets.NewCodeView("plaintext", true), hex: NewHexView(deps)}
	stream.list.FollowEnd = true
	return stream
}

// SetMessages replaces the messages shown.
func (m *MessageStream) SetMessages(messages []logic.WebSocketDisplayMessage) {
	m.source = messages
	m.applyFilter()
}

func (m *MessageStream) applyFilter() {
	if m.Filter == DirectionAll {
		m.filtered = m.source
		return
	}
	m.filtered = m.filtered[:0:0]
	for _, message := range m.source {
		if message.Direction == m.Filter {
			m.filtered = append(m.filtered, message)
		}
	}
}

func (m *MessageStream) cycleFilter() {
	switch m.Filter {
	case DirectionAll:
		m.Filter = DirectionSend
	case DirectionSend:
		m.Filter = DirectionReceive
	default:
		m.Filter = DirectionAll
	}
	m.applyFilter()
}

func (m *MessageStream) filterLabel() string {
	switch m.Filter {
	case DirectionSend:
		return locales.WorkspaceWebsocketClientFilterSend()
	case DirectionReceive:
		return locales.WorkspaceWebsocketClientFilterReceive()
	default:
		return locales.WorkspaceWebsocketClientFilterAll()
	}
}

// Build shows the stream. truncated tells that older messages were dropped.
func (m *MessageStream) Build(c *ui.Context, theme nui.Theme, truncated bool) {
	if m.clipboard != nil {
		c.WriteClipboard(*m.clipboard)
		m.clipboard = nil
	}
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(8).PaddingX(10).PaddingY(4).Children(func() {
			nui.Button(c.Key("filter"), theme, m.filterLabel(), nui.ButtonOptions{Variant: nui.Soft, Color: nui.Neutral, Size: nui.XS, Leading: lucide.Funnel()}).OnClick(m.cycleFilter)
			label := locales.WorkspaceWebsocketClientViewModeList()
			if m.Conversation {
				label = locales.WorkspaceWebsocketClientViewModeConversation()
			}
			if m.OnClear != nil {
				nui.Button(c.Key("clear"), theme, locales.WorkspaceWebsocketClientClearMessages(), nui.ButtonOptions{Variant: nui.Soft, Color: nui.Neutral, Size: nui.XS, Leading: lucide.Trash(), Disabled: len(m.source) == 0}).OnClick(m.OnClear)
			}
			nui.Button(c.Key("mode"), theme, label, nui.ButtonOptions{Variant: nui.Soft, Color: nui.Neutral, Size: nui.XS, Leading: lucide.MessagesSquare()}).OnClick(func() { m.Conversation = !m.Conversation })
		})
		if truncated {
			warningAlert(c, theme, locales.TrafficWsMsgsTruncated())
		}
		if len(m.filtered) == 0 {
			emptyState(c, theme, lucide.MessagesSquare(), locales.CommonNoContent())
			return
		}
		ui.List(c.Key("messages"), &m.list, len(m.filtered), func(index int) {
			if m.Conversation {
				m.bubble(c, theme, index)
				return
			}
			m.row(c, theme, index)
		}).Grow(1).Basis(0).MinHeight(0).FillWidth()
	})
	m.detail(c, theme)
}

func directionIcon(direction string) *ui.SVG {
	if direction == DirectionReceive {
		return lucide.ArrowDown()
	}
	return lucide.ArrowUp()
}

func (m *MessageStream) open_(message logic.WebSocketDisplayMessage) {
	active := message
	m.active, m.open, m.showHex = &active, true, false
}

func (m *MessageStream) preview(message logic.WebSocketDisplayMessage) string {
	if message.MsgType == "binary" {
		return locales.WorkspaceWebsocketClientBinarySize(strconv.Itoa(message.DataSize))
	}
	return message.Data
}

func (m *MessageStream) row(c *ui.Context, theme nui.Theme, index int) {
	message := m.filtered[index]
	color := theme.Success
	if message.Direction == DirectionSend {
		color = theme.Primary
	}
	row := ui.Row(c.Key(message.ID)).FillWidth().Height(44).Gap(8).PaddingX(10).AlignItems(ui.Center)
	row.OnClick(func() { m.open_(message) })
	row.Children(func() {
		ui.Icon(c, directionIcon(message.Direction)).Size(14, 14).Shrink(0).TextColor(color)
		ui.Text(c, m.preview(message)).Grow(1).Basis(0).MinWidth(0).SingleLine().FontSize(13).TextColor(theme.Text)
		ui.Text(c, logic.FormatFileSize(int64(message.DataSize))).Shrink(0).FontSize(12).TextColor(theme.Muted)
	})
}

func (m *MessageStream) bubble(c *ui.Context, theme nui.Theme, index int) {
	message := m.filtered[index]
	send := message.Direction == DirectionSend
	ui.Row(c.Key(message.ID)).FillWidth().PaddingX(10).PaddingY(4).Children(func() {
		if send {
			ui.Box(c).Grow(1)
		}
		background := theme.Accented
		if send {
			background = theme.Primary.Alpha(0.14)
		}
		bubble := ui.Box(c).MaxWidth(520).Padding(8).Radius(10).Background(background)
		bubble.OnClick(func() { m.open_(message) })
		bubble.Children(func() { ui.Text(c, m.preview(message)).FontSize(13).TextColor(theme.Text) })
	})
}

func (m *MessageStream) detailTitle() string {
	if m.active == nil {
		return locales.WorkspaceWebsocketClientMessageDetail()
	}
	if m.active.Direction == DirectionSend {
		return locales.WorkspaceWebsocketClientMessageDetailSend()
	}
	return locales.WorkspaceWebsocketClientMessageDetailReceive()
}

func (m *MessageStream) detail(c *ui.Context, theme nui.Theme) {
	if !m.open || m.active == nil {
		return
	}
	active := m.active
	binary := active.MsgType == "binary"
	nui.ModalWithOptions(c.Key("ws-detail"), theme, &m.open, nui.ModalOptions{Title: m.detailTitle(), Width: nui.Set[float32](720), Footer: func(c *ui.Context, palette nui.Theme) {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			if !binary {
				nui.Button(c.Key("ws-copy"), palette, locales.DetailCopyBody(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral}).OnClick(func() {
					text := active.Data
					m.clipboard = &text
					m.deps.Notifier.Success(locales.DetailBodyCopied())
				})
			}
			nui.Button(c.Key("ws-save"), palette, locales.DetailSaveBody(), nui.ButtonOptions{Disabled: m.exporting}).OnClick(func() { m.save(active) })
		})
	}}, func(palette nui.Theme) {
		if active.Error != "" {
			warningAlert(c, palette, active.Error)
		}
		if binary {
			ui.Text(c, locales.WorkspaceWebsocketClientBinarySize(strconv.Itoa(active.DataSize))).FontSize(13).TextColor(palette.Muted)
			ui.Box(c).FillWidth().Height(320).Children(func() { m.hex.Build(c, palette, 1, active.Data, "base64") })
			return
		}
		if m.view.Text() != active.Data {
			m.view.SetText(active.Data)
		}
		ui.Box(c).FillWidth().Height(320).Children(func() {
			m.view.Build(c, ui.Font{Family: palette.MonoFont, Size: 13}).Fill()
		})
	})
	if !m.open {
		m.active = nil
	}
}

func (m *MessageStream) save(message *logic.WebSocketDisplayMessage) {
	name, encoding, contentType := "websocket-message.txt", "", "text/plain"
	if message.MsgType == "binary" {
		name, encoding, contentType = "websocket-message.bin", "base64", "application/octet-stream"
	}
	saveToFile(m.deps, &m.exporting, name, proxyservice.SaveBodyToFileRequest{Body: message.Data, BodyEncoding: encoding, ContentType: contentType})
}

func (p *Panel) webSocketPane(c *ui.Context, theme nui.Theme) {
	body := p.data.BodyView()
	if body == nil {
		if p.data.BodyLoading() {
			ui.Column(c).Fill().Center().Children(func() { ui.Spinner(c).Size(24, 24).TextColor(theme.Muted) })
			return
		}
		emptyState(c, theme, lucide.MessagesSquare(), locales.CommonNoContent())
		return
	}
	messages := make([]logic.WebSocketDisplayMessage, 0, len(body.WebSocketMessages))
	for index, item := range body.WebSocketMessages {
		if item != nil {
			messages = append(messages, logic.ToWebSocketDisplayMessage(item, fmt.Sprintf("detail-ws-msg:%d:%d", p.entryID, index), time.Time{}))
		}
	}
	p.messages.SetMessages(messages)
	p.messages.Build(c, theme, body.WsMsgsTruncated)
}
