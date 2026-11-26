package inspect

import (
	"testing"

	"github.com/josexy/flowlens/locales"
)

func TestLoadingHistoryDetailRemainsClosable(t *testing.T) {
	for _, dark := range []bool{false, true} {
		f := newFixture(t, nil, nil)
		f.setDark(dark)
		f.data.loading = true
		closed := false
		f.panel.OnClose = func() { closed = true }
		f.settle()
		f.click(locales.DetailClosePanel())
		if !closed {
			t.Fatal("loading detail lost its close action")
		}
	}
}
