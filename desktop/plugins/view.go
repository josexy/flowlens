// Package plugins builds the Python Plugins section and the API help.
package plugins

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	pythonpluginservice "github.com/josexy/flowlens/backend/services/python_plugin_service"
	"github.com/josexy/flowlens/desktop/pythonapi"
	"github.com/josexy/flowlens/desktop/shortcuts"
	"github.com/josexy/flowlens/desktop/store"
	"github.com/josexy/flowlens/desktop/widgets"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const (
	pluginRowHeight float32 = 58
	ruleRowHeight   float32 = 44
)

// View is the Python Plugins sidebar and page.
type View struct {
	Data       *store.Store
	Dispatcher *shortcuts.Dispatcher
	Notifier   *widgets.Notifier
	Kbds       func(commandID string) []string
	// OpenHelp shows the API help.
	OpenHelp func(scope string)
	// Select shows the Python Plugins section.
	Select func()

	search    string
	drag      listDrag
	create    pluginDialog
	metadata  pluginDialog
	fileModal fileDialog
	deleting  confirmState
	detailTab string
	python    *widgets.CodeView
	text      *widgets.CodeView
	params    *widgets.CodeView
	shown     string
	shownText string
	paramsSet string
	rules     rulesPane
}

type listDrag struct {
	key    string
	y      float32
	active bool
}

type confirmState struct {
	file, plugin bool
}

// New returns the section of a window.
func New(data *store.Store, dispatcher *shortcuts.Dispatcher, notifier *widgets.Notifier) *View {
	v := &View{Data: data, Dispatcher: dispatcher, Notifier: notifier, detailTab: "rules"}
	v.python = widgets.NewCodeView("python", false)
	v.text = widgets.NewCodeView("plaintext", false)
	v.params = widgets.NewCodeView("json", false)
	change := func(text string) {
		plugins := v.Data.Plugins
		v.shownText, plugins.EditorContent, plugins.EditorError = text, text, ""
	}
	v.python.OnChange = change
	v.text.OnChange = change
	v.params.OnChange = func(text string) {
		plugins := v.Data.Plugins
		v.paramsSet, plugins.ParamsDraft, plugins.ParamsError = text, text, ""
	}
	pythonapi.Attach(v.python)
	return v
}

func (v *View) filtered() []*store.Plugin {
	plugins := v.Data.Plugins.Plugins
	query := strings.ToLower(strings.TrimSpace(v.search))
	if query == "" {
		return plugins
	}
	var matched []*store.Plugin
	for _, plugin := range plugins {
		if strings.Contains(strings.ToLower(plugin.Name+"\n"+plugin.Description), query) {
			matched = append(matched, plugin)
		}
	}
	return matched
}

func (v *View) runtimeBadge() (string, nui.Color) {
	status := v.Data.Plugins.Runtime
	switch {
	case status == nil:
		return locales.PythonPluginsRuntimeUnknown(), nui.Neutral
	case !status.Enabled:
		return locales.PythonPluginsRuntimeDisabled(), nui.Neutral
	case status.Error != "":
		return locales.PythonPluginsRuntimeError(), nui.Error
	case status.Ready:
		return locales.PythonPluginsRuntimeReady(), nui.Success
	default:
		return locales.PythonPluginsRuntimeIdle(), nui.Warning
	}
}

func validationLabel(status pythonpluginservice.ValidationStatus) string {
	switch status {
	case "valid":
		return locales.PythonPluginsValidationValid()
	case "invalid":
		return locales.PythonPluginsValidationInvalid()
	default:
		return locales.PythonPluginsValidationUnavailable()
	}
}

func validationColor(theme nui.Theme, status pythonpluginservice.ValidationStatus) ui.Color {
	switch status {
	case "valid":
		return theme.Success
	case "invalid":
		return theme.Error
	default:
		return theme.Warning
	}
}

func action(c *ui.Context, theme nui.Theme, key string, icon *ui.SVG, label string, color nui.Color, variant nui.Variant, disabled bool, run func()) {
	button := nui.Button(c.Key(key), theme, "", nui.ButtonOptions{Variant: variant, Color: color, Size: nui.XS, Square: true, Leading: icon, AccessibleLabel: label, Disabled: disabled})
	if !disabled {
		button.OnClick(run)
	}
	nui.Tooltip(c, theme, button, label, nui.TooltipOptions{})
}

func (v *View) reportDirectory(err error) {
	if err != nil {
		v.Notifier.Error(locales.PythonPluginsOpenDirectoryFailed(err.Error()))
	}
}

// Sidebar builds the plugin list.
func (v *View) Sidebar(c *ui.Context, theme nui.Theme) {
	plugins := v.Data.Plugins
	plugins.Initialize()
	ui.Column(c.Key("plugins-sidebar")).Fill().MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Shrink(0).Gap(10).Padding(10).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Gap(6).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(6).MinWidth(0).Children(func() {
					ui.Text(c, locales.PythonPluginsTitle()).SingleLine().FontSize(13).Bold().TextColor(theme.Highlighted)
					label, color := v.runtimeBadge()
					nui.Badge(c.Key("runtime"), theme, label, nui.BadgeOptions{Color: color, Variant: nui.Soft, Size: nui.SM})
				})
				ui.Row(c).Gap(2).Children(func() {
					if v.OpenHelp != nil {
						action(c, theme, "help", lucide.BookOpen(), locales.PythonPluginHelpTitle(), nui.Neutral, nui.Ghost, false, func() { v.OpenHelp(pythonapi.ScopeGlobal) })
					}
					action(c, theme, "root", lucide.FolderOpen(), locales.PythonPluginsOpenRootDirectory(), nui.Neutral, nui.Ghost, false, func() { plugins.OpenPluginsDirectory(v.reportDirectory) })
					action(c, theme, "create", lucide.Plus(), locales.PythonPluginsCreate(), nui.Primary, nui.Soft, false, v.create.showEmpty)
				})
			})
			nui.Input(c.Key("search"), theme, &v.search, nui.InputOptions{Label: locales.PythonPluginsSearchPlaceholder(), Placeholder: locales.PythonPluginsSearchPlaceholder(), Leading: lucide.Search(), Size: nui.SM}).FillWidth()
		})
		visible := v.filtered()
		ui.Scroll(c.Key("plugin-list")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(6).Children(func() {
				switch {
				case plugins.LoadingPlugins && len(plugins.Plugins) == 0:
					ui.Row(c.Key("loading")).FillWidth().Height(96).Center().Children(func() { ui.Spinner(c) })
				case len(visible) == 0:
					title := locales.PythonPluginsEmpty()
					if strings.TrimSpace(v.search) != "" {
						title = locales.PythonPluginsNoSearchResults()
					}
					nui.Empty(c.Key("empty"), theme, nui.EmptyOptions{Icon: lucide.FileCode(), Title: title, Variant: nui.Ghost})
				}
				for index, plugin := range visible {
					v.pluginRow(c, theme, index, plugin, visible)
				}
				if plugins.Error != "" {
					ui.Text(c.Key("error"), plugins.Error).FontSize(12).TextColor(theme.Error).FillWidth().Padding(8)
				}
			})
		})
	})
	v.create.build(c, theme, v, locales.PythonPluginsCreateTitle(), locales.PythonPluginsCreate(), "plugin:create", v.submitCreate)
}

func (v *View) submitCreate(d *pluginDialog) {
	if strings.TrimSpace(d.name) == "" {
		d.err = locales.PythonPluginsNameRequired()
		return
	}
	d.err = ""
	v.Data.Plugins.CreatePlugin(d.name, d.description, func(_ *store.Plugin, err error) {
		if err != nil {
			d.err = err.Error()
			return
		}
		d.open = false
		v.Notifier.Success(locales.PythonPluginsCreateSuccess())
	})
}

func (v *View) selectPlugin(id string) {
	if v.Select != nil {
		v.Select()
	}
	v.Data.Plugins.SelectPlugin(id, func(err error) {
		if err != nil {
			v.Notifier.Error(locales.PythonPluginsLoadFailed(err.Error()))
		}
	})
}

func (v *View) pluginRow(c *ui.Context, theme nui.Theme, index int, plugin *store.Plugin, visible []*store.Plugin) {
	plugins := v.Data.Plugins
	selected := plugins.SelectedPluginID == plugin.ID
	row := ui.Row(c.Key(plugin.ID)).FillWidth().Height(pluginRowHeight-4).Margin(0, 0, 4, 0).AlignItems(ui.Center).Gap(6).Padding(6, 8).Radius(theme.Radius).Border(1, ui.Transparent).Label(plugin.Name)
	if selected {
		row.Border(1, theme.Primary).Background(theme.Primary.Alpha(0.1))
	} else if row.Hovered() {
		row.Background(theme.Elevated)
	}
	row.OnClick(func() { v.selectPlugin(plugin.ID) })
	row.Children(func() {
		v.grip(c, theme, plugin, visible)
		ui.Box(c.Key("status")).Size(8, 8).Shrink(0).Radius(4).Background(validationColor(theme, plugin.ValidationStatus)).Label(validationLabel(plugin.ValidationStatus))
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, plugin.Name).SingleLine().FontSize(13).Bold().TextColor(theme.Highlighted)
			if plugin.Description != "" {
				ui.Text(c, plugin.Description).SingleLine().FontSize(12).TextColor(theme.Muted)
			}
		})
		enabled := plugin.Enabled
		key := "plugin:enabled:" + plugin.ID
		busy := plugins.IsMutating(key)
		nui.Switch(c.Key("enabled"), theme, &enabled, nui.SwitchOptions{Size: nui.SM, Disabled: busy, Loading: busy}).Label(locales.PythonPluginsEnabled() + " " + plugin.Name)
		if enabled != plugin.Enabled {
			plugins.SetPluginEnabled(plugin.ID, enabled, func(_ *store.Plugin, err error) {
				if err != nil {
					v.Notifier.Error(locales.PythonPluginsEnableFailed(err.Error()))
				}
			})
		}
	})
}

func (v *View) grip(c *ui.Context, theme nui.Theme, plugin *store.Plugin, visible []*store.Plugin) {
	plugins := v.Data.Plugins
	grip := ui.Icon(c.Key("grip"), lucide.GripVertical()).Size(14, 14).Shrink(0).TextColor(theme.Dimmed).Label("drag " + plugin.Name)
	if strings.TrimSpace(v.search) != "" {
		return
	}
	grip.Cursor(ui.CursorGrab).OnClick(func() {})
	if grip.PressedInside() {
		v.drag = listDrag{key: plugin.ID}
	}
	if v.drag.key != plugin.ID {
		return
	}
	if _, dy, moved := grip.Dragged(); moved {
		v.drag.active = true
		v.drag.y += dy
		for v.drag.y > pluginRowHeight/2 || v.drag.y < -pluginRowHeight/2 {
			direction := 1
			if v.drag.y < 0 {
				direction = -1
			}
			ordered := slices.Clone(plugins.Plugins)
			index := slices.IndexFunc(ordered, func(item *store.Plugin) bool { return item.ID == plugin.ID })
			target := index + direction
			if index < 0 || target < 0 || target >= len(ordered) {
				v.drag.y = 0
				break
			}
			ordered[index], ordered[target] = ordered[target], ordered[index]
			plugins.SetOrderLocal(ordered)
			v.drag.y -= float32(direction) * pluginRowHeight
		}
	}
	if _, _, ended := grip.DragEnded(); ended && v.drag.active {
		v.drag = listDrag{}
		ids := make([]string, len(plugins.Plugins))
		for index, item := range plugins.Plugins {
			ids[index] = item.ID
		}
		plugins.ReorderPlugins(ids, func(err error) {
			if err != nil {
				v.Notifier.Error(locales.PythonPluginsReorderFailed(err.Error()))
			}
		})
	}
}

func fileLanguage(path string) string {
	lower := strings.ToLower(path)
	for suffix, language := range map[string]string{".py": "python", ".json": "json", ".js": "javascript", ".html": "html", ".css": "css", ".xml": "xml"} {
		if strings.HasSuffix(lower, suffix) {
			return language
		}
	}
	return "plaintext"
}

func (v *View) activeEditor() *widgets.CodeView {
	if fileLanguage(v.Data.Plugins.SelectedFilePath) == "python" {
		return v.python
	}
	return v.text
}

func (v *View) syncEditors() {
	plugins := v.Data.Plugins
	identity := plugins.SelectedPluginID + "\x00" + plugins.SelectedFilePath
	view := v.activeEditor()
	view.SetLanguage(fileLanguage(plugins.SelectedFilePath))
	if identity != v.shown || plugins.EditorContent != v.shownText {
		v.shown, v.shownText = identity, plugins.EditorContent
		view.SetText(plugins.EditorContent)
	}
	if plugins.ParamsDraft != v.paramsSet {
		v.paramsSet = plugins.ParamsDraft
		v.params.SetText(plugins.ParamsDraft)
	}
}

func (v *View) saveFile(showSuccess bool, done func(error)) {
	v.Data.Plugins.SaveFile(func(_ *store.Plugin, err error) {
		if err == nil && showSuccess {
			v.Notifier.Success(locales.PythonPluginsFileSaved())
		}
		if done != nil {
			done(err)
		}
	})
}

func (v *View) saveParams() {
	v.Data.Plugins.SaveParams(func(_ *store.Plugin, err error) {
		if err == nil {
			v.Notifier.Success(locales.PythonPluginsParamsSaved())
		}
	})
}

func (v *View) declareShortcut() {
	plugins := v.Data.Plugins
	v.Dispatcher.Handle(shortcuts.Handler{
		CommandID: shortcuts.AppSave,
		When:      func() bool { return (v.detailTab == "params" && plugins.ParamsDirty()) || plugins.EditorDirty() },
		Run: func() {
			if v.detailTab == "params" && plugins.ParamsDirty() {
				v.saveParams()
				return
			}
			if plugins.EditorDirty() {
				v.saveFile(true, nil)
			}
		},
	})
}

func (v *View) validate() {
	plugins := v.Data.Plugins
	plugin := plugins.SelectedPlugin()
	if plugin == nil {
		return
	}
	run := func() {
		plugins.ValidatePlugin(plugin.ID, func(_ *store.Plugin, err error) {
			if err != nil {
				v.Notifier.Error(locales.PythonPluginsValidationFailed(err.Error()))
				return
			}
			v.Notifier.Success(locales.PythonPluginsValidationSuccess())
		})
	}
	if plugins.EditorDirty() {
		v.saveFile(false, func(err error) {
			if err != nil {
				v.Notifier.Error(locales.PythonPluginsValidationFailed(err.Error()))
				return
			}
			run()
		})
		return
	}
	run()
}

func (v *View) reload() {
	plugins := v.Data.Plugins
	plugin := plugins.SelectedPlugin()
	if plugin == nil {
		return
	}
	plugins.ReloadPlugin(plugin.ID, func(_ *store.Plugin, err error) {
		if err != nil {
			v.Notifier.Error(locales.PythonPluginsReloadFailed(err.Error()))
			return
		}
		v.Notifier.Success(locales.PythonPluginsReloadSuccess())
	})
}

// Content builds the page of the selected plugin.
func (v *View) Content(c *ui.Context, theme nui.Theme) {
	plugins := v.Data.Plugins
	plugins.Initialize()
	v.declareShortcut()
	plugin := plugins.SelectedPlugin()
	ui.Column(c.Key("plugins-page")).Fill().MinHeight(0).Background(theme.Background).Children(func() {
		if plugin == nil {
			ui.Column(c).Fill().Center().Children(func() {
				nui.Empty(c, theme, nui.EmptyOptions{Icon: lucide.FileCode(), Title: locales.PythonPluginsEmpty(), Description: locales.PythonPluginsEmptyHint(), Variant: nui.Ghost})
			})
			return
		}
		v.syncEditors()
		v.header(c, theme, plugin)
		ui.Row(c.Key("workspace")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			v.filesPane(c, theme)
			ui.Column(c.Key("right")).Grow(1).Basis(0).MinWidth(0).FillHeight().Children(func() {
				v.editorPane(c, theme)
				v.detailPane(c, theme, plugin)
			})
		})
		v.statusBar(c, theme, plugin)
	})
	v.dialogs(c, theme, plugin)
}

func (v *View) header(c *ui.Context, theme nui.Theme, plugin *store.Plugin) {
	plugins := v.Data.Plugins
	ui.Row(c.Key("header")).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(8).Padding(8, 12).BorderWidth(0, 0, 1, 0).BorderColor(theme.Border).Children(func() {
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Text(c, plugin.Name).SingleLine().FontSize(14).Bold().TextColor(theme.Highlighted)
				if plugins.EditorDirty() {
					ui.Box(c.Key("dirty")).Size(6, 6).Shrink(0).Radius(3).Background(theme.Primary)
				}
			})
			if plugin.Description != "" {
				ui.Text(c, plugin.Description).SingleLine().FontSize(12).TextColor(theme.Muted)
			}
		})
		saving := plugins.IsMutating("file:save:" + plugins.SelectedPluginID + ":" + plugins.SelectedFilePath)
		save := nui.Button(c.Key("save"), theme, locales.PythonPluginsSave(), nui.ButtonOptions{Size: nui.SM, Leading: lucide.Save(), Disabled: !plugins.EditorDirty(), Loading: saving})
		if plugins.EditorDirty() {
			save.OnClick(func() { v.saveFile(true, nil) })
		}
		var kbds []string
		if v.Kbds != nil {
			kbds = v.Kbds(shortcuts.AppSave)
		}
		nui.Tooltip(c, theme, save, locales.PythonPluginsSaveFile(), nui.TooltipOptions{Kbds: kbds})
		validate := nui.Button(c.Key("validate"), theme, locales.PythonPluginsValidate(), nui.ButtonOptions{Size: nui.SM, Variant: nui.Outline, Color: nui.Neutral, Leading: lucide.CircleCheckBig(), Loading: plugins.IsMutating("plugin:validate:" + plugin.ID)})
		validate.OnClick(v.validate)
		nui.Tooltip(c, theme, validate, locales.PythonPluginsValidateHint(), nui.TooltipOptions{})
		reload := nui.Button(c.Key("reload"), theme, "", nui.ButtonOptions{Size: nui.SM, Variant: nui.Ghost, Color: nui.Neutral, Square: true, Leading: lucide.RefreshCw(), AccessibleLabel: locales.PythonPluginsReload(), Loading: plugins.IsMutating("plugin:reload:" + plugin.ID)})
		reload.OnClick(v.reload)
		nui.Tooltip(c, theme, reload, locales.PythonPluginsReloadHint(), nui.TooltipOptions{})
		action(c, theme, "edit", lucide.Pencil(), locales.PythonPluginsRenamePlugin(), nui.Neutral, nui.Ghost, false, func() { v.metadata.show(plugin.Name, plugin.Description) })
		action(c, theme, "open", lucide.FolderOpen(), locales.PythonPluginsOpenDirectory(), nui.Neutral, nui.Ghost, false, func() { plugins.OpenPluginDirectory(plugin.ID, v.reportDirectory) })
		action(c, theme, "delete", lucide.Trash(), locales.PythonPluginsDeletePlugin(), nui.Error, nui.Ghost, false, func() { v.deleting.plugin = true })
	})
}

type fileRow struct {
	key, label, path string
	depth            int
	directory        bool
}

type fileNode struct {
	children map[string]*fileNode
	file     bool
}

// FileTreeRows arranges paths as a tree, folders first.
func fileTreeRows(paths []string) []fileRow {
	root := &fileNode{children: map[string]*fileNode{}}
	for _, path := range paths {
		node := root
		parts := strings.Split(path, "/")
		for index, part := range parts {
			child := node.children[part]
			if child == nil {
				child = &fileNode{children: map[string]*fileNode{}}
				node.children[part] = child
			}
			if index == len(parts)-1 {
				child.file = true
			}
			node = child
		}
	}
	var rows []fileRow
	var visit func(*fileNode, string, int)
	visit = func(node *fileNode, prefix string, depth int) {
		names := make([]string, 0, len(node.children))
		for name := range node.children {
			names = append(names, name)
		}
		isDirectory := func(name string) bool { return len(node.children[name].children) > 0 && !node.children[name].file }
		slices.SortFunc(names, func(a, b string) int {
			if isDirectory(a) != isDirectory(b) {
				if isDirectory(a) {
					return -1
				}
				return 1
			}
			return strings.Compare(a, b)
		})
		for _, name := range names {
			child := node.children[name]
			path := name
			if prefix != "" {
				path = prefix + "/" + name
			}
			directory := isDirectory(name)
			key := "file:" + path
			if directory {
				key = "dir:" + path
			}
			rows = append(rows, fileRow{key: key, label: name, path: path, depth: depth, directory: directory})
			if len(child.children) > 0 {
				visit(child, path, depth+1)
			}
		}
	}
	visit(root, "", 0)
	return rows
}

func (v *View) filesPane(c *ui.Context, theme nui.Theme) {
	plugins := v.Data.Plugins
	ui.Column(c.Key("files")).Width(220).Shrink(0).FillHeight().BorderWidth(0, 1, 0, 0).BorderColor(theme.Border).Children(func() {
		ui.Row(c.Key("files-head")).FillWidth().Shrink(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Padding(6, 8).Children(func() {
			ui.Text(c, locales.PythonPluginsFiles()).FontSize(12).Bold().TextColor(theme.Muted)
			ui.Row(c).Gap(2).Children(func() {
				none := plugins.SelectedFilePath == ""
				action(c, theme, "new-file", lucide.FilePlus(), locales.PythonPluginsNewFile(), nui.Neutral, nui.Ghost, false, func() { v.fileModal.show(false, "") })
				action(c, theme, "rename-file", lucide.Pencil(), locales.PythonPluginsRenameFile(), nui.Neutral, nui.Ghost, none, func() { v.fileModal.show(true, plugins.SelectedFilePath) })
				action(c, theme, "delete-file", lucide.Trash(), locales.PythonPluginsDeleteFile(), nui.Error, nui.Ghost, none, func() { v.deleting.file = true })
			})
		})
		ui.Scroll(c.Key("file-tree")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(0, 4, 4, 4).Children(func() {
				for _, row := range fileTreeRows(plugins.Files) {
					v.fileRow(c, theme, row)
				}
			})
		})
	})
}

func (v *View) fileRow(c *ui.Context, theme nui.Theme, row fileRow) {
	plugins := v.Data.Plugins
	selected := !row.directory && plugins.SelectedFilePath == row.path
	line := ui.Row(c.Key(row.key)).FillWidth().Height(26).AlignItems(ui.Center).Gap(4).Padding(0, 6, 0, float32(row.depth)*12+6).Radius(theme.Radius).Label(row.path)
	if selected {
		line.Background(theme.Primary.Alpha(0.1))
	} else if !row.directory && line.Hovered() {
		line.Background(theme.Elevated)
	}
	if !row.directory {
		path := row.path
		line.OnClick(func() {
			plugins.SelectFile(path, func(err error) {
				if err != nil {
					v.Notifier.Error(locales.PythonPluginsLoadFailed(err.Error()))
				}
			})
		})
	}
	line.Children(func() {
		icon, color := lucide.FileCode(), theme.Text
		switch {
		case row.directory:
			icon, color = lucide.Folder(), theme.Muted
		case selected:
			color = theme.Primary
		}
		ui.Icon(c, icon).Size(14, 14).Shrink(0).TextColor(color)
		ui.Text(c, row.label).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(13).TextColor(color)
		if selected && plugins.EditorDirty() {
			ui.Box(c.Key("dirty")).Size(6, 6).Shrink(0).Radius(3).Background(theme.Primary)
		}
	})
}

func (v *View) editorPane(c *ui.Context, theme nui.Theme) {
	plugins := v.Data.Plugins
	ui.Column(c.Key("editor")).FillWidth().Grow(3).Basis(0).MinHeight(0).Children(func() {
		if plugins.SelectedFilePath == "" {
			ui.Column(c).Fill().Center().Children(func() {
				nui.Empty(c, theme, nui.EmptyOptions{Icon: lucide.FileCode(), Title: locales.PythonPluginsNoFileSelected(), Variant: nui.Ghost})
			})
		} else {
			ui.Box(c.Key(fileLanguage(plugins.SelectedFilePath) == "python")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
				v.activeEditor().Build(c, ui.Font{Family: theme.MonoFont, Size: 13}).Fill()
			})
		}
		if plugins.EditorError != "" {
			ui.Row(c.Key("editor-error")).FillWidth().Shrink(0).Gap(6).AlignItems(ui.Center).Padding(6, 10).Background(theme.Error.Alpha(0.1)).Children(func() {
				ui.Icon(c, lucide.CircleAlert()).Size(14, 14).Shrink(0).TextColor(theme.Error)
				ui.Text(c, plugins.EditorError).FontSize(12).TextColor(theme.Error).Grow(1).Basis(0).MinWidth(0)
			})
		}
	})
}

func (v *View) detailPane(c *ui.Context, theme nui.Theme, plugin *store.Plugin) {
	items := []nui.TabItem{{ID: "rules", Label: locales.PythonPluginsRules()}, {ID: "params", Label: locales.PythonPluginsParams()}}
	ui.Column(c.Key("detail")).FillWidth().Grow(2).Basis(0).MinHeight(0).BorderWidth(1, 0, 0, 0).BorderColor(theme.Border).Children(func() {
		ui.Box(c.Key("tabs")).FillWidth().Shrink(0).PaddingX(6).Children(func() {
			nui.Tabs(c.Key("detail-tabs"), theme, &v.detailTab, items, nui.TabsOptions{Label: locales.PythonPluginsRules(), Size: nui.SM, Content: nui.Set(false)})
		})
		ui.Column(c.Key(v.detailTab)).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
			if v.detailTab == "params" {
				v.paramsPane(c, theme)
				return
			}
			v.rules.build(c, theme, v, plugin)
		})
	})
}

func (v *View) paramsPane(c *ui.Context, theme nui.Theme) {
	plugins := v.Data.Plugins
	ui.Row(c.Key("params-head")).FillWidth().Shrink(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Padding(6, 10).Gap(8).Children(func() {
		ui.Text(c, locales.PythonPluginsParamsHint()).FontSize(12).TextColor(theme.Muted).Grow(1).Basis(0).MinWidth(0)
		ui.Row(c).Gap(4).Children(func() {
			nui.Button(c.Key("format"), theme, locales.PythonPluginsFormat(), nui.ButtonOptions{Variant: nui.Ghost, Color: nui.Neutral, Size: nui.XS, Leading: lucide.TextAlignStart()}).OnClick(v.formatParams)
			saving := plugins.IsMutating("params:save:" + plugins.SelectedPluginID)
			save := nui.Button(c.Key("save"), theme, locales.PythonPluginsSaveParams(), nui.ButtonOptions{Size: nui.XS, Leading: lucide.Save(), Disabled: !plugins.ParamsDirty(), Loading: saving})
			if plugins.ParamsDirty() {
				save.OnClick(v.saveParams)
			}
		})
	})
	if plugins.ParamsError != "" {
		ui.Box(c.Key("params-error")).FillWidth().Shrink(0).Padding(0, 10, 6, 10).Children(func() {
			nui.Alert(c, theme, nui.AlertOptions{Description: plugins.ParamsError, Color: nui.Error, Variant: nui.Soft})
		})
	}
	ui.Box(c.Key("params-editor")).FillWidth().Grow(1).Basis(0).MinHeight(0).Children(func() {
		v.params.Build(c, ui.Font{Family: theme.MonoFont, Size: 13}).Fill()
	})
}

func (v *View) formatParams() {
	plugins := v.Data.Plugins
	if err := store.ValidatePluginParams(plugins.ParamsDraft); err != nil {
		if err.Error() == "plugin params must be a JSON object" {
			plugins.ParamsError = locales.PythonPluginsParamsObjectRequired()
		} else {
			plugins.ParamsError = err.Error()
		}
		return
	}
	plugins.ParamsDraft, plugins.ParamsError = store.PrettyPluginParams(plugins.ParamsDraft), ""
}

func (v *View) statusBar(c *ui.Context, theme nui.Theme, plugin *store.Plugin) {
	ui.Row(c.Key("status-bar")).FillWidth().Shrink(0).Height(32).AlignItems(ui.Center).Gap(10).PaddingX(12).BorderWidth(1, 0, 0, 0).BorderColor(theme.Border).Children(func() {
		color := nui.Warning
		switch plugin.ValidationStatus {
		case "valid":
			color = nui.Success
		case "invalid":
			color = nui.Error
		}
		nui.Badge(c.Key("validation"), theme, validationLabel(plugin.ValidationStatus), nui.BadgeOptions{Color: color, Variant: nui.Soft, Size: nui.SM})
		revision := locales.PythonPluginsNoRevision()
		if plugin.ActiveRevision != "" {
			revision = plugin.ActiveRevision[:min(12, len(plugin.ActiveRevision))]
		}
		label := ui.Text(c.Key("revision"), locales.PythonPluginsRevision(revision)).FontSize(12).Font(theme.MonoFont).TextColor(theme.Muted).Shrink(0)
		if plugin.ActiveRevision != "" {
			nui.Tooltip(c, theme, label, plugin.ActiveRevision, nui.TooltipOptions{})
		}
		ui.Text(c.Key("validation-error"), plugin.ValidationError).SingleLine().Grow(1).Basis(0).MinWidth(0).FontSize(12).TextColor(theme.Error)
		state := locales.PythonPluginsDisabled()
		if plugin.Enabled {
			state = locales.PythonPluginsEnabled()
		}
		ui.Text(c.Key("state"), state).FontSize(12).TextColor(theme.Muted)
	})
}

type pluginDialog struct {
	open              bool
	name, description string
	err               string
}

func (d *pluginDialog) showEmpty() { d.show("", "") }

func (d *pluginDialog) show(name, description string) {
	*d = pluginDialog{open: true, name: name, description: description}
}

func (d *pluginDialog) build(c *ui.Context, theme nui.Theme, v *View, title, confirm, key string, submit func(*pluginDialog)) {
	if !d.open {
		return
	}
	v.Dispatcher.NoteModal()
	busy := v.Data.Plugins.IsMutating(key)
	nui.ModalWithOptions(c.Key(title), theme, &d.open, nui.ModalOptions{Title: title, Width: nui.Set[float32](460), Dismissible: nui.Set(!busy), Footer: func(c *ui.Context, palette nui.Theme) {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			nui.Button(c.Key("cancel"), palette, locales.PythonPluginsCancel(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Disabled: busy}).OnClick(func() { d.open = false })
			nui.Button(c.Key("submit"), palette, confirm, nui.ButtonOptions{Loading: busy, AccessibleLabel: confirm + " " + title}).OnClick(func() { submit(d) })
		})
	}}, func(palette nui.Theme) {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			nui.FormField(c.Key("name"), palette, nui.FormFieldOptions{Label: locales.PythonPluginsName()}, func(field nui.Theme) {
				nui.Input(c, field, &d.name, nui.InputOptions{Label: locales.PythonPluginsName(), Disabled: busy, OnSubmit: func(string) { submit(d) }}).FillWidth()
			})
			nui.FormField(c.Key("description"), palette, nui.FormFieldOptions{Label: locales.PythonPluginsDescription()}, func(field nui.Theme) {
				nui.Textarea(c, field, &d.description, nui.TextareaOptions{InputOptions: nui.InputOptions{Label: locales.PythonPluginsDescription(), Disabled: busy}, Rows: 3}).FillWidth()
			})
			if d.err != "" {
				nui.Alert(c.Key("error"), palette, nui.AlertOptions{Description: d.err, Color: nui.Error, Variant: nui.Soft})
			}
		})
	})
}

type fileDialog struct {
	open   bool
	rename bool
	path   string
	err    string
}

func (d *fileDialog) show(rename bool, path string) {
	*d = fileDialog{open: true, rename: rename, path: path}
}

func (d *fileDialog) submit(v *View) {
	path := strings.TrimSpace(d.path)
	if path == "" {
		d.err = locales.PythonPluginsFilePathRequired()
		return
	}
	d.err = ""
	finish := func(_ *store.Plugin, err error) {
		if err != nil {
			d.err = err.Error()
			return
		}
		d.open = false
	}
	if d.rename {
		v.Data.Plugins.RenameFile(path, finish)
		return
	}
	v.Data.Plugins.CreateFile(path, finish)
}

func (d *fileDialog) build(c *ui.Context, theme nui.Theme, v *View) {
	if !d.open {
		return
	}
	v.Dispatcher.NoteModal()
	title := locales.PythonPluginsNewFile()
	if d.rename {
		title = locales.PythonPluginsRenameFile()
	}
	nui.ModalWithOptions(c.Key("file-dialog"), theme, &d.open, nui.ModalOptions{Title: title, Width: nui.Set[float32](460), Footer: func(c *ui.Context, palette nui.Theme) {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			nui.Button(c.Key("cancel"), palette, locales.PythonPluginsCancel(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral}).OnClick(func() { d.open = false })
			nui.Button(c.Key("submit"), palette, locales.PythonPluginsConfirm(), nui.ButtonOptions{}).OnClick(func() { d.submit(v) })
		})
	}}, func(palette nui.Theme) {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			nui.FormField(c.Key("path"), palette, nui.FormFieldOptions{Label: locales.PythonPluginsFilePath(), Hint: locales.PythonPluginsFilePathHint()}, func(field nui.Theme) {
				nui.Input(c, field, &d.path, nui.InputOptions{Label: locales.PythonPluginsFilePath(), OnSubmit: func(string) { d.submit(v) }}).FillWidth()
			})
			if d.err != "" {
				nui.Alert(c.Key("error"), palette, nui.AlertOptions{Description: d.err, Color: nui.Error, Variant: nui.Soft})
			}
		})
	})
}

func (v *View) dialogs(c *ui.Context, theme nui.Theme, plugin *store.Plugin) {
	plugins := v.Data.Plugins
	if plugin != nil {
		v.metadata.build(c, theme, v, locales.PythonPluginsEditPlugin(), locales.PythonPluginsSave(), "plugin:update:"+plugin.ID, func(d *pluginDialog) {
			if strings.TrimSpace(d.name) == "" {
				d.err = locales.PythonPluginsNameRequired()
				return
			}
			d.err = ""
			plugins.UpdateMetadata(plugin.ID, d.name, d.description, func(_ *store.Plugin, err error) {
				if err != nil {
					d.err = err.Error()
					return
				}
				d.open = false
				v.Notifier.Success(locales.PythonPluginsMetadataSaved())
			})
		})
	}
	v.fileModal.build(c, theme, v)
	if v.deleting.file {
		v.confirmModal(c, theme, "delete-file-confirm", locales.PythonPluginsDeleteFile(), locales.PythonPluginsDeleteFileConfirm(plugins.SelectedFilePath), &v.deleting.file, "file:delete:"+plugins.SelectedPluginID+":"+plugins.SelectedFilePath, func() {
			plugins.DeleteFile(func(_ *store.Plugin, err error) {
				if err != nil {
					v.Notifier.Error(locales.PythonPluginsDeleteFileFailed(err.Error()))
					return
				}
				v.deleting.file = false
				v.Notifier.Success(locales.PythonPluginsFileDeleted())
			})
		})
	}
	if v.deleting.plugin && plugin != nil {
		v.confirmModal(c, theme, "delete-plugin-confirm", locales.PythonPluginsDeletePlugin(), locales.PythonPluginsDeletePluginConfirm(plugin.Name), &v.deleting.plugin, "plugin:delete:"+plugin.ID, func() {
			plugins.DeletePlugin(plugin.ID, func(err error) {
				if err != nil {
					v.Notifier.Error(locales.PythonPluginsDeleteFailed(err.Error()))
					return
				}
				v.deleting.plugin = false
				v.Notifier.Success(locales.PythonPluginsDeleteSuccess())
			})
		})
	}
}

func (v *View) confirmModal(c *ui.Context, theme nui.Theme, key, title, message string, open *bool, mutation string, run func()) {
	v.Dispatcher.NoteModal()
	busy := v.Data.Plugins.IsMutating(mutation)
	nui.ModalWithOptions(c.Key(key), theme, open, nui.ModalOptions{Title: title, Width: nui.Set[float32](440), Footer: func(c *ui.Context, palette nui.Theme) {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			nui.Button(c.Key("cancel"), palette, locales.PythonPluginsCancel(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral, Disabled: busy}).OnClick(func() { *open = false })
			nui.Button(c.Key("confirm"), palette, locales.PythonPluginsDelete(), nui.ButtonOptions{Color: nui.Error, Loading: busy}).OnClick(run)
		})
	}}, func(palette nui.Theme) {
		ui.Text(c, message).FontSize(14).TextColor(palette.Text).FillWidth()
	})
}
