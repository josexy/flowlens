// Package collection builds the API Collections section and the dialog that
// saves request tabs into it.
package collection

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/store"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

const (
	rootZoneHeight  float32 = 32
	dragExpandDelay         = 600 * time.Millisecond
)

var methodColors = map[string]ui.Color{
	"GET": ui.Hex("#16a34a"), "POST": ui.Hex("#2563eb"), "PUT": ui.Hex("#d97706"), "DELETE": ui.Hex("#dc2626"),
	"PATCH": ui.Hex("#7c3aed"), "HEAD": ui.Hex("#0891b2"), "OPTIONS": ui.Hex("#4f46e5"), "WS": ui.Hex("#0891b2"),
}

// View is the API Collections sidebar.
type View struct {
	Data     *store.Store
	Notifier *widgets.Notifier
	Confirm  *widgets.ConfirmDialog

	started            bool
	creating           bool
	folderName         string
	folderParent       string
	createBusy         bool
	multi              bool
	checked            map[string]bool
	treeState          nui.TreeState
	treeVersion        uint64
	treeItems          map[string]*collectionTreeItem
	visibleItems       []*collectionTreeItem
	treeViewport       ui.Handle
	treeSelected       string
	treeSelectedParent string
	menu               widgets.PointMenu
	menuID             string
	drag               treeDrag
	renaming           renameDialog
	save               saveDialog
	openSequence       int
	copying            bool
}

type treeDrag struct {
	source       collectionDrag
	valid        bool
	active       bool
	target       string
	root         bool
	hoverFolder  string
	hoverStarted time.Time
}

// collectionDrag identifies a node in the tree snapshot at pointer-down.
type collectionDrag struct {
	owner   *View
	id      string
	version uint64
}

// New returns the view of a window.
func New(data *store.Store, notifier *widgets.Notifier, confirm *widgets.ConfirmDialog) *View {
	return &View{Data: data, Notifier: notifier, Confirm: confirm, checked: map[string]bool{}}
}

func (v *View) refresh() {
	v.Data.Collection.Load(func(err error) {
		if err != nil {
			v.Notifier.Error(locales.ApiCollectionLoadFailed(err.Error()))
		}
	})
}

func (v *View) beginCreate(parent string) {
	if v.createBusy || v.Data.Collection.Mutating {
		return
	}
	if v.multi {
		v.exitMulti()
	}
	collection := v.Data.Collection
	v.creating, v.folderParent, v.folderName = true, parent, ""
	if parent != "" && !collection.Expanded[parent] {
		collection.ToggleExpanded(parent)
	}
	collection.SelectNode(parent)
}

func (v *View) cancelCreate() {
	if !v.createBusy {
		v.creating, v.folderName, v.folderParent = false, "", ""
	}
}

func (v *View) submitCreate() {
	if !v.creating || v.createBusy || v.Data.Collection.Mutating {
		return
	}
	name := strings.TrimSpace(v.folderName)
	if name == "" {
		v.Notifier.Warning(locales.ApiCollectionFolderNameRequired())
		return
	}
	v.createBusy = true
	v.Data.Collection.CreateFolder(v.folderParent, name, func(_ *store.CollectionFolder, err error) {
		v.createBusy = false
		if err != nil {
			v.Notifier.Error(locales.ApiCollectionCreateFolderFailed(err.Error()))
			return
		}
		v.cancelCreate()
	})
}

func (v *View) exitMulti() {
	v.multi = false
	v.checked = map[string]bool{}
}

func (v *View) toggleMulti() {
	if v.multi {
		v.exitMulti()
		return
	}
	v.cancelCreate()
	v.multi = true
}

func (v *View) pruneChecked() {
	for id := range v.checked {
		if !v.Data.Collection.Exists(id) {
			delete(v.checked, id)
		}
	}
}

// deleteRoots drops checked nodes whose folder is also checked.
func (v *View) deleteRoots(rows []store.CollectionRow) []string {
	collection := v.Data.Collection
	var roots []string
	add := func(id string) {
		seen := map[string]bool{}
		for parent := collection.ParentOf(id); parent != "" && !seen[parent]; parent = collection.ParentOf(parent) {
			if v.checked[parent] {
				return
			}
			seen[parent] = true
		}
		roots = append(roots, id)
	}
	listed := map[string]bool{}
	for _, row := range rows {
		if v.checked[row.ID] {
			add(row.ID)
			listed[row.ID] = true
		}
	}
	for id := range v.checked {
		if !listed[id] && collection.Exists(id) {
			add(id)
		}
	}
	return roots
}

func (v *View) askDelete(ids []string, multi bool) {
	if len(ids) == 0 {
		return
	}
	collection := v.Data.Collection
	message := locales.ApiCollectionDeleteMultiConfirm(strconv.Itoa(len(ids)))
	if len(ids) == 1 {
		if collection.IsFolder(ids[0]) {
			message = locales.ApiCollectionDeleteFolderConfirm(collection.NodeName(ids[0]))
		} else {
			message = locales.ApiCollectionDeleteApiConfirm(collection.NodeName(ids[0]))
		}
	}
	v.Confirm.Ask(widgets.ConfirmRequest{
		Title: locales.ApiCollectionDeleteTitle(), Message: message, CancelLabel: locales.ApiCollectionCancel(),
		Confirm: widgets.ConfirmAction{Label: locales.ApiCollectionDelete(), Color: nui.Error, Run: func() {
			collection.DeleteNodes(ids, func(err error) {
				if err != nil {
					v.Notifier.Error(err.Error())
					return
				}
				if multi {
					v.checked = map[string]bool{}
				}
				v.Notifier.Success(locales.ApiCollectionDeleted())
			})
		}},
	})
}

func (v *View) duplicate(id string) {
	if v.copying {
		return
	}
	v.copying = true
	v.Data.Collection.Duplicate(id, func(err error) {
		v.copying = false
		if err != nil {
			v.Notifier.Error(locales.ApiCollectionCopyFailed(err.Error()))
			return
		}
		v.Notifier.Success(locales.ApiCollectionCopied())
	})
}

func (v *View) open(id string) {
	collection, workspace := v.Data.Collection, v.Data.Workspace
	v.openSequence++
	sequence := v.openSequence
	collection.SelectNode(id)
	if tab := workspace.FindTabByAPIID(id); tab != nil {
		workspace.Activate(tab.Key)
		return
	}
	collection.GetRequest(id, func(request *store.CollectionRequest, err error) {
		if sequence != v.openSequence {
			return
		}
		if err == nil {
			_, err = workspace.OpenSavedAPI(request)
		}
		if err != nil {
			v.Notifier.Error(locales.ApiCollectionOpenFailed(err.Error()))
		}
	})
}

// Sidebar builds the tree.
func (v *View) Sidebar(c *ui.Context, theme nui.Theme) {
	collection := v.Data.Collection
	if !v.started {
		v.started = true
		if !collection.Loaded && !collection.Loading {
			v.refresh()
		}
	}
	v.pruneChecked()
	if v.menuID != "" && !collection.Exists(v.menuID) {
		v.menuID, v.menu = "", widgets.PointMenu{}
	}
	if v.dragDisabled() || v.drag.source.version != collection.Version {
		v.drag.valid = false
		v.drag.active, v.drag.target, v.drag.root = false, "", false
	}
	rows := collection.Rows()
	ui.Column(c.Key("api-collection")).Fill().MinHeight(0).Children(func() {
		v.toolbar(c, theme, rows)
		ui.Box(c.Key("tree-area")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() { v.area(c, theme) })
	})
	v.createDialog(c, theme)
	v.renaming.build(c, theme, v)
}

func (v *View) toolbar(c *ui.Context, theme nui.Theme, rows []store.CollectionRow) {
	collection := v.Data.Collection
	action := func(key string, icon *ui.SVG, label string, unavailable, busy, active bool, run func()) {
		variant, color := nui.Ghost, nui.Neutral
		if active {
			variant, color = nui.Soft, nui.Primary
		}
		disabled := unavailable || busy
		button := nui.Button(c.Key(key), theme, "", nui.ButtonOptions{Variant: variant, Color: color, Size: nui.SM, Square: true, Leading: icon, AccessibleLabel: label, Disabled: disabled, Loading: key == "refresh" && collection.Loading}).DimDisabled(unavailable)
		if !disabled {
			button.OnClick(run)
		}
		nui.Tooltip(c, theme, button, label, nui.TooltipOptions{})
	}
	header := ui.Row(c.Key("toolbar")).FillWidth().Shrink(0).MinHeight(40).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Padding(0, 10, 0, 12).Background(theme.Elevated)
	header.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
		ui.Text(c, locales.ApiCollectionTitle()).FontSize(apptheme.TextSize(c, 14)).FontWeight(600).TextColor(theme.Muted).SingleLine().Shrink(1)
		ui.Row(c).Gap(6).Shrink(0).Children(func() {
			action("refresh", lucide.RefreshCw(), locales.ApiCollectionRefresh(), false, collection.Loading, false, v.refresh)
			action("new-folder", lucide.Plus(), locales.ApiCollectionNewFolder(), false, collection.Mutating, false, func() { v.beginCreate("") })
			label, icon := locales.ApiCollectionExpandAll(), lucide.ListChevronsUpDown()
			if collection.AllExpanded() {
				label, icon = locales.ApiCollectionCollapseAll(), lucide.ListChevronsDownUp()
			}
			action("expand", icon, label, !collection.HasExpandableFolders(), collection.Loading, false, collection.ToggleAllExpanded)
			multi := locales.ApiCollectionMultiSelect()
			if v.multi {
				multi = locales.ApiCollectionExitMultiSelect()
			}
			action("multi", lucide.ListChecks(), multi, !v.multi && collection.Empty(), false, v.multi, v.toggleMulti)
			roots := v.deleteRoots(rows)
			unavailable := (v.multi && len(roots) == 0) || (!v.multi && collection.SelectedNodeID == "")
			action("delete", lucide.Trash(), locales.ApiCollectionDelete(), unavailable, collection.Mutating, false, func() {
				if v.multi {
					v.askDelete(roots, true)
				} else {
					v.askDelete([]string{collection.SelectedNodeID}, false)
				}
			})
		})
	})
}

func (v *View) area(c *ui.Context, theme nui.Theme) {
	collection := v.Data.Collection
	status := func(text string) {
		ui.Column(c).Fill().Center().Padding(16).Children(func() {
			ui.Text(c, text).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
		})
	}
	ui.Column(c.Key("collection-content")).Fill().MinHeight(0).Children(func() {
		if collection.Loaded && collection.Error != "" {
			nui.Alert(c.Key("refresh-error"), theme, nui.AlertOptions{Description: collection.Error, Color: nui.Error, Variant: nui.Soft}).FillWidth().Shrink(0)
		}
		ui.Box(c.Key("tree-content")).FillWidth().Grow(1).Basis(0).MinHeight(0).Disabled(collection.Loading).DimDisabled(false).Children(func() {
			switch {
			case collection.Loading && !collection.Loaded:
				ui.Column(c).Fill().Center().Children(func() { widgets.Spinner(c, theme) })
			case collection.Error != "" && !collection.Loaded:
				status(collection.Error)
			case collection.Empty():
				status(locales.ApiCollectionEmpty())
			default:
				v.tree(c, theme)
			}
		})
	})
}

func (v *View) dragDisabled() bool {
	collection := v.Data.Collection
	return collection.Loading || collection.Mutating || v.creating || v.multi
}

func (v *View) rootZone(c *ui.Context, theme nui.Theme) {
	zone := ui.Row(c.Key("root-zone")).FillWidth().Height(rootZoneHeight).AlignItems(ui.Center).Gap(6).PaddingX(10).Label(locales.ApiCollectionMoveToRoot())
	zone.Radius(theme.Radius).Border(1, theme.BorderAccented).BorderStyle(ui.BorderDashed).Background(theme.Elevated)
	if source, over := ui.DragOver[collectionDrag](zone); over && v.canDrop(source, "") {
		v.drag.root = true
		zone.Background(theme.Accented).Border(1, theme.Primary)
	}
	if source, dropped := ui.Drop[collectionDrag](zone); dropped {
		v.drop(source, "")
	}
	zone.Children(func() {
		ui.Icon(c, lucide.FolderOpen()).Size(14, 14).TextColor(theme.Muted)
		ui.Text(c, locales.ApiCollectionMoveToRoot()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Text)
	})
}

func rowIcon(row store.CollectionRow) *ui.SVG {
	switch {
	case row.Folder && row.Expanded:
		return lucide.FolderOpen()
	case row.Folder:
		return lucide.Folder()
	case row.Request != nil && row.Request.WebSocket != nil:
		return lucide.Radio()
	default:
		return lucide.FileText()
	}
}

// requestMethod is a request's method, or WS, in 14 px semibold in its color.
func (v *View) requestMethod(c *ui.Context, theme nui.Theme, row store.CollectionRow) {
	method := "WS"
	if row.Request != nil && row.Request.WebSocket == nil {
		method = "GET"
		if row.Request.HTTP != nil && strings.TrimSpace(row.Request.HTTP.Method) != "" {
			method = strings.ToUpper(strings.TrimSpace(row.Request.HTTP.Method))
		}
	}
	color, ok := methodColors[method]
	if !ok {
		color = ui.Hex("#64748b")
	}
	ui.Text(c, method).Shrink(0).SingleLine().FontSize(apptheme.TextSize(c, 14)).FontWeight(600).LineHeight(1).TextColor(color)
}

// menuItems are the actions on a row.
func (v *View) menuItems(row store.CollectionRow) []nui.MenuItem {
	var items []nui.MenuItem
	if row.Folder {
		items = append(items,
			nui.MenuItem{ID: "new-child-folder", Label: locales.ApiCollectionNewChildFolder(), OnSelect: func() { v.beginCreate(row.ID) }},
			widgets.MenuSeparator("folder-separator"))
	}
	items = append(items, nui.MenuItem{ID: "rename", Label: locales.ApiCollectionRename(), OnSelect: func() { v.renaming.show(row.ID, row.Name) }})
	if !row.Folder {
		items = append(items, nui.MenuItem{ID: "copy", Label: locales.ApiCollectionCopy(), Disabled: v.copying, OnSelect: func() { v.duplicate(row.ID) }})
	}
	if row.Folder && v.Data.Collection.CanMoveToRoot(row.ID) {
		items = append(items, nui.MenuItem{ID: "move-to-root", Label: locales.ApiCollectionMoveToRoot(), OnSelect: func() { v.move(row.ID, "") }})
	}
	return append(items, widgets.MenuSeparator("delete-separator"),
		nui.MenuItem{ID: "delete", Label: locales.ApiCollectionDelete(), Color: nui.Error, OnSelect: func() { v.askDelete([]string{row.ID}, false) }})
}

func (v *View) move(id, parent string) {
	collection := v.Data.Collection
	if collection.Mutating {
		return
	}
	collection.Move(id, parent, func(err error) {
		if err != nil {
			v.Notifier.Error(locales.ApiCollectionMoveFailed(err.Error()))
		}
	})
}

func (v *View) validDrag(source collectionDrag) bool {
	return v.drag.valid && source.owner == v && source == v.drag.source &&
		!v.dragDisabled() && source.version == v.Data.Collection.Version && v.Data.Collection.Exists(source.id)
}

func (v *View) canDrop(source collectionDrag, parent string) bool {
	if !v.validDrag(source) {
		return false
	}
	if parent == "" {
		return v.Data.Collection.CanMoveToRoot(source.id)
	}
	return v.Data.Collection.CanMove(source.id, parent)
}

func (v *View) drop(source collectionDrag, parent string) {
	if !v.canDrop(source, parent) {
		return
	}
	v.drag.valid = false
	v.move(source.id, parent)
}

// hoverFolder opens a collapsed drop target without changing the tree snapshot.
func (v *View) hoverFolder(c *ui.Context, row store.CollectionRow) {
	if row.Expanded || !row.HasChildren {
		v.drag.hoverFolder = ""
		return
	}
	if v.drag.hoverFolder != row.ID {
		v.drag.hoverFolder, v.drag.hoverStarted = row.ID, c.Now()
	}
	if elapsed := c.Now().Sub(v.drag.hoverStarted); elapsed >= dragExpandDelay {
		v.Data.Collection.ToggleExpanded(row.ID)
		v.drag.hoverFolder = ""
	} else {
		c.After(dragExpandDelay - elapsed)
	}
}

type renameDialog struct {
	open bool
	id   string
	name string
	busy bool
}

func (d *renameDialog) show(id, name string) { d.open, d.id, d.name = true, id, name }

func (d *renameDialog) submit(v *View) {
	name := strings.TrimSpace(d.name)
	if d.busy || d.id == "" {
		return
	}
	if name == "" {
		v.Notifier.Warning(locales.ApiCollectionRenameNameRequired())
		return
	}
	d.busy = true
	v.Data.Collection.Rename(d.id, name, func(err error) {
		d.busy = false
		if err != nil {
			v.Notifier.Error(locales.ApiCollectionRenameFailed(err.Error()))
			return
		}
		d.open, d.id, d.name = false, "", ""
		v.Notifier.Success(locales.ApiCollectionRenamed())
	})
}

func (d *renameDialog) build(c *ui.Context, theme nui.Theme, v *View) {
	if !d.open {
		return
	}
	nui.ModalWithOptions(c.Key("rename"), theme, &d.open, nui.ModalOptions{Title: locales.ApiCollectionRenameTitle(), Width: nui.Set[float32](420), Dismissible: nui.Set(!d.busy), Footer: func(c *ui.Context, palette nui.Theme) {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			nui.Button(c.Key("cancel"), palette, locales.ApiCollectionCancel(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Disabled: d.busy}).DimDisabled(false).OnClick(func() { d.open = false })
			nui.Button(c.Key("rename"), palette, locales.ApiCollectionRename(), nui.ButtonOptions{Color: nui.Primary, Leading: lucide.Pencil(), Disabled: strings.TrimSpace(d.name) == "" || d.busy, Loading: d.busy}).DimDisabled(strings.TrimSpace(d.name) == "").OnClick(func() { d.submit(v) })
		})
	}}, func(palette nui.Theme) {
		nui.Input(c.Key("name"), palette, &d.name, nui.InputOptions{Label: locales.ApiCollectionRenameNamePlaceholder(), Placeholder: locales.ApiCollectionRenameNamePlaceholder(), Disabled: d.busy, OnSubmit: func(string) { d.submit(v) }, Style: widgets.AutoFocus()}).DimDisabled(false).FillWidth()
	})
}
