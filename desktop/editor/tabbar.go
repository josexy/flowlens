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
	tabBarHeight  float32 = 34
	tabWidth      float32 = 176
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
	bar := ui.Row(c.Key("workspace-tabs")).FillWidth().Height(tabBarHeight).Shrink(0).AlignItems(ui.Center)
	bar.Background(theme.MutedBackground).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border)
	bar.Children(func() {
		if e.tabScroll.X > 0 {
			e.scrollButton(c, theme, "tabs-left", lucide.ChevronLeft(), -tabScrollStep)
		}
		ui.ScrollHorizontal(c.Key("tab-scroll")).TrackScroll(&e.tabScroll).Grow(1).Basis(0).MinWidth(0).FillHeight().Children(func() {
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

func (e *Editor) scrollButton(c *ui.Context, theme nui.Theme, key string, icon *ui.SVG, delta float32) {
	nui.Button(c.Key(key), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Square: true, Leading: icon, AccessibleLabel: key}).
		OnClick(func() { e.tabScroll.ScrollTo(max(0, e.tabScroll.X+delta), e.tabScroll.Y, ui.Smooth) })
}

func (e *Editor) newButton(c *ui.Context, theme nui.Theme) {
	workspace := e.Data.Workspace
	anchor := nui.Button(c.Key("tabs-new"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.Plus(), AccessibleLabel: locales.WorkspaceTabBarNew()})
	anchor.OnClick(func() { e.newMenu.Open = !e.newMenu.Open })
	nui.DropdownMenu(c.Key("tabs-new-menu"), theme, anchor, &e.newMenu, []nui.MenuItem{
		{ID: "http", Label: locales.WorkspaceTabBarNewHttp(), OnSelect: func() { workspace.CreateHTTPRequestTab(logic.DraftNew, nil) }},
		{ID: "ws", Label: locales.WorkspaceTabBarNewWs(), OnSelect: func() { workspace.CreateWebSocketClientTab(logic.DraftNew, nil) }},
	}, nui.MenuOptions{Label: locales.WorkspaceTabBarNew()})
}

func (e *Editor) tab(c *ui.Context, theme nui.Theme, index int, tab *logic.WorkspaceTab) {
	workspace := e.Data.Workspace
	active := workspace.ActiveKey == tab.Key
	title := DisplayTitle(tab)
	box := ui.Row(c.Key(tab.Key)).Width(tabWidth).FillHeight().Shrink(0).AlignItems(ui.Center).Gap(6).PaddingX(10).Role(ui.RoleTab).Label(title)
	box.BorderWidth(2, 1, 0, 0).BorderColor(theme.Border)
	if active {
		box.Background(theme.Background).BorderColor(theme.Primary)
	} else if box.Hovered() {
		box.Background(theme.Elevated)
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
		color := theme.Muted
		if active {
			color = theme.Primary
		}
		ui.Icon(c, tabIcon(tab.Type)).Size(14, 14).Shrink(0).TextColor(color)
		ui.Text(c, title).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(13).TextColor(theme.Text)
		if tab.Closable && workspace.IsDirty(tab.Key) {
			dot := ui.Box(c.Key("dirty")).Size(6, 6).Shrink(0).Radius(3).Background(theme.Primary).Label(locales.WorkspaceTabBarUnsavedChanges())
			nui.Tooltip(c, theme, dot, locales.WorkspaceTabBarUnsavedChanges(), nui.TooltipOptions{})
		}
		if tab.Closable {
			nui.Button(c.Key("close"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Square: true, Leading: lucide.X(), AccessibleLabel: locales.WorkspaceTabContextCloseCurrent() + " " + title}).
				OnClick(func() { e.closeWithPrompt(tab.Key) })
		}
	})
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
