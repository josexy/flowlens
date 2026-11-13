package editor

import (
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/shortcuts"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const (
	tabBarHeight  float32 = 34
	tabMinWidth   float32 = 124
	tabMaxWidth   float32 = 220
	tabScrollStep float32 = 220
)

// tabDrag is a workspace tab being dragged along the strip.
type tabDrag struct {
	key string
	// from is where the tab was among the workspace's tabs, target where it
	// would drop now.
	from, target int
	// width is the dragged tab's width, which its neighbors slide by.
	width float32
	// scrolled is how far the strip was scrolled when the drag began.
	scrolled float32
}

// TabBar builds the strip of workspace tabs.
func (e *Editor) TabBar(c *ui.Context, theme nui.Theme) {
	e.declareShortcuts()
	e.prune()
	workspace := e.Data.Workspace
	bar := ui.Row(c.Key("workspace-tabs")).FillWidth().Height(tabBarHeight).Shrink(0).AlignItems(ui.Stretch)
	bar.Background(theme.Background).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border)
	bar.Children(func() {
		if e.tabScroll.X > 0 {
			e.scrollButton(c, theme, "tabs-left", lucide.ChevronLeft(), -tabScrollStep)
		}
		// The strip is as wide as its tabs, so the new tab button follows them.
		strip := widgets.HorizontalWheel(c, ui.ScrollHorizontal(c.Key("tab-scroll")).Shrink(1).MinWidth(0).FillHeight().Scrollbars(false), &e.tabScroll, func() {
			// The user scrolling the strip ends bringing the active tab
			// into view.
			e.revealUntil = time.Time{}
		})
		e.strip = strip.Bounds()
		strip.Children(func() {
			ui.Row(c).FillHeight().Children(func() {
				for index, tab := range workspace.Tabs {
					e.tab(c, theme, index, tab)
				}
				e.tabMenus.Sweep()
			})
		})
		if e.tabScroll.X < e.tabScroll.MaxX {
			e.scrollButton(c, theme, "tabs-right", lucide.ChevronRight(), tabScrollStep)
		}
		e.newButton(c, theme)
	})
	if e.Host.Dialogs != nil {
		e.Host.Dialogs(c, theme)
	}
}

// tabStripButton is a full-height icon button of the tab strip.
func tabStripButton(c *ui.Context, theme nui.Theme, key, label string, icon *ui.SVG, width float32) ui.Element {
	button := ui.ButtonBase(c.Key(key)).Width(width).FillHeight().Shrink(0).Center().Label(label)
	color := theme.Muted
	if button.Hovered() || button.Pressed() || button.FocusVisible() {
		button.Background(theme.Elevated)
		color = theme.Text
	}
	button.Children(func() { ui.Icon(c, icon).Size(14, 14).TextColor(color) })
	return button
}

func (e *Editor) scrollButton(c *ui.Context, theme nui.Theme, key string, icon *ui.SVG, delta float32) {
	button := tabStripButton(c, theme, key, key, icon, 26)
	if delta < 0 {
		button.BorderWidth(0, 1, 0, 0)
	} else {
		button.BorderWidth(0, 1, 0, 1)
	}
	button.BorderColor(theme.Border).OnClick(func() { e.tabScroll.ScrollTo(max(0, e.tabScroll.X+delta), e.tabScroll.Y, ui.Smooth) })
}

func (e *Editor) newButton(c *ui.Context, theme nui.Theme) {
	workspace := e.Data.Workspace
	// DropdownMenu opens and closes the menu on the button's click itself;
	// toggling it here as well would undo each click.
	anchor := tabStripButton(c, theme, "tabs-new", locales.WorkspaceTabBarNew(), lucide.Plus(), 30)
	nui.DropdownMenu(c.Key("tabs-new-menu"), theme, anchor, &e.newMenu, []nui.MenuItem{
		{ID: "http", Label: locales.WorkspaceTabBarNewHttp(), Shortcut: e.menuShortcut(shortcuts.WorkspaceNewHTTPRequest), OnSelect: func() { workspace.CreateHTTPRequestTab(logic.DraftNew, nil) }},
		{ID: "ws", Label: locales.WorkspaceTabBarNewWs(), Shortcut: e.menuShortcut(shortcuts.WorkspaceNewWebSocket), OnSelect: func() { workspace.CreateWebSocketClientTab(logic.DraftNew, nil) }},
	}, nui.MenuOptions{Label: locales.WorkspaceTabBarNew(), Placement: nui.Set(nui.BottomEnd)})
}

// tabDividerColor separates the tabs.
func tabDividerColor(theme nui.Theme) ui.Color {
	if theme.Dark {
		return theme.Border
	}
	return ui.RGBA(0, 0, 0, 0.16)
}

func (e *Editor) tab(c *ui.Context, theme nui.Theme, index int, tab *logic.WorkspaceTab) {
	workspace := e.Data.Workspace
	active := workspace.ActiveKey == tab.Key
	title := DisplayTitle(tab)
	// A dropped tab takes a new key, so it shows in its new place at once
	// rather than gliding from where it was let go.
	box := ui.Row(c.Key(tab.Key + "#" + strconv.Itoa(e.dropped[tab.Key]))).MinWidth(tabMinWidth).MaxWidth(tabMaxWidth).FillHeight().Shrink(0).AlignItems(ui.Center).Role(ui.RoleTab).Label(title)
	box.BorderWidth(0, 1, 0, 0).BorderColor(tabDividerColor(theme))
	hovered := box.Hovered()
	switch {
	case active:
		box.Background(theme.Background)
	case hovered:
		box.Background(theme.Elevated.Alpha(0.82))
	}
	if active {
		e.reveal(c, box, tab.Key)
	}
	if tab.Closable {
		e.dragTab(c, theme, box, index, tab.Key)
	} else if shift := e.slide(index); shift != 0 {
		box.Translate(shift, 0)
	}
	// A press the drag took is no click: one that does not move activates.
	box.OnClick(func() { workspace.Activate(tab.Key) })
	box.Children(func() {
		if active {
			ui.Box(c.Key("active-line")).Absolute().Top(0).Left(0).Right(0).Height(2).Background(theme.Primary).IgnoreInput(true)
		}
		iconColor := theme.Muted.Alpha(0.72)
		if active {
			iconColor = theme.Toned
		}
		ui.Row(c).Grow(1).Basis(0).MinWidth(0).FillHeight().AlignItems(ui.Center).Gap(6).PaddingX(12).Children(func() {
			ui.Icon(c, tabIcon(tab.Type)).Size(14, 14).Shrink(0).TextColor(iconColor)
			ui.Text(c, title).SingleLine().Shrink(1).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).FontWeight(500).TextColor(theme.Toned)
		})
		if tab.Closable && workspace.IsDirty(tab.Key) {
			dot := ui.Box(c.Key("dirty")).Size(6, 6).Shrink(0).Radius(3).Margin(0, 4, 0, 0).Background(theme.Primary).Label(locales.WorkspaceTabBarUnsavedChanges())
			nui.Tooltip(c, theme, dot, locales.WorkspaceTabBarUnsavedChanges(), nui.TooltipOptions{})
		}
		if tab.Closable {
			e.closeButton(c, theme, tab, title, active || hovered)
		}
	})
	widgets.ContextMenu(c.Key("menu:"+tab.Key), theme, box, e.tabMenus.For(tab.Key), func() []nui.MenuItem { return e.tabMenuItems(index, tab) }, nui.MenuOptions{Label: title})
}

// revealTime bounds how long a newly active tab is brought into view.
const revealTime = 2 * time.Second

// reveal brings the active tab fully into view, by the least scrolling, as
// 1.0.5's scrollIntoView({inline: 'nearest'}), however it became active. The
// scroll buttons come and go as the strip scrolls, which moves where the tab
// must scroll to, so it is revealed until it shows whole and the strip rests,
// or the user scrolls the strip.
func (e *Editor) reveal(c *ui.Context, box ui.Element, key string) {
	now := c.Now()
	if e.revealed != key {
		e.revealed, e.revealUntil = key, now.Add(revealTime)
		if e.tabScroll.Scrolling() {
			// Turn a glide toward a tab active before back to where the
			// strip is.
			e.tabScroll.ScrollTo(e.tabScroll.X, e.tabScroll.Y, ui.Smooth)
		}
	}
	if !now.Before(e.revealUntil) {
		return
	}
	tab, strip := box.Bounds(), e.strip
	if tab.W <= 0 || strip.W <= 0 {
		return
	}
	var delta float32
	switch {
	case tab.X < strip.X:
		delta = tab.X - strip.X
	case tab.X+tab.W > strip.X+strip.W:
		delta = min(tab.X+tab.W-strip.X-strip.W, tab.X-strip.X)
	}
	switch {
	case delta < -0.5 || delta > 0.5:
		e.tabScroll.ScrollTo(max(0, e.tabScroll.X+delta), e.tabScroll.Y, ui.Smooth)
	case !e.tabScroll.Scrolling():
		e.revealUntil = time.Time{}
	}
}

// closeButton shows on the active tab and the tab under the pointer.
func (e *Editor) closeButton(c *ui.Context, theme nui.Theme, tab *logic.WorkspaceTab, title string, shown bool) {
	button := ui.ButtonBase(c.Key("close")).Size(18, 18).Shrink(0).Radius(9).Margin(0, 6, 0, 0).Center().
		Label(locales.WorkspaceTabContextCloseCurrent() + " " + title)
	color := theme.Muted
	opacity := float32(0)
	switch {
	case button.Hovered() || button.Pressed():
		button.Background(theme.Accented)
		color, opacity = theme.Text, 1
	case shown || button.FocusVisible():
		opacity = 0.72
	}
	button.Opacity(opacity).OnClick(func() { e.closeWithPrompt(tab.Key) })
	button.Children(func() { ui.Icon(c, lucide.X()).Size(12, 12).TextColor(color) })
	nui.Tooltip(c, theme, button, locales.WorkspaceClose(), nui.TooltipOptions{})
}

// tabEdgeScroll is how far the strip scrolls in a frame while a dragged tab
// is at its edge, and tabEdge how near the edge that starts.
const (
	tabEdgeScroll float32 = 8
	tabEdge       float32 = 32
)

// dragTab lets the pointer drag a tab along the strip, as 1.0.5 did: the tab
// follows the pointer, its neighbors slide aside to open the slot it would
// drop in, and the strip scrolls while the tab is at its edge. Letting go
// moves the tab there; the Capture tab stays first.
func (e *Editor) dragTab(c *ui.Context, theme nui.Theme, box ui.Element, index int, key string) {
	box.Draggable(nil, ui.DraggableOptions{Axis: ui.Horizontal, Snap: func(ui.Point) ui.Point { return ui.Point{} }})
	if bounds := box.Bounds(); bounds.W > 0 && e.drag.key != key {
		e.widths[key] = bounds.W
	}
	if !box.Dragging() {
		if e.drag.key == key {
			e.drop()
		} else if shift := e.slide(index); shift != 0 {
			box.Translate(shift, 0)
		}
		return
	}
	if e.drag.key != key {
		e.drag = tabDrag{key: key, from: index, target: index, width: e.widths[key], scrolled: e.tabScroll.X}
	}
	// The tab stays under the pointer as the strip scrolls under it.
	scrolled := e.tabScroll.X - e.drag.scrolled
	box.Translate(scrolled, 0).ZIndex(1).Background(theme.Background).Opacity(0.92)
	e.drag.target = e.dropTarget(box.DragOffset().X + scrolled)
	e.edgeScroll(c, box.Bounds())
}

// dropTarget returns where the dragged tab drops, moved by offset from its
// place: past every neighbor whose middle it passed, never before Capture.
func (e *Editor) dropTarget(offset float32) int {
	tabs := e.Data.Workspace.Tabs
	target := e.drag.from
	if offset > 0 {
		for moved := offset; target+1 < len(tabs) && moved >= e.widths[tabs[target+1].Key]/2; target++ {
			moved -= e.widths[tabs[target+1].Key]
		}
		return target
	}
	for moved := -offset; target-1 >= 1 && moved >= e.widths[tabs[target-1].Key]/2; target-- {
		moved -= e.widths[tabs[target-1].Key]
	}
	return target
}

// slide is how far the tab at index moves aside for the dragged tab.
func (e *Editor) slide(index int) float32 {
	switch drag := e.drag; {
	case drag.key == "":
		return 0
	case drag.from < drag.target && index > drag.from && index <= drag.target:
		return -drag.width
	case drag.target < drag.from && index >= drag.target && index < drag.from:
		return drag.width
	default:
		return 0
	}
}

// edgeScroll scrolls the strip while the dragged tab is at its edge.
func (e *Editor) edgeScroll(c *ui.Context, tab ui.Rect) {
	strip := e.strip
	if strip.W <= 0 || tab.W <= 0 {
		return
	}
	switch {
	case tab.X+tab.W > strip.X+strip.W-tabEdge:
		if widgets.ScrollStripBy(&e.tabScroll, tabEdgeScroll) {
			c.Invalidate()
		}
	case tab.X < strip.X+tabEdge:
		if widgets.ScrollStripBy(&e.tabScroll, -tabEdgeScroll) {
			c.Invalidate()
		}
	}
}

// drop moves the dragged tab where it was let go.
func (e *Editor) drop() {
	drag := e.drag
	e.drag = tabDrag{}
	tabs := e.Data.Workspace.Tabs
	if drag.target == drag.from || drag.from >= len(tabs) || tabs[drag.from].Key != drag.key {
		return
	}
	keys := make([]string, 0, len(tabs))
	for _, tab := range tabs {
		if tab.Key != logic.CaptureTabKey && tab.Key != drag.key {
			keys = append(keys, tab.Key)
		}
	}
	// Among the request tabs, Capture being first.
	at := min(max(drag.target-1, 0), len(keys))
	keys = slices.Insert(keys, at, drag.key)
	e.Data.Workspace.Reorder(keys)
	e.dropped[drag.key]++
}

func (e *Editor) closeWithPrompt(key string) {
	workspace := e.Data.Workspace
	if !workspace.IsDirty(key) {
		workspace.CloseTab(key)
		return
	}
	e.Confirm.Ask(widgets.ConfirmRequest{
		Title: locales.WorkspaceTabContextCloseConfirmTitle(), Message: locales.WorkspaceTabContextCloseConfirmContent(),
		Confirm:     widgets.ConfirmAction{Label: locales.WorkspaceTabContextCloseCurrent(), Color: nui.Error, Run: func() { workspace.CloseTab(key) }},
		CancelLabel: locales.HistoryCancel(),
	})
}

func (e *Editor) closeSeveral(keys []string, title, message string) {
	workspace := e.Data.Workspace
	if !workspace.HasDirtyTabs(keys) {
		workspace.CloseTabs(keys, true)
		return
	}
	e.Confirm.Ask(widgets.ConfirmRequest{
		Title: title, Message: message,
		Confirm:     widgets.ConfirmAction{Label: title, Color: nui.Error, Run: func() { workspace.CloseTabs(keys, true) }},
		CancelLabel: locales.HistoryCancel(),
	})
}

// tabMenuItems are the actions on a workspace tab, as 1.0.5's tab menu.
func (e *Editor) tabMenuItems(index int, tab *logic.WorkspaceTab) []nui.MenuItem {
	workspace := e.Data.Workspace
	var items []nui.MenuItem
	if tab.HTTP != nil || tab.WebSocket != nil {
		items = append(items, nui.MenuItem{
			ID: "save-api", Label: locales.WorkspaceTabContextSaveApi(), Shortcut: e.menuShortcut(shortcuts.AppSave), Disabled: e.Host.SaveTab == nil,
			OnSelect: func() { e.Host.SaveTab(tab.Key) },
		}, widgets.MenuSeparator("save-separator"))
	}
	others := workspace.ClosableKeys(func(_ int, other *logic.WorkspaceTab) bool { return other.Key != tab.Key })
	right := workspace.ClosableKeys(func(position int, _ *logic.WorkspaceTab) bool { return position > index })
	all := workspace.ClosableKeys(func(int, *logic.WorkspaceTab) bool { return true })
	return append(items,
		nui.MenuItem{
			ID: "close", Label: locales.WorkspaceTabContextCloseCurrent(), Shortcut: e.menuShortcut(shortcuts.WorkspaceCloseTab), Disabled: !tab.Closable,
			OnSelect: func() { e.closeWithPrompt(tab.Key) },
		},
		nui.MenuItem{ID: "force-close", Label: locales.WorkspaceTabContextForceCloseCurrent(), Disabled: !tab.Closable, OnSelect: func() { workspace.CloseTab(tab.Key) }},
		widgets.MenuSeparator("close-separator"),
		nui.MenuItem{ID: "close-others", Label: locales.WorkspaceTabContextCloseOther(), Disabled: len(others) == 0, OnSelect: func() {
			e.closeSeveral(others, locales.WorkspaceTabContextCloseOtherConfirmTitle(), locales.WorkspaceTabContextCloseOtherConfirmContent())
		}},
		nui.MenuItem{ID: "force-close-others", Label: locales.WorkspaceTabContextForceCloseOther(), Disabled: len(others) == 0, OnSelect: func() { workspace.CloseTabs(others, true) }},
		nui.MenuItem{ID: "close-right", Label: locales.WorkspaceTabContextCloseRight(), Disabled: len(right) == 0, OnSelect: func() {
			e.closeSeveral(right, locales.WorkspaceTabContextCloseRightConfirmTitle(), locales.WorkspaceTabContextCloseRightConfirmContent())
		}},
		nui.MenuItem{ID: "force-close-right", Label: locales.WorkspaceTabContextForceCloseRight(), Disabled: len(right) == 0, OnSelect: func() { workspace.CloseTabs(right, true) }},
		nui.MenuItem{ID: "close-all", Label: locales.WorkspaceTabContextCloseAll(), Disabled: len(all) == 0, OnSelect: func() {
			e.closeSeveral(all, locales.WorkspaceTabContextCloseAllConfirmTitle(), locales.WorkspaceTabContextCloseAllConfirmContent())
		}},
		nui.MenuItem{ID: "force-close-all", Label: locales.WorkspaceTabContextForceCloseAll(), Disabled: len(all) == 0, OnSelect: func() { workspace.CloseTabs(all, true) }},
	)
}
