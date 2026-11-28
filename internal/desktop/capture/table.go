package capture

import (
	"context"
	"maps"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// revealFrames is how many frames scrolling to the newest requests does not
// count as the user moving away from them.
const revealFrames = 3

type columnDefinition struct {
	key      logic.TrafficColumnKey
	title    func() string
	width    float32
	minWidth float32
	align    ui.Align
}

var columnDefinitions = []columnDefinition{
	{key: logic.ColumnID, title: locales.TrafficId, width: 80, minWidth: 60},
	{key: logic.ColumnMethod, title: locales.TrafficMethod, width: 100, minWidth: 70},
	{key: logic.ColumnHost, title: locales.TrafficHost, width: 200, minWidth: 120},
	{key: logic.ColumnPath, title: locales.TrafficPath, width: 0, minWidth: 120},
	{key: logic.ColumnProcess, title: locales.TrafficProcess, width: 160, minWidth: 120},
	{key: logic.ColumnStatusCode, title: locales.TrafficStatus, width: 80, minWidth: 70},
	{key: logic.ColumnType, title: locales.TrafficType, width: 80, minWidth: 70},
	{key: logic.ColumnDestination, title: locales.TrafficDestination, width: 150, minWidth: 120},
	{key: logic.ColumnProtocol, title: locales.TrafficProtocol, width: 100, minWidth: 80},
	{key: logic.ColumnDuration, title: locales.TrafficDuration, width: 100, minWidth: 80},
	{key: logic.ColumnSize, title: locales.TrafficSize, width: 100, minWidth: 80},
}

// TableDependencies are what a table needs besides its traffic.
type TableDependencies struct {
	Runtime    store.Runtime
	Settings   *store.SettingsStore
	Notifier   *widgets.Notifier
	Confirm    *widgets.ConfirmDialog
	Icons      *widgets.ProcessIcons
	Resend     func(ctx context.Context, id uint64, config proxyservice.ResendConfig) (proxyservice.ResendResult, error)
	OpenEditor func(entry *logic.TrafficEntry, body *proxyservice.TrafficBodyView, focus bool, historyKey string, done func(error))
	Exporter   *Exporter
	MacOS      bool
}

// Table shows traffic in a virtualized table: only the rows in view are built.
type Table struct {
	source Source
	deps   TableDependencies

	state       nui.KeyedTableState[*logic.TrafficEntry, uint64]
	selectedRow int
	lastSort    ui.SortOrder

	sorter       *logic.TrafficTableSorter
	rows         []*logic.TrafficEntry
	rowsVersion  uint64
	rowsSort     logic.TrafficSort
	rowsComputed bool

	fullOrder   []string
	layoutOrder []string
	layoutWidth map[string]float32
	loaded      bool

	hovered        bool
	wasAtEdge      bool
	lastFirst      int
	revealing      int
	observer       func(ui.InputEvent)
	menuSpans      []rowSpan
	focusRequested bool
	// historyShown is the HistoryKey the selection belongs to.
	historyShown string
	resend       resendDialog
	// clipboard is text waiting for the next build to copy, for work that
	// finishes outside one.
	clipboard *string

	// menu is the context menu of the rows and the header, and menuTarget
	// what it acts on.
	menu         nui.MenuState
	menuTarget   tableMenuTarget
	menuShown    bool
	menuRevision uint64
	// menuPress is where a right-click went down, relative to the table,
	// until the build after it finds what it pressed.
	menuPress *float32
	// headerBottom is where the header ended in the last frame, in window
	// coordinates.
	headerBottom float32

	// HistoryKey, when set, names the saved session the table shows.
	HistoryKey func() string
	// HistoryAlias, when set, returns the alias of the saved session the
	// table shows, "" for none.
	HistoryAlias func() string

	// CellBuilds counts built cells, to verify that a frame's work follows
	// the rows in view.
	CellBuilds int
}

// NewTable returns a table of source.
func NewTable(source Source, deps TableDependencies) *Table {
	t := &Table{source: source, deps: deps, selectedRow: -1, sorter: logic.NewTrafficTableSorter(source.Filter().ProcessingPolicy()), lastFirst: -1}
	t.state.List.Selected = &t.selectedRow
	t.state.List.DragSelect = true
	t.observer = t.observePointer
	return t
}

// Reset drops the rows, the sorter's remembered values, and the selection and
// scroll state, so the table keeps no entry of a source that was closed. The
// column layout and sort order stay.
func (t *Table) Reset() {
	columns, sort := t.state.List.Columns, t.state.Sort
	t.state = nui.KeyedTableState[*logic.TrafficEntry, uint64]{}
	t.state.List.Selected = &t.selectedRow
	t.state.List.DragSelect = true
	t.state.List.Columns, t.state.Sort = columns, sort
	t.selectedRow, t.lastFirst, t.revealing = -1, -1, 0
	t.sorter = logic.NewTrafficTableSorter(t.source.Filter().ProcessingPolicy())
	t.rows, t.rowsComputed, t.rowsVersion, t.rowsSort = nil, false, 0, logic.TrafficSort{}
	t.historyShown = ""
	t.menuTarget = tableMenuTarget{}
}

func trafficEntryID(entry *logic.TrafficEntry) uint64 { return entry.ID }

func (t *Table) trafficSort() logic.TrafficSort {
	if t.state.Sort.Column == "" {
		return logic.TrafficSort{}
	}
	order := logic.SortAsc
	if t.state.Sort.Descending {
		order = logic.SortDesc
	}
	return logic.TrafficSort{Key: logic.TrafficColumnKey(t.state.Sort.Column), Order: order}
}

func (t *Table) visibleColumns() []columnDefinition {
	hidden := map[string]bool{}
	for _, key := range t.deps.Settings.HiddenTrafficColumns() {
		hidden[key] = true
	}
	columns := make([]columnDefinition, 0, len(columnDefinitions))
	for _, column := range columnDefinitions {
		if !hidden[string(column.key)] {
			columns = append(columns, column)
		}
	}
	return columns
}

func (t *Table) loadLayout() {
	if t.loaded {
		return
	}
	t.loaded = true
	order, widths := t.deps.Settings.TrafficColumnLayout()
	t.fullOrder = logic.CompleteColumnOrder(order)
	t.state.List.Columns = ui.TableLayout{Order: slices.Clone(t.fullOrder), Widths: widths}
	t.layoutOrder, t.layoutWidth = slices.Clone(t.fullOrder), maps.Clone(widths)
}

// syncLayout merges the table's new column order into the full order and
// saves a change the user made.
func (t *Table) syncLayout(visible []columnDefinition) {
	order := t.state.List.Columns.Order
	if !slices.Equal(order, t.layoutOrder) {
		shown := make(map[string]bool, len(visible))
		for _, column := range visible {
			shown[string(column.key)] = true
		}
		movedVisible := make([]string, 0, len(visible))
		for _, key := range order {
			if shown[key] {
				movedVisible = append(movedVisible, key)
			}
		}
		t.fullOrder = logic.MergeVisibleColumnOrder(t.fullOrder, movedVisible)
		t.state.List.Columns.Order = slices.Clone(t.fullOrder)
		t.layoutOrder = slices.Clone(t.fullOrder)
		t.deps.Settings.SetTrafficColumnLayout(t.fullOrder, t.state.List.Columns.Widths)
		t.layoutWidth = maps.Clone(t.state.List.Columns.Widths)
		return
	}
	if !maps.Equal(t.state.List.Columns.Widths, t.layoutWidth) && !t.state.List.DragSelecting() {
		t.layoutWidth = maps.Clone(t.state.List.Columns.Widths)
		t.deps.Settings.SetTrafficColumnLayout(t.fullOrder, t.state.List.Columns.Widths)
	}
}

// applySortCycle turns the table's two-way header click into ascending,
// descending, then unsorted.
func (t *Table) applySortCycle(visible []columnDefinition) {
	if t.state.Sort != t.lastSort {
		if t.state.Sort.Column == t.lastSort.Column && t.lastSort.Descending && !t.state.Sort.Descending {
			t.state.Sort = ui.SortOrder{}
		}
		t.lastSort = t.state.Sort
	}
	if t.state.Sort.Column != "" && !slices.ContainsFunc(visible, func(column columnDefinition) bool { return string(column.key) == t.state.Sort.Column }) {
		t.state.Sort, t.lastSort = ui.SortOrder{}, ui.SortOrder{}
	}
}

func (t *Table) refreshRows() bool {
	filter := t.source.Filter()
	version := filter.Version()
	sort := t.trafficSort()
	if t.rowsComputed && version == t.rowsVersion && sort == t.rowsSort {
		return false
	}
	t.rowsComputed, t.rowsVersion, t.rowsSort = true, version, sort
	t.rows = t.sorter.Sort(filter.Filtered(), sort, version)
	if t.state.SetBorrowedRows(t.rows, trafficEntryID) {
		t.pruneSelection()
	}
	return true
}

// pruneSelection reconciles traffic details after Nuxt removes absent IDs.
func (t *Table) pruneSelection() {
	if t.state.Selection.Len() == 0 {
		t.selectedRow = -1
		t.focusEntry(nil)
		return
	}
	if focused := t.source.SelectedEntry(); focused != nil && t.state.Selection.Has(focused.ID) {
		return
	}
	for _, entry := range t.rows {
		if t.state.Selection.Has(entry.ID) {
			t.selectedRow = t.rowIndex(entry.ID)
			t.source.SelectEntry(entry)
			return
		}
	}
}

// followHistory starts another saved session at the top and clears its
// selection, focus and context-menu target. Sessions number their entries
// alike, so the rows chosen in one are not the same rows in another.
func (t *Table) followHistory() {
	if t.HistoryKey == nil {
		return
	}
	key := t.HistoryKey()
	if key == t.historyShown {
		return
	}
	t.historyShown = key
	t.state.Selection.Clear()
	t.selectedRow = -1
	t.menuPress, t.menuTarget = nil, tableMenuTarget{}
	t.menu.Open = false
	t.focusEntry(nil)
	t.state.List.ScrollTo(0, ui.Start)
}

func (t *Table) replaceSelection(index int) {
	t.state.Selection.Clear()
	entry := t.rows[index]
	t.state.Selection.Add(entry.ID)
	t.selectedRow = index
}

func (t *Table) applyPendingFocus() {
	id := t.source.PendingFocus()
	if id == 0 {
		return
	}
	index := slices.IndexFunc(t.rows, func(entry *logic.TrafficEntry) bool { return entry.ID == id })
	if index < 0 {
		return
	}
	t.replaceSelection(index)
	t.source.SelectEntry(t.rows[index])
	t.state.List.ScrollTo(index, ui.Start)
	t.source.ClearPendingFocus()
}

func (t *Table) atLatestEdge(first, last int) bool {
	return logic.IsAtLatestEdge(t.rowsSort, first, last, len(t.rows))
}

// followNewest keeps the newest requests in view while the user is at them.
func (t *Table) followNewest(rowsChanged bool) {
	live := t.source.Live()
	edge := logic.TrafficLatestEdge(t.rowsSort)
	t.state.List.FollowEnd = live != nil && edge == logic.EdgeEnd
	if live != nil && edge == logic.EdgeStart && rowsChanged && t.wasAtEdge && live.PendingLiveEntryCount() == 0 {
		t.state.List.ScrollTo(0, ui.Start)
	}
}

// holdWhileReading keeps the window in place while the pointer is over the
// table or the user has scrolled away from the newest requests.
func (t *Table) holdWhileReading(element ui.Element) {
	live := t.source.Live()
	first, last := t.state.List.Visible()
	atEdge := t.atLatestEdge(first, last)
	hovered := element.Hovered()
	if live != nil {
		moved := first != t.lastFirst && t.lastFirst >= 0
		if t.revealing > 0 {
			t.revealing--
		} else if (hovered && !t.hovered) || (moved && !atEdge) {
			live.PauseLiveEviction()
		}
		if !hovered && t.hovered && !t.state.List.DragSelecting() && atEdge && live.PendingLiveEntryCount() == 0 {
			live.ResumeLiveEviction()
		}
	}
	t.hovered, t.wasAtEdge, t.lastFirst = hovered, atEdge, first
}

func (t *Table) showNewRequests() {
	live := t.source.Live()
	if live == nil {
		return
	}
	t.revealing = revealFrames
	live.ResumeLiveEviction()
	t.refreshRows()
	newest := -1
	var newestID uint64
	for index, entry := range t.rows {
		if entry.ID > newestID {
			newest, newestID = index, entry.ID
		}
	}
	if newest >= 0 {
		t.state.List.ScrollTo(newest, ui.Start)
	}
	if t.hovered {
		live.PauseLiveEviction()
	}
}

// Build builds the table.
func (t *Table) Build(c *ui.Context, theme nui.Theme) {
	if t.clipboard != nil {
		c.WriteClipboard(*t.clipboard)
		t.clipboard = nil
	}
	t.loadLayout()
	visible := t.visibleColumns()
	t.applySortCycle(visible)
	t.followHistory()
	rowsChanged := t.refreshRows()
	t.applyPendingFocus()
	t.followNewest(rowsChanged)

	columns := make([]nui.TableColumn[*logic.TrafficEntry], len(visible))
	// Marked cells share paint callbacks without retaining row data.
	outlines := make(map[rowOutlineStyle]func(*ui.Painter, ui.Rect))
	order := t.displayOrder(visible)
	// A resized Path uses its saved width; an unsized Path stays flexible.
	flexPath := slices.ContainsFunc(visible, func(column columnDefinition) bool {
		return column.key == logic.ColumnPath && t.state.List.Columns.Widths[string(column.key)] <= 0
	})
	// Keep a flex tail after fixed columns and a small resize-grip tail after Path.
	fill := !flexPath || len(order) > 0 && order[len(order)-1] == string(logic.ColumnPath)
	var doubleClicked bool
	buildCell := func(key logic.TrafficColumnKey, first, last bool) func(*ui.Context, nui.Theme, ui.Element, nui.TableCell[*logic.TrafficEntry]) {
		return func(c *ui.Context, theme nui.Theme, element ui.Element, cell nui.TableCell[*logic.TrafficEntry]) {
			if first {
				t.noteRowSpan(element, cell.Index)
			}
			if element.RowDoubleClicked() {
				doubleClicked = true
			}
			t.cellBackground(element, theme, cell.Index, first, last, outlines)
			if key != "" {
				t.CellBuilds++
				t.cell(c, theme, cell.Value, key)
			}
		}
	}
	for index, column := range visible {
		key := string(column.key)
		columns[index] = nui.TableColumn[*logic.TrafficEntry]{
			Title: column.title(), ID: key, Width: column.width, MinWidth: column.minWidth, Align: column.align, Sortable: nui.Set(true),
			BuildCell: buildCell(column.key, key == order[0], !fill && key == order[len(order)-1]),
		}
	}
	if fill {
		var width float32
		if flexPath {
			width = 16
		}
		columns = append(columns, nui.TableColumn[*logic.TrafficEntry]{
			ID: "row-fill", Width: width, Fixed: true,
			BuildCell: buildCell("", len(order) == 0, true),
		})
	}
	t.state.List.HeaderCell = t.headerCell(c, theme, visible)
	t.menuSpans = t.menuSpans[:0]
	var element ui.Element
	ui.Box(c.Key("traffic-table-pane")).Fill().MinHeight(0).Background(theme.Background).Children(func() {
		element = widgets.Table(c.Key("traffic-table"), theme, &t.state, columns, nui.KeyedTableOptions[uint64]{
			Label: locales.TrafficColumns(), ManualSorting: true,
			BuildEmpty: func(*ui.Context, nui.Theme) {},
		})
		element.ObserveInput(t.observer).OnSubmit(func() {
			// Modifier double-clicks only change selection.
			if c.Modifiers() == 0 {
				if doubleClicked && t.source.ShowDetail() {
					t.CloseDetails()
				} else {
					t.openFocusedDetails()
				}
			}
		})
		if live := t.source.Live(); live != nil && live.PendingLiveEntryCount() > 0 {
			t.newRequestsButton(c, theme, live.PendingLiveEntryCount())
		}
	})
	t.resolveMenu(element)
	t.syncChoice(element.Changed() || rowsChanged)
	if t.focusRequested && t.state.List.Focus(c) {
		t.focusRequested = false
	}
	t.syncLayout(visible)
	t.holdWhileReading(element)
	var columnConfig any
	if current := t.deps.Settings.Current; current != nil {
		columnConfig = current.TrafficTableConfig
	}
	widgets.ContextMenu(c.Key("traffic-menu"), theme, element, &t.menu, t.menuItems, nui.MenuOptions{Label: t.menuLabel()}, t.menuRevision, t.rowsVersion, columnConfig, t.deps.Settings.SavingTrafficTableConfig, t.deps.Exporter != nil && t.deps.Exporter.Exporting)
	t.resend.build(c, theme, t)
}

func (t *Table) newRequestsButton(c *ui.Context, theme nui.Theme, count int) {
	icon := lucide.RefreshCw()
	switch logic.TrafficLatestEdge(t.rowsSort) {
	case logic.EdgeStart:
		icon = lucide.ArrowUpToLine()
	case logic.EdgeEnd:
		icon = lucide.ArrowDownToLine()
	}
	ui.Row(c).Absolute().Left(0).Right(0).Bottom(12).Justify(ui.Center).Children(func() {
		nui.Button(c.Key("show-new"), theme, locales.TrafficShowNewRequests(strconv.Itoa(count)), nui.ButtonOptions{Size: nui.SM, Leading: icon}).OnClick(t.showNewRequests)
	})
}

// cellBackground colors selected and highlighted rows, stripes other rows, and marks
// the entry the detail panel shows, and highlighted rows, with a bar along
// the row's start and a 1 px outline, as 1.0.5 did. Tints and outlines are
// stronger in the dark theme. first and last say whether the cell starts or
// ends the row as shown.
func (t *Table) cellBackground(cell ui.Element, theme nui.Theme, row int, first, last bool, outlines map[rowOutlineStyle]func(*ui.Painter, ui.Rect)) {
	entry := t.rows[row]
	chosen := t.state.Selection.Has(entry.ID)
	highlight := t.source.Highlight(entry.ID)
	// Opaque cell backgrounds cover the native table's hover tint.
	background := theme.Background
	switch {
	case chosen:
		background = selectionBackground(theme)
	case highlight != "":
		alpha := float32(0.15)
		if theme.Dark {
			alpha = 0.35
		}
		background = ui.Hex(highlight).Alpha(alpha).Over(background)
	case row%2 == 0:
		background = trafficStripeColor(theme).Over(background)
	}
	cell.Background(background)
	var bar, outline ui.Color
	switch {
	case t.source.SelectedEntry() != nil && t.source.SelectedEntry().ID == entry.ID:
		alpha := float32(0.22)
		if theme.Dark {
			alpha = 0.35
		}
		bar, outline = theme.Primary, theme.Primary.Alpha(alpha)
	case !chosen && highlight != "":
		alpha := float32(0.25)
		if theme.Dark {
			alpha = 0.45
		}
		bar, outline = ui.Hex(highlight), ui.Hex(highlight).Alpha(alpha)
	default:
		return
	}
	style := rowOutlineStyle{outline: outline, marker: bar, first: first, last: last}
	paint := outlines[style]
	if paint == nil {
		paint = style.paint
		outlines[style] = paint
	}
	cell.DrawOver(paint)
}

func cellText(c *ui.Context, theme nui.Theme, text string) ui.Element {
	return ui.Text(c, logic.TrafficCellPreview(text)).SingleLine().Shrink(1).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Text)
}

// numericText is a cell of numbers that line up across rows.
func numericText(c *ui.Context, theme nui.Theme, text string) ui.Element {
	return cellText(c, theme, text).FontFeatures("tnum")
}

func mutedText(c *ui.Context, theme nui.Theme, text string) ui.Element {
	return ui.Text(c, logic.TrafficCellPreview(text)).SingleLine().FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
}

func (t *Table) cell(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry, key logic.TrafficColumnKey) {
	switch key {
	case logic.ColumnID:
		cellText(c, theme, strconv.FormatUint(entry.ID, 10))
	case logic.ColumnProcess:
		t.processCell(c, theme, entry)
	case logic.ColumnMethod:
		if entry.Method == "" {
			mutedText(c, theme, logic.UnknownValue)
			return
		}
		badge(c, theme, logic.TrafficMethodLabel(entry), methodTone(entry.Method))
	case logic.ColumnHost:
		target := logic.TrafficTarget(entry)
		if target == "" {
			target = logic.UnknownValue
		}
		cellText(c, theme, target)
	case logic.ColumnPath:
		cellText(c, theme, logic.TrafficPathLabel(entry))
	case logic.ColumnStatusCode:
		switch {
		case entry.Error != nil:
			badge(c, theme, "ERR", toneError)
		case entry.StatusCode != 0:
			tone, _ := statusTone(entry.StatusCode)
			badge(c, theme, strconv.Itoa(entry.StatusCode), tone)
		default:
			mutedText(c, theme, logic.UnknownValue)
		}
	case logic.ColumnType:
		cellText(c, theme, logic.TrafficTypeLabel(entry))
	case logic.ColumnDestination:
		cellText(c, theme, logic.TrafficDestinationIP(entry))
	case logic.ColumnProtocol:
		protocol := logic.TrafficProtocol(entry)
		if protocol == "" {
			protocol = logic.UnknownValue
		}
		cellText(c, theme, protocol)
	case logic.ColumnDuration:
		numericText(c, theme, durationLabel(entry))
	case logic.ColumnSize:
		numericText(c, theme, sizeLabel(entry))
	}
}

func durationLabel(entry *logic.TrafficEntry) string {
	duration, known := logic.TrafficTotalDurationMicros(entry)
	if !known {
		return logic.FormatDurationMicros(0, -1)
	}
	return logic.FormatDurationMicros(0, duration)
}

func sizeLabel(entry *logic.TrafficEntry) string {
	size, known := logic.TrafficTotalSizeBytes(entry)
	if !known {
		return logic.FormatFileSize(logic.UnknownSize)
	}
	return logic.FormatFileSize(size)
}

func processName(process *proxyservice.ProcessInfo) string {
	switch {
	case process.DisplayName != "":
		return process.DisplayName
	case process.ProcessName != "":
		return process.ProcessName
	case process.PID != 0:
		return locales.TrafficProcessPid(strconv.FormatUint(uint64(process.PID), 10))
	default:
		return ""
	}
}

func (t *Table) processCell(c *ui.Context, theme nui.Theme, entry *logic.TrafficEntry) {
	if entry.Metadata == nil || entry.Metadata.Process == nil {
		mutedText(c, theme, logic.UnknownValue)
		return
	}
	process := entry.Metadata.Process
	switch process.Status {
	case proxyservice.ProcessStatusPending:
		ui.Row(c).MinWidth(0).AlignItems(ui.Center).Gap(6).Children(func() {
			widgets.Spinner(c, theme).Size(14, 14).TextColor(theme.Muted)
			mutedText(c, theme, locales.TrafficProcessIdentifying())
		})
	case proxyservice.ProcessStatusResolved:
		ui.Row(c).MinWidth(0).AlignItems(ui.Center).Gap(6).Children(func() {
			if t.deps.Icons != nil {
				widgets.ProcessIcon(c, t.deps.Icons, process.IconKey, 16, theme.Muted).Shrink(0)
			}
			cellText(c, theme, processName(process))
		})
	case proxyservice.ProcessStatusRemote:
		mutedText(c, theme, locales.TrafficProcessRemote())
	default:
		mutedText(c, theme, logic.UnknownValue)
	}
}

// columnMenuItems lets the user show and hide columns, as 1.0.5's header
// menu did: choosing a column keeps the menu open.
func (t *Table) columnMenuItems() []nui.MenuItem {
	settings := t.deps.Settings
	visible := t.visibleColumns()
	shown := make(map[logic.TrafficColumnKey]bool, len(visible))
	for _, column := range visible {
		shown[column.key] = true
	}
	saving := settings.SavingTrafficTableConfig
	onFailure := func(err error) {
		if err != nil {
			t.deps.Notifier.Error(locales.TrafficColumnSettingsSaveFailed(err.Error()))
		}
	}
	items := make([]nui.MenuItem, 0, len(columnDefinitions)+3)
	items = append(items, nui.MenuItem{ID: "columns", Label: locales.TrafficColumns(), Kind: nui.MenuLabel})
	for _, column := range columnDefinitions {
		checked := shown[column.key]
		items = append(items, nui.MenuItem{
			ID: "column-" + string(column.key), Label: column.title(), Checked: nui.Set(checked), Close: nui.Set(false),
			Disabled:        saving || (checked && len(visible) == 1),
			OnCheckedChange: func(on bool) { settings.SetTrafficColumnVisible(column.key, on, onFailure) },
		})
	}
	return append(items, widgets.MenuSeparator("columns-separator"), nui.MenuItem{
		ID: "show-all-columns", Label: locales.TrafficShowAllColumns(), Icon: lucide.ListChecks(), Close: nui.Set(false),
		Disabled: saving || len(visible) == len(columnDefinitions),
		OnSelect: func() { settings.ShowAllTrafficColumns(onFailure) },
	})
}
