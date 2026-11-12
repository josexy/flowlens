package capture

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
)

const (
	maxEditEntries      = 50
	quickResendCount    = 5
	quickResendInterval = 1000
)

type highlightColor struct {
	name  string
	color string
	label func() string
}

var highlightColors = []highlightColor{
	{"red", "#ef4444", locales.ContextMenuTrafficTableHighlightRed},
	{"orange", "#f97316", locales.ContextMenuTrafficTableHighlightOrange},
	{"yellow", "#eab308", locales.ContextMenuTrafficTableHighlightYellow},
	{"green", "#22c55e", locales.ContextMenuTrafficTableHighlightGreen},
	{"blue", "#3b82f6", locales.ContextMenuTrafficTableHighlightBlue},
	{"purple", "#a855f7", locales.ContextMenuTrafficTableHighlightPurple},
}

var errRequestBodyUnavailable = errors.New("request body unavailable")

// selectedEntries returns the chosen rows in table order.
func (t *Table) selectedEntries() []*logic.TrafficEntry {
	entries := make([]*logic.TrafficEntry, 0, t.selection.Len())
	for _, entry := range t.rows {
		if t.selection.Has(entry.ID) {
			entries = append(entries, entry)
		}
	}
	return entries
}

func allOf(entries []*logic.TrafficEntry, match func(*logic.TrafficEntry) bool) bool {
	return len(entries) > 0 && !slices.ContainsFunc(entries, func(entry *logic.TrafficEntry) bool { return !match(entry) })
}

func anyOf(entries []*logic.TrafficEntry, match func(*logic.TrafficEntry) bool) bool {
	return slices.ContainsFunc(entries, match)
}

// tableMenuTarget is what the table's context menu acts on: the header, or
// rows, or nothing, which opens no menu.
type tableMenuTarget struct {
	header bool
	ids    []uint64
}

// resolveMenu finds what a right-click pressed, once the table has built,
// as 1.0.5 did: a row not selected becomes the selection, and its entry the
// focus, a row selected keeps the selection, the header has the column menu,
// and the space past the rows has none. The Menu key and Shift+F10 open the
// menu of the selection.
func (t *Table) resolveMenu(table ui.Element) {
	opened := t.menu.Open && !t.menuShown
	t.menuShown = t.menu.Open
	if press := t.menuPress; press != nil {
		t.menuPress = nil
		top := table.Bounds().Y
		switch id := t.rowAt(top, *press); {
		case id != 0:
			if index := t.rowIndex(id); index >= 0 && !t.selection.Has(id) {
				t.replaceSelection(index)
				t.focusEntry(t.rows[index])
			}
			t.menuTarget = tableMenuTarget{ids: t.selectedIDs()}
		case t.headerBottom > 0 && top+*press < t.headerBottom:
			t.menuTarget = tableMenuTarget{header: true}
		default:
			t.menuTarget = tableMenuTarget{}
		}
		return
	}
	if opened {
		t.menuTarget = tableMenuTarget{ids: t.selectedIDs()}
	}
}

// selectedIDs returns the IDs of the rows selected, in table order.
func (t *Table) selectedIDs() []uint64 {
	entries := t.selectedEntries()
	ids := make([]uint64, len(entries))
	for index, entry := range entries {
		ids[index] = entry.ID
	}
	return ids
}

// menuItems returns the items of the table's context menu.
func (t *Table) menuItems() []nui.MenuItem {
	if t.menuTarget.header {
		return t.columnMenuItems()
	}
	entries := make([]*logic.TrafficEntry, 0, len(t.menuTarget.ids))
	for _, id := range t.menuTarget.ids {
		if index := t.rowIndex(id); index >= 0 {
			entries = append(entries, t.rows[index])
		}
	}
	return t.entryMenuItems(entries)
}

// menuLabel names the column menu; the rows' menu, as 1.0.5's, has no name.
func (t *Table) menuLabel() string {
	if t.menuTarget.header {
		return locales.TrafficColumns()
	}
	return ""
}

// entryMenuItems returns the actions on entries, as 1.0.5's traffic menu
// listed them, or none without entries.
func (t *Table) entryMenuItems(entries []*logic.TrafficEntry) []nui.MenuItem {
	if len(entries) == 0 {
		return nil
	}
	first := entries[0]
	multiple := len(entries) > 1
	items := []nui.MenuItem{{
		ID: "show-details", Label: locales.ContextMenuTrafficTableShowDetails(), Disabled: multiple,
		OnSelect: func() {
			t.source.SelectEntry(first)
			t.source.SetShowDetail(true)
		},
	}}
	if t.deps.Exporter != nil {
		items = append(items, t.exportMenuItem(entries))
	}
	canCurl := func(entry *logic.TrafficEntry) bool { return logic.CapabilitiesOf(entry).CanCopyCurl }
	if anyOf(entries, canCurl) {
		items = append(items, widgets.MenuSeparator("d1"),
			nui.MenuItem{ID: "copy-curl", Label: locales.ContextMenuTrafficTableCopyCurl(), OnSelect: func() { t.copyCurl(entries) }})
	}
	items = append(items, widgets.MenuSeparator("d2"), t.copyMenuItem(entries))
	if allOf(entries, func(entry *logic.TrafficEntry) bool { return logic.CapabilitiesOf(entry).CanEditRequest }) {
		items = append(items, widgets.MenuSeparator("d3"),
			nui.MenuItem{ID: "edit", Label: locales.ContextMenuTrafficTableEdit(), OnSelect: func() { t.editEntries(entries) }})
	}
	if !multiple && logic.CapabilitiesOf(first).CanResend {
		items = append(items, t.resendMenuItem(first))
	}
	return append(items,
		widgets.MenuSeparator("d4"), t.highlightMenuItem(entries),
		widgets.MenuSeparator("d5"),
		nui.MenuItem{ID: "delete", Label: locales.ContextMenuTrafficTableDelete(), Color: nui.Error, OnSelect: func() { t.requestDelete(entries) }},
	)
}

func (t *Table) historyKey() string {
	if t.HistoryKey == nil {
		return ""
	}
	return t.HistoryKey()
}

func (t *Table) exportSelection(format logic.TrafficExportFormat, entries []*logic.TrafficEntry) {
	ids := make([]uint64, len(entries))
	for index, entry := range entries {
		ids[index] = entry.ID
	}
	// As 1.0.5: a saved session's alias names the file, else the count.
	hint := "flowlens-selection-" + strconv.Itoa(len(entries))
	if t.HistoryAlias != nil {
		if alias := t.HistoryAlias(); alias != "" {
			hint = alias + "-selection"
		}
	}
	t.deps.Exporter.Export(format, ExportOptions{
		HistoryKey: t.historyKey(), IDs: ids, Entry: entries[0], FilenameHint: hint,
	})
}

func (t *Table) exportMenuItem(entries []*logic.TrafficEntry) nui.MenuItem {
	exporting := t.deps.Exporter.Exporting
	var children []nui.MenuItem
	for index, group := range logic.TrafficExportGroups {
		if index > 0 {
			children = append(children, widgets.MenuSeparator("export-separator-"+strconv.Itoa(index)))
		}
		for _, format := range group {
			unavailable := exporting || !anyOf(entries, func(entry *logic.TrafficEntry) bool { return logic.CanExportTraffic(entry, format) })
			children = append(children, nui.MenuItem{
				ID: "export-" + string(format), Label: exportFormatLabel(format), Disabled: unavailable,
				OnSelect: func() { t.exportSelection(format, entries) },
			})
		}
	}
	return nui.MenuItem{ID: "export", Label: locales.TrafficExportTitle(), Children: children}
}

func (t *Table) copyMenuItem(entries []*logic.TrafficEntry) nui.MenuItem {
	copyValues := func(values []string) {
		text := strings.Join(values, "\n")
		t.clipboard = &text
		t.deps.Notifier.Success(locales.ContextMenuTrafficTableCopied())
	}
	field := func(id, label string, name logic.CopyField) nui.MenuItem {
		return nui.MenuItem{ID: id, Label: label, OnSelect: func() {
			values := make([]string, len(entries))
			for index, entry := range entries {
				values[index] = logic.TrafficCopyValue(entry, name)
			}
			copyValues(values)
		}}
	}
	var children []nui.MenuItem
	if allOf(entries, logic.IsRawTCP) {
		children = append(children, field("copy-target", locales.ContextMenuTrafficTableCopyTarget(), logic.CopyTarget))
	} else {
		children = append(children,
			field("copy-url", locales.ContextMenuTrafficTableCopyUrl(), logic.CopyURL),
			field("copy-domain", locales.ContextMenuTrafficTableCopyDomain(), logic.CopyDomain),
			field("copy-path", locales.ContextMenuTrafficTableCopyPath(), logic.CopyPath),
			widgets.MenuSeparator("copy-separator-1"),
		)
	}
	children = append(children,
		field("copy-server-ip", locales.ContextMenuTrafficTableCopyServerip(), logic.CopyServerIP),
		field("copy-client-ip", locales.ContextMenuTrafficTableCopyClientip(), logic.CopyClientIP),
		field("copy-server-addr", locales.ContextMenuTrafficTableCopyServeraddr(), logic.CopyServerAddr),
		field("copy-client-addr", locales.ContextMenuTrafficTableCopyClientaddr(), logic.CopyClientAddr),
		widgets.MenuSeparator("copy-separator-2"),
		nui.MenuItem{ID: "copy-row", Label: locales.ContextMenuTrafficTableCopyRow(), OnSelect: func() {
			values := make([]string, len(entries))
			for index, entry := range entries {
				values[index] = t.rowText(entry)
			}
			copyValues(values)
		}},
	)
	return nui.MenuItem{ID: "copy", Label: locales.ContextMenuTrafficTableCopy(), Children: children}
}

// rowText returns the visible columns of a row, separated by tabs.
func (t *Table) rowText(entry *logic.TrafficEntry) string {
	columns := t.visibleColumns()
	values := make([]string, len(columns))
	for index, column := range columns {
		values[index] = t.columnText(entry, column.key)
	}
	return strings.Join(values, "\t")
}

func (t *Table) columnText(entry *logic.TrafficEntry, key logic.TrafficColumnKey) string {
	orUnknown := func(value string) string {
		if value == "" {
			return logic.UnknownValue
		}
		return value
	}
	switch key {
	case logic.ColumnID:
		return strconv.FormatUint(entry.ID, 10)
	case logic.ColumnMethod:
		return logic.TrafficMethodLabel(entry)
	case logic.ColumnHost:
		return orUnknown(logic.TrafficTarget(entry))
	case logic.ColumnPath:
		return logic.TrafficPathLabel(entry)
	case logic.ColumnProcess:
		return t.processText(entry)
	case logic.ColumnStatusCode:
		if entry.Error != nil {
			return "ERR"
		}
		return logic.FormatStatusCode(entry.StatusCode)
	case logic.ColumnType:
		return logic.TrafficTypeLabel(entry)
	case logic.ColumnDestination:
		return logic.TrafficDestinationIP(entry)
	case logic.ColumnProtocol:
		return orUnknown(logic.TrafficProtocol(entry))
	case logic.ColumnDuration:
		return durationLabel(entry)
	case logic.ColumnSize:
		return sizeLabel(entry)
	default:
		return ""
	}
}

func (t *Table) processText(entry *logic.TrafficEntry) string {
	if entry.Metadata == nil || entry.Metadata.Process == nil {
		return logic.UnknownValue
	}
	process := entry.Metadata.Process
	switch process.Status {
	case proxyservice.ProcessStatusPending:
		return locales.TrafficProcessIdentifying()
	case proxyservice.ProcessStatusResolved:
		return processName(process)
	default:
		return logic.UnknownValue
	}
}

// collectBodies loads the bodies of entries one after another, stopping at the
// first failure.
func (t *Table) collectBodies(entries []*logic.TrafficEntry, done func([]*proxyservice.TrafficBodyView, error)) {
	bodies := make([]*proxyservice.TrafficBodyView, 0, len(entries))
	var next func(int)
	next = func(index int) {
		if index == len(entries) {
			done(bodies, nil)
			return
		}
		t.source.BodyView(entries[index].ID, func(view *proxyservice.TrafficBodyView) {
			if view == nil || view.RequestBodyUnavailable {
				done(nil, errRequestBodyUnavailable)
				return
			}
			bodies = append(bodies, view)
			next(index + 1)
		})
	}
	next(0)
}

func (t *Table) copyCurl(entries []*logic.TrafficEntry) {
	var http []*logic.TrafficEntry
	for _, entry := range entries {
		if logic.CapabilitiesOf(entry).CanCopyCurl {
			http = append(http, entry)
		}
	}
	skipped := len(entries) - len(http)
	t.collectBodies(http, func(bodies []*proxyservice.TrafficBodyView, err error) {
		if err != nil {
			t.deps.Notifier.Error(locales.DetailRequestBodyUnavailable())
			return
		}
		commands := make([]string, len(http))
		for index, entry := range http {
			commands[index] = logic.BuildCurl(entry, bodies[index].RequestBody, bodies[index].RequestBodyEncoding)
		}
		text := strings.Join(commands, "\n\n")
		t.clipboard = &text
		if skipped > 0 {
			t.deps.Notifier.Success(locales.ContextMenuTrafficTableCurlCopiedSkipped(strconv.Itoa(len(commands)), strconv.Itoa(skipped)))
			return
		}
		t.deps.Notifier.Success(locales.ContextMenuTrafficTableCopied())
	})
}

func (t *Table) editEntries(entries []*logic.TrafficEntry) {
	if t.deps.OpenEditor == nil {
		return
	}
	if len(entries) > maxEditEntries {
		t.deps.Notifier.Warning(locales.ContextMenuTrafficTableEditLimit(strconv.Itoa(maxEditEntries)))
		return
	}
	historyKey := ""
	if t.HistoryKey != nil {
		historyKey = t.HistoryKey()
	}
	var open func(int)
	open = func(index int) {
		if index == len(entries) {
			return
		}
		entry := entries[index]
		t.source.BodyView(entry.ID, func(view *proxyservice.TrafficBodyView) {
			if view != nil && view.RequestBodyUnavailable {
				t.deps.Notifier.Error(locales.DetailRequestBodyUnavailable())
				open(index + 1)
				return
			}
			t.deps.OpenEditor(entry, view, index == 0, historyKey, func(err error) {
				if err != nil {
					t.deps.Notifier.Error(err.Error())
				}
				open(index + 1)
			})
		})
	}
	open(0)
}

func (t *Table) resendMenuItem(entry *logic.TrafficEntry) nui.MenuItem {
	children := make([]nui.MenuItem, 0, quickResendCount+2)
	for count := 1; count <= quickResendCount; count++ {
		children = append(children, nui.MenuItem{
			ID: "resend-" + strconv.Itoa(count), Label: locales.ContextMenuTrafficTableResendN(strconv.Itoa(count)),
			OnSelect: func() { t.quickResend(entry, count) },
		})
	}
	children = append(children, widgets.MenuSeparator("resend-separator"),
		nui.MenuItem{ID: "resend-times", Label: locales.ContextMenuTrafficTableResendTimes(), OnSelect: func() { t.resend.show(entry.ID) }})
	return nui.MenuItem{ID: "resend", Label: locales.ContextMenuTrafficTableResend(), Children: children}
}

func (t *Table) quickResend(entry *logic.TrafficEntry, count int) {
	if t.deps.Resend == nil {
		return
	}
	t.source.BodyView(entry.ID, func(view *proxyservice.TrafficBodyView) {
		if view != nil && view.RequestBodyUnavailable {
			t.deps.Notifier.Error(locales.DetailRequestBodyUnavailable())
			return
		}
		t.resend.quick(t, entry.ID, proxyservice.ResendConfig{IntervalMs: quickResendInterval, Count: count, UseProxy: true})
	})
}

func (t *Table) highlightMenuItem(entries []*logic.TrafficEntry) nui.MenuItem {
	apply := func(color string) {
		for _, entry := range entries {
			t.source.SetHighlight(entry.ID, color)
		}
	}
	children := make([]nui.MenuItem, 0, len(highlightColors)+2)
	for _, option := range highlightColors {
		children = append(children, widgets.SwatchMenuItem("highlight-"+option.name, option.label(), option.color, func() { apply(option.color) }))
	}
	children = append(children, widgets.MenuSeparator("highlight-separator"),
		nui.MenuItem{ID: "highlight-clear", Label: locales.ContextMenuTrafficTableHighlightClear(), OnSelect: func() { apply("") }})
	return nui.MenuItem{ID: "highlight", Label: locales.ContextMenuTrafficTableHighlight(), Children: children}
}

func (t *Table) requestDelete(entries []*logic.TrafficEntry) {
	ids := make([]uint64, len(entries))
	for index, entry := range entries {
		ids[index] = entry.ID
	}
	if len(ids) == 1 {
		t.source.Delete(ids)
		return
	}
	t.deps.Confirm.Ask(widgets.ConfirmRequest{
		Title:       locales.ContextMenuTrafficTableDelete(),
		Message:     locales.ContextMenuTrafficTableDeleteMultiConfirm(strconv.Itoa(len(ids))),
		Confirm:     widgets.ConfirmAction{Label: locales.ContextMenuTrafficTableConfirm(), Color: nui.Error, Run: func() { t.deleteConfirmed(ids) }},
		CancelLabel: locales.ContextMenuTrafficTableCancel(),
	})
}

func (t *Table) deleteConfirmed(ids []uint64) {
	t.source.Delete(ids)
	t.deps.Notifier.Success(locales.ContextMenuTrafficTableDeleteMultiSuccess(strconv.Itoa(len(ids))))
}
