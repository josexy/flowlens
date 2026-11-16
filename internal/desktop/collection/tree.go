package collection

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/store"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

type collectionTreeItem struct {
	row    store.CollectionRow
	anchor ui.Handle
}

func (v *View) updateTree() {
	collection := v.Data.Collection
	if v.treeItems == nil || v.treeVersion != collection.Version {
		rows := collection.AllRows()
		items := make(map[string]*collectionTreeItem, len(rows))
		children := make(map[string][]string)
		for _, row := range rows {
			item := v.treeItems[row.ID]
			if item == nil {
				item = &collectionTreeItem{}
			}
			item.row = row
			items[row.ID] = item
			children[row.ParentID] = append(children[row.ParentID], row.ID)
		}
		v.treeItems, v.treeVersion = items, collection.Version
		var nodes func(string) []nui.TreeNode
		nodes = func(parent string) []nui.TreeNode {
			result := make([]nui.TreeNode, 0, len(children[parent]))
			for _, id := range children[parent] {
				item := items[id]
				node := nui.TreeNode{ID: id, Label: item.row.Name,
					Style: func(slot nui.Slot, element ui.Element) {
						if slot == nui.Item {
							element.Bind(&item.anchor)
							v.visibleItems = append(v.visibleItems, item)
						}
					},
				}
				if item.row.Folder {
					node.Children = nodes(id)
				}
				result = append(result, node)
			}
			return result
		}
		v.treeState.SetNodes(nodes(""))
	}
	for id, item := range v.treeItems {
		if item.row.Folder {
			v.treeState.SetExpanded(id, collection.Expanded[id])
		}
	}
	v.treeState.Selection.Clear()
	if v.multi {
		for id := range v.checked {
			v.treeState.Selection.Add(id)
		}
	} else if collection.Exists(collection.SelectedNodeID) {
		v.treeState.Selection.Add(collection.SelectedNodeID)
	}
}

func (v *View) tree(c *ui.Context, theme nui.Theme) {
	collection := v.Data.Collection
	v.updateTree()
	wasActive := v.drag.active
	v.drag.active, v.drag.target, v.drag.root = false, "", false
	v.visibleItems = v.visibleItems[:0]
	ui.Column(c.Key("tree-layout")).Fill().MinHeight(0).Children(func() {
		rootSlot := ui.Box(c.Key("root-slot")).FillWidth().Shrink(0).Height(0)
		ui.Box(c.Key("tree-viewport")).Bind(&v.treeViewport).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			behavior := nui.SelectionReplace
			if v.multi {
				behavior = nui.SelectionToggle
			}
			nui.Tree(c.Key("tree"), theme, &v.treeState, nui.TreeOptions{
				Label: locales.ApiCollectionTitle(), Height: v.treeViewport.Bounds(c).H,
				Multiple: v.multi, SelectionBehavior: behavior, ExpandOnClick: nui.Set(!v.multi),
				BuildLeading: v.treeLeading, Build: v.treeLabel,
				OnSelect: func(event *nui.TreeEvent) {
					if v.multi {
						return
					}
					// The store owns normal selection, including Ctrl-clicks.
					event.PreventDefault()
					v.selectTreeNode(event.ID)
				},
				OnToggle: func(event *nui.TreeEvent) {
					if event.Expanded {
						delete(collection.Expanded, event.ID)
					} else {
						collection.Expanded[event.ID] = true
					}
				},
				OnChange: func() {
					if v.multi {
						clear(v.checked)
						for id := range v.treeState.Selection.All() {
							v.checked[id] = true
						}
					}
				},
			}).Fill()
		})
		for _, item := range v.visibleItems {
			v.decorateTreeItem(c, theme, item)
		}
		// Keep the root target outside the virtual viewport, including the
		// release build, so it never covers the first folder or scroll edge.
		if (wasActive || v.drag.active) && v.validDrag(v.drag.source) && collection.CanMoveToRoot(v.drag.source.id) {
			rootSlot.Height(rootZoneHeight+4).Padding(0, 6, 4, 6).Children(func() { v.rootZone(c, theme) })
		}
	})
	if wasActive && !v.drag.active {
		v.treeState.Outline.List.Focus(c)
	}
	if v.drag.target == "" || !v.drag.active {
		v.drag.hoverFolder = ""
	}
	selectedParent := collection.ParentOf(collection.SelectedNodeID)
	if v.treeSelected != collection.SelectedNodeID || v.treeSelectedParent != selectedParent {
		v.treeSelected = collection.SelectedNodeID
		v.treeSelectedParent = selectedParent
		for i := range v.treeState.Rows() {
			if v.treeState.Item(i) == v.treeSelected {
				v.treeState.Active = v.treeSelected
				v.treeState.Outline.List.ScrollIntoView(i)
				break
			}
		}
	}
	v.treeTooltip(c, theme)
	if !v.menu.State.Open {
		v.menuID = ""
	}
}

func (v *View) treeLeading(c *ui.Context, theme nui.Theme, state nui.TreeItemState) {
	row := v.treeItems[state.ID].row
	row.Expanded = state.Expanded
	ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
		if v.multi {
			// Reuse the standard checkbox appearance while Tree owns input and
			// accessibility, including clicks on the indicator and range selection.
			checked := state.Selected
			nui.Checkbox(c.Key("check"), theme, &checked, nui.CheckboxOptions{}).
				Label(locales.ApiCollectionSelectItem(row.Name)).IgnoreInput(true).TabStop(false).AccessHidden(true)
		}
		if !v.multi {
			ui.Box(c.Key("grip")).Shrink(0).Disabled(v.dragDisabled()).DimDisabled(false).Children(func() {
				widgets.ReorderGrip(c, theme, "drag "+row.Name, true)
			})
		}
		size := apptheme.TextSize(c, 20)
		ui.Icon(c, rowIcon(row)).Size(size, size).TextColor(theme.Muted)
	})
}

func (v *View) treeLabel(c *ui.Context, theme nui.Theme, state nui.TreeItemState) {
	row := v.treeItems[state.ID].row
	ui.Row(c.Key("label")).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(6).Children(func() {
		if !row.Folder {
			v.requestMethod(c, theme, row)
		}
		ui.Text(c, row.Name).SingleLine().Shrink(1).MinWidth(0).FontSize(apptheme.TextSize(c, 14))
		if row.Folder {
			ui.Row(c.Key("count")).Shrink(0).MinHeight(20).MinWidth(20).PaddingX(6).Radius(10).Center().Background(theme.Elevated).
				Label("child-count").Children(func() {
				ui.Text(c, strconv.Itoa(row.Children)).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
			})
		}
	})
}

func (v *View) showTreeMenu(id string, event ui.InputEvent) {
	if event.Kind != ui.InputPointerDown || event.Button != 1 {
		return
	}
	if v.menuID != id {
		v.menu = widgets.PointMenu{}
	}
	v.menuID = id
	v.menu.Take(event)
	v.Data.Collection.SelectNode(id)
	v.treeState.Active = id
}

func (v *View) decorateTreeItem(c *ui.Context, theme nui.Theme, item *collectionTreeItem) {
	row := item.row
	row.Expanded = v.Data.Collection.Expanded[row.ID]
	line := c.Resolve(item.anchor)
	line.ObserveInput(func(event ui.InputEvent) {
		v.showTreeMenu(row.ID, event)
		if event.Kind == ui.InputPointerDown && event.Button == 0 {
			v.drag = treeDrag{source: collectionDrag{owner: v, id: row.ID, version: v.Data.Collection.Version}, valid: !v.dragDisabled()}
		}
		if event.Kind == ui.InputPointerUp && event.Canceled {
			v.drag.valid = false
		}
	})
	if !v.multi {
		line.KeepFocus().OnClick(func() {
			v.selectTreeNode(row.ID)
			if row.Folder {
				v.Data.Collection.ToggleExpanded(row.ID)
			}
			v.treeState.Active = row.ID
			v.treeState.Outline.List.Focus(c)
		})
	}
	if !v.dragDisabled() {
		source := collectionDrag{owner: v, id: row.ID, version: v.Data.Collection.Version}
		if v.drag.source.id == row.ID {
			source = v.drag.source
		}
		line.Drag(source)
		if line.Dragging() && v.validDrag(source) {
			v.drag.active = true
			// A focused row stays mounted when a virtual list scrolls it out.
			// This retains the native drag source and its preview until release.
			line.Opacity(0.4).Focusable().TabStop(false)
			item.anchor.Focus(c)
		}
		if row.Folder {
			if source, over := ui.DragOver[collectionDrag](line); over && v.canDrop(source, row.ID) {
				v.drag.target = row.ID
				line.Background(theme.Primary.Alpha(0.1).Over(theme.Background)).Border(1, theme.Primary)
				v.hoverFolder(c, row)
			}
			if source, dropped := ui.Drop[collectionDrag](line); dropped {
				v.drop(source, row.ID)
			}
		}
	}

	if v.treeState.Active == row.ID {
		height := item.anchor.Bounds(c).H
		open := func() {
			v.showTreeMenu(row.ID, ui.InputEvent{Kind: ui.InputPointerDown, Button: 1, Y: height})
		}
		v.treeState.Outline.List.OnShortcut(c, 0, ui.KeyContextMenu, open)
		v.treeState.Outline.List.OnShortcut(c, ui.Shift, ui.KeyF10, open)
	}
	if v.menuID == row.ID {
		line.Children(func() {
			v.menu.Build(c.Key("menu"), theme, func() []nui.MenuItem { return v.menuItems(row) }, nui.MenuOptions{
				Label: row.Name,
				OnOpenChange: func(open bool) {
					if !open {
						v.treeState.Outline.List.Focus(c)
					}
				},
			})
		})
	}
}

// Attach the tooltip to the list's input owner, then position it by the row.
// Tooltip makes its anchor clickable; attaching it to a content surface would
// swallow Tree's selection, range selection and expansion clicks.
func (v *View) treeTooltip(c *ui.Context, theme nui.Theme) {
	if v.drag.active || v.menu.State.Open || v.creating || v.renaming.open {
		return
	}
	list := c.Resolve(v.treeState.Outline.List.Handle)
	x, y, over := list.PointerPosition()
	bounds := v.treeState.Outline.List.Bounds(c)
	x, y = x+bounds.X, y+bounds.Y
	id := v.treeState.Active
	hoveredRow := false
	if over {
		for _, item := range v.visibleItems {
			r := item.anchor.Bounds(c)
			if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
				id = item.row.ID
				hoveredRow = true
				break
			}
		}
	}
	if item := v.treeItems[id]; item != nil {
		tip := nui.Tooltip(c.Key("tree-tooltip"), theme, list, item.row.Name, nui.TooltipOptions{
			Placement: nui.Set(nui.RightCenter), Disabled: over && !hoveredRow,
		})
		if anchor := c.Resolve(item.anchor); tip.Valid() && anchor.Valid() {
			tip.AttachTo(anchor, ui.AnchorEnd, ui.AnchorStart)
		}
	}
}

func (v *View) selectTreeNode(id string) {
	if v.Data.Collection.IsFolder(id) {
		v.Data.Collection.SelectNode(id)
	} else {
		v.open(id)
	}
}
