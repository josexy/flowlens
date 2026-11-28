package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/internal/rendertest"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/locales"
)

func TestEditorContentEdgesStayAligned(t *testing.T) {
	assertTop := func(t *testing.T, f *fixture, want float32, role ui.Role, right bool, border float32) float32 {
		t.Helper()
		for _, node := range f.tester.AccessAll() {
			if node.Role != role || (node.Bounds.X > 700) != right || role == ui.RoleTextField && !node.Multiline {
				continue
			}
			top := node.Bounds.Y - border
			if want != 0 && top != want {
				t.Fatalf("%v %q starts at %v, want %v", role, node.Name, top, want)
			}
			return top
		}
		t.Fatalf("no %v on right=%v in %v", role, right, f.tester.Texts())
		return 0
	}
	t.Run("HTTP", func(t *testing.T) {
		f := newFake(t)
		state := f.newHTTP().HTTP
		state.Headers = []logic.EditableRow{{Enabled: true, Key: "Cookie", Value: "sample=one"}}
		state.Response = &logic.HTTPResponseView{
			StatusCode: 200, StatusText: "OK", Protocol: "HTTP/2.0", Body: `{"result":"ok"}`,
			HeadersTruncated: true,
			Headers:          []logic.EditableRow{{Enabled: true, Key: "Content-Type", Value: "application/json"}},
		}
		state.ScriptConsoleEntries = []logic.PluginLogEntry{consoleEntry(1, "sample", "stdout", "info", "output")}
		state.ActiveRequestTab, state.ActiveResponseTab = logic.RequestTabCookies, logic.ResponseTabBody
		f.settle()
		top := assertTop(t, f, 0, ui.RoleList, false, 0)
		assertTop(t, f, top, ui.RoleTextField, true, 1)
		rendertest.ShootThemes(t, f.tester, "http-cookie-and-response-body", f.setDark)
		state.ActiveRequestTab, state.ActiveResponseTab = logic.RequestTabBody, logic.ResponseTabHeaders
		state.RequestBodyType, state.RequestBodyText = logic.BodyTypeJSON, `{"request":true}`
		f.settle()
		assertTop(t, f, top, ui.RoleTextField, false, 1)
		assertTop(t, f, top, ui.RoleList, true, 0)
		state.ActiveResponseTab = logic.ResponseTabConsole
		for _, bodyType := range []string{logic.BodyTypeFormData, logic.BodyTypeURLEncoded} {
			state.RequestBodyType = bodyType
			f.settle()
			assertTop(t, f, top, ui.RoleList, false, 0)
			assertTop(t, f, top, ui.RoleTable, true, 0)
		}
		state.ActiveRequestTab = logic.RequestTabScript
		f.settle()
		assertTop(t, f, top, ui.RoleTextField, false, 1)
	})
	t.Run("WebSocket", func(t *testing.T) {
		f := newFake(t)
		key := f.data.Workspace.CreateWebSocketClientTab(logic.DraftNew, nil)
		state := f.data.Workspace.FindTab(key).WebSocket
		state.ResponseHeaders = []logic.EditableRow{{Enabled: true, Key: "Set-Cookie", Value: "sample=one"}}
		state.Messages = []logic.WebSocketDisplayMessage{{ID: "1", Direction: "receive", MsgType: "text", Data: "hello"}}
		state.ActiveLeftTab, state.ActiveRightTab = socketTabMessage, socketTabResponse
		f.settle()
		top := assertTop(t, f, 0, ui.RoleList, true, 0)
		assertTop(t, f, top, ui.RoleTextField, false, 1)
		rendertest.ShootThemes(t, f.tester, "websocket-draft-and-messages", f.setDark)
		state.ActiveLeftTab, state.ActiveRightTab = socketTabHeaders, socketTabResponseCookies
		f.settle()
		assertTop(t, f, top, ui.RoleList, false, 0)
		assertTop(t, f, top, ui.RoleList, true, 0)
	})
}

func TestWebSocketSendStaysInsideDraftAfterRebuildAndResize(t *testing.T) {
	f := newFake(t)
	f.data.Workspace.CreateWebSocketClientTab(logic.DraftNew, nil)
	for _, width := range []int{1400, 1000, 1400} {
		f.tester.SetSize(width, 800)
		for range 12 {
			f.settle()
		}
		button, ok := f.tester.Find(locales.WorkspaceWebsocketClientSendMessage())
		if !ok || button.X+button.W > float32(width)/2-24 || button.Y < 700 || button.Y+button.H > 790 {
			t.Fatalf("width %d: send button outside padded draft: %+v", width, button)
		}
	}
}

func TestCustomProxySettingsPlacement(t *testing.T) {
	for _, editor := range []string{"HTTP", "WebSocket"} {
		t.Run(editor, func(t *testing.T) {
			f := newFake(t)
			if editor == "HTTP" {
				f.newHTTP().HTTP.ActiveRequestTab = logic.RequestTabSettings
			} else {
				key := f.data.Workspace.CreateWebSocketClientTab(logic.DraftNew, nil)
				f.data.Workspace.FindTab(key).WebSocket.ActiveLeftTab = socketTabSettings
			}
			f.settle()

			label := locales.WorkspaceHttpRequestCustomProxy()
			if f.tester.HasText(label) {
				t.Fatal("custom proxy field is visible with no proxy selected")
			}
			f.click(locales.WorkspaceHttpRequestProxyModeNone())
			f.click(locales.WorkspaceHttpRequestProxyModeCustom())
			field, ok := f.tester.AccessByRole(ui.RoleTextField, label)
			if !ok {
				t.Fatal("custom proxy field is missing")
			}
			_, modeY := f.center(locales.WorkspaceHttpRequestProxyMode())
			_, tlsY := f.center(locales.WorkspaceHttpRequestSkipVerifyTls())
			if fieldY := field.Bounds.Y + field.Bounds.H/2; !(modeY < fieldY && fieldY < tlsY) {
				t.Fatalf("custom proxy field at %v must be between proxy mode at %v and TLS at %v", fieldY, modeY, tlsY)
			}
			rendertest.ShootThemes(t, f.tester, editor+"-proxy-settings", f.setDark)

			f.click(locales.WorkspaceHttpRequestProxyModeCustom())
			f.click(locales.WorkspaceHttpRequestProxyModeNone())
			if f.tester.HasText(label) {
				t.Fatal("custom proxy field remained visible after switching away")
			}
		})
	}
}
