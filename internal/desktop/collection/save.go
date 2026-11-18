package collection

import (
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// folderTreeHeight is the height of the destination folder tree, in DIPs.
const folderTreeHeight float32 = 220

type saveDialog struct {
	open   bool
	key    string
	name   string
	parent string
	saving bool
	direct map[string]bool
	// folders picks the destination. foldersVersion is the collection version
	// its nodes show; zero rebuilds them and reveals parent.
	folders        nui.TreeState
	foldersVersion uint64
	paths          map[string]string
	reveal         bool
}

// syncFolders mirrors the collection's folders into the destination tree. A
// newly opened dialog expands only the path to its folder.
func (d *saveDialog) syncFolders(collection *store.CollectionStore) {
	if d.foldersVersion == collection.Version {
		return
	}
	opened := d.foldersVersion == 0
	d.foldersVersion = collection.Version
	options := collection.FolderOptions()
	children := map[string][]store.CollectionFolderOption{}
	d.paths = make(map[string]string, len(options))
	for _, option := range options {
		children[option.ParentID] = append(children[option.ParentID], option)
		d.paths[option.ID] = option.Path
	}
	var nodes func(string) []nui.TreeNode
	nodes = func(parent string) []nui.TreeNode {
		result := make([]nui.TreeNode, 0, len(children[parent]))
		for _, option := range children[parent] {
			node := nui.TreeNode{ID: option.ID, Label: option.Name}
			if len(children[option.ID]) > 0 {
				// Tree switches between the closed and open folder icons.
				node.Children = nodes(option.ID)
			} else {
				node.Icon = lucide.Folder()
			}
			result = append(result, node)
		}
		return result
	}
	// SetNodes drops the selection and expansion of folders that are gone.
	d.folders.SetNodes(nodes(""))
	if _, ok := d.paths[d.parent]; !ok {
		d.parent = ""
	}
	if !opened {
		return
	}
	d.folders.Outline.Open.Clear()
	d.folders.Selection.Clear()
	d.folders.Active = d.parent
	if d.parent == "" {
		return
	}
	d.folders.Selection.Add(d.parent)
	for parent := collection.ParentOf(d.parent); parent != ""; parent = collection.ParentOf(parent) {
		d.folders.SetExpanded(parent, true)
	}
	d.reveal = true
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
		v.save.foldersVersion = 0
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
	d.syncFolders(v.Data.Collection)
	nui.ModalWithOptions(c.Key("save-api"), theme, &d.open, nui.ModalOptions{Title: locales.ApiCollectionSaveModalTitle(), Width: nui.Set[float32](460), Dismissible: nui.Set(!d.saving), Footer: func(c *ui.Context, palette nui.Theme) {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			nui.Button(c.Key("cancel"), palette, locales.ApiCollectionCancel(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Disabled: d.saving}).DimDisabled(false).OnClick(func() { d.open = false })
			invalid := strings.TrimSpace(d.name) == "" || d.parent == ""
			nui.Button(c.Key("save"), palette, locales.ApiCollectionSave(), nui.ButtonOptions{Color: nui.Primary, Leading: lucide.Save(), Disabled: invalid || d.saving, Loading: d.saving}).DimDisabled(invalid).OnClick(v.submitSave)
		})
	}}, func(palette nui.Theme) {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			nui.FormField(c.Key("name"), palette, nui.FormFieldOptions{Label: locales.ApiCollectionRequestName()}, func(field nui.Theme) {
				nui.Input(c, field, &d.name, nui.InputOptions{Label: locales.ApiCollectionRequestName(), Placeholder: locales.ApiCollectionRequestNamePlaceholder(), Disabled: d.saving, OnSubmit: func(string) { v.submitSave() }, Style: widgets.AutoFocus()}).DimDisabled(false).FillWidth()
			})
			nui.FormField(c.Key("folder"), palette, nui.FormFieldOptions{Label: locales.ApiCollectionDestinationFolder(), Help: d.paths[d.parent]}, func(field nui.Theme) {
				ui.Box(c.Key("folders")).FillWidth().Padding(4).Radius(field.Radius).Border(1, field.Border).Clip().Children(func() {
					nui.Tree(c.Key("tree"), field, &d.folders, nui.TreeOptions{
						Label: locales.ApiCollectionDestinationFolder(), Height: folderTreeHeight, Disabled: d.saving,
						SelectionBehavior: nui.SelectionReplace,
						OnSelect: func(event *nui.TreeEvent) {
							// A destination stays chosen, even on Ctrl-click.
							if event.Selected {
								event.PreventDefault()
								return
							}
							d.parent = event.ID
						},
					}).FillWidth().DimDisabled(false)
				})
				if d.reveal {
					d.reveal = false
					for row := range d.folders.Rows() {
						if d.folders.Item(row) == d.parent {
							d.folders.Outline.List.ScrollIntoView(row)
							break
						}
					}
				}
			})
		})
	})
}
