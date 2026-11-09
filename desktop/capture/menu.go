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
	color string
	label func() string
}

var highlightColors = []highlightColor{
	{"#ef4444", locales.ContextMenuTrafficTableHighlightRed},
	{"#f97316", locales.ContextMenuTrafficTableHighlightOrange},
	{"#eab308", locales.ContextMenuTrafficTableHighlightYellow},
	{"#22c55e", locales.ContextMenuTrafficTableHighlightGreen},
	{"#3b82f6", locales.ContextMenuTrafficTableHighlightBlue},
	{"#a855f7", locales.ContextMenuTrafficTableHighlightPurple},
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

func (t *Table) rowMenu(c *ui.Context, menu *ui.Menu, row int) {
	if row >= len(t.rows) {
		return
	}
	entries := t.selectedEntries()
	if !t.selection.Has(t.rows[row].ID) {
		entries = []*logic.TrafficEntry{t.rows[row]}
	}
	if len(entries) == 0 {
		return
	}
	t.entriesMenu(c, menu, entries)
}

// entriesMenu adds the actions on entries to menu.
func (t *Table) entriesMenu(c *ui.Context, menu *ui.Menu, entries []*logic.TrafficEntry) {
	first := entries[0]
	multiple := len(entries) > 1

	if menu.Item(locales.ContextMenuTrafficTableShowDetails()).Disabled(multiple).Chosen() {
		t.source.SelectEntry(first)
		t.source.SetShowDetail(true)
	}
	if t.deps.Exporter != nil {
		t.exportMenu(menu, entries)
	}

	canCurl := func(entry *logic.TrafficEntry) bool { return logic.CapabilitiesOf(entry).CanCopyCurl }
	if anyOf(entries, canCurl) {
		menu.Separator()
		if menu.Item(locales.ContextMenuTrafficTableCopyCurl()).Chosen() {
			t.copyCurl(entries)
		}
	}
	menu.Separator()
	t.copyMenu(c, menu, entries)

	canEdit := allOf(entries, func(entry *logic.TrafficEntry) bool { return logic.CapabilitiesOf(entry).CanEditRequest })
	if canEdit {
		menu.Separator()
		if menu.Item(locales.ContextMenuTrafficTableEdit()).Chosen() {
			t.editEntries(entries)
		}
	}
	if !multiple && logic.CapabilitiesOf(first).CanResend {
		t.resendMenu(menu, first)
	}
	menu.Separator()
	t.highlightMenu(menu, entries)
	menu.Separator()
	if menu.Item(locales.ContextMenuTrafficTableDelete()).Danger().Chosen() {
		t.requestDelete(entries)
	}
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
	t.deps.Exporter.Export(format, ExportOptions{
		HistoryKey: t.historyKey(), IDs: ids, Entry: entries[0], FilenameHint: "flowlens-selection-" + strconv.Itoa(len(entries)),
	})
}

func (t *Table) exportMenu(menu *ui.Menu, entries []*logic.TrafficEntry) {
	exporting := t.deps.Exporter.Exporting
	menu.Submenu(locales.TrafficExportTitle(), func(sub *ui.Menu) {
		for index, group := range logic.TrafficExportGroups {
			if index > 0 {
				sub.Separator()
			}
			for _, format := range group {
				unavailable := exporting || !anyOf(entries, func(entry *logic.TrafficEntry) bool { return logic.CanExportTraffic(entry, format) })
				if sub.Item(exportFormatLabel(format)).Disabled(unavailable).Chosen() {
					t.exportSelection(format, entries)
				}
			}
		}
	})
}

func (t *Table) copyMenu(c *ui.Context, menu *ui.Menu, entries []*logic.TrafficEntry) {
	copyValues := func(values []string) {
		c.WriteClipboard(strings.Join(values, "\n"))
		t.deps.Notifier.Success(locales.ContextMenuTrafficTableCopied())
	}
	field := func(sub *ui.Menu, label string, name logic.CopyField) {
		if sub.Item(label).Chosen() {
			values := make([]string, len(entries))
			for index, entry := range entries {
				values[index] = logic.TrafficCopyValue(entry, name)
			}
			copyValues(values)
		}
	}
	menu.Submenu(locales.ContextMenuTrafficTableCopy(), func(sub *ui.Menu) {
		if allOf(entries, logic.IsRawTCP) {
			field(sub, locales.ContextMenuTrafficTableCopyTarget(), logic.CopyTarget)
		} else {
			field(sub, locales.ContextMenuTrafficTableCopyUrl(), logic.CopyURL)
			field(sub, locales.ContextMenuTrafficTableCopyDomain(), logic.CopyDomain)
			field(sub, locales.ContextMenuTrafficTableCopyPath(), logic.CopyPath)
			sub.Separator()
		}
		field(sub, locales.ContextMenuTrafficTableCopyServerip(), logic.CopyServerIP)
		field(sub, locales.ContextMenuTrafficTableCopyClientip(), logic.CopyClientIP)
		field(sub, locales.ContextMenuTrafficTableCopyServeraddr(), logic.CopyServerAddr)
		field(sub, locales.ContextMenuTrafficTableCopyClientaddr(), logic.CopyClientAddr)
		sub.Separator()
		if sub.Item(locales.ContextMenuTrafficTableCopyRow()).Chosen() {
			values := make([]string, len(entries))
			for index, entry := range entries {
				values[index] = t.rowText(entry)
			}
			copyValues(values)
		}
	})
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

func (t *Table) resendMenu(menu *ui.Menu, entry *logic.TrafficEntry) {
	menu.Submenu(locales.ContextMenuTrafficTableResend(), func(sub *ui.Menu) {
		for count := 1; count <= quickResendCount; count++ {
			if sub.Item(locales.ContextMenuTrafficTableResendN(strconv.Itoa(count))).Chosen() {
				t.quickResend(entry, count)
			}
		}
		sub.Separator()
		if sub.Item(locales.ContextMenuTrafficTableResendTimes()).Chosen() {
			t.resend.show(entry.ID)
		}
	})
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

func (t *Table) highlightMenu(menu *ui.Menu, entries []*logic.TrafficEntry) {
	apply := func(color string) {
		for _, entry := range entries {
			t.source.SetHighlight(entry.ID, color)
		}
	}
	menu.Submenu(locales.ContextMenuTrafficTableHighlight(), func(sub *ui.Menu) {
		for _, option := range highlightColors {
			if sub.Item(option.label()).Chosen() {
				apply(option.color)
			}
		}
		sub.Separator()
		if sub.Item(locales.ContextMenuTrafficTableHighlightClear()).Chosen() {
			apply("")
		}
	})
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
