package capture

import (
	"context"
	"maps"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
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

	list        ui.ListState
	selection   ui.Selection[uint64]
	selectedRow int
	sort        ui.SortOrder
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

	hovered   bool
	wasAtEdge bool
	lastFirst int
	revealing int
	// gesture applies the 1.0.5 click model over MyGo's selection.
	gesture  rowGesture
	observer func(ui.InputEvent)
	// historyShown is the HistoryKey the selection belongs to.
	historyShown string
	resend       resendDialog
	// clipboard is text waiting for the next build to copy, for work that
	// finishes outside one.
	clipboard *string

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
	t := &Table{source: source, deps: deps, selectedRow: -1, sorter: logic.NewTrafficTableSorter(), lastFirst: -1}
	t.list.Selected = &t.selectedRow
	t.list.Selection = &t.selection
	t.list.DragSelect = true
	t.list.Sort = &t.sort
	t.list.Key = func(row int) any {
		if row < len(t.rows) {
			return t.rows[row].ID
		}
		return uint64(0)
	}
	t.list.HeaderMenu = t.headerMenu
	t.observer = t.observePointer
	return t
}

// Rows returns the rows in table order.
func (t *Table) Rows() []*logic.TrafficEntry { return t.rows }

func (t *Table) trafficSort() logic.TrafficSort {
	if t.sort.Column == "" {
		return logic.TrafficSort{}
	}
	order := logic.SortAsc
	if t.sort.Descending {
		order = logic.SortDesc
	}
	return logic.TrafficSort{Key: logic.TrafficColumnKey(t.sort.Column), Order: order}
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
	t.list.Columns = ui.TableLayout{Order: slices.Clone(t.fullOrder), Widths: widths}
	t.layoutOrder, t.layoutWidth = slices.Clone(t.fullOrder), maps.Clone(widths)
}

// syncLayout merges the table's new column order into the full order and
// saves a change the user made.
func (t *Table) syncLayout(visible []columnDefinition) {
	order := t.list.Columns.Order
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
		t.list.Columns.Order = slices.Clone(t.fullOrder)
		t.layoutOrder = slices.Clone(t.fullOrder)
		t.deps.Settings.SetTrafficColumnLayout(t.fullOrder, t.list.Columns.Widths)
		t.layoutWidth = maps.Clone(t.list.Columns.Widths)
		return
	}
	if !maps.Equal(t.list.Columns.Widths, t.layoutWidth) && !t.list.DragSelecting() {
		t.layoutWidth = maps.Clone(t.list.Columns.Widths)
		t.deps.Settings.SetTrafficColumnLayout(t.fullOrder, t.list.Columns.Widths)
	}
}

// applySortCycle turns the table's two-way header click into ascending,
// descending, then unsorted.
func (t *Table) applySortCycle(visible []columnDefinition) {
	if t.sort != t.lastSort {
		if t.sort.Column == t.lastSort.Column && t.lastSort.Descending && !t.sort.Descending {
			t.sort = ui.SortOrder{}
		}
		t.lastSort = t.sort
	}
	if t.sort.Column != "" && !slices.ContainsFunc(visible, func(column columnDefinition) bool { return string(column.key) == t.sort.Column }) {
		t.sort, t.lastSort = ui.SortOrder{}, ui.SortOrder{}
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
	t.rows = t.sorter.Sort(filter.Filtered(), sort)
	return true
}

// pruneSelection removes rows that left the table and, as 1.0.5 did, moves
// the focus to the first row still selected in table order.
func (t *Table) pruneSelection() {
	if t.selection.Len() == 0 {
		return
	}
	present := make(map[uint64]bool, len(t.rows))
	for _, entry := range t.rows {
		present[entry.ID] = true
	}
	for id := range t.selection.All() {
		if !present[id] {
			t.selection.Remove(id)
		}
	}
	if t.selection.Len() == 0 {
		t.selectedRow = -1
		t.source.SelectEntry(nil)
		return
	}
	if focused := t.source.SelectedEntry(); focused != nil && t.selection.Has(focused.ID) {
		return
	}
	for _, entry := range t.rows {
		if t.selection.Has(entry.ID) {
			t.source.SelectEntry(entry)
			return
		}
	}
}

// followHistory clears the selection, the focus and any gesture when the
// table shows another saved session, as 1.0.5 did: sessions number their
// entries alike, so the rows chosen in one are not the same rows in another.
func (t *Table) followHistory() {
	if t.HistoryKey == nil {
		return
	}
	key := t.HistoryKey()
	if key == t.historyShown {
		return
	}
	t.historyShown = key
	t.selection.Clear()
	t.selectedRow = -1
	t.gesture = rowGesture{}
	t.focusEntry(nil)
}

func (t *Table) replaceSelection(index int) {
	t.selection.Clear()
	entry := t.rows[index]
	t.selection.Add(entry.ID)
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
	t.list.ScrollTo(index, ui.Start)
	t.source.ClearPendingFocus()
}

func (t *Table) atLatestEdge(first, last int) bool {
	return logic.IsAtLatestEdge(t.rowsSort, first, last, len(t.rows))
}

// followNewest keeps the newest requests in view while the user is at them.
func (t *Table) followNewest(rowsChanged bool) {
	edge := logic.TrafficLatestEdge(t.rowsSort)
	t.list.FollowEnd = edge == logic.EdgeEnd
	if edge == logic.EdgeStart && rowsChanged && t.wasAtEdge && (t.source.Live() == nil || t.source.Live().PendingLiveEntryCount() == 0) {
		t.list.ScrollTo(0, ui.Start)
	}
}

// holdWhileReading keeps the window in place while the pointer is over the
// table or the user has scrolled away from the newest requests.
func (t *Table) holdWhileReading(element ui.Element) {
	live := t.source.Live()
	first, last := t.list.Visible()
	atEdge := t.atLatestEdge(first, last)
	hovered := element.Hovered()
	if live != nil {
		moved := first != t.lastFirst && t.lastFirst >= 0
		if t.revealing > 0 {
			t.revealing--
		} else if (hovered && !t.hovered) || (moved && !atEdge) {
			live.PauseLiveEviction()
		}
		if !hovered && t.hovered && !t.list.DragSelecting() && atEdge && live.PendingLiveEntryCount() == 0 {
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
		t.list.ScrollTo(newest, ui.Start)
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
	if rowsChanged {
		t.pruneSelection()
	}
	t.applyPendingFocus()
	t.followNewest(rowsChanged)

	columns := make([]ui.TableColumn, len(visible))
	for index, column := range visible {
		columns[index] = ui.TableColumn{
			Title: column.title(), ID: string(column.key), Width: column.width, MinWidth: column.minWidth, Align: column.align, Sortable: true,
		}
	}
	t.list.CellMenu = func(menu *ui.Menu, row, _ int) { t.rowMenu(c, menu, row) }
	t.list.HeaderCell = t.headerCell(c, theme, visible)
	order := t.displayOrder(visible)
	t.beginSelection()
	var element ui.Element
	ui.Themed(c, tableTheme(theme)).Fill().MinHeight(0).Children(func() {
		element = ui.Table(c.Key("traffic-table"), &t.list, columns, len(t.rows), func(row, col int) {
			t.CellBuilds++
			key := string(visible[col].key)
			if key == order[0] {
				t.noteRowSpan(cellFill(c, "row-span"), row)
			}
			t.cellBackground(c, theme, row, key == order[0], key == order[len(order)-1])
			t.cell(c, theme, t.rows[row], visible[col].key)
		}).Grow(1).Basis(0).MinHeight(0).FillWidth().Label(locales.TrafficColumns()).ObserveInput(t.observer)
		if live := t.source.Live(); live != nil && live.PendingLiveEntryCount() > 0 {
			t.newRequestsButton(c, theme, live.PendingLiveEntryCount())
		}
	})
	handled := t.resolveSelection(element)
	t.syncChoice(!handled && element.Changed())
	t.syncLayout(visible)
	t.holdWhileReading(element)
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

// cellFill fills a cell's box behind its content.
func cellFill(c *ui.Context, key string) ui.Element {
	return ui.Box(c.Key(key)).Absolute().Top(0).Bottom(0).Left(0).Right(0).ZIndex(-1).IgnoreInput(true)
}

// cellBackground stripes every other row, colors highlighted rows, and marks
// the entry the detail panel shows, and highlighted rows, with a bar along
// the row's start and a 1 px outline, as 1.0.5 did. Tints and outlines are
// stronger in the dark theme. first and last say whether the cell starts or
// ends the row as shown.
func (t *Table) cellBackground(c *ui.Context, theme nui.Theme, row int, first, last bool) {
	entry := t.rows[row]
	chosen := t.selection.Has(entry.ID)
	highlight := t.source.Highlight(entry.ID)
	switch {
	case chosen:
	case highlight != "":
		alpha := float32(0.15)
		if theme.Dark {
			alpha = 0.35
		}
		cellFill(c, "highlight").Background(ui.Hex(highlight).Alpha(alpha))
	case row%2 == 0:
		cellFill(c, "stripe").Background(theme.Elevated.Alpha(0.52))
	}
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
	rowOutline(c, outline, first, last)
	if first {
		ui.Box(c.Key("marker")).Absolute().Top(0).Bottom(0).Left(0).Width(3).Background(bar).IgnoreInput(true).Label("row-marker")
	}
}

func cellText(c *ui.Context, theme nui.Theme, text string) ui.Element {
	return ui.Text(c, text).SingleLine().Shrink(1).FontSize(14).TextColor(theme.Text)
}

// numericText is a cell of numbers that line up across rows.
func numericText(c *ui.Context, theme nui.Theme, text string) ui.Element {
	return cellText(c, theme, text).FontFeatures("tnum")
}

func mutedText(c *ui.Context, theme nui.Theme, text string) ui.Element {
	return ui.Text(c, text).SingleLine().FontSize(14).TextColor(theme.Muted)
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
			ui.Spinner(c).Size(14, 14).TextColor(theme.Muted)
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

func (t *Table) headerMenu(menu *ui.Menu, _ int) {
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
	for _, column := range columnDefinitions {
		checked := shown[column.key]
		item := menu.Item(column.title()).Checked(checked).Disabled(saving || (checked && len(visible) == 1))
		if item.Chosen() {
			settings.SetTrafficColumnVisible(column.key, !checked, onFailure)
		}
	}
	menu.Separator()
	if menu.Item(locales.TrafficShowAllColumns()).Icon(widgets.ShowAllMenuIcon()).Disabled(saving || len(visible) == len(columnDefinitions)).Chosen() {
		settings.ShowAllTrafficColumns(onFailure)
	}
}
