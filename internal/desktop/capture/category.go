package capture

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/store"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// categoryRefreshInterval limits tree rebuilds from live traffic updates.
const categoryRefreshInterval = 250 * time.Millisecond

const (
	sectionHost      = "host"
	sectionProcess   = "process"
	sectionStructure = "structure"
)

type categoryTreeItem struct {
	node   *logic.StructureNode
	anchor ui.Handle
}

// categoryView is the Categories sidebar.
type categoryView struct {
	search     string
	sections   []string
	expanded   map[string]bool
	activeLeaf string
	activeNode string

	context                        string
	version                        uint64
	computed                       time.Time
	selectedHosts                  []string
	selectedProcessKeys            []string
	entryCount                     int
	roots                          []*logic.StructureNode
	baseVersion                    uint64
	ready                          bool
	locale, fallback               string
	query, sourceSearch, sourceTab string
	hosts                          []logic.HostSummaryItem
	processes                      []logic.ProcessSummaryItem
	filteredHosts                  []logic.HostSummaryItem
	filteredProcesses              []logic.ProcessSummaryItem
	filteredRoots                  []*logic.StructureNode
	processByID                    map[string]logic.ProcessSummaryItem

	scroll                          ui.ScrollState
	hostSelection, processSelection nui.SelectionState[string]
	hostList                        *nui.ListboxState
	processList                     *nui.ListboxState
	hostView                        categoryViewport
	processView                     categoryViewport
	structureView                   categoryViewport
	tree                            *nui.TreeState
	treeDirty                       bool
	treeSearch                      string
	treeItems                       map[string]*categoryTreeItem
	visible                         []*categoryTreeItem
	facetBuilds                     int
	rootBuilds                      int
	itemBuilds                      int
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
	filter := source.filter
	version, baseVersion := filter.Version(), filter.BaseVersion()
	query := strings.ToLower(strings.TrimSpace(v.search))
	locale, fallback := c.Locale(), locales.CategoryProcessUnidentified()
	changedContext := source.key != v.context
	changedSelection := !slices.Equal(v.selectedHosts, filter.SelectedHosts) ||
		!slices.Equal(v.selectedProcessKeys, filter.SelectedProcessKeys)
	changedLocale := locale != v.locale || fallback != v.fallback
	changedQuery := query != v.query
	changedFilter := filter.SearchText != v.sourceSearch || filter.ActiveTab != v.sourceTab
	if v.ready && !changedContext && !changedSelection && !changedLocale && !changedQuery && version == v.version && baseVersion == v.baseVersion {
		return
	}
	now := c.Now()
	if v.ready && !changedContext && !changedSelection && !changedLocale && !changedQuery && !changedFilter && source.key == "capture" {
		if remaining := categoryRefreshInterval - now.Sub(v.computed); remaining > 0 {
			c.After(remaining)
			return
		}
	}
	if changedContext {
		v.reset()
	}
	reopened := !v.ready
	facetsChanged := reopened || changedContext || changedSelection || changedLocale || baseVersion != v.baseVersion
	rootsChanged := reopened || changedContext || version != v.version
	if v.tree != nil && v.treeItems != nil {
		v.activeNode = v.tree.Active
	}
	// Base is borrowed until the filter's next refresh; keep only its size.
	base := filter.Base()
	v.entryCount = len(base)
	if facetsChanged {
		v.hosts, v.processes = logic.BuildCategoryFacets(base, toSet(filter.SelectedHosts), toSet(filter.SelectedProcessKeys), fallback)
		v.facetBuilds++
		v.selectedHosts, v.selectedProcessKeys = slices.Clone(filter.SelectedHosts), slices.Clone(filter.SelectedProcessKeys)
		v.hostSelection.Set(filter.SelectedHosts)
		v.processSelection.Set(filter.SelectedProcessKeys)
	}
	if rootsChanged {
		v.roots = logic.BuildStructureTree(filter.Filtered())
		v.rootBuilds++
		if reopened {
			// Data may have changed while hidden. While visible, a table filter
			// can hide nodes only temporarily, so their intent must survive.
			v.pruneIntent()
		}
	}
	if facetsChanged || changedQuery {
		v.filteredHosts = logic.FilterHostSummary(v.hosts, query)
		v.filteredProcesses = logic.FilterProcessSummary(v.processes, query)
		hostItems := make([]nui.ListboxItem, len(v.filteredHosts))
		for i, host := range v.filteredHosts {
			hostItems[i] = nui.ListboxItem{ID: host.Host, Label: host.Host, Icon: lucide.Globe()}
		}
		processItems := make([]nui.ListboxItem, len(v.filteredProcesses))
		v.processByID = make(map[string]logic.ProcessSummaryItem, len(v.filteredProcesses))
		for i, process := range v.filteredProcesses {
			processItems[i] = nui.ListboxItem{ID: process.ProcessKey, Label: process.Label}
			v.processByID[process.ProcessKey] = process
		}
		v.hostList.SetItems(hostItems)
		v.processList.SetItems(processItems)
		v.itemBuilds += 2
	}
	if rootsChanged || changedQuery {
		v.filteredRoots = logic.FilterStructureByHost(v.roots, query)
		v.treeDirty = true
		// Empty/no-match states skip the tree control entirely. Drop its old
		// nodes now, and replace rather than mutate an already-built state.
		v.tree = &nui.TreeState{}
		v.treeItems, v.visible = nil, nil
	}
	v.context, v.version, v.baseVersion, v.computed = source.key, version, baseVersion, now
	v.locale, v.fallback, v.query = locale, fallback, query
	v.sourceSearch, v.sourceTab, v.ready = filter.SearchText, filter.ActiveTab, true
}

// releaseDerived preserves same-source navigation, but retains no derived
// records, tree nodes, prepared item data, list callbacks or anchor handles.
func (v *categoryView) releaseDerived() {
	v.ready = false
	v.entryCount, v.roots, v.filteredRoots = 0, nil, nil
	v.hosts, v.filteredHosts, v.processes, v.filteredProcesses = nil, nil, nil, nil
	v.processByID = nil
	v.selectedHosts, v.selectedProcessKeys = nil, nil
	v.hostSelection.Clear()
	v.processSelection.Clear()
	hostActive, processActive := "", ""
	if v.hostList != nil {
		hostActive = v.hostList.Active
		v.hostList.Close()
	}
	if v.processList != nil {
		processActive = v.processList.Active
		v.processList.Close()
	}
	v.hostList = &nui.ListboxState{Active: hostActive}
	v.processList = &nui.ListboxState{Active: processActive}
	v.scroll = ui.ScrollState{Y: v.scroll.Y}
	v.hostView, v.processView, v.structureView = categoryViewport{}, categoryViewport{}, categoryViewport{}
	if v.tree != nil {
		v.activeNode = v.tree.Active
	}
	// An already-built tree may still be used during this frame's layout.
	v.tree = &nui.TreeState{}
	v.treeItems, v.visible, v.treeDirty = nil, nil, true
}

// reset also discards the previous source's navigation intent.
func (v *categoryView) reset() {
	v.releaseDerived()
	v.context, v.version, v.computed, v.activeLeaf = "", 0, time.Time{}, ""
	v.activeNode = ""
	v.scroll = ui.ScrollState{}
	v.hostList, v.processList = &nui.ListboxState{}, &nui.ListboxState{}
	v.expanded = map[string]bool{}
}

func (v *categoryView) pruneIntent() {
	missing := make(map[string]bool, len(v.expanded)+2)
	for key := range v.expanded {
		missing[key] = true
	}
	missing[v.activeLeaf], missing[v.activeNode] = true, true
	var visit func([]*logic.StructureNode)
	visit = func(nodes []*logic.StructureNode) {
		for _, node := range nodes {
			delete(missing, node.Key)
			visit(node.Children)
		}
	}
	visit(v.roots)
	for key := range missing {
		delete(v.expanded, key)
	}
	if missing[v.activeLeaf] {
		v.activeLeaf = ""
	}
	if missing[v.activeNode] {
		v.activeNode = ""
	}
}

// CategorySidebarHidden is called on the main thread when the sidebar leaves
// view. Applied filters and same-source navigation remain available on return.
func (w *Workspace) CategorySidebarHidden() {
	w.category.releaseDerived()
	w.categoryMenus.Reset()
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func (v *categoryView) facets(_ *store.FilterStore) ([]logic.HostSummaryItem, []logic.ProcessSummaryItem) {
	return v.filteredHosts, v.filteredProcesses
}

// CategorySidebar builds the Categories section's sidebar.
func (w *Workspace) CategorySidebar(c *ui.Context, theme nui.Theme) {
	view := &w.category
	if view.hostList == nil {
		view.hostList = &nui.ListboxState{}
	}
	if view.processList == nil {
		view.processList = &nui.ListboxState{}
	}
	if view.tree == nil {
		view.tree = &nui.TreeState{}
	}
	if view.expanded == nil {
		view.sections = []string{sectionHost, sectionProcess, sectionStructure}
		view.expanded = map[string]bool{}
	}
	source := w.categoryDataset()
	ui.Column(c).Fill().MinHeight(0).Children(func() {
		header := ui.Row(c.Key("category-header")).FillWidth().Shrink(0).MinHeight(32).AlignItems(ui.Center).PaddingX(8).Background(theme.Elevated)
		header.BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
			if source != nil {
				ui.Text(c, source.label).SingleLine().Shrink(1).FontSize(apptheme.TextSize(c, 14)).FontWeight(600).TextColor(theme.Muted)
			}
		})
		ui.Column(c.Key("category-body")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			w.categoryBody(c, theme, view, source)
		})
		search := ui.Box(c.Key("category-search")).FillWidth().Shrink(0).Padding(6, 8).Background(theme.Elevated)
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
	defer w.categoryMenus.SweepHidden()
	status := func(text string) {
		ui.Column(c).Fill().Center().Padding(16).Children(func() {
			ui.Text(c, text).FontSize(apptheme.TextSize(c, 14)).TextAlign(ui.Center).TextColor(theme.Muted)
		})
	}
	if source == nil {
		if view.ready {
			w.CategorySidebarHidden()
		}
		status(locales.CategoryUnsupported())
		return
	}
	view.refresh(c, source)
	if view.entryCount == 0 {
		status(locales.CategoryEmpty())
		return
	}
	hosts, processes := view.facets(source.filter)
	roots := view.filteredRoots
	if strings.TrimSpace(view.search) != "" && len(hosts)+len(processes)+len(roots) == 0 {
		status(locales.CategoryEmptyFiltered())
		return
	}
	ui.Scroll(c.Key("category-sections")).TrackScroll(&view.scroll).Fill().Label(locales.CategoryTitle()).Children(func() {
		nui.Accordion(c.Key("groups"), theme, &view.sections, []nui.AccordionItem{
			{ID: sectionHost, Label: locales.CategoryHost(), Content: func(c *ui.Context, theme nui.Theme) {
				view.hostView.build(c.Key("hosts"), &view.scroll, categoryContentHeight(theme, len(hosts), 8), func(height float32) {
					options := categoryListOptions(theme, locales.CategoryHost(), height, &view.hostView)
					options.OnChange = func() { source.filter.SelectedHosts = view.hostSelection.Values() }
					nui.Listbox(c, theme, &view.hostSelection, view.hostList, options).FillWidth()
				})
			}},
			{ID: sectionProcess, Label: locales.CategoryProcess(), Content: func(c *ui.Context, theme nui.Theme) {
				w.categoryProcesses(c, theme, source, processes)
			}},
			{ID: sectionStructure, Label: locales.CategoryStructure(), Content: func(c *ui.Context, theme nui.Theme) {
				w.categoryStructure(c, theme, source, roots)
			}},
		}, nui.AccordionOptions{
			Multiple: true, Collapsible: nui.Set(true),
			UI: nui.Styles{
				nui.Trigger: {PaddingX: nui.Set(float32(12)), PaddingY: nui.Set(float32(6))},
				nui.Body:    {PaddingY: nui.Set(float32(0))},
			},
		}).FillWidth()
	})
}

func categoryListOptions(theme nui.Theme, label string, height float32, viewport *categoryViewport) nui.ListboxOptions {
	return nui.ListboxOptions{
		Label: locales.CategoryTitle() + ": " + label, Multiple: true, SelectionBehavior: nui.SelectionToggle,
		Height: height, Size: nui.MD, EmptyText: locales.CategoryEmptyFiltered(),
		Style: viewport.style, UI: nui.Styles{
			nui.Root:      {BorderWidth: nui.Set(float32(0)), Radius: nui.Set(float32(0))},
			nui.Indicator: {Foreground: nui.Set(theme.Primary)},
		},
	}
}

func (w *Workspace) categoryProcesses(c *ui.Context, theme nui.Theme, source *categorySource, processes []logic.ProcessSummaryItem) {
	view := &w.category
	byID := view.processByID
	view.processView.build(c.Key("processes"), &view.scroll, categoryContentHeight(theme, len(processes), 8), func(height float32) {
		options := categoryListOptions(theme, locales.CategoryProcess(), height, &view.processView)
		options.Build = func(c *ui.Context, theme nui.Theme, item nui.ListboxItem) {
			process := byID[item.ID]
			ui.Row(c).MinWidth(0).AlignItems(ui.Center).Gap(6).Children(func() {
				size := apptheme.TextSize(c, 20)
				if process.Kind == logic.ProcessResolved && w.Table.deps.Icons != nil {
					widgets.ProcessIcon(c, w.Table.deps.Icons, process.IconKey, size, theme.Muted).Shrink(0)
				} else {
					ui.Icon(c, lucide.AppWindow()).Size(size, size).Shrink(0).TextColor(theme.Muted)
				}
				ui.Text(c, item.Label).SingleLine().Grow(1).MinWidth(0)
			})
		}
		options.OnChange = func() { source.filter.SelectedProcessKeys = view.processSelection.Values() }
		nui.Listbox(c, theme, &view.processSelection, view.processList, options).FillWidth()
	})
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

func (v *categoryView) updateTree(roots []*logic.StructureNode) {
	if !v.treeDirty && v.treeSearch == v.query && v.treeItems != nil {
		return
	}
	v.treeDirty, v.treeSearch = false, v.query
	v.treeItems = map[string]*categoryTreeItem{}
	var build func([]*logic.StructureNode) []nui.TreeNode
	build = func(nodes []*logic.StructureNode) []nui.TreeNode {
		var result []nui.TreeNode
		for _, node := range nodes {
			item := &categoryTreeItem{node: node}
			v.treeItems[node.Key] = item
			result = append(result, nui.TreeNode{
				ID: node.Key, Label: node.Label, Children: build(node.Children),
				DefaultExpanded: v.expanded[node.Key],
				Style: func(slot nui.Slot, element ui.Element) {
					if slot == nui.Item && node.Type == logic.StructureLeaf {
						element.Bind(&item.anchor)
						v.visible = append(v.visible, item)
					}
				},
			})
		}
		return result
	}
	v.tree.SetNodes(build(roots))
	if v.treeItems[v.activeNode] != nil {
		v.tree.Active = v.activeNode
	}
}

func (w *Workspace) categoryStructure(c *ui.Context, theme nui.Theme, source *categorySource, roots []*logic.StructureNode) {
	view := &w.category
	view.updateTree(roots)
	view.tree.Selection.Clear()
	if view.treeItems[view.activeLeaf] != nil {
		view.tree.Selection.Add(view.activeLeaf)
	}
	view.visible = view.visible[:0]
	// The outer scroll area includes the full height of all expanded rows.
	var count func([]*logic.StructureNode) int
	count = func(nodes []*logic.StructureNode) int {
		n := len(nodes)
		for _, node := range nodes {
			if view.expanded[node.Key] {
				n += count(node.Children)
			}
		}
		return n
	}
	view.structureView.build(c.Key("structure"), &view.scroll, categoryContentHeight(theme, count(roots), 0), func(height float32) {
		nui.Tree(c, theme, view.tree, nui.TreeOptions{
			Label:  locales.CategoryTitle() + ": " + locales.CategoryStructure(),
			Height: height, Size: nui.MD, EmptyText: locales.CategoryEmptyFiltered(),
			Style:             view.structureView.style,
			SelectionBehavior: nui.SelectionReplace, ExpandOnClick: nui.Set(false),
			BuildLeading: func(c *ui.Context, theme nui.Theme, item nui.TreeItemState) {
				size := apptheme.TextSize(c, 20)
				ui.Icon(c, structureIcon(view.treeItems[item.ID].node, item.Expanded)).Size(size, size).TextColor(theme.Muted)
			},
			OnSelect: func(event *nui.TreeEvent) {
				node := view.treeItems[event.ID].node
				if node.Type != logic.StructureLeaf {
					event.PreventDefault()
					view.expanded[event.ID] = !event.Expanded
					view.tree.SetExpanded(event.ID, !event.Expanded)
					return
				}
				w.selectCategoryLeaf(node, source)
			},
			OnToggle: func(event *nui.TreeEvent) { view.expanded[event.ID] = !event.Expanded },
		}).FillWidth()
	})
	// Tree's synchronous style slot binds handles; resolve only this build's
	// visible leaves to attach menus without retaining Elements or Contexts.
	for _, item := range view.visible {
		node := item.node
		anchor := c.Resolve(item.anchor)
		menu := w.categoryMenus.For(node.Key)
		widgets.ContextMenu(c.Key("leaf-menu:"+node.Key), theme, anchor, menu, func() []nui.MenuItem {
			return w.categoryLeafMenuItems(node)
		}, nui.MenuOptions{Label: node.Label}, node, view.version, w.exporter != nil && w.exporter.Exporting)
		if !menu.Open && item.anchor.Focused(c) {
			// Menus restore their trigger; Tree keeps focus on its list.
			view.tree.Outline.List.Focus(c)
		}
		// ContextMenu makes the surface interactive. Forward its primary click
		// to the same action as Tree, keeping keyboard navigation on the tree.
		anchor.TabStop(false).KeepFocus().OnClick(func() {
			w.selectCategoryLeaf(node, source)
			view.tree.Outline.List.Focus(c)
		})
		if view.tree.Active == node.Key {
			open := func() { menu.Open = true }
			view.tree.Outline.List.OnShortcut(c, 0, ui.KeyContextMenu, open)
			view.tree.Outline.List.OnShortcut(c, ui.Shift, ui.KeyF10, open)
		}
	}
}

func (w *Workspace) selectCategoryLeaf(node *logic.StructureNode, source *categorySource) {
	view := &w.category
	view.activeLeaf, view.tree.Active = node.Key, node.Key
	view.tree.Selection.Clear()
	view.tree.Selection.Add(node.Key)
	if len(node.EntryIDs) > 0 {
		source.focus(node.EntryIDs[0])
	}
}

// categoryLeafMenuItems are the actions on a leaf's entries, as the
// traffic menu's.
func (w *Workspace) categoryLeafMenuItems(node *logic.StructureNode) []nui.MenuItem {
	table := w.activeTable()
	wanted := make(map[uint64]bool, len(node.EntryIDs))
	for _, id := range node.EntryIDs {
		wanted[id] = true
	}
	entries := make([]*logic.TrafficEntry, 0, len(node.EntryIDs))
	for _, entry := range table.rows {
		if wanted[entry.ID] {
			entries = append(entries, entry)
		}
	}
	return table.entryMenuItems(entries)
}

func (w *Workspace) activeTable() *Table {
	if _, open := w.selectedHistory(); open {
		return w.HistoryTable
	}
	return w.Table
}
