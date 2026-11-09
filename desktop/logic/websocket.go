package logic

import (
	"strings"
	"time"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

// WebSocketDisplayMessage is a message as message lists show it.
type WebSocketDisplayMessage struct {
	ID        string
	Direction string
	MsgType   string
	Data      string
	DataSize  int
	CreatedAt time.Time
	Error     string
}

// NormalizeWebSocketDirection maps a captured direction to send or receive.
func NormalizeWebSocketDirection(direction string) string {
	if strings.EqualFold(direction, "receive") {
		return "receive"
	}
	return "send"
}

// NormalizeWebSocketMsgType maps a captured type to text or binary.
func NormalizeWebSocketMsgType(msgType string) string {
	if msgType == "binary" {
		return "binary"
	}
	return "text"
}

// ToWebSocketDisplayMessage converts a captured message.
func ToWebSocketDisplayMessage(item *proxyservice.WebSocketMessage, id string, createdAt time.Time) WebSocketDisplayMessage {
	message := WebSocketDisplayMessage{
		ID:        id,
		Direction: NormalizeWebSocketDirection(item.Direction),
		MsgType:   NormalizeWebSocketMsgType(item.MsgType),
		Data:      item.Data,
		DataSize:  item.DataSize,
		CreatedAt: createdAt,
	}
	if item.Error != nil {
		message.Error = item.Error.Error
	}
	return message
}
