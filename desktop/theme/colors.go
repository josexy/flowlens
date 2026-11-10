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
