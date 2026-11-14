// Package collection builds the API Collections section and the dialog that
// saves request tabs into it.
package collection

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/store"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const (
	rowHeight       float32 = 32
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

	started      bool
	creating     bool
	folderName   string
	folderParent string
	multi        bool
	checked      map[string]bool
	scroll       ui.ScrollState
	drag         treeDrag
	renaming     renameDialog
	save         saveDialog
	openSequence int
	copying      bool
	menus        widgets.MenuStates
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

func (v *View) cancelCreate() { v.creating, v.folderName, v.folderParent = false, "", "" }

func (v *View) submitCreate() {
	name := strings.TrimSpace(v.folderName)
	if name == "" {
		v.Notifier.Warning(locales.ApiCollectionFolderNameRequired())
		return
	}
	v.Data.Collection.CreateFolder(v.folderParent, name, func(_ *store.CollectionFolder, err error) {
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
	if v.dragDisabled() || v.drag.source.version != collection.Version {
		v.drag.valid = false
		v.drag.active, v.drag.target, v.drag.root = false, "", false
	}
	rows := collection.Rows()
	ui.Column(c.Key("api-collection")).Fill().MinHeight(0).Children(func() {
		v.toolbar(c, theme, rows)
		ui.Box(c.Key("tree-area")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() { v.area(c, theme, rows) })
	})
	v.renaming.build(c, theme, v)
}

func (v *View) toolbar(c *ui.Context, theme nui.Theme, rows []store.CollectionRow) {
	collection := v.Data.Collection
	action := func(key string, icon *ui.SVG, label string, disabled bool, active bool, run func()) {
		variant, color := nui.Ghost, nui.Neutral
		if active {
			variant, color = nui.Soft, nui.Primary
		}
		button := nui.Button(c.Key(key), theme, "", nui.ButtonOptions{Variant: variant, Color: color, Size: nui.SM, Square: true, Leading: icon, AccessibleLabel: label, Disabled: disabled})
		if !disabled {
			button.OnClick(run)
		}
		nui.Tooltip(c, theme, button, label, nui.TooltipOptions{})
	}
	header := ui.Row(c.Key("toolbar")).FillWidth().Shrink(0).MinHeight(40).AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(8).Padding(0, 10, 0, 12).Background(theme.Elevated)
	header.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
		ui.Text(c, locales.ApiCollectionTitle()).FontSize(apptheme.TextSize(c, 14)).FontWeight(600).TextColor(theme.Muted).SingleLine().Shrink(1)
		ui.Row(c).Gap(6).Shrink(0).Children(func() {
			action("refresh", lucide.RefreshCw(), locales.ApiCollectionRefresh(), collection.Loading, false, v.refresh)
			action("new-folder", lucide.Plus(), locales.ApiCollectionNewFolder(), false, false, func() { v.beginCreate("") })
			label, icon := locales.ApiCollectionExpandAll(), lucide.ListChevronsUpDown()
			if collection.AllExpanded() {
				label, icon = locales.ApiCollectionCollapseAll(), lucide.ListChevronsDownUp()
			}
			action("expand", icon, label, collection.Loading || !collection.HasExpandableFolders(), false, collection.ToggleAllExpanded)
			multi := locales.ApiCollectionMultiSelect()
			if v.multi {
				multi = locales.ApiCollectionExitMultiSelect()
			}
			action("multi", lucide.ListChecks(), multi, !v.multi && collection.Empty(), v.multi, v.toggleMulti)
			roots := v.deleteRoots(rows)
			disabled := collection.Mutating || (v.multi && len(roots) == 0) || (!v.multi && collection.SelectedNodeID == "")
			action("delete", lucide.Trash(), locales.ApiCollectionDelete(), disabled, false, func() {
				if v.multi {
					v.askDelete(roots, true)
				} else {
					v.askDelete([]string{collection.SelectedNodeID}, false)
				}
			})
		})
	})
}

func (v *View) area(c *ui.Context, theme nui.Theme, rows []store.CollectionRow) {
	collection := v.Data.Collection
	status := func(text string) {
		ui.Column(c).Fill().Center().Padding(16).Children(func() {
			ui.Text(c, text).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
		})
	}
	switch {
	case collection.Loading:
		ui.Column(c).Fill().Center().Children(func() { ui.Spinner(c) })
	case collection.Error != "":
		status(collection.Error)
	case collection.Empty() && !v.creating:
		status(locales.ApiCollectionEmpty())
	default:
		v.tree(c, theme, rows)
	}
}

func (v *View) dragDisabled() bool {
	collection := v.Data.Collection
	return collection.Loading || collection.Mutating || v.creating || v.multi
}

func (v *View) tree(c *ui.Context, theme nui.Theme, rows []store.CollectionRow) {
	collection := v.Data.Collection
	wasActive := v.drag.active
	v.drag.active, v.drag.target, v.drag.root = false, "", false
	ui.Column(c.Key("tree-layout")).Fill().MinHeight(0).Children(func() {
		rootSlot := ui.Box(c.Key("root-slot")).FillWidth().Shrink(0).Height(0)
		ui.Scroll(c.Key("tree")).TrackScroll(&v.scroll).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				if v.creating && v.folderParent == "" {
					v.createRow(c, theme, 0)
				}
				for _, row := range rows {
					v.row(c, theme, row)
					if v.creating && row.ID == v.folderParent {
						v.createRow(c, theme, row.Depth+1)
					}
				}
			})
		})
		// Keep the target for the release build, when the native source is
		// no longer dragging but the drop still needs to be consumed. It has
		// its own space above the scroll viewport, leaving every row reachable.
		if (wasActive || v.drag.active) && v.validDrag(v.drag.source) && collection.CanMoveToRoot(v.drag.source.id) {
			rootSlot.Height(rootZoneHeight+4).Padding(0, 6, 4, 6).Children(func() { v.rootZone(c, theme) })
		}
	})
	v.menus.Sweep()
	if v.drag.target == "" || !v.drag.active {
		v.drag.hoverFolder = ""
	}
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

func (v *View) createRow(c *ui.Context, theme nui.Theme, depth int) {
	ui.Row(c.Key("create-folder")).FillWidth().Height(36).AlignItems(ui.Center).Gap(4).Padding(0, 6, 0, float32(depth)*14+6).Children(func() {
		ui.Icon(c, lucide.Folder()).Size(14, 14).Shrink(0).TextColor(theme.Muted)
		ui.Box(c.Key("name")).Grow(1).Basis(0).MinWidth(0).Children(func() {
			widgets.InlineInput(c, theme, &v.folderName, widgets.InlineInputOptions{Label: locales.ApiCollectionFolderNamePlaceholder(), Placeholder: locales.ApiCollectionFolderNamePlaceholder(), Size: nui.XS, OnSubmit: func(string) { v.submitCreate() }, OnCancel: v.cancelCreate}).FillWidth()
		})
		nui.Button(c.Key("create"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Primary, Size: nui.XS, Square: true, Leading: lucide.Check(), AccessibleLabel: locales.ApiCollectionCreate()}).OnClick(v.submitCreate)
		nui.Button(c.Key("cancel"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Square: true, Leading: lucide.X(), AccessibleLabel: locales.ApiCollectionCancel()}).OnClick(v.cancelCreate)
	})
}

// row uses MyGo's native drag preview and folder drop targets while retaining
// the tree's indentation, request method, child count and keyboard activation.
func (v *View) row(c *ui.Context, theme nui.Theme, row store.CollectionRow) {
	collection := v.Data.Collection
	selected := collection.SelectedNodeID == row.ID
	line := ui.ButtonBase(c.Key(row.ID)).Role(ui.RoleListItem).FillWidth().Height(rowHeight).Justify(ui.Start).Gap(6).Radius(6).Padding(0, 10, 0, float32(row.Depth)*14+10).FocusRing(true).AccessSelected(selected).Label(row.Name)
	line.Background(theme.Background)
	hovered := line.Hovered()
	target := false
	if !v.dragDisabled() {
		if v.dragSource(line, row.ID) {
			line.Opacity(0.4)
		}
		if row.Folder {
			if source, over := ui.DragOver[collectionDrag](line); over && v.canDrop(source, row.ID) {
				target = true
				v.drag.target = row.ID
				v.hoverFolder(c, row)
			}
			if source, dropped := ui.Drop[collectionDrag](line); dropped {
				v.drop(source, row.ID)
			}
		}
	}
	switch {
	case target:
		line.Background(theme.Primary.Alpha(0.1).Over(theme.Background)).Border(1, theme.Primary)
	case selected:
		line.Background(theme.Accented)
	case hovered:
		line.Background(theme.Elevated)
	}
	activate := func() {
		switch {
		case v.multi:
			v.toggleChecked(row.ID)
		case row.Folder:
			collection.SelectNode(row.ID)
			collection.ToggleExpanded(row.ID)
		default:
			v.open(row.ID)
		}
	}
	line.OnClick(activate)
	// The full name shows to the right of the row, for names the sidebar
	// cuts short.
	nui.Tooltip(c, theme, line, row.Name, nui.TooltipOptions{Placement: nui.Set(nui.RightCenter)})
	line.Children(func() {
		if v.multi {
			checked := v.checked[row.ID]
			nui.Checkbox(c.Key("check"), theme, &checked, nui.CheckboxOptions{Size: nui.XS}).Label(locales.ApiCollectionSelectItem(row.Name))
			if checked != v.checked[row.ID] {
				v.toggleChecked(row.ID)
			}
		}
		if !v.dragDisabled() {
			widgets.ReorderGrip(c.Key("grip"), theme, "drag "+row.Name, true)
		}
		ui.Icon(c, rowIcon(row)).Size(20, 20).Shrink(0).TextColor(theme.Muted)
		label := ui.Row(c.Key("label")).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(6)
		label.Children(func() {
			if !row.Folder {
				v.requestMethod(c, theme, row)
			}
			ui.Text(c, row.Name).SingleLine().Shrink(1).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).LineHeight(20.0 / 14).TextColor(theme.Text)
			if row.Folder {
				ui.Row(c.Key("count")).Shrink(0).MinHeight(20).MinWidth(20).PaddingX(6).Radius(10).Center().Background(theme.Elevated.Alpha(0.92)).
					Label("child-count").Children(func() {
					ui.Text(c, strconv.Itoa(row.Children)).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
				})
			}
		})
		if row.Folder && row.Children > 0 {
			chevron := lucide.ChevronDown()
			if row.Expanded {
				chevron = lucide.ChevronUp()
			}
			ui.Icon(c.Key("chevron"), chevron).Size(20, 20).Shrink(0).MarginStart(4).TextColor(theme.Muted)
		}
	})
	// As 1.0.5's, opening the menu selects its row.
	widgets.ContextMenu(c.Key("menu:"+row.ID), theme, line, v.menus.For(row.ID), func() []nui.MenuItem { return v.menuItems(row) }, nui.MenuOptions{
		Label: row.Name, OnOpenChange: func(open bool) {
			if open {
				v.Data.Collection.SelectNode(row.ID)
			}
		},
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

func (v *View) toggleChecked(id string) {
	if v.checked[id] {
		delete(v.checked, id)
		return
	}
	v.checked[id] = true
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

// dragSource lets MyGo own the threshold, preview, scrolling and cancellation.
// Freeze the tree version at pointer-down, including a press on the grip.
func (v *View) dragSource(line ui.Element, id string) bool {
	line.ObserveInput(func(event ui.InputEvent) {
		if event.Kind == ui.InputPointerDown && event.Button == 0 {
			v.drag = treeDrag{source: collectionDrag{owner: v, id: id, version: v.Data.Collection.Version}, valid: !v.dragDisabled()}
		}
		if event.Kind == ui.InputPointerUp && event.Canceled {
			v.drag.valid = false
		}
	})
	source := collectionDrag{owner: v, id: id, version: v.Data.Collection.Version}
	if v.drag.source.id == id {
		source = v.drag.source
	}
	line.Drag(source)
	dragging := line.Dragging()
	if dragging && v.validDrag(source) {
		v.drag.active = true
	}
	return dragging
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
			nui.Button(c.Key("cancel"), palette, locales.ApiCollectionCancel(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Disabled: d.busy}).OnClick(func() { d.open = false })
			nui.Button(c.Key("rename"), palette, locales.ApiCollectionRename(), nui.ButtonOptions{Color: nui.Primary, Disabled: strings.TrimSpace(d.name) == "" || d.busy, Loading: d.busy}).OnClick(func() { d.submit(v) })
		})
	}}, func(palette nui.Theme) {
		nui.Input(c.Key("name"), palette, &d.name, nui.InputOptions{Label: locales.ApiCollectionRenameNamePlaceholder(), Placeholder: locales.ApiCollectionRenameNamePlaceholder(), Disabled: d.busy, OnSubmit: func(string) { d.submit(v) }, Style: widgets.AutoFocus()}).FillWidth()
	})
}
