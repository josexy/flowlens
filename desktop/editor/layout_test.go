package editor

import (
	"testing"

	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/locales"
)

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
