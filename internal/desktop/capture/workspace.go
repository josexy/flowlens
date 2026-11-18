// Package capture builds the traffic capture workspace: the proxy toolbar, the
// filter bar, the traffic table, and the actions on its rows.
package capture

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// Limits of the split between the table and the detail panel.
const (
	defaultTableSplit = 60
	minTableSplit     = 20
	maxTableSplit     = 90
)

// Host is what the workspace needs from the rest of the application.
type Host struct {
	// ListLocalIPs lists the addresses the proxy can listen on. It runs in a background job.
	ListLocalIPs func() []settingservice.LocalIPAddress
	// Resend sends a captured request again.
	Resend func(ctx context.Context, id uint64, config proxyservice.ResendConfig) (proxyservice.ResendResult, error)
	// OpenEditor opens a captured request in a new editor tab.
	OpenEditor func(entry *logic.TrafficEntry, body *proxyservice.TrafficBodyView, focus bool, historyKey string, done func(error))
	// Export holds what exporting needs. Exporting is unavailable when nil.
	Export *ExportHost
	// PickHARFiles asks for files to import.
	PickHARFiles func() ([]string, error)
}

// Workspace is the live capture page of the main window.
type Workspace struct {
	Data       *store.Store
	Host       Host
	Dispatcher *shortcuts.Dispatcher
	Notifier   *widgets.Notifier
	Confirm    *widgets.ConfirmDialog
	// Detail, when set, builds the panel for the selected entry.
	Detail func(c *ui.Context, theme nui.Theme)
	// ReleaseDetail drops the live selection's viewer caches and jobs.
	ReleaseDetail func()
	// History, when set, builds the saved sessions below the capture item.
	History func(c *ui.Context, theme nui.Theme)

	Table *Table
	// HistoryTable shows the opened saved session.
	HistoryTable *Table
	// HistoryDetail, when set, builds the detail panel of a saved session.
	HistoryDetail func(c *ui.Context, theme nui.Theme)
	// ReleaseHistoryDetail drops the saved session's viewer caches and jobs.
	ReleaseHistoryDetail func()
	// TabBar, when set, builds the workspace tab strip.
	TabBar func(c *ui.Context, theme nui.Theme)
	// RequestTab, when set, builds a request editor tab.
	RequestTab func(c *ui.Context, theme nui.Theme, tab *logic.WorkspaceTab)

	exporter           *Exporter
	historyFilterFocus ui.Handle
	clearingHistory    bool
	filterFocus        ui.Handle
	split              float32
	splitName          string
	category           categoryView
	lastSelection      string
	lastActive         string
	historyListSeen    uint64
	toolbar            toolbarState
	alias              aliasDialog
	// captureMenu, historyMenus and categoryMenus are the states of the
	// sidebars' context menus.
	captureMenu   nui.MenuState
	historyMenus  widgets.MenuStates
	categoryMenus widgets.MenuStates
}

// New returns the capture workspace.
func New(data *store.Store, host Host, dispatcher *shortcuts.Dispatcher, notifier *widgets.Notifier, confirm *widgets.ConfirmDialog, icons *widgets.ProcessIcons) *Workspace {
	w := &Workspace{Data: data, Host: host, Dispatcher: dispatcher, Notifier: notifier, Confirm: confirm}
	if host.Export != nil {
		w.exporter = &Exporter{Host: *host.Export, Runtime: data.Runtime, Notifier: notifier}
	}
	deps := TableDependencies{
		Settings: data.Settings, Notifier: notifier, Confirm: confirm, Icons: icons,
		Resend: host.Resend, OpenEditor: host.OpenEditor, Exporter: w.exporter,
		Runtime: data.Runtime, MacOS: shortcuts.IsMacOS(),
	}
	w.Table = NewTable(LiveSource{Traffic: data.Traffic, Filters: data.Filter}, deps)
	data.Traffic.OnDetailReset = func() {
		if w.ReleaseDetail != nil {
			w.ReleaseDetail()
		}
	}
	data.Traffic.OnReset = func() {
		data.Filter.Release()
		w.Table.Reset()
		if w.category.context == "capture" {
			w.category.reset()
		}
	}
	if data.History != nil {
		w.HistoryTable = NewTable(HistorySource{History: data.History}, deps)
		data.History.Traffic.OnDetailReset = func() {
			if w.ReleaseHistoryDetail != nil {
				w.ReleaseHistoryDetail()
			}
		}
		data.History.Traffic.OnReset = func() {
			data.History.Filter.Release()
			w.HistoryTable.Reset()
			if strings.HasPrefix(w.category.context, "history:") {
				w.category.reset()
			}
		}
		w.HistoryTable.HistoryKey = func() string { return data.History.Traffic.Key }
		w.HistoryTable.HistoryAlias = func() string {
			key := data.History.Traffic.Key
			for _, metadata := range data.History.Metadata {
				if metadata.Key == key {
					return metadata.Alias
				}
			}
			return ""
		}
		w.History = w.historySidebar
	}
	w.refreshLocalIPs()
	return w
}

// Sidebar builds the capture list of the secondary sidebar.
func (w *Workspace) Sidebar(c *ui.Context, theme nui.Theme) {
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		w.captureItem(c, theme)
		if w.History != nil {
			w.History(c, theme)
		}
	})
}

func (w *Workspace) captureItem(c *ui.Context, theme nui.Theme) {
	workbench := w.Data.Workbench
	active := workbench.CaptureSelection == store.CaptureItemLive
	label := strings.TrimSpace(w.Data.Proxy.Status.CaptureAlias)
	if label == "" {
		label = locales.MenuCapture()
	}
	row := ui.Row(c.Key("capture-item")).Role(ui.RoleGroup).TabStop(false).MinHeight(40).Shrink(0).AlignItems(ui.Center).Gap(8).PaddingX(10).Margin(4, 8, 0, 8).Radius(10)
	switch {
	case active:
		row.Background(SidebarSelectedColor(theme)).Border(1, theme.BorderAccented)
	case row.Hovered():
		row.Background(theme.Elevated).Border(1, ui.Transparent)
	default:
		row.Background(theme.Elevated.Mix(theme.Background, 0.12)).Border(1, ui.Transparent)
	}
	row.OnClick(func() { w.selectItem(store.CaptureItemLive) })
	row.Children(func() {
		color := theme.Muted.Alpha(0.7)
		if active {
			color = theme.Primary
		}
		ui.ButtonBase(c.Key("open")).Grow(1).Basis(0).MinWidth(0).FillHeight().Justify(ui.Start).Gap(8).Radius(4).FocusRing(true).Label(label).
			OnClick(func() { w.selectItem(store.CaptureItemLive) }).Children(func() {
			ui.Icon(c, lucide.Radio()).Size(16, 16).Shrink(0).TextColor(color)
			ui.Text(c, label).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).FontWeight(600).TextColor(theme.Text)
		})
		restart := nui.Button(c.Key("restart"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.SM, Square: true, Leading: lucide.SkipForward(), AccessibleLabel: locales.CaptureRestart()})
		restart.OnClick(w.Restart)
		nui.Tooltip(c, theme, restart, locales.CaptureRestart(), nui.TooltipOptions{})
		CountPill(c, theme, strconv.FormatInt(w.Data.Traffic.Statistics.Total, 10), false)
	})
	widgets.ContextMenu(c.Key("capture-menu"), theme, row, &w.captureMenu, w.captureMenuItems, nui.MenuOptions{Label: label})
}

// captureMenuItems are the actions on the live capture.
func (w *Workspace) captureMenuItems() []nui.MenuItem {
	var items []nui.MenuItem
	if w.exporter != nil {
		alias := w.Data.Proxy.Status.CaptureAlias
		items = append(items, sessionExportItem(w.exporter.Exporting, func(format logic.TrafficExportFormat) {
			w.exporter.Export(format, ExportOptions{FilenameHint: alias})
		}))
	}
	return append(items, nui.MenuItem{ID: "set-alias", Label: locales.CaptureSetAlias(), Icon: lucide.Tag(), OnSelect: w.ShowAliasDialog})
}

// SidebarSelectedColor fills the chosen item of a sidebar list: a tint of the
// accent over the accented background.
func SidebarSelectedColor(theme nui.Theme) ui.Color {
	return theme.Primary.Alpha(0.12).Over(theme.Accented)
}

// CountPill is the rounded count at the end of a sidebar item, in the accent
// on the chosen item.
func CountPill(c *ui.Context, theme nui.Theme, count string, active bool) ui.Element {
	background, foreground := theme.Elevated.Alpha(0.88), theme.Muted
	if active {
		background, foreground = theme.Primary.Alpha(0.14).Over(theme.Elevated), theme.Primary
	}
	return ui.Box(c).MinHeight(20).MinWidth(22).PaddingX(7).Radius(10).Center().Shrink(0).Background(background).Children(func() {
		ui.Text(c, count).SingleLine().FontSize(apptheme.TextSize(c, 14)).LineHeight(20.0 / 14.0).TextColor(foreground)
	})
}

// Content builds the page.
func (w *Workspace) Content(c *ui.Context, theme nui.Theme) {
	w.declareShortcuts()
	w.pruneHistoryTab()
	w.syncTabs()
	// The proxy toolbar heads every tab, above the tab strip.
	w.buildToolbar(c, theme)
	if w.TabBar != nil {
		w.TabBar(c, theme)
	}
	if tab := w.requestTab(); tab != nil && w.RequestTab != nil {
		ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() { w.RequestTab(c, theme, tab) })
		w.alias.build(c, theme, w)
		return
	}
	ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
		if _, open := w.selectedHistory(); open {
			w.historyPane(c, theme)
			return
		}
		BuildFilterBar(c, theme, w.Data.Filter, &w.filterFocus)
		area := ui.Box(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() { w.tableAndDetail(c, theme) })
		w.dropTarget(c, theme, area)
	})
	w.alias.build(c, theme, w)
}

func (w *Workspace) requestTab() *logic.WorkspaceTab {
	if w.Data.Workspace == nil {
		return nil
	}
	tab := w.Data.Workspace.ActiveTab()
	if tab.HTTP == nil && tab.WebSocket == nil {
		return nil
	}
	return tab
}

func (w *Workspace) pruneHistoryTab() {
	if w.Data.Workspace == nil || w.Data.History == nil || w.Data.History.ListVersion == w.historyListSeen {
		return
	}
	w.historyListSeen = w.Data.History.ListVersion
	w.Data.Workspace.PruneHistoryTab(func(key string) bool { return w.Data.History.MetadataFor(key) != nil })
}

// historyTitle names a saved session's tab.
func (w *Workspace) historyTitle(key string) string {
	if w.Data.History == nil {
		return key
	}
	metadata := w.Data.History.MetadataFor(key)
	if metadata == nil {
		return key
	}
	return historyTabTitle(metadata)
}

func historyTabTitle(metadata *proxyservice.HistoryMetadata) string {
	switch {
	case metadata.Alias != "":
		return metadata.Alias
	case metadata.CreatedAt == 0:
		return "-"
	default:
		return time.UnixMilli(metadata.CreatedAt).Format("2006-01-02 15:04")
	}
}

// selectItem shows a Capture section item in its workspace tab.
func (w *Workspace) selectItem(item string) {
	w.Data.Workbench.SelectCaptureItem(item)
	w.lastSelection = item
	workspace := w.Data.Workspace
	if workspace == nil {
		return
	}
	if key, open := historyKeyOf(item); open {
		workspace.OpenHistoryTab(key, w.historyTitle(key))
	} else {
		workspace.ActivateCapture()
	}
	w.lastActive = workspace.ActiveKey
}

// syncTabs keeps the sidebar selection and the active workspace tab equal.
func (w *Workspace) syncTabs() {
	workspace := w.Data.Workspace
	if workspace == nil {
		return
	}
	defer w.syncHistoryData()
	workbench := w.Data.Workbench
	if selection := workbench.CaptureSelection; selection != w.lastSelection {
		w.selectItem(selection)
		return
	}
	if workspace.ActiveKey == w.lastActive {
		if _, open := historyKeyOf(workbench.CaptureSelection); open && workspace.FindTab(logic.HistoryTabKey) == nil {
			w.lastSelection = store.CaptureItemLive
			workbench.SelectCaptureItem(store.CaptureItemLive)
		}
		return
	}
	w.lastActive = workspace.ActiveKey
	selection := workbench.CaptureSelection
	switch tab := workspace.ActiveTab(); tab.Type {
	case logic.TabTypeCapture:
		selection = store.CaptureItemLive
	case logic.TabTypeHistory:
		selection = historyItemPrefix + tab.HistoryKey
	}
	if _, open := historyKeyOf(selection); open && workspace.FindTab(logic.HistoryTabKey) == nil {
		selection = store.CaptureItemLive
	}
	if selection != workbench.CaptureSelection {
		workbench.SelectCaptureItem(selection)
	}
	w.lastSelection = selection
}

// syncHistoryData closes a history whose workspace tab was removed.
func (w *Workspace) syncHistoryData() {
	history := w.Data.History
	if history == nil {
		return
	}
	if w.Data.Workspace.FindTab(logic.HistoryTabKey) == nil &&
		(history.Traffic.Key != "" || history.SelectedKey != "" || history.LoadingHistory) {
		history.CloseOpened()
	}
}

func (w *Workspace) detailVisible() bool {
	return w.Detail != nil && w.Data.Traffic.Selected != nil && w.Data.Traffic.ShowDetailPanel
}

func (w *Workspace) tableAndDetail(c *ui.Context, theme nui.Theme) {
	if !w.detailVisible() {
		w.Table.Build(c, theme)
		return
	}
	w.buildSplit(c, theme, "tableDetail", func() { w.Table.Build(c, theme) }, func() { w.Detail(c, theme) })
}

func (w *Workspace) buildSplit(c *ui.Context, theme nui.Theme, prefix string, first, second func()) {
	settings := w.Data.Settings
	layout := settings.TrafficDetailLayout()
	name := prefix + ":" + string(layout)
	if name != w.splitName {
		w.splitName, w.split = name, settings.TrafficDetailSplit(name, defaultTableSplit)
	}
	nui.Splitter(c.Key(prefix), theme, &w.split, nui.SplitterOptions{
		Percent: true, Vertical: layout == settingservice.TrafficDetailLayoutHorizontal,
		Min: nui.Set[float32](minTableSplit), Max: nui.Set[float32](maxTableSplit),
		OnChange: func(percent float32) { settings.SetTrafficDetailSplit(name, percent) },
	}, first, second).Fill()
}

func (w *Workspace) declareShortcuts() {
	w.Dispatcher.Handle(shortcuts.Handler{
		CommandID: shortcuts.CaptureFocusFilter,
		Run:       func() { w.filterFocus.Focus() },
	})
	w.Dispatcher.Handle(shortcuts.Handler{
		CommandID: shortcuts.CaptureToggleProxy,
		When:      func() bool { return !w.Data.Proxy.Toggling },
		Run:       w.toggleProxy,
	})
	w.Dispatcher.Handle(shortcuts.Handler{
		CommandID: shortcuts.CaptureToggleSystemProxy,
		When: func() bool {
			proxy := w.Data.Proxy
			return proxy.SystemProxy.Supported && !proxy.SystemProxyApplying && (proxy.SystemProxy.ModeSupported || proxy.SystemProxy.Active)
		},
		Run: w.toggleSystemProxy,
	})
	w.Dispatcher.Handle(shortcuts.Handler{
		CommandID: shortcuts.CaptureClearTraffic,
		When:      func() bool { return len(w.Data.Traffic.Entries) > 0 },
		Run:       w.clearTraffic,
	})
}
