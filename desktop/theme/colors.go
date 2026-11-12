package theme

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// URLColors color the parts of a URL shown with syntax highlighting.
type URLColors struct {
	Host, Path, QueryPunct, QueryKey, QueryValue, Hash ui.Color
}

var (
	lightURLColors = URLColors{
		Host: ui.Hex("#16a34a"), Path: ui.Hex("#21a2df"), QueryPunct: ui.Hex("#cc3333"),
		QueryKey: ui.Hex("#7c3aed"), QueryValue: ui.Hex("#f59e0b"), Hash: ui.Hex("#ec4899"),
	}
	darkURLColors = URLColors{
		Host: ui.Hex("#4ade80"), Path: ui.Hex("#38bdf8"), QueryPunct: ui.Hex("#f87171"),
		QueryKey: ui.Hex("#a78bfa"), QueryValue: ui.Hex("#fcd34d"), Hash: ui.Hex("#f9a8d4"),
	}
)

// URLPalette returns the URL highlighting colors of a light or dark theme.
func URLPalette(dark bool) URLColors {
	if dark {
		return darkURLColors
	}
	return lightURLColors
}

// WebSocketColors color a WebSocket message by its direction, as 1.0.5 did:
// the icon in Send or Receive, the direction tag's text in SendTag or
// ReceiveTag on a 12% tint of the icon's color, and a conversation bubble in
// SendBubble or ReceiveBubble, the same in both themes.
type WebSocketColors struct {
	Send, SendTag, Receive, ReceiveTag ui.Color
	SendBubble, ReceiveBubble          ui.Color
}

var (
	lightWebSocketColors = WebSocketColors{
		Send: ui.Hex("#d97706"), SendTag: ui.Hex("#b45309"), Receive: ui.Hex("#2563eb"), ReceiveTag: ui.Hex("#1d4ed8"),
		SendBubble: ui.Hex("#f59e0b").Alpha(0.08), ReceiveBubble: ui.Hex("#2563eb").Alpha(0.08),
	}
	darkWebSocketColors = WebSocketColors{
		Send: ui.Hex("#fbbf24"), SendTag: ui.Hex("#f59e0b"), Receive: ui.Hex("#60a5fa"), ReceiveTag: ui.Hex("#3b82f6"),
		SendBubble: ui.Hex("#f59e0b").Alpha(0.08), ReceiveBubble: ui.Hex("#2563eb").Alpha(0.08),
	}
)

// WebSocketPalette returns the WebSocket direction colors of a light or dark
// theme.
func WebSocketPalette(dark bool) WebSocketColors {
	if dark {
		return darkWebSocketColors
	}
	return lightWebSocketColors
}

// methodColors give each common HTTP method its own color.
var methodColors = map[string]string{
	"GET": "#16a34a", "POST": "#2563eb", "PUT": "#d97706", "DELETE": "#dc2626",
	"PATCH": "#7c3aed", "HEAD": "#0891b2", "OPTIONS": "#4f46e5", "CONNECT": "#9333ea",
}

// MethodColor returns the color of an HTTP method, slate for the others.
func MethodColor(method string) ui.Color {
	if hex, ok := methodColors[strings.ToUpper(strings.TrimSpace(method))]; ok {
		return ui.Hex(hex)
	}
	return ui.Hex("#475569")
}
