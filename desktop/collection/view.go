// Package collection builds the API Collections section and the dialog that
// saves request tabs into it.
package collection

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const (
	rowHeight        float32 = 32
	rootZoneHeight   float32 = 32
	dragThreshold    float32 = 4
	dragExpandDelay          = 600 * time.Millisecond
	autoScrollEdge   float32 = 32
	autoScrollStep   float32 = 12
	autoScrollPeriod         = 16 * time.Millisecond
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
	viewport     float32
	drag         treeDrag
	suppress     string
	renaming     renameDialog
	save         saveDialog
	openSequence int
	copying      bool
}

type treeDrag struct {
	id           string
	index        int
	dy           float32
	startScroll  float32
	active       bool
	target       string
	root         bool
	hoverFolder  string
	hoverStarted time.Time
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
		ui.Text(c, locales.ApiCollectionTitle()).FontSize(14).FontWeight(600).TextColor(theme.Muted).SingleLine().Shrink(1)
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
			ui.Text(c, text).FontSize(14).TextColor(theme.Muted)
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
	area := ui.Scroll(c.Key("tree")).TrackScroll(&v.scroll).Fill().MinHeight(0)
	if bounds := area.Bounds(); bounds.H > 0 {
		v.viewport = bounds.H
	}
	area.Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			if v.creating && v.folderParent == "" {
				v.createRow(c, theme, 0)
			}
			for index, row := range rows {
				v.row(c, theme, index, row, rows)
				if v.creating && row.ID == v.folderParent {
					v.createRow(c, theme, row.Depth+1)
				}
			}
		})
	})
	if v.drag.active {
		v.continueDrag(c, rows)
		if collection.CanMoveToRoot(v.drag.id) {
			v.rootZone(c, theme, area)
		}
	}
}

func (v *View) rootZone(c *ui.Context, theme nui.Theme, area ui.Element) {
	ui.Overlay(c, func() {
		bounds := area.Bounds()
		zone := ui.Row(c.Key("root-zone")).Absolute().Left(bounds.X + 6).Top(bounds.Y).Width(bounds.W - 12).Height(rootZoneHeight).AlignItems(ui.Center).Gap(6).PaddingX(10).IgnoreInput(true)
		zone.Radius(theme.Radius).Border(1, theme.BorderAccented).BorderStyle(ui.BorderDashed).Background(theme.Elevated)
		if v.drag.root {
			zone.Background(theme.Accented).Border(1, theme.Primary)
		}
		zone.Children(func() {
			ui.Icon(c, lucide.FolderOpen()).Size(14, 14).TextColor(theme.Muted)
			ui.Text(c, locales.ApiCollectionMoveToRoot()).FontSize(14).TextColor(theme.Text)
		})
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

// row shows a tree row as 1.0.5 did: 32 px and rounded, with a grip shown
// on hover that alone drags the row, a 20 px icon, a request's method, the
// name with a tooltip of it, a folder's child count, and an end-aligned
// chevron that turns when the folder opens.
func (v *View) row(c *ui.Context, theme nui.Theme, index int, row store.CollectionRow, rows []store.CollectionRow) {
	collection := v.Data.Collection
	selected := collection.SelectedNodeID == row.ID
	line := ui.Row(c.Key(row.ID)).FillWidth().Height(rowHeight).AlignItems(ui.Center).Gap(6).Radius(6).Padding(0, 10, 0, float32(row.Depth)*14+10).Label(row.Name)
	hovered := line.Hovered()
	switch {
	case v.drag.active && v.drag.target == row.ID:
		line.Background(theme.Primary.Alpha(0.1)).Border(1, theme.Primary)
	case selected:
		line.Background(theme.Accented)
	case hovered:
		line.Background(theme.Elevated)
	}
	if v.drag.active && v.drag.id == row.ID {
		line.Opacity(0.5)
	}
	line.OnClick(func() {
		if v.suppress == row.ID {
			v.suppress = ""
			return
		}
		switch {
		case v.multi:
			v.toggleChecked(row.ID)
		case row.Folder:
			collection.SelectNode(row.ID)
			collection.ToggleExpanded(row.ID)
		default:
			v.open(row.ID)
		}
	})
	line.ContextMenu(func(menu *ui.Menu) { v.menu(menu, row) })
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
			v.grip(c, theme, index, row, hovered)
		}
		ui.Icon(c, rowIcon(row)).Size(20, 20).Shrink(0).TextColor(theme.Muted)
		label := ui.Row(c.Key("label")).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(6)
		label.Children(func() {
			if !row.Folder {
				v.requestMethod(c, theme, row)
			}
			ui.Text(c, row.Name).SingleLine().Shrink(1).MinWidth(0).FontSize(14).LineHeight(20.0 / 14).TextColor(theme.Text)
			if row.Folder {
				ui.Row(c.Key("count")).Shrink(0).Height(20).MinWidth(20).PaddingX(6).Radius(10).Center().Background(theme.Elevated.Alpha(0.92)).
					Label("child-count").Children(func() {
					ui.Text(c, strconv.Itoa(row.Children)).FontSize(14).TextColor(theme.Muted)
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

// grip is the handle that drags its row, shown while the row is hovered or
// dragged, as 1.0.5's.
func (v *View) grip(c *ui.Context, theme nui.Theme, index int, row store.CollectionRow, hovered bool) {
	dragging := v.drag.active && v.drag.id == row.ID
	// Hidden from assistive technology, as 1.0.5's; a click on it does not
	// reach the row.
	handle := ui.Box(c.Key("grip")).Size(14, 14).Shrink(0).Center().Cursor(ui.CursorGrab).OnClick(func() {})
	if handle.Pressed() {
		handle.Cursor(ui.CursorGrabbing)
	}
	opacity := float32(0)
	if hovered || dragging {
		opacity = 1
	}
	handle.Opacity(opacity).Children(func() {
		ui.Icon(c, lucide.GripVertical()).Size(14, 14).TextColor(theme.Dimmed)
	})
	v.dragRow(handle, index, row, v.Data.Collection.Rows())
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
	ui.Text(c, method).Shrink(0).SingleLine().FontSize(14).FontWeight(600).LineHeight(1).TextColor(color)
}

func (v *View) menu(menu *ui.Menu, row store.CollectionRow) {
	collection := v.Data.Collection
	collection.SelectNode(row.ID)
	if row.Folder {
		if menu.Item(locales.ApiCollectionNewChildFolder()).Chosen() {
			v.beginCreate(row.ID)
		}
		menu.Separator()
	}
	if menu.Item(locales.ApiCollectionRename()).Chosen() {
		v.renaming.show(row.ID, row.Name)
	}
	if !row.Folder {
		if menu.Item(locales.ApiCollectionCopy()).Disabled(v.copying).Chosen() {
			v.duplicate(row.ID)
		}
	}
	if row.Folder && collection.CanMoveToRoot(row.ID) {
		if menu.Item(locales.ApiCollectionMoveToRoot()).Chosen() {
			v.move(row.ID, "")
		}
	}
	menu.Separator()
	if menu.Item(locales.ApiCollectionDelete()).Danger().Chosen() {
		v.askDelete([]string{row.ID}, false)
	}
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

func (v *View) dragRow(line ui.Element, index int, row store.CollectionRow, rows []store.CollectionRow) {
	if line.PressedInside() {
		v.drag = treeDrag{id: row.ID, index: index, startScroll: v.scroll.Y}
		v.suppress = ""
	}
	if v.drag.id != row.ID {
		return
	}
	if _, dy, moved := line.Dragged(); moved {
		v.drag.dy += dy
		if !v.drag.active && (v.drag.dy > dragThreshold || v.drag.dy < -dragThreshold) {
			v.drag.active = true
		}
	}
	if _, _, ended := line.DragEnded(); !ended || !v.drag.active {
		return
	}
	drag := v.drag
	v.drag = treeDrag{}
	v.suppress = row.ID
	v.finishDrag(drag)
}

func (v *View) finishDrag(drag treeDrag) {
	collection := v.Data.Collection
	switch {
	case drag.root && collection.CanMoveToRoot(drag.id):
		v.move(drag.id, "")
	case drag.target != "" && collection.CanMove(drag.id, drag.target):
		v.move(drag.id, drag.target)
	}
}

// continueDrag finds what the dragged row is over, scrolls at the edges, and
// opens a folder the row rests on.
func (v *View) continueDrag(c *ui.Context, rows []store.CollectionRow) {
	collection := v.Data.Collection
	drag := &v.drag
	contentY := float32(drag.index)*rowHeight + rowHeight/2 + drag.dy + v.scroll.Y - drag.startScroll
	viewportY := contentY - v.scroll.Y
	drag.root = collection.CanMoveToRoot(drag.id) && viewportY >= 0 && viewportY < rootZoneHeight
	drag.target = ""
	if !drag.root && contentY >= 0 {
		if index := int(contentY / rowHeight); index < len(rows) && collection.CanMove(drag.id, rows[index].ID) {
			drag.target = rows[index].ID
		}
	}
	if target := drag.target; target != "" && !collection.Expanded[target] && collection.IsFolder(target) && v.hasChildren(rows, target) {
		if drag.hoverFolder != target {
			drag.hoverFolder, drag.hoverStarted = target, c.Now()
		}
		if elapsed := c.Now().Sub(drag.hoverStarted); elapsed >= dragExpandDelay {
			collection.ToggleExpanded(target)
			drag.hoverFolder = ""
		} else {
			c.After(dragExpandDelay - elapsed)
		}
	} else {
		drag.hoverFolder = ""
	}
	switch {
	case viewportY < autoScrollEdge && v.scroll.Y > 0:
		v.scroll.Y = max(0, v.scroll.Y-autoScrollStep)
		c.After(autoScrollPeriod)
	case v.viewport > 0 && viewportY > v.viewport-autoScrollEdge && v.scroll.Y < v.scroll.MaxY:
		v.scroll.Y = min(v.scroll.MaxY, v.scroll.Y+autoScrollStep)
		c.After(autoScrollPeriod)
	}
}

func (v *View) hasChildren(rows []store.CollectionRow, id string) bool {
	for _, row := range rows {
		if row.ID == id {
			return row.HasChildren
		}
	}
	return false
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
