package editor

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/locales"
)

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
