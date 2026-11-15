package collection

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

func (v *View) createDialog(c *ui.Context, theme nui.Theme) {
	if !v.creating {
		return
	}
	title := locales.ApiCollectionNewFolder()
	if v.folderParent != "" {
		title = locales.ApiCollectionNewChildFolder()
	}
	nui.ModalWithOptions(c.Key("create-folder"), theme, &v.creating, nui.ModalOptions{
		Title: title, Width: nui.Set[float32](420), Dismissible: nui.Set(!v.createBusy),
		Close: &nui.ButtonOptions{Disabled: v.createBusy}, OnClose: v.cancelCreate,
		Footer: func(c *ui.Context, palette nui.Theme) {
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				nui.Button(c.Key("cancel"), palette, locales.ApiCollectionCancel(), nui.ButtonOptions{
					Variant: nui.Outline, Color: nui.Neutral, Disabled: v.createBusy,
				}).OnClick(v.cancelCreate)
				nui.Button(c.Key("create"), palette, locales.ApiCollectionCreate(), nui.ButtonOptions{
					Color: nui.Primary, Disabled: strings.TrimSpace(v.folderName) == "" || v.createBusy || v.Data.Collection.Mutating,
					Loading: v.createBusy,
				}).OnClick(v.submitCreate)
			})
		},
	}, func(palette nui.Theme) {
		nui.Input(c.Key("name"), palette, &v.folderName, nui.InputOptions{
			Label: locales.ApiCollectionFolderNamePlaceholder(), Placeholder: locales.ApiCollectionFolderNamePlaceholder(),
			Disabled: v.createBusy, OnSubmit: func(string) { v.submitCreate() }, Style: widgets.AutoFocus(),
		}).FillWidth()
	})
}
