package app

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

func TestRunCreatesApplicationBeforeOpeningDatabase(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "app.go", nil, 0)
	if err != nil {
		t.Fatalf("parse app.go: %v", err)
	}

	var runBody *ast.BlockStmt
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "Run" {
			runBody = function.Body
			break
		}
	}
	if runBody == nil {
		t.Fatal("Run function not found")
	}

	bootstrapIndex := topLevelCallStatementIndex(runBody, "logger", "EnableBootstrapOutput")
	applicationNewIndex := topLevelCallStatementIndex(runBody, "application", "New")
	databaseOpenIndex := topLevelCallStatementIndex(runBody, "appdatabase", "Open")
	if bootstrapIndex < 0 {
		t.Fatal("logger.EnableBootstrapOutput top-level call not found in Run")
	}
	if applicationNewIndex < 0 {
		t.Fatal("application.New call not found in Run")
	}
	if databaseOpenIndex < 0 {
		t.Fatal("appdatabase.Open call not found in Run")
	}
	if bootstrapIndex > applicationNewIndex {
		t.Fatalf("bootstrap logger must be enabled before application.New: bootstrap=%d application.New=%d", bootstrapIndex, applicationNewIndex)
	}
	if applicationNewIndex > databaseOpenIndex {
		t.Fatalf("application.New must acquire the single-instance lock before appdatabase.Open: application.New=%d appdatabase.Open=%d", applicationNewIndex, databaseOpenIndex)
	}
}

func topLevelCallStatementIndex(body *ast.BlockStmt, packageName string, functionName string) int {
	for index, statement := range body.List {
		var expressions []ast.Expr
		switch value := statement.(type) {
		case *ast.AssignStmt:
			expressions = value.Rhs
		case *ast.ExprStmt:
			expressions = []ast.Expr{value.X}
		}
		for _, expression := range expressions {
			call, ok := expression.(*ast.CallExpr)
			if !ok {
				continue
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != functionName {
				continue
			}
			identifier, ok := selector.X.(*ast.Ident)
			if ok && identifier.Name == packageName {
				return index
			}
		}
	}
	return -1
}

func TestFallbackTrayLabels(t *testing.T) {
	tests := []struct {
		name     string
		language string
		want     trayLabels
	}{
		{
			name:     "english",
			language: "en",
			want: trayLabels{
				OpenMainWindow: "Open Main Window",
				Close:          "Close",
			},
		},
		{
			name:     "chinese default",
			language: "zh",
			want: trayLabels{
				OpenMainWindow: "打开主窗口",
				Close:          "关闭",
			},
		},
		{
			name:     "unknown falls back to chinese",
			language: "fr",
			want: trayLabels{
				OpenMainWindow: "打开主窗口",
				Close:          "关闭",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fallbackTrayLabels(tt.language)
			if got != tt.want {
				t.Fatalf("fallbackTrayLabels(%q) = %+v, want %+v", tt.language, got, tt.want)
			}
		})
	}
}

func TestParseTrayLabels(t *testing.T) {
	tests := []struct {
		name string
		data any
		want trayLabels
	}{
		{
			name: "map string any from unregistered event",
			data: map[string]any{
				"openMainWindow": "打开主窗口",
				"close":          "关闭",
			},
			want: trayLabels{OpenMainWindow: "打开主窗口", Close: "关闭"},
		},
		{
			name: "map string string",
			data: map[string]string{
				"openMainWindow": "Open Main Window",
				"close":          "Close",
			},
			want: trayLabels{OpenMainWindow: "Open Main Window", Close: "Close"},
		},
		{
			name: "raw json",
			data: json.RawMessage(`{"openMainWindow":"Open","close":"Quit"}`),
			want: trayLabels{OpenMainWindow: "Open", Close: "Quit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseTrayLabels(tt.data)
			if !ok {
				t.Fatalf("parseTrayLabels(%#v) returned false", tt.data)
			}
			if got != tt.want {
				t.Fatalf("parseTrayLabels(%#v) = %+v, want %+v", tt.data, got, tt.want)
			}
		})
	}
}

func TestParseTrayLabelsRejectsInvalidPayload(t *testing.T) {
	tests := []struct {
		name string
		data any
	}{
		{
			name: "missing close",
			data: map[string]any{"openMainWindow": "Open"},
		},
		{
			name: "blank open",
			data: map[string]any{"openMainWindow": " ", "close": "Close"},
		},
		{
			name: "wrong type",
			data: map[string]any{"openMainWindow": 1, "close": "Close"},
		},
		{
			name: "invalid json",
			data: "{",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := parseTrayLabels(tt.data); ok {
				t.Fatalf("parseTrayLabels(%#v) = %+v, true; want false", tt.data, got)
			}
		})
	}
}

func TestBoolFromEventData(t *testing.T) {
	tests := []struct {
		name string
		data any
		want bool
	}{
		{name: "bool true", data: true, want: true},
		{name: "bool false", data: false, want: false},
		{name: "object", data: map[string]any{"dirty": true}, want: true},
		{name: "json bool", data: json.RawMessage(`true`), want: true},
		{name: "json object", data: json.RawMessage(`{"dirty":true}`), want: true},
		{name: "string bool", data: "true", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := boolFromEventData(tt.data)
			if !ok {
				t.Fatalf("boolFromEventData(%#v) returned false", tt.data)
			}
			if got != tt.want {
				t.Fatalf("boolFromEventData(%#v) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func TestBoolFromEventDataRejectsInvalidPayload(t *testing.T) {
	tests := []struct {
		name string
		data any
	}{
		{name: "missing dirty", data: map[string]any{}},
		{name: "wrong dirty type", data: map[string]any{"dirty": "yes"}},
		{name: "invalid json", data: "{"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := boolFromEventData(tt.data); ok {
				t.Fatalf("boolFromEventData(%#v) = %v, true; want false", tt.data, got)
			}
		})
	}
}

func TestNormalizeMainWindowCloseBehavior(t *testing.T) {
	tests := []struct {
		name     string
		behavior settingservice.MainWindowCloseBehavior
		want     settingservice.MainWindowCloseBehavior
	}{
		{
			name:     "hide to tray",
			behavior: settingservice.MainWindowCloseBehaviorHideToTray,
			want:     settingservice.MainWindowCloseBehaviorHideToTray,
		},
		{
			name:     "quit",
			behavior: settingservice.MainWindowCloseBehaviorQuit,
			want:     settingservice.MainWindowCloseBehaviorQuit,
		},
		{
			name:     "unknown falls back to hide to tray",
			behavior: settingservice.MainWindowCloseBehavior("unknown"),
			want:     settingservice.MainWindowCloseBehaviorHideToTray,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeMainWindowCloseBehavior(tt.behavior); got != tt.want {
				t.Fatalf("normalizeMainWindowCloseBehavior(%q) = %q, want %q", tt.behavior, got, tt.want)
			}
		})
	}
}
