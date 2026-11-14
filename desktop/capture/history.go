package capture

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/logic"
	desktopruntime "github.com/josexy/flowlens/desktop/runtime"
	"github.com/josexy/flowlens/desktop/store"
	apptheme "github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const (
	historyItemPrefix = "history:"
	jobPickHAR        = "capture:pick-har"
)

// HistorySource shows a saved session.
type HistorySource struct {
	History *store.HistoryStore
}

// Filter implements Source.
func (s HistorySource) Filter() *store.FilterStore { return s.History.Filter }

// SelectedEntry implements Source.
func (s HistorySource) SelectedEntry() *logic.TrafficEntry { return s.History.Traffic.Selected }

// SelectEntry implements Source.
func (s HistorySource) SelectEntry(entry *logic.TrafficEntry) { s.History.Traffic.Select(entry) }

// ShowDetail implements Source.
func (s HistorySource) ShowDetail() bool { return s.History.Traffic.ShowDetailPanel }

// SetShowDetail implements Source.
func (s HistorySource) SetShowDetail(show bool) { s.History.Traffic.SetShowDetailPanel(show) }

// Highlight implements Source.
func (s HistorySource) Highlight(id uint64) string { return s.History.Traffic.Highlights[id] }

// SetHighlight implements Source.
func (s HistorySource) SetHighlight(id uint64, color string) {
	s.History.Traffic.SetHighlight(id, color)
}

// Delete implements Source.
func (s HistorySource) Delete(ids []uint64) { s.History.Traffic.Delete(ids) }

// BodyView implements Source.
func (s HistorySource) BodyView(id uint64, done func(*proxyservice.TrafficBodyView)) {
	s.History.Traffic.GetBodyView(id, done)
}

// PendingFocus implements Source.
func (s HistorySource) PendingFocus() uint64 { return s.History.Traffic.PendingFocusID }

// ClearPendingFocus implements Source.
func (s HistorySource) ClearPendingFocus() { s.History.Traffic.PendingFocusID = 0 }

// SetSelectedCount implements Source.
func (s HistorySource) SetSelectedCount(count int) { s.History.Traffic.SelectedCount = count }

// Live implements Source.
func (s HistorySource) Live() LiveControl { return nil }

func historyKeyOf(item string) (string, bool) { return strings.CutPrefix(item, historyItemPrefix) }

// selectedHistory returns the opened history's key, if one is shown.
func (w *Workspace) selectedHistory() (string, bool) {
	if w.Data.History == nil {
		return "", false
	}
	return historyKeyOf(w.Data.Workbench.CaptureSelection)
}

func (w *Workspace) openHistory(key string) {
	w.selectItem(historyItemPrefix + key)
	w.Data.History.Select(key)
}

func historyLabel(metadata *proxyservice.HistoryMetadata) string {
	if metadata.Alias != "" {
		return metadata.Alias
	}
	if metadata.CreatedAt == 0 {
		return "-"
	}
	return time.UnixMilli(metadata.CreatedAt).Format("2006-01-02")
}

func historyTime(metadata *proxyservice.HistoryMetadata) string {
	if metadata.CreatedAt == 0 {
		return "-"
	}
	return time.UnixMilli(metadata.CreatedAt).Format("15:04:05")
}

func (w *Workspace) importLabel() string {
	history := w.Data.History
	if history.Importing && history.TotalFiles > 0 {
		return locales.HarImportProgress(strconv.Itoa(history.CurrentFile), strconv.Itoa(history.TotalFiles))
	}
	return locales.HarImportTitle()
}

// historySidebar builds the list of saved sessions.
func (w *Workspace) historySidebar(c *ui.Context, theme nui.Theme) {
	history := w.Data.History
	if history == nil {
		return
	}
	header := ui.Row(c).FillWidth().MinHeight(38).Shrink(0).Margin(6, 0, 0, 0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Padding(0, 8, 0, 10).Background(theme.Elevated)
	header.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border)
	header.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, locales.HistoryTitle()).FontSize(apptheme.TextSize(c, 14)).FontWeight(600).TextColor(theme.Muted)
			ui.Box(c).PaddingX(6).PaddingY(1).Radius(8).Background(theme.Elevated.Mix(theme.Background, 0.18)).Children(func() {
				ui.Text(c, strconv.Itoa(len(history.Metadata))).SingleLine().FontSize(apptheme.TextSize(c, 14)).LineHeight(1.6).TextColor(theme.Muted)
			})
		})
		ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
			toolbarButton(c, theme, toolButton{key: "import", icon: lucide.FileInput(), tooltip: w.importLabel(), disabled: history.Importing || w.clearingHistory, run: func() { w.ImportHAR(nil) }})
			toolbarButton(c, theme, toolButton{key: "refresh", icon: lucide.RefreshCw(), tooltip: locales.HistoryRefresh(), disabled: history.Loading, run: history.LoadList})
			toolbarButton(c, theme, toolButton{key: "clear-all", icon: lucide.Trash(), tooltip: locales.HistoryClearAll(), disabled: len(history.Metadata) == 0 || w.clearingHistory || history.Importing, run: w.askClearHistories})
		})
	})
	ui.Column(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
		switch {
		case history.Loading && len(history.Metadata) == 0:
			ui.Column(c).Fill().Center().Children(func() { ui.Spinner(c).Size(20, 20).TextColor(theme.Muted) })
		case len(history.Metadata) == 0:
			ui.Column(c).Fill().Center().Padding(16).Children(func() {
				ui.Text(c, locales.HistoryEmpty()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
			})
		default:
			ui.Scroll(c).Fill().Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					for _, metadata := range history.Metadata {
						w.historyRow(c, theme, metadata)
					}
				})
			})
		}
	})
	w.historyMenus.Sweep()
}

func (w *Workspace) historyRow(c *ui.Context, theme nui.Theme, metadata *proxyservice.HistoryMetadata) {
	key := metadata.Key
	active := w.Data.Workbench.CaptureSelection == historyItemPrefix+key
	row := ui.ButtonBase(c.Key(key)).MinHeight(44).Justify(ui.Start).Gap(8).PaddingX(10).Margin(4, 8, 0, 8).Radius(10).FocusRing(true).AccessSelected(active).Label(historyLabel(metadata))
	switch {
	case active:
		row.Background(SidebarSelectedColor(theme)).Border(1, theme.BorderAccented)
	case row.Hovered():
		row.Background(theme.Elevated).Border(1, ui.Transparent)
	default:
		row.Border(1, ui.Transparent)
	}
	row.OnClick(func() { w.openHistory(key) })
	row.Children(func() {
		color := theme.Muted.Alpha(0.7)
		if active {
			color = theme.Primary
		}
		ui.Icon(c, lucide.Archive()).Size(15, 15).Shrink(0).TextColor(color)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, historyLabel(metadata)).SingleLine().FontSize(apptheme.TextSize(c, 14)).FontWeight(600).TextColor(theme.Text)
			ui.Text(c, historyTime(metadata)).FontSize(apptheme.TextSize(c, 12)).LineHeight(16.0 / 12.0).TextColor(theme.Muted)
		})
		CountPill(c, theme, strconv.Itoa(metadata.Total), active)
	})
	widgets.ContextMenu(c.Key("menu:"+key), theme, row, w.historyMenus.For(key), func() []nui.MenuItem { return w.historyMenuItems(metadata) }, nui.MenuOptions{Label: historyLabel(metadata)})
}

// historyMenuItems are the actions on a saved session.
func (w *Workspace) historyMenuItems(metadata *proxyservice.HistoryMetadata) []nui.MenuItem {
	disabled := w.exporter == nil || w.exporter.Exporting || !logic.IsHARExportableHistoryFormat(int(metadata.FormatVersion))
	return []nui.MenuItem{
		sessionExportItem(disabled, func(format logic.TrafficExportFormat) {
			hint := metadata.Alias
			if hint == "" {
				hint = metadata.Key
			}
			w.exporter.Export(format, ExportOptions{HistoryKey: metadata.Key, FilenameHint: hint})
		}),
		{ID: "delete", Label: locales.HistoryDelete(), Icon: lucide.Trash(), Color: nui.Error, OnSelect: func() { w.askDeleteHistory(metadata) }},
	}
}

// sessionExportItem is the Export submenu of the live capture or a saved
// session: CSV and HAR.
func sessionExportItem(disabled bool, export func(logic.TrafficExportFormat)) nui.MenuItem {
	formats := []logic.TrafficExportFormat{logic.ExportCSV, logic.ExportHAR}
	children := make([]nui.MenuItem, len(formats))
	for index, format := range formats {
		children[index] = nui.MenuItem{ID: "export-" + string(format), Label: exportFormatLabel(format), Disabled: disabled, OnSelect: func() { export(format) }}
	}
	return nui.MenuItem{ID: "export", Label: locales.TrafficExportTitle(), Icon: lucide.FileDown(), Children: children}
}

func (w *Workspace) askDeleteHistory(metadata *proxyservice.HistoryMetadata) {
	key := metadata.Key
	w.Confirm.Ask(widgets.ConfirmRequest{
		Title: locales.HistoryDelete(), Message: locales.HistoryDeleteConfirm(), CancelLabel: locales.HistoryCancel(),
		Confirm: widgets.ConfirmAction{Label: locales.HistoryDelete(), Color: nui.Error, Run: func() {
			w.Data.History.Delete(key, func(err error) {
				if err != nil {
					w.Notifier.Error(err.Error())
					return
				}
				if w.Data.Workbench.CaptureSelection == historyItemPrefix+key {
					w.Data.Workbench.SelectCaptureItem(store.CaptureItemLive)
				}
				w.Notifier.Success(locales.HistoryDeleted())
			})
		}},
	})
}

func (w *Workspace) askClearHistories() {
	w.Confirm.Ask(widgets.ConfirmRequest{
		Title: locales.HistoryClearAll(), Message: locales.HistoryClearAllConfirm(), CancelLabel: locales.HistoryCancel(),
		Confirm: widgets.ConfirmAction{Label: locales.HistoryClearAll(), Color: nui.Error, Run: func() {
			w.clearingHistory = true
			w.Data.History.ClearAll(func(err error) {
				w.clearingHistory = false
				if err != nil {
					w.Notifier.Error(err.Error())
					return
				}
				if _, open := w.selectedHistory(); open {
					w.Data.Workbench.SelectCaptureItem(store.CaptureItemLive)
				}
			})
		}},
	})
}

// How long HAR import results stay, as in FlowLens 1.0.5: longer when a file
// failed, so its reason can be read.
const (
	harImportResultTime = 5 * time.Second
	harImportFailedTime = 8 * time.Second
)

// ImportHAR imports paths, or asks for files when paths is nil.
func (w *Workspace) ImportHAR(paths []string) {
	history := w.Data.History
	if history == nil || history.Importing {
		return
	}
	if paths != nil {
		w.startImport(paths)
		return
	}
	if w.Host.PickHARFiles == nil {
		return
	}
	desktopruntime.Run(w.Data.Runtime.Jobs, jobPickHAR, func(context.Context) ([]string, error) {
		return w.Host.PickHARFiles()
	}, func(chosen []string, err error) {
		if err != nil {
			w.Notifier.Error(locales.HarImportFailed(err.Error()))
			return
		}
		w.startImport(chosen)
	})
}

func (w *Workspace) startImport(paths []string) {
	w.Data.History.ImportHAR(paths, runtime.GOOS == "windows", w.finishImport)
}

func (w *Workspace) finishImport(summary store.HARImportSummary) {
	history := w.Data.History
	if summary.First != nil {
		history.LoadList()
		w.openHistory(summary.First.Key)
		// The list refresh is asynchronous. Use the import result's metadata
		// now, rather than leaving the new tab named after its internal key.
		if workspace := w.Data.Workspace; workspace != nil {
			workspace.OpenHistoryTab(summary.First.Key, historyTabTitle(summary.First))
		}
	}
	result := locales.HarImportResult(strconv.Itoa(summary.Failed), strconv.Itoa(summary.Files), strconv.Itoa(summary.Imported), strconv.Itoa(summary.Missing), strconv.Itoa(summary.Skipped))
	first := ""
	if summary.FirstFailedFile != "" {
		reason := summary.FirstFailure
		if summary.NoEntries {
			reason = locales.HarImportNoEntries()
		}
		first = locales.HarImportFileFailed(reason, summary.FirstFailedFile)
	}
	failed, finished := w.Notifier.WithTimeout(harImportFailedTime), w.Notifier.WithTimeout(harImportResultTime)
	switch {
	case summary.Failed > 0 && summary.Files == 0:
		failed.Error(first)
	case summary.Failed > 0:
		failed.Titled(widgets.SeverityWarning, first, result)
	case summary.Skipped > 0 || summary.Missing > 0:
		finished.Warning(result)
	default:
		finished.Success(result)
	}
}

// dropTarget makes content accept dropped HAR files.
func (w *Workspace) dropTarget(c *ui.Context, theme nui.Theme, element ui.Element) {
	if w.Data.History == nil || w.Data.History.Importing {
		return
	}
	if dropped := element.DroppedFiles(); len(dropped) > 0 {
		w.ImportHAR(dropped)
		return
	}
	if !element.FileDragOver() {
		return
	}
	ui.Overlay(c, func() {
		bounds := element.Bounds()
		ui.Box(c).Absolute().Left(bounds.X).Top(bounds.Y).Width(bounds.W).Height(bounds.H).Center().IgnoreInput(true).
			Background(theme.Elevated.Alpha(0.9)).Border(2, theme.Primary).Children(func() {
			ui.Text(c, locales.HarImportDropHint()).FontSize(apptheme.TextSize(c, 14)).Bold().TextColor(theme.Primary)
		})
	})
}

// historyPane builds the opened saved session.
func (w *Workspace) historyPane(c *ui.Context, theme nui.Theme) {
	history := w.Data.History
	if history.Traffic.Key == "" && !history.LoadingHistory {
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, locales.HistorySelectHint()).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
		})
		return
	}
	BuildFilterBar(c, theme, history.Filter, &w.historyFilterFocus)
	area := ui.Box(c).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() { w.historyTableAndDetail(c, theme) })
	w.dropTarget(c, theme, area)
}

func (w *Workspace) historyTableAndDetail(c *ui.Context, theme nui.Theme) {
	traffic := w.Data.History.Traffic
	if w.HistoryDetail == nil || traffic.Selected == nil || !traffic.ShowDetailPanel {
		w.historyTable(c, theme)
		return
	}
	w.buildSplit(c, theme, "history", func() { w.historyTable(c, theme) }, func() { w.HistoryDetail(c, theme) })
}

func (w *Workspace) historyTable(c *ui.Context, theme nui.Theme) {
	loading := w.Data.History.LoadingHistory
	ui.Box(c.Key("history-table")).Fill().MinWidth(0).MinHeight(0).Children(func() {
		// Keep the table mounted so its header, rows and scroll position remain
		// visible while the next session loads, without accepting row actions.
		ui.Box(c.Key("content")).Fill().Disabled(loading).DimDisabled(false).Children(func() {
			w.HistoryTable.Build(c, theme)
		})
		if loading {
			ui.Column(c.Key("loading")).Absolute().Top(0).Bottom(0).Left(0).Right(0).ZIndex(1).
				Center().Background(theme.Background.Alpha(0.65)).Role(ui.RoleStatus).Label(locales.AppLoading()).Busy(true).Children(func() {
				ui.Spinner(c).Size(24, 24).TextColor(theme.Primary)
			})
		}
	})
}
