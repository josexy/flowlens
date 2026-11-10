package editor

import (
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const (
	tabBarHeight float32 = 34
	tabMinWidth  float32 = 124
	tabMaxWidth  float32 = 220
	// tabWidth is how far a dragged tab moves to pass a neighbor.
	tabWidth      float32 = 172
	tabScrollStep float32 = 220
	dragThreshold float32 = 4
)

type tabDrag struct {
	key   string
	x     float32
	moved bool
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
		ui.ScrollHorizontal(c.Key("tab-scroll")).TrackScroll(&e.tabScroll).Shrink(1).MinWidth(0).FillHeight().Scrollbars(false).Children(func() {
			ui.Row(c).FillHeight().Children(func() {
				for index, tab := range workspace.Tabs {
					e.tab(c, theme, index, tab)
				}
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
	anchor := tabStripButton(c, theme, "tabs-new", locales.WorkspaceTabBarNew(), lucide.Plus(), 30)
	anchor.OnClick(func() { e.newMenu.Open = !e.newMenu.Open })
	nui.DropdownMenu(c.Key("tabs-new-menu"), theme, anchor, &e.newMenu, []nui.MenuItem{
		{ID: "http", Label: locales.WorkspaceTabBarNewHttp(), OnSelect: func() { workspace.CreateHTTPRequestTab(logic.DraftNew, nil) }},
		{ID: "ws", Label: locales.WorkspaceTabBarNewWs(), OnSelect: func() { workspace.CreateWebSocketClientTab(logic.DraftNew, nil) }},
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
	box := ui.Row(c.Key(tab.Key)).MinWidth(tabMinWidth).MaxWidth(tabMaxWidth).FillHeight().Shrink(0).AlignItems(ui.Center).Role(ui.RoleTab).Label(title)
	box.BorderWidth(0, 1, 0, 0).BorderColor(tabDividerColor(theme))
	hovered := box.Hovered()
	switch {
	case active:
		box.Background(theme.Background)
	case hovered:
		box.Background(theme.Elevated.Alpha(0.82))
	}
	if tab.Closable {
		e.dragTab(box, tab.Key)
	}
	box.OnClick(func() {
		if e.drag.moved && e.drag.key == tab.Key {
			e.drag = tabDrag{}
			return
		}
		workspace.Activate(tab.Key)
	})
	box.ContextMenu(func(menu *ui.Menu) { e.tabMenu(menu, index, tab) })
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
			ui.Text(c, title).SingleLine().Shrink(1).MinWidth(0).FontSize(14).FontWeight(500).TextColor(theme.Toned)
		})
		if tab.Closable && workspace.IsDirty(tab.Key) {
			dot := ui.Box(c.Key("dirty")).Size(6, 6).Shrink(0).Radius(3).Margin(0, 4, 0, 0).Background(theme.Primary).Label(locales.WorkspaceTabBarUnsavedChanges())
			nui.Tooltip(c, theme, dot, locales.WorkspaceTabBarUnsavedChanges(), nui.TooltipOptions{})
		}
		if tab.Closable {
			e.closeButton(c, theme, tab, title, active || hovered)
		}
	})
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

// dragTab moves a tab past its neighbors while it is dragged.
func (e *Editor) dragTab(box ui.Element, key string) {
	if box.PressedInside() {
		e.drag = tabDrag{key: key}
	}
	dx, _, moved := box.Dragged()
	if !moved || e.drag.key != key {
		return
	}
	e.drag.x += dx
	if !e.drag.moved && e.drag.x > -dragThreshold && e.drag.x < dragThreshold {
		return
	}
	e.drag.moved = true
	for e.drag.x > tabWidth/2 || e.drag.x < -tabWidth/2 {
		direction := 1
		if e.drag.x < 0 {
			direction = -1
		}
		if !e.moveTab(key, direction) {
			e.drag.x = 0
			break
		}
		e.drag.x -= float32(direction) * tabWidth
	}
}

func (e *Editor) moveTab(key string, direction int) bool {
	workspace := e.Data.Workspace
	keys := make([]string, 0, len(workspace.Tabs))
	for _, tab := range workspace.Tabs {
		if tab.Key != logic.CaptureTabKey {
			keys = append(keys, tab.Key)
		}
	}
	index := slices.Index(keys, key)
	target := index + direction
	if index < 0 || target < 0 || target >= len(keys) {
		return false
	}
	keys[index], keys[target] = keys[target], keys[index]
	workspace.Reorder(keys)
	return true
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

func (e *Editor) tabMenu(menu *ui.Menu, index int, tab *logic.WorkspaceTab) {
	workspace := e.Data.Workspace
	if tab.HTTP != nil || tab.WebSocket != nil {
		if menu.Item(locales.WorkspaceTabContextSaveApi()).Disabled(e.Host.SaveTab == nil).Chosen() {
			e.Host.SaveTab(tab.Key)
		}
		menu.Separator()
	}
	if menu.Item(locales.WorkspaceTabContextCloseCurrent()).Disabled(!tab.Closable).Chosen() {
		e.closeWithPrompt(tab.Key)
	}
	if menu.Item(locales.WorkspaceTabContextForceCloseCurrent()).Disabled(!tab.Closable).Chosen() {
		workspace.CloseTab(tab.Key)
	}
	menu.Separator()
	others := workspace.ClosableKeys(func(_ int, other *logic.WorkspaceTab) bool { return other.Key != tab.Key })
	right := workspace.ClosableKeys(func(position int, _ *logic.WorkspaceTab) bool { return position > index })
	all := workspace.ClosableKeys(func(int, *logic.WorkspaceTab) bool { return true })
	if menu.Item(locales.WorkspaceTabContextCloseOther()).Disabled(len(others) == 0).Chosen() {
		e.closeSeveral(others, locales.WorkspaceTabContextCloseOtherConfirmTitle(), locales.WorkspaceTabContextCloseOtherConfirmContent())
	}
	if menu.Item(locales.WorkspaceTabContextForceCloseOther()).Disabled(len(others) == 0).Chosen() {
		workspace.CloseTabs(others, true)
	}
	if menu.Item(locales.WorkspaceTabContextCloseRight()).Disabled(len(right) == 0).Chosen() {
		e.closeSeveral(right, locales.WorkspaceTabContextCloseRightConfirmTitle(), locales.WorkspaceTabContextCloseRightConfirmContent())
	}
	if menu.Item(locales.WorkspaceTabContextForceCloseRight()).Disabled(len(right) == 0).Chosen() {
		workspace.CloseTabs(right, true)
	}
	if menu.Item(locales.WorkspaceTabContextCloseAll()).Disabled(len(all) == 0).Chosen() {
		e.closeSeveral(all, locales.WorkspaceTabContextCloseAllConfirmTitle(), locales.WorkspaceTabContextCloseAllConfirmContent())
	}
	if menu.Item(locales.WorkspaceTabContextForceCloseAll()).Disabled(len(all) == 0).Chosen() {
		workspace.CloseTabs(all, true)
	}
}
