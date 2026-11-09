package collection

import (
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
)

type saveDialog struct {
	open   bool
	key    string
	name   string
	parent string
	saving bool
	direct map[string]bool
}

// defaultName suggests a name for a tab.
func defaultName(tab *logic.WorkspaceTab) string {
	if title := strings.TrimSpace(tab.Title); title != "" {
		return title
	}
	raw := ""
	if tab.HTTP != nil {
		raw = tab.HTTP.URL
	} else if tab.WebSocket != nil {
		raw = tab.WebSocket.URL
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return locales.WorkspaceTabBarUntitled()
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return parsed.Hostname() + parsed.Path
	}
	return parsed.Hostname()
}

// SaveTab saves a request tab: linked tabs update their entry, and others ask
// where to go.
func (v *View) SaveTab(key string) {
	workspace, collection := v.Data.Workspace, v.Data.Collection
	tab := workspace.FindTab(key)
	if tab == nil || (tab.HTTP == nil && tab.WebSocket == nil) {
		return
	}
	if tab.APIID != "" {
		if v.save.direct == nil {
			v.save.direct = map[string]bool{}
		}
		if v.save.direct[key] {
			return
		}
		v.save.direct[key] = true
		workspace.SaveExistingTab(key, func(_ *store.CollectionRequest, err error) {
			delete(v.save.direct, key)
			if err != nil {
				v.Notifier.Error(locales.ApiCollectionSaveFailed(err.Error()))
				return
			}
			collection.Load(nil)
			v.Notifier.Success(locales.ApiCollectionSaved())
		})
		return
	}
	collection.EnsureLoaded(func(err error) {
		if err != nil {
			v.Notifier.Error(locales.ApiCollectionLoadFailed(err.Error()))
			return
		}
		if len(collection.FolderOptions()) == 0 {
			v.Notifier.Warning(locales.ApiCollectionCreateFolderFirst())
			return
		}
		v.save.open, v.save.key, v.save.name, v.save.parent = true, key, defaultName(tab), collection.LastSelectedFolderID
	})
}

func (v *View) submitSave() {
	d := &v.save
	name := strings.TrimSpace(d.name)
	if name == "" || d.parent == "" || d.saving {
		return
	}
	workspace, collection := v.Data.Workspace, v.Data.Collection
	d.saving = true
	parent := d.parent
	workspace.SaveTabAsAPI(d.key, parent, name, func(request *store.CollectionRequest, err error) {
		d.saving = false
		if err != nil {
			v.Notifier.Error(locales.ApiCollectionSaveFailed(err.Error()))
			return
		}
		d.open = false
		id := request.ID
		collection.Load(func(error) { collection.SelectNode(id) })
		v.Notifier.Success(locales.ApiCollectionSaved())
	})
}

// Dialogs builds the save dialog.
func (v *View) Dialogs(c *ui.Context, theme nui.Theme) {
	d := &v.save
	if !d.open {
		return
	}
	nui.ModalWithOptions(c.Key("save-api"), theme, &d.open, nui.ModalOptions{Title: locales.ApiCollectionSaveModalTitle(), Width: nui.Set[float32](460), Dismissible: nui.Set(!d.saving), Footer: func(c *ui.Context, palette nui.Theme) {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			nui.Button(c.Key("cancel"), palette, locales.ApiCollectionCancel(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Disabled: d.saving}).OnClick(func() { d.open = false })
			nui.Button(c.Key("save"), palette, locales.ApiCollectionSave(), nui.ButtonOptions{Color: nui.Primary, Disabled: strings.TrimSpace(d.name) == "" || d.parent == "" || d.saving, Loading: d.saving}).OnClick(v.submitSave)
		})
	}}, func(palette nui.Theme) {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			nui.FormField(c.Key("name"), palette, nui.FormFieldOptions{Label: locales.ApiCollectionRequestName()}, func(field nui.Theme) {
				nui.Input(c, field, &d.name, nui.InputOptions{Label: locales.ApiCollectionRequestName(), Placeholder: locales.ApiCollectionRequestNamePlaceholder(), Disabled: d.saving, OnSubmit: func(string) { v.submitSave() }}).FillWidth()
			})
			nui.FormField(c.Key("folder"), palette, nui.FormFieldOptions{Label: locales.ApiCollectionDestinationFolder()}, func(field nui.Theme) {
				options := v.Data.Collection.FolderOptions()
				items := make([]nui.Option[string], len(options))
				for index, option := range options {
					items[index] = nui.Option[string]{Value: option.ID, Label: option.Path}
				}
				nui.Select(c, field, &d.parent, items, nui.SelectOptions{Label: locales.ApiCollectionDestinationFolder(), Placeholder: locales.ApiCollectionDestinationFolder(), Disabled: d.saving})
			})
		})
	})
}
