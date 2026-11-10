package capture

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

// categoryRefreshInterval limits how often a live capture rebuilds the tree.
const categoryRefreshInterval = 250 * time.Millisecond

const (
	sectionHost      = "host"
	sectionProcess   = "process"
	sectionStructure = "structure"
	structureIndent  = 26
)

type categoryRowKind int

const (
	categorySection categoryRowKind = iota
	categoryHost
	categoryProcess
	categoryStructure
)

type categoryRow struct {
	kind    categoryRowKind
	key     string
	label   string
	count   int
	section string
	host    string
	process logic.ProcessSummaryItem
	node    *logic.StructureNode
}

// categoryView is the Categories sidebar.
type categoryView struct {
	search     string
	collapsed  map[string]bool
	expanded   map[string]bool
	activeLeaf string

	context  string
	version  uint64
	computed time.Time
	entries  []*logic.TrafficEntry
	roots    []*logic.StructureNode

	list ui.ListState
}

// CategorySource is the traffic a category view summarizes.
type categorySource struct {
	key    string
	label  string
	filter *store.FilterStore
	focus  func(id uint64)
}

func (w *Workspace) categoryDataset() *categorySource {
	if key, open := w.selectedHistory(); open {
		history := w.Data.History
		if history.Traffic.Key != key {
			return nil
		}
		label := key
		if metadata := history.MetadataFor(key); metadata != nil {
			label = historyLabel(metadata)
		}
		return &categorySource{
			key: "history:" + key, label: locales.CategorySourceHistory(label), filter: history.Filter,
			focus: func(id uint64) { history.Traffic.PendingFocusID = id },
		}
	}
	traffic := w.Data.Traffic
	return &categorySource{key: "capture", label: locales.CategorySourceCapture(), filter: w.Data.Filter, focus: traffic.FocusEntry}
}

func (v *categoryView) refresh(c *ui.Context, source *categorySource) {
	base := source.filter.Base()
	version := source.filter.Version()
	now := c.Now()
	changedContext := source.key != v.context
	if !changedContext && version == v.version && v.entries != nil {
		return
	}
	if !changedContext && source.key == "capture" {
		if remaining := categoryRefreshInterval - now.Sub(v.computed); remaining > 0 {
			c.After(remaining)
			return
		}
	}
	if changedContext {
		v.activeLeaf = ""
	}
	v.context, v.version, v.computed = source.key, version, now
	v.entries = base
	v.roots = logic.BuildStructureTree(base)
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func (v *categoryView) rows(filter *store.FilterStore) []categoryRow {
	hosts, processes := logic.BuildCategoryFacets(v.entries, toSet(filter.SelectedHosts), toSet(filter.SelectedProcessKeys), locales.CategoryProcessUnidentified())
	hosts = logic.FilterHostSummary(hosts, v.search)
	processes = logic.FilterProcessSummary(processes, v.search)
	roots := logic.FilterStructureByHost(v.roots, v.search)

	var rows []categoryRow
	section := func(id, label string, children func()) {
		rows = append(rows, categoryRow{kind: categorySection, key: "section:" + id, label: label, section: id})
		if !v.collapsed[id] {
			children()
		}
	}
	section(sectionHost, locales.CategoryHost(), func() {
		for _, item := range hosts {
			rows = append(rows, categoryRow{kind: categoryHost, key: "host:" + item.Host, label: item.Host, count: item.Count, host: item.Host})
		}
	})
	section(sectionProcess, locales.CategoryProcess(), func() {
		for _, item := range processes {
			rows = append(rows, categoryRow{kind: categoryProcess, key: "process:" + item.ProcessKey, label: item.Label, count: item.Count, process: item})
		}
	})
	section(sectionStructure, locales.CategoryStructure(), func() {
		var walk func(nodes []*logic.StructureNode)
		walk = func(nodes []*logic.StructureNode) {
			for _, node := range nodes {
				rows = append(rows, categoryRow{kind: categoryStructure, key: "node:" + node.Key, label: node.Label, count: len(node.EntryIDs), node: node})
				if v.expanded[node.Key] {
					walk(node.Children)
				}
			}
		}
		walk(roots)
	})
	return rows
}

// CategorySidebar builds the Categories section's sidebar.
func (w *Workspace) CategorySidebar(c *ui.Context, theme nui.Theme) {
	view := &w.category
	if view.collapsed == nil {
		view.collapsed, view.expanded = map[string]bool{}, map[string]bool{}
	}
	source := w.categoryDataset()
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		header := ui.Row(c.Key("category-header")).FillWidth().Shrink(0).MinHeight(40).AlignItems(ui.Center).PaddingX(12).Background(theme.Elevated)
		header.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
			if source != nil {
				ui.Text(c, source.label).SingleLine().Shrink(1).FontSize(14).FontWeight(600).TextColor(theme.Muted)
			}
		})
		ui.Column(c.Key("category-body")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			w.categoryBody(c, theme, view, source)
		})
		search := ui.Box(c.Key("category-search")).FillWidth().Shrink(0).Padding(10).Background(theme.Elevated)
		search.BorderWidth(1, 0, 0, 0).BorderColor(theme.Border).Children(func() {
			nui.Input(c, theme, &view.search, nui.InputOptions{
				Label: locales.CategorySearchPlaceholder(), Placeholder: locales.CategorySearchPlaceholder(),
				Leading: lucide.Search(), Clearable: true, ClearLabel: locales.ToolbarClear(),
			})
		})
	})
}

// categoryBody is the tree of the sidebar, or why there is none.
func (w *Workspace) categoryBody(c *ui.Context, theme nui.Theme, view *categoryView, source *categorySource) {
	status := func(text string) {
		ui.Column(c).Fill().Center().Padding(16).Children(func() {
			ui.Text(c, text).FontSize(14).TextAlign(ui.Center).TextColor(theme.Muted)
		})
	}
	if source == nil {
		status(locales.CategoryUnsupported())
		return
	}
	view.refresh(c, source)
	if len(view.entries) == 0 {
		status(locales.CategoryEmpty())
		return
	}
	rows := view.rows(source.filter)
	if strings.TrimSpace(view.search) != "" && len(rows) == 3 {
		status(locales.CategoryEmptyFiltered())
		return
	}
	ui.List(c.Key("category-tree"), &view.list, len(rows), func(index int) { w.categoryRow(c, theme, rows[index], source) }).Grow(1).Basis(0).MinHeight(0).FillWidth().Label(locales.CategoryTitle())
}

func leafIcon(node *logic.StructureNode) *ui.SVG {
	if node.TrafficKind == "raw-tcp" {
		return lucide.PlugZap()
	}
	media, _, _ := strings.Cut(strings.ToLower(node.ContentType), ";")
	media = strings.TrimSpace(media)
	switch {
	case strings.HasPrefix(media, "image/"):
		return lucide.Image()
	case strings.HasPrefix(media, "text/") || strings.Contains(media, "javascript") || strings.Contains(media, "json") || strings.Contains(media, "xml") || strings.Contains(media, "css") || strings.Contains(media, "yaml") || strings.Contains(media, "toml"):
		return lucide.Code()
	default:
		return lucide.File()
	}
}

func structureIcon(node *logic.StructureNode, expanded bool) *ui.SVG {
	switch node.Type {
	case logic.StructureHost:
		return lucide.Globe()
	case logic.StructureSegment:
		switch {
		case node.TrafficKind == "raw-tcp":
			return lucide.Network()
		case expanded:
			return lucide.FolderOpen()
		default:
			return lucide.Folder()
		}
	default:
		return leafIcon(node)
	}
}

func (w *Workspace) categoryRow(c *ui.Context, theme nui.Theme, row categoryRow, source *categorySource) {
	view := &w.category
	selected := false
	indent := float32(10)
	var icon *ui.SVG
	switch row.kind {
	case categoryHost:
		selected = toSet(source.filter.SelectedHosts)[row.host]
		icon = lucide.Globe()
	case categoryProcess:
		selected = toSet(source.filter.SelectedProcessKeys)[row.process.ProcessKey]
	case categoryStructure:
		selected = row.node.Key == view.activeLeaf
		indent += float32(row.node.Depth) * structureIndent
		icon = structureIcon(row.node, view.expanded[row.node.Key])
	}
	element := ui.Row(c.Key(row.key)).FillWidth().Height(32).AlignItems(ui.Center).Gap(6).Padding(0, 10, 0, indent).Label(row.label)
	if selected {
		element.Background(theme.Accented)
	}
	element.OnClick(func() { w.activateCategoryRow(row, source) })
	if row.kind == categoryStructure && row.node.Type == logic.StructureLeaf && len(row.node.EntryIDs) > 0 {
		element.ContextMenu(func(menu *ui.Menu) { w.categoryLeafMenu(c, menu, row.node) })
	}
	element.Children(func() {
		if row.kind == categorySection {
			chevron := lucide.ChevronDown()
			if view.collapsed[row.section] {
				chevron = lucide.ChevronRight()
			}
			ui.Icon(c, chevron).Size(14, 14).Shrink(0).TextColor(theme.Muted)
			ui.Text(c, row.label).FontSize(14).Bold().TextColor(theme.Muted)
			return
		}
		if row.kind == categoryStructure && len(row.node.Children) > 0 {
			chevron := lucide.ChevronRight()
			if view.expanded[row.node.Key] {
				chevron = lucide.ChevronDown()
			}
			ui.Icon(c, chevron).Size(12, 12).Shrink(0).TextColor(theme.Muted)
		}
		if row.kind == categoryProcess && row.process.Kind == logic.ProcessResolved && w.Table.deps.Icons != nil {
			widgets.ProcessIcon(c, w.Table.deps.Icons, row.process.IconKey, 16, theme.Muted).Shrink(0)
		} else if icon != nil {
			ui.Icon(c, icon).Size(15, 15).Shrink(0).TextColor(theme.Muted)
		}
		ui.Text(c, row.label).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(14).TextColor(theme.Text)
		ui.Text(c, strconv.Itoa(row.count)).Shrink(0).FontSize(12).TextColor(theme.Muted)
	})
}

func (w *Workspace) activateCategoryRow(row categoryRow, source *categorySource) {
	view := &w.category
	switch row.kind {
	case categorySection:
		view.collapsed[row.section] = !view.collapsed[row.section]
	case categoryHost:
		source.filter.ToggleHost(row.host)
	case categoryProcess:
		source.filter.ToggleProcess(row.process.ProcessKey)
	case categoryStructure:
		if row.node.Type == logic.StructureLeaf {
			view.activeLeaf = row.node.Key
			if len(row.node.EntryIDs) > 0 {
				source.focus(row.node.EntryIDs[0])
			}
			return
		}
		view.expanded[row.node.Key] = !view.expanded[row.node.Key]
	}
}

func (w *Workspace) categoryLeafMenu(c *ui.Context, menu *ui.Menu, node *logic.StructureNode) {
	table := w.activeTable()
	entries := make([]*logic.TrafficEntry, 0, len(node.EntryIDs))
	wanted := make(map[uint64]bool, len(node.EntryIDs))
	for _, id := range node.EntryIDs {
		wanted[id] = true
	}
	for _, entry := range table.rows {
		if wanted[entry.ID] {
			entries = append(entries, entry)
		}
	}
	if len(entries) > 0 {
		table.entriesMenu(c, menu, entries)
	}
}

func (w *Workspace) activeTable() *Table {
	if _, open := w.selectedHistory(); open {
		return w.HistoryTable
	}
	return w.Table
}
