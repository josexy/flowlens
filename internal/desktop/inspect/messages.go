package inspect

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	desktoptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
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

	filter   string
	source   []logic.WebSocketDisplayMessage
	filtered []logic.WebSocketDisplayMessage

	list ui.ListState
	// listHeight is the list's height in the last frame: rows that leave
	// room under them draw a line under the last one too.
	listHeight float32

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
	stream := &MessageStream{deps: deps, filter: DirectionAll, view: widgets.NewCodeView("plaintext", true), hex: NewHexView(deps)}
	stream.hex.SelectionFilename = "websocket-selection.bin"
	stream.hex.Exporting = &stream.exporting
	stream.list.FollowEnd = true
	return stream
}

// SetMessages replaces the messages shown.
func (m *MessageStream) SetMessages(messages []logic.WebSocketDisplayMessage) {
	m.source = messages
	m.applyFilter()
	if m.active != nil && !slices.ContainsFunc(messages, func(message logic.WebSocketDisplayMessage) bool { return message == *m.active }) {
		m.closeDetail()
	}
}

func (m *MessageStream) closeDetail() {
	m.open, m.active, m.showHex = false, nil, false
	if m.view.Text() != "" {
		m.view = widgets.NewCodeView("plaintext", true)
	}
	if m.hex.source != nil {
		m.hex = NewHexView(m.deps)
		m.hex.SelectionFilename = "websocket-selection.bin"
		m.hex.Exporting = &m.exporting
	}
}

func (m *MessageStream) applyFilter() {
	if m.filter == DirectionAll {
		m.filtered = m.source
		return
	}
	m.filtered = m.filtered[:0:0]
	for _, message := range m.source {
		if message.Direction == m.filter {
			m.filtered = append(m.filtered, message)
		}
	}
}

func (m *MessageStream) cycleFilter() {
	switch m.filter {
	case DirectionAll:
		m.filter = DirectionSend
	case DirectionSend:
		m.filter = DirectionReceive
	default:
		m.filter = DirectionAll
	}
	m.applyFilter()
}

func (m *MessageStream) filterButton(c *ui.Context, theme nui.Theme) {
	icon, label := lucide.ArrowUpDown(), locales.WorkspaceWebsocketClientFilterAll()
	switch m.filter {
	case DirectionSend:
		icon, label = lucide.ArrowUp(), locales.WorkspaceWebsocketClientFilterSend()
	case DirectionReceive:
		icon, label = lucide.ArrowDown(), locales.WorkspaceWebsocketClientFilterReceive()
	}
	iconButton(c, theme, "filter", icon, label, false, m.cycleFilter)
}

// Build shows the stream. truncated tells that older messages were dropped.
func (m *MessageStream) Build(c *ui.Context, theme nui.Theme, truncated bool) {
	if m.clipboard != nil {
		c.WriteClipboard(*m.clipboard)
		m.clipboard = nil
	}
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		widgets.PaneToolbar(c, nil, func() {
			m.filterButton(c, theme)
			if m.OnClear != nil {
				iconButton(c, theme, "clear", lucide.Trash(), locales.WorkspaceWebsocketClientClearMessages(), len(m.source) == 0, m.OnClear)
			}
		})
		widgets.PaneContent(c, func() {
			if len(m.filtered) == 0 {
				emptyState(c, theme, lucide.MessagesSquare(), locales.WorkspaceWebsocketClientNoMessages())
				return
			}
			list := ui.List(c.Key("messages"), &m.list, len(m.filtered), func(index int) {
				m.row(c, theme, index)
			}).Fill().MinHeight(0).Border(1, theme.Border)
			m.listHeight = list.Bounds().H
		})
		if truncated {
			widgets.PaneWarning(c, theme, locales.TrafficWsMsgsTruncated())
		}
	})
	m.detail(c, theme)
}

// directionTag is a message's Send or Receive tag, in 1.0.5's direction
// colors.
func directionTag(c *ui.Context, theme nui.Theme, direction string) {
	colors := desktoptheme.WebSocketPalette(theme.Dark)
	label, tint, text := locales.WorkspaceWebsocketClientFilterReceive(), colors.Receive, colors.ReceiveTag
	if direction == DirectionSend {
		label, tint, text = locales.WorkspaceWebsocketClientFilterSend(), colors.Send, colors.SendTag
	}
	ui.Row(c).Shrink(0).MinHeight(20).AlignItems(ui.Center).PaddingX(8).Radius(999).Background(tint.Alpha(0.12)).Children(func() {
		ui.Text(c, label).SingleLine().FontSize(desktoptheme.TextSize(c, 12)).FontWeight(700).TextColor(text)
	})
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

// row is a message of the list as 1.0.5 drew it: its direction's arrow and
// tag, then its text or its size, with a line under it but the last when the
// rows fill the list.
func (m *MessageStream) row(c *ui.Context, theme nui.Theme, index int) {
	message := m.filtered[index]
	colors := desktoptheme.WebSocketPalette(theme.Dark)
	color := colors.Receive
	if message.Direction == DirectionSend {
		color = colors.Send
	}
	row := ui.Row(c.Key(message.ID)).FillWidth().Height(44).Gap(10).PaddingX(12).AlignItems(ui.Center)
	if index < len(m.filtered)-1 || float32(len(m.filtered)*44) < m.listHeight {
		row.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border)
	}
	if row.Hovered() {
		row.Background(theme.Elevated)
	}
	row.OnClick(func() { m.open_(message) })
	row.Children(func() {
		ui.Row(c).Width(28).Shrink(0).Justify(ui.Center).Children(func() {
			ui.Icon(c, directionIcon(message.Direction)).Size(16, 16).TextColor(color)
		})
		ui.Row(c).Width(88).Shrink(0).Children(func() { directionTag(c, theme, message.Direction) })
		ui.Text(c, m.preview(message)).Grow(1).Basis(0).MinWidth(0).SingleLine().FontSize(desktoptheme.TextSize(c, 14)).TextColor(theme.Text)
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
	saving := m.exporting
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
			ui.Text(c, locales.WorkspaceWebsocketClientBinarySize(strconv.Itoa(active.DataSize))).FontSize(desktoptheme.TextSize(c, 14)).MinWidth(0).TextColor(palette.Muted)
		},
		Content: func(c *ui.Context, palette nui.Theme) {
			if binary {
				m.hex.Build(c, palette, 1, active.Data, true)
				return
			}
			if m.view.Text() != active.Data {
				m.view.SetText(active.Data)
			}
			m.view.SetWordWrap(m.wrap.On())
			m.view.BuildUnframed(c, ui.Font{Family: palette.Font, Size: desktoptheme.TextSize(c, 14)})
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
		m.closeDetail()
	}
}

func (m *MessageStream) save(message *logic.WebSocketDisplayMessage) {
	name, contentType := "websocket-message.txt", "text/plain"
	if message.MsgType == "binary" {
		name, contentType = "websocket-message.bin", "application/octet-stream"
	}
	saveToFile(m.deps, &m.exporting, name, proxyservice.SaveBodyToFileRequest{Body: message.Data, ContentType: contentType})
}

// webSocketPane lists the entry's messages. Without a body view, once it is
// not loading, the stream shows its toolbar and "No messages", as in 1.0.5.
func (p *Panel) webSocketPane(c *ui.Context, theme nui.Theme) {
	body := p.data.BodyView()
	if body == nil && p.data.BodyLoading() {
		ui.Column(c).Fill().Center().Children(func() { widgets.Spinner(c, theme).Size(24, 24).TextColor(theme.Muted) })
		return
	}
	var messages []logic.WebSocketDisplayMessage
	truncated := false
	if body != nil {
		messages = make([]logic.WebSocketDisplayMessage, 0, len(body.WebSocketMessages))
		for index, item := range body.WebSocketMessages {
			if item != nil {
				messages = append(messages, logic.ToWebSocketDisplayMessage(item, fmt.Sprintf("detail-ws-msg:%d:%d", p.entryID, index), time.Time{}))
			}
		}
		truncated = body.WsMsgsTruncated
	}
	p.messages.SetMessages(messages)
	p.messages.Build(c, theme, truncated)
}
