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
	wrap      widgets.DetailWrap
	exporting bool
	clipboard *string
	hex       *HexView
	showHex   bool
}

// NewMessageStream returns an empty stream.
func NewMessageStream(deps Dependencies) *MessageStream {
	stream := &MessageStream{deps: deps, Filter: DirectionAll, view: widgets.NewCodeView("plaintext", true), hex: NewHexView(deps)}
	stream.hex.SelectionFilename = "websocket-selection.bin"
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
			ui.Box(c).Grow(1)
			iconButton(c, theme, "filter", lucide.ArrowUpDown(), m.filterLabel(), false, m.cycleFilter)
			if m.OnClear != nil {
				iconButton(c, theme, "clear", lucide.Trash(), locales.WorkspaceWebsocketClientClearMessages(), len(m.source) == 0, m.OnClear)
			}
			label, icon := locales.WorkspaceWebsocketClientViewModeList(), lucide.List()
			if m.Conversation {
				label, icon = locales.WorkspaceWebsocketClientViewModeConversation(), lucide.MessagesSquare()
			}
			iconButton(c, theme, "mode", icon, label, false, func() { m.Conversation = !m.Conversation })
		})
		if truncated {
			warningAlert(c, theme, locales.TrafficWsMsgsTruncated())
		}
		if len(m.filtered) == 0 {
			emptyState(c, theme, lucide.MessagesSquare(), locales.WorkspaceWebsocketClientNoMessages())
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
		ui.Text(c, m.preview(message)).Grow(1).Basis(0).MinWidth(0).SingleLine().FontSize(14).TextColor(theme.Text)
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
		bubble.Children(func() { ui.Text(c, m.preview(message)).FontSize(14).TextColor(theme.Text) })
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

// detail shows the open message as 1.0.5 did: its direction, type and size,
// then its text, or a hex view of a binary message.
func (m *MessageStream) detail(c *ui.Context, theme nui.Theme) {
	if m.active == nil {
		m.open = false
	}
	active := m.active
	binary := active != nil && active.MsgType == "binary"
	saving := m.exporting || m.hex.exporting
	var actions []widgets.DetailAction
	if !binary {
		actions = append(actions, m.wrap.Action(locales.DetailWrapBody()), widgets.DetailAction{Key: "copy", Icon: lucide.Copy(), Label: locales.DetailCopyBody(), Run: func() {
			text := active.Data
			m.clipboard = &text
			m.deps.Notifier.Success(locales.DetailBodyCopied())
		}})
	}
	actions = append(actions, widgets.DetailAction{Key: "save", Icon: lucide.Download(), Label: locales.DetailSaveBody(), Disabled: saving, Run: func() { m.save(active) }})
	options := widgets.DetailDialogOptions{
		Title:   m.detailTitle(),
		Actions: actions,
		Wrap:    &m.wrap,
		Header: func(c *ui.Context, palette nui.Theme) {
			direction, color := locales.WorkspaceWebsocketClientFilterReceive(), nui.Info
			if active.Direction == DirectionSend {
				direction, color = locales.WorkspaceWebsocketClientFilterSend(), nui.Warning
			}
			nui.Badge(c, palette, direction, nui.BadgeOptions{Color: color, Size: nui.LG}).Shrink(0)
			kind := locales.WorkspaceWebsocketClientDraftTypeText()
			if binary {
				kind = locales.WorkspaceWebsocketClientDraftTypeBinaryFile()
			}
			nui.Badge(c, palette, kind, nui.BadgeOptions{Color: nui.Neutral, Size: nui.LG}).Shrink(0)
			ui.Text(c, locales.WorkspaceWebsocketClientBinarySize(strconv.Itoa(active.DataSize))).FontSize(14).MinWidth(0).TextColor(palette.Muted)
		},
		Content: func(c *ui.Context, palette nui.Theme) {
			if binary {
				m.hex.Build(c, palette, 1, active.Data, "base64")
				return
			}
			if m.view.Text() != active.Data {
				m.view.SetText(active.Data)
			}
			m.view.SetWordWrap(m.wrap.On())
			m.view.Build(c, ui.Font{Family: palette.Font, Size: 14}).Fill()
		},
	}
	if active != nil && active.Error != "" {
		// Native messages carry their send error, which 1.0.5's had not.
		options.Lead = func(c *ui.Context, palette nui.Theme) {
			nui.Alert(c, palette, nui.AlertOptions{Description: active.Error, Color: nui.Warning, Variant: nui.Soft, Icon: lucide.TriangleAlert()}).FillWidth()
		}
	}
	widgets.DetailDialog(c.Key("ws-detail"), theme, &m.open, options)
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
