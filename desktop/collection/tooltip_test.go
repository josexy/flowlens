package collection

import (
	"testing"

	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/locales"
)

func TestToolbarActionsShowTooltips(t *testing.T) {
	f := newFixture(t)
	folder := f.folder("", "Folder")
	f.request(folder, "Req", "http://example.com/")
	f.reload()
	for _, label := range []string{locales.ApiCollectionRefresh(), locales.ApiCollectionNewFolder(), locales.ApiCollectionExpandAll(), locales.ApiCollectionMultiSelect()} {
		rendertest.RequireTooltip(t, f.tester, label, label)
	}
}
