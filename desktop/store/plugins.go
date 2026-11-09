package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	pythonpluginservice "github.com/josexy/flowlens/backend/services/python_plugin_service"
	"github.com/josexy/flowlens/desktop/runtime"
)

// Plugin types of the Python plugin service.
type (
	Plugin              = pythonpluginservice.Plugin
	PluginRule          = pythonpluginservice.Rule
	PluginRuntimeStatus = pythonpluginservice.RuntimeStatus
	PluginFile          = pythonpluginservice.ManagedPluginFile
	PluginRegistryEvent = pythonpluginservice.RegistryEvent
	PluginStatusEvent   = pythonpluginservice.StatusEvent
)

// PluginBackend manages Python plugins.
type PluginBackend interface {
	ListPlugins() ([]*Plugin, error)
	GetPlugin(id string) (*Plugin, error)
	CreatePlugin(input pythonpluginservice.CreatePluginInput) (*Plugin, error)
	UpdatePlugin(id string, input pythonpluginservice.UpdatePluginInput) (*Plugin, error)
	UpdatePluginParams(id, params string) (*Plugin, error)
	DeletePlugin(id string) error
	ReorderPlugins(ids []string) error
	SetPluginEnabled(id string, enabled bool) (*Plugin, error)
	ValidatePlugin(id string) (*Plugin, error)
	ReloadPlugin(id string) (*Plugin, error)
	ListPluginFiles(id string) ([]string, error)
	ReadPluginFile(id, path string) (*PluginFile, error)
	WritePluginFile(id, path, content string) (*Plugin, error)
	RenamePluginFile(id, oldPath, newPath string) (*Plugin, error)
	DeletePluginFile(id, path string) (*Plugin, error)
	CreateRule(id string, input pythonpluginservice.CreateRuleInput) (*PluginRule, error)
	UpdateRule(id, ruleID string, input pythonpluginservice.UpdateRuleInput) (*PluginRule, error)
	DeleteRule(id, ruleID string) error
	ReorderRules(id string, ruleIDs []string) error
	GetRuntimeStatus() (*PluginRuntimeStatus, error)
	OpenPluginsDirectory() error
	OpenPluginDirectory(id string) error
}

// PluginEvents subscribes to plugin changes.
type PluginEvents func(registry func(PluginRegistryEvent), status func(PluginStatusEvent)) (off func())

type pluginDraft struct {
	content, saved, err string
}

// PluginsStore holds the Python plugins and their editors.
type PluginsStore struct {
	rt      Runtime
	backend PluginBackend
	connect PluginEvents

	Plugins          []*Plugin
	SelectedPluginID string
	Files            []string
	SelectedFilePath string
	EditorContent    string
	SavedEditor      string
	EditorError      string
	editorPluginID   string
	ParamsDraft      string
	SavedParams      string
	ParamsError      string
	Runtime          *PluginRuntimeStatus
	LoadingPlugins   bool
	LoadingWorkspace bool
	Error            string
	Version          uint64

	active        map[string]bool
	waiters       map[string][]func(any, error)
	selectedFiles map[string]string
	editorDrafts  map[string]pluginDraft
	paramsDrafts  map[string]pluginDraft

	listSequence, pluginSequence, filesSequence, fileSequence, statusSequence int
	refreshing                                                                bool
	pendingEvents                                                             []PluginRegistryEvent
	off                                                                       func()
	lastRegistry, lastStatus                                                  uint64
	initialized                                                               bool
}

// NewPluginsStore returns an unloaded store.
func NewPluginsStore(rt Runtime, backend PluginBackend, connect PluginEvents) *PluginsStore {
	return &PluginsStore{rt: rt, backend: backend, connect: connect, ParamsDraft: "{}", SavedParams: "{}", active: map[string]bool{}, waiters: map[string][]func(any, error){},
		selectedFiles: map[string]string{}, editorDrafts: map[string]pluginDraft{}, paramsDrafts: map[string]pluginDraft{}}
}

// SelectedPlugin returns the plugin on show.
func (s *PluginsStore) SelectedPlugin() *Plugin { return s.plugin(s.SelectedPluginID) }

func (s *PluginsStore) plugin(id string) *Plugin {
	index := slices.IndexFunc(s.Plugins, func(plugin *Plugin) bool { return plugin.ID == id })
	if index < 0 {
		return nil
	}
	return s.Plugins[index]
}

// EditorDirty reports whether the open file has unsaved edits.
func (s *PluginsStore) EditorDirty() bool {
	return s.SelectedFilePath != "" && s.EditorContent != s.SavedEditor
}

// ParamsDirty reports whether the params have unsaved edits.
func (s *PluginsStore) ParamsDirty() bool { return s.ParamsDraft != s.SavedParams }

// IsMutating reports whether a keyed change is running.
func (s *PluginsStore) IsMutating(key string) bool { return s.active[key] }

func pluginMutationKey(kind string, parts ...string) string {
	return strings.Join(append([]string{kind}, parts...), ":")
}

func pluginDraftKey(id, path string) string { return id + "\x00" + path }

func sortPluginRules(rules []*PluginRule) []*PluginRule {
	result := slices.DeleteFunc(slices.Clone(rules), func(rule *PluginRule) bool { return rule == nil })
	slices.SortStableFunc(result, func(left, right *PluginRule) int {
		if left.SortOrder != right.SortOrder {
			return left.SortOrder - right.SortOrder
		}
		return strings.Compare(left.ID, right.ID)
	})
	return result
}

func normalizePlugin(plugin *Plugin) *Plugin {
	copied := *plugin
	copied.Rules = sortPluginRules(plugin.Rules)
	return &copied
}

func sortPlugins(plugins []*Plugin) {
	slices.SortStableFunc(plugins, func(left, right *Plugin) int {
		if left.SortOrder != right.SortOrder {
			return left.SortOrder - right.SortOrder
		}
		return strings.Compare(left.ID, right.ID)
	})
}

// PrettyPluginParams indents valid JSON and leaves anything else alone.
func PrettyPluginParams(value string) string {
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(strings.TrimSpace(value)), "", "  "); err != nil {
		return value
	}
	return out.String()
}

// ValidatePluginParams checks that params are a JSON object.
func ValidatePluginParams(value string) error {
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return err
	}
	if _, ok := decoded.(map[string]any); !ok {
		return errors.New("plugin params must be a JSON object")
	}
	return nil
}

func (s *PluginsStore) upsert(value *Plugin) *Plugin {
	if value == nil {
		return nil
	}
	plugin := normalizePlugin(value)
	next := slices.DeleteFunc(slices.Clone(s.Plugins), func(item *Plugin) bool { return item.ID == plugin.ID })
	next = append(next, plugin)
	sortPlugins(next)
	s.Plugins = next
	s.Version++
	return plugin
}

func (s *PluginsStore) stashEditor() {
	if s.editorPluginID == "" || s.SelectedFilePath == "" {
		return
	}
	s.editorDrafts[pluginDraftKey(s.editorPluginID, s.SelectedFilePath)] = pluginDraft{s.EditorContent, s.SavedEditor, s.EditorError}
}

func (s *PluginsStore) stashParams(id string) {
	if id != "" {
		s.paramsDrafts[id] = pluginDraft{s.ParamsDraft, s.SavedParams, s.ParamsError}
	}
}

func (s *PluginsStore) restoreParams(plugin *Plugin) {
	if draft, ok := s.paramsDrafts[plugin.ID]; ok {
		s.ParamsDraft, s.SavedParams, s.ParamsError = draft.content, draft.saved, draft.err
		return
	}
	value := PrettyPluginParams(plugin.ParamsJSON)
	s.ParamsDraft, s.SavedParams, s.ParamsError = value, value, ""
}

func (s *PluginsStore) clearEditor() {
	s.SelectedFilePath, s.editorPluginID, s.EditorContent, s.SavedEditor, s.EditorError = "", "", "", "", ""
}

func runPlugin[T any](s *PluginsStore, job string, work func() (T, error), done func(T, error)) {
	runtime.Run(s.rt.Jobs, "plugins:"+job, func(context.Context) (T, error) { return work() }, done)
}

// mutate runs one change per key; a second call joins the first.
func mutate[T any](s *PluginsStore, key string, work func() (T, error), after func(T, error) (T, error), done func(T, error)) {
	if s.active[key] {
		s.waiters[key] = append(s.waiters[key], func(value any, err error) {
			typed, _ := value.(T)
			done(typed, err)
		})
		return
	}
	s.active[key] = true
	runtime.Run(s.rt.Jobs, "plugins:mutate:"+key, func(context.Context) (T, error) { return work() }, func(value T, err error) {
		finish := func(result T, failure error) {
			delete(s.active, key)
			waiters := s.waiters[key]
			delete(s.waiters, key)
			done(result, failure)
			for _, waiter := range waiters {
				waiter(result, failure)
			}
		}
		if after != nil {
			value, err = after(value, err)
		}
		finish(value, err)
	})
}

// LoadPlugins reads the list.
func (s *PluginsStore) LoadPlugins(done func(error)) {
	s.listSequence++
	sequence := s.listSequence
	s.LoadingPlugins, s.Error = true, ""
	runPlugin(s, "list", s.backend.ListPlugins, func(values []*Plugin, err error) {
		if sequence != s.listSequence {
			if done != nil {
				done(nil)
			}
			return
		}
		s.LoadingPlugins = false
		if err != nil {
			s.Error = err.Error()
			if done != nil {
				done(err)
			}
			return
		}
		plugins := make([]*Plugin, 0, len(values))
		for _, value := range values {
			if value != nil {
				plugins = append(plugins, normalizePlugin(value))
			}
		}
		sortPlugins(plugins)
		s.Plugins = plugins
		s.Version++
		if s.plugin(s.SelectedPluginID) == nil {
			s.stashEditor()
			s.stashParams(s.SelectedPluginID)
			s.SelectedPluginID = ""
			if len(plugins) > 0 {
				s.SelectedPluginID = plugins[0].ID
			}
			if s.SelectedPluginID == "" {
				s.Files = nil
				s.clearEditor()
				s.ParamsDraft, s.SavedParams, s.ParamsError = "{}", "{}", ""
			}
		}
		if done != nil {
			done(nil)
		}
	})
}

func (s *PluginsStore) loadPlugin(id string, done func(error)) {
	if id == "" {
		done(nil)
		return
	}
	s.pluginSequence++
	sequence := s.pluginSequence
	runPlugin(s, "plugin", func() (*Plugin, error) { return s.backend.GetPlugin(id) }, func(value *Plugin, err error) {
		if sequence != s.pluginSequence || s.SelectedPluginID != id {
			done(nil)
			return
		}
		if err != nil {
			done(err)
			return
		}
		if plugin := s.upsert(value); plugin != nil {
			draft, drafted := s.paramsDrafts[plugin.ID]
			paramsDirty := s.SelectedPluginID == plugin.ID && s.ParamsDirty()
			if !paramsDirty && (!drafted || draft.content == draft.saved) {
				pretty := PrettyPluginParams(plugin.ParamsJSON)
				s.ParamsDraft, s.SavedParams, s.ParamsError = pretty, pretty, ""
				delete(s.paramsDrafts, plugin.ID)
			}
		}
		done(nil)
	})
}

func (s *PluginsStore) loadFile(id, path string, force bool, done func(error)) {
	s.fileSequence++
	sequence := s.fileSequence
	if id == "" || path == "" {
		s.clearEditor()
		done(nil)
		return
	}
	key := pluginDraftKey(id, path)
	if draft, ok := s.editorDrafts[key]; ok && !force {
		if s.SelectedPluginID == id {
			s.editorPluginID, s.SelectedFilePath = id, path
			s.EditorContent, s.SavedEditor, s.EditorError = draft.content, draft.saved, draft.err
		}
		done(nil)
		return
	}
	runPlugin(s, "file", func() (*PluginFile, error) { return s.backend.ReadPluginFile(id, path) }, func(value *PluginFile, err error) {
		if sequence != s.fileSequence || s.SelectedPluginID != id || s.selectedFiles[id] != path {
			done(nil)
			return
		}
		if err == nil && value == nil {
			err = errors.New("read plugin file returned no content")
		}
		if err != nil {
			done(err)
			return
		}
		delete(s.editorDrafts, key)
		s.editorPluginID, s.SelectedFilePath = id, value.Path
		s.EditorContent, s.SavedEditor, s.EditorError = value.Content, value.Content, ""
		done(nil)
	})
}

func (s *PluginsStore) loadFiles(id string, reloadClean bool, done func(error)) {
	if id == "" {
		s.Files = nil
		s.clearEditor()
		done(nil)
		return
	}
	s.filesSequence++
	sequence := s.filesSequence
	runPlugin(s, "files", func() ([]string, error) { return s.backend.ListPluginFiles(id) }, func(values []string, err error) {
		if sequence != s.filesSequence || s.SelectedPluginID != id {
			done(nil)
			return
		}
		if err != nil {
			done(err)
			return
		}
		files := slices.Clone(values)
		slices.Sort(files)
		s.Files = files
		path := s.selectedFiles[id]
		if !slices.Contains(files, path) {
			path = ""
			switch {
			case slices.Contains(files, "main.py"):
				path = "main.py"
			case len(files) > 0:
				path = files[0]
			}
			s.selectedFiles[id] = path
		}
		if path == "" {
			s.clearEditor()
			done(nil)
			return
		}
		draft, drafted := s.editorDrafts[pluginDraftKey(id, path)]
		editorDirty := s.editorPluginID == id && s.SelectedFilePath == path && s.EditorContent != s.SavedEditor
		force := reloadClean && !editorDirty && (!drafted || draft.content == draft.saved)
		s.loadFile(id, path, force, done)
	})
}

func (s *PluginsStore) loadWorkspace(id string, done func(error)) {
	if id == "" {
		done(nil)
		return
	}
	s.LoadingWorkspace = true
	var firstErr error
	s.loadPlugin(id, func(err error) {
		firstErr = err
		s.loadFiles(id, false, func(err error) {
			if s.SelectedPluginID == id {
				s.LoadingWorkspace = false
			}
			if firstErr == nil {
				firstErr = err
			}
			done(firstErr)
		})
	})
}

// SelectPlugin shows a plugin, keeping the drafts of the last one.
func (s *PluginsStore) SelectPlugin(id string, done func(error)) {
	if id == "" || id == s.SelectedPluginID {
		done(nil)
		return
	}
	s.stashEditor()
	s.stashParams(s.SelectedPluginID)
	s.SelectedPluginID = id
	s.Files = nil
	s.clearEditor()
	if plugin := s.plugin(id); plugin != nil {
		s.restoreParams(plugin)
	}
	s.loadWorkspace(id, done)
}

// SelectFile opens a file of the selected plugin.
func (s *PluginsStore) SelectFile(path string, done func(error)) {
	id := s.SelectedPluginID
	if id == "" || path == "" || path == s.SelectedFilePath {
		done(nil)
		return
	}
	s.stashEditor()
	s.selectedFiles[id] = path
	s.loadFile(id, path, false, done)
}

// LoadRuntimeStatus reads whether the runtime is ready.
func (s *PluginsStore) LoadRuntimeStatus(done func(error)) {
	s.statusSequence++
	sequence := s.statusSequence
	runPlugin(s, "status", s.backend.GetRuntimeStatus, func(value *PluginRuntimeStatus, err error) {
		if sequence == s.statusSequence && err == nil {
			s.Runtime = value
		}
		done(err)
	})
}

func (s *PluginsStore) drainRegistry() {
	if len(s.pendingEvents) == 0 {
		s.refreshing = false
		return
	}
	events := s.pendingEvents
	s.pendingEvents = nil
	s.LoadPlugins(func(err error) {
		if err != nil {
			s.refreshing = false
			return
		}
		selected := s.SelectedPluginID
		touched, files := false, false
		for _, event := range events {
			if event.PluginID == selected && selected != "" {
				touched = true
				files = files || event.Change == "files_changed"
			}
		}
		next := func(error) { s.drainRegistry() }
		switch {
		case !touched:
			s.drainRegistry()
		case files:
			s.loadFiles(selected, true, next)
		default:
			s.loadPlugin(selected, next)
		}
	})
}

func (s *PluginsStore) onRegistry(event PluginRegistryEvent) {
	if event.EventID <= s.lastRegistry {
		return
	}
	s.lastRegistry = event.EventID
	s.pendingEvents = append(s.pendingEvents, event)
	if !s.refreshing {
		s.refreshing = true
		s.drainRegistry()
	}
}

func (s *PluginsStore) onStatus(event PluginStatusEvent) {
	if event.EventID <= s.lastStatus {
		return
	}
	s.lastStatus = event.EventID
	status := event.Status
	s.Runtime = &status
}

// Initialize follows changes and loads the plugins once.
func (s *PluginsStore) Initialize() {
	if s.initialized {
		return
	}
	s.initialized = true
	if s.connect != nil {
		s.off = s.connect(s.onRegistry, s.onStatus)
	}
	report := func(err error) {
		if err != nil {
			s.Error = err.Error()
		}
	}
	s.LoadRuntimeStatus(report)
	s.LoadPlugins(func(err error) {
		report(err)
		if s.SelectedPluginID != "" {
			s.loadWorkspace(s.SelectedPluginID, report)
		}
	})
}

// Cleanup stops following changes.
func (s *PluginsStore) Cleanup() {
	if s.off != nil {
		s.off()
		s.off = nil
	}
}

// CreatePlugin adds a plugin and shows it.
func (s *PluginsStore) CreatePlugin(name, description string, done func(*Plugin, error)) {
	mutate(s, "plugin:create", func() (*Plugin, error) {
		return s.backend.CreatePlugin(pythonpluginservice.CreatePluginInput{Name: name, Description: description, ParamsJSON: "{}"})
	}, nil, func(value *Plugin, err error) {
		if err != nil {
			done(nil, err)
			return
		}
		plugin := s.upsert(value)
		if plugin == nil {
			done(nil, errors.New("create plugin returned no plugin"))
			return
		}
		s.SelectPlugin(plugin.ID, func(err error) { done(plugin, err) })
	})
}

func (s *PluginsStore) upsertAfter(message string) func(*Plugin, error) (*Plugin, error) {
	return func(value *Plugin, err error) (*Plugin, error) {
		if err != nil {
			return nil, err
		}
		plugin := s.upsert(value)
		if plugin == nil {
			return nil, errors.New(message)
		}
		return plugin, nil
	}
}

// UpdateMetadata changes a plugin's name and description.
func (s *PluginsStore) UpdateMetadata(id, name, description string, done func(*Plugin, error)) {
	current := s.plugin(id)
	if current == nil {
		done(nil, errors.New("plugin not found"))
		return
	}
	params := current.ParamsJSON
	mutate(s, pluginMutationKey("plugin:update", id), func() (*Plugin, error) {
		return s.backend.UpdatePlugin(id, pythonpluginservice.UpdatePluginInput{Name: name, Description: description, ParamsJSON: params})
	}, s.upsertAfter("update plugin returned no plugin"), done)
}

// DeletePlugin removes a plugin and its files.
func (s *PluginsStore) DeletePlugin(id string, done func(error)) {
	mutate(s, pluginMutationKey("plugin:delete", id), func() (struct{}, error) {
		return struct{}{}, s.backend.DeletePlugin(id)
	}, nil, func(_ struct{}, err error) {
		if err != nil {
			done(err)
			return
		}
		s.Plugins = slices.DeleteFunc(slices.Clone(s.Plugins), func(plugin *Plugin) bool { return plugin.ID == id })
		s.Version++
		for key := range s.editorDrafts {
			if strings.HasPrefix(key, id+"\x00") {
				delete(s.editorDrafts, key)
			}
		}
		delete(s.paramsDrafts, id)
		delete(s.selectedFiles, id)
		if s.SelectedPluginID != id {
			done(nil)
			return
		}
		s.SelectedPluginID = ""
		s.Files = nil
		s.clearEditor()
		if len(s.Plugins) == 0 {
			done(nil)
			return
		}
		selected := s.Plugins[0]
		s.SelectedPluginID = selected.ID
		s.restoreParams(selected)
		s.loadWorkspace(selected.ID, done)
	})
}

func (s *PluginsStore) reloadOnFailure(err error) {
	if err != nil {
		s.LoadPlugins(nil)
	}
}

func (s *PluginsStore) changePlugin(key, message string, work func() (*Plugin, error), done func(*Plugin, error)) {
	mutate(s, key, work, s.upsertAfter(message), func(plugin *Plugin, err error) {
		s.reloadOnFailure(err)
		done(plugin, err)
	})
}

// SetPluginEnabled turns a plugin on or off.
func (s *PluginsStore) SetPluginEnabled(id string, enabled bool, done func(*Plugin, error)) {
	s.changePlugin(pluginMutationKey("plugin:enabled", id), "update plugin state returned no plugin", func() (*Plugin, error) {
		return s.backend.SetPluginEnabled(id, enabled)
	}, done)
}

// ValidatePlugin checks a plugin's saved files.
func (s *PluginsStore) ValidatePlugin(id string, done func(*Plugin, error)) {
	s.changePlugin(pluginMutationKey("plugin:validate", id), "validation returned no plugin", func() (*Plugin, error) {
		return s.backend.ValidatePlugin(id)
	}, done)
}

// ReloadPlugin reloads a plugin from its saved files.
func (s *PluginsStore) ReloadPlugin(id string, done func(*Plugin, error)) {
	s.changePlugin(pluginMutationKey("plugin:reload", id), "reload returned no plugin", func() (*Plugin, error) {
		return s.backend.ReloadPlugin(id)
	}, done)
}

// SetOrderLocal arranges the list before the order is saved.
func (s *PluginsStore) SetOrderLocal(ordered []*Plugin) {
	next := make([]*Plugin, len(ordered))
	for index, plugin := range ordered {
		copied := *plugin
		copied.SortOrder = index
		next[index] = &copied
	}
	s.Plugins = next
	s.Version++
}

// ReorderPlugins saves the plugin order.
func (s *PluginsStore) ReorderPlugins(ids []string, done func(error)) {
	previous := slices.Clone(s.Plugins)
	ordered := make([]*Plugin, 0, len(ids))
	for _, id := range ids {
		if plugin := s.plugin(id); plugin != nil {
			ordered = append(ordered, plugin)
		}
	}
	s.SetOrderLocal(ordered)
	mutate(s, "plugin:reorder", func() (struct{}, error) { return struct{}{}, s.backend.ReorderPlugins(ids) }, nil, func(_ struct{}, err error) {
		if err != nil {
			s.LoadPlugins(func(loadErr error) {
				if loadErr != nil {
					s.Plugins = previous
				}
				done(err)
			})
			return
		}
		done(nil)
	})
}

// SaveFile writes the open file.
func (s *PluginsStore) SaveFile(done func(*Plugin, error)) {
	id, path := s.SelectedPluginID, s.SelectedFilePath
	if id == "" || path == "" {
		done(nil, errors.New("select a plugin file before saving"))
		return
	}
	content, savedBefore := s.EditorContent, s.SavedEditor
	key := pluginDraftKey(id, path)
	s.EditorError = ""
	mutate(s, pluginMutationKey("file:save", id, path), func() (*Plugin, error) {
		return s.backend.WritePluginFile(id, path, content)
	}, func(value *Plugin, err error) (*Plugin, error) {
		var plugin *Plugin
		if err == nil {
			if plugin = s.upsert(value); plugin == nil {
				err = errors.New("save returned no plugin")
			}
		}
		current := s.SelectedPluginID == id && s.SelectedFilePath == path
		if err != nil {
			if current {
				s.EditorError = err.Error()
				s.stashEditor()
			} else {
				draft, drafted := s.editorDrafts[key]
				if !drafted {
					draft = pluginDraft{content: content, saved: savedBefore}
				}
				draft.err = err.Error()
				s.editorDrafts[key] = draft
			}
			return nil, err
		}
		if current {
			s.SavedEditor = content
			s.editorDrafts[key] = pluginDraft{content: s.EditorContent, saved: content}
		} else if draft, drafted := s.editorDrafts[key]; drafted {
			s.editorDrafts[key] = pluginDraft{content: draft.content, saved: content}
		}
		return plugin, nil
	}, done)
}

func (s *PluginsStore) reloadFiles(id string, plugin *Plugin, done func(*Plugin, error)) {
	s.loadFiles(id, false, func(err error) { done(plugin, err) })
}

// CreateFile adds an empty file and opens it.
func (s *PluginsStore) CreateFile(path string, done func(*Plugin, error)) {
	id := s.SelectedPluginID
	if id == "" {
		done(nil, errors.New("select a plugin before creating a file"))
		return
	}
	mutate(s, pluginMutationKey("file:create", id, path), func() (*Plugin, error) {
		return s.backend.WritePluginFile(id, path, "")
	}, s.upsertAfter("create file returned no plugin"), func(plugin *Plugin, err error) {
		if err != nil {
			done(nil, err)
			return
		}
		s.selectedFiles[id] = path
		delete(s.editorDrafts, pluginDraftKey(id, path))
		s.reloadFiles(id, plugin, done)
	})
}

// RenameFile renames the open file, keeping its draft.
func (s *PluginsStore) RenameFile(next string, done func(*Plugin, error)) {
	id, current := s.SelectedPluginID, s.SelectedFilePath
	if id == "" || current == "" {
		done(nil, errors.New("select a plugin file before renaming"))
		return
	}
	s.stashEditor()
	mutate(s, pluginMutationKey("file:rename", id, current), func() (*Plugin, error) {
		return s.backend.RenamePluginFile(id, current, next)
	}, s.upsertAfter("rename file returned no plugin"), func(plugin *Plugin, err error) {
		if err != nil {
			done(nil, err)
			return
		}
		oldKey := pluginDraftKey(id, current)
		if draft, ok := s.editorDrafts[oldKey]; ok {
			delete(s.editorDrafts, oldKey)
			s.editorDrafts[pluginDraftKey(id, next)] = draft
		}
		s.selectedFiles[id] = next
		s.SelectedFilePath = ""
		s.reloadFiles(id, plugin, done)
	})
}

// DeleteFile removes the open file.
func (s *PluginsStore) DeleteFile(done func(*Plugin, error)) {
	id, path := s.SelectedPluginID, s.SelectedFilePath
	if id == "" || path == "" {
		done(nil, errors.New("select a plugin file before deleting"))
		return
	}
	mutate(s, pluginMutationKey("file:delete", id, path), func() (*Plugin, error) {
		return s.backend.DeletePluginFile(id, path)
	}, s.upsertAfter("delete file returned no plugin"), func(plugin *Plugin, err error) {
		if err != nil {
			done(nil, err)
			return
		}
		delete(s.editorDrafts, pluginDraftKey(id, path))
		delete(s.selectedFiles, id)
		s.reloadFiles(id, plugin, done)
	})
}

// SaveParams writes the params.
func (s *PluginsStore) SaveParams(done func(*Plugin, error)) {
	id := s.SelectedPluginID
	if id == "" {
		done(nil, errors.New("select a plugin before saving params"))
		return
	}
	toSave, savedBefore := s.ParamsDraft, s.SavedParams
	s.ParamsError = ""
	mutate(s, pluginMutationKey("params:save", id), func() (*Plugin, error) {
		if err := ValidatePluginParams(toSave); err != nil {
			return nil, err
		}
		return s.backend.UpdatePluginParams(id, toSave)
	}, func(value *Plugin, err error) (*Plugin, error) {
		var plugin *Plugin
		if err == nil {
			if plugin = s.upsert(value); plugin == nil {
				err = errors.New("save params returned no plugin")
			}
		}
		current := s.SelectedPluginID == id
		if err != nil {
			if current {
				s.ParamsError = err.Error()
				s.stashParams(id)
			} else {
				draft, drafted := s.paramsDrafts[id]
				if !drafted {
					draft = pluginDraft{content: toSave, saved: savedBefore}
				}
				draft.err = err.Error()
				s.paramsDrafts[id] = draft
			}
			return nil, err
		}
		pretty := PrettyPluginParams(plugin.ParamsJSON)
		if current {
			if s.ParamsDraft == toSave {
				s.ParamsDraft = pretty
			}
			s.SavedParams = pretty
			if s.ParamsDraft == pretty {
				delete(s.paramsDrafts, id)
			} else {
				s.stashParams(id)
			}
		} else if draft, drafted := s.paramsDrafts[id]; drafted {
			content := draft.content
			if content == toSave {
				content = pretty
			}
			if content == pretty {
				delete(s.paramsDrafts, id)
			} else {
				s.paramsDrafts[id] = pluginDraft{content: content, saved: pretty}
			}
		}
		return plugin, nil
	}, done)
}

func (s *PluginsStore) replaceRules(id string, rules []*PluginRule) {
	plugin := s.plugin(id)
	if plugin == nil {
		return
	}
	copied := *plugin
	copied.Rules = rules
	s.upsert(&copied)
}

func (s *PluginsStore) rulesOf(id string) []*PluginRule {
	if plugin := s.plugin(id); plugin != nil {
		return sortPluginRules(plugin.Rules)
	}
	return nil
}

// CreateRule adds a match-all rule.
func (s *PluginsStore) CreateRule(id string, done func(*PluginRule, error)) {
	mutate(s, pluginMutationKey("rule:create", id), func() (*PluginRule, error) {
		return s.backend.CreateRule(id, pythonpluginservice.CreateRuleInput{Enabled: true, Method: "*", URLPattern: "*"})
	}, nil, func(rule *PluginRule, err error) {
		if err == nil && rule == nil {
			err = errors.New("create rule returned no rule")
		}
		if err != nil {
			done(nil, err)
			return
		}
		s.replaceRules(id, append(s.rulesOf(id), rule))
		done(rule, nil)
	})
}

// UpdateRule changes a rule.
func (s *PluginsStore) UpdateRule(id, ruleID string, input pythonpluginservice.UpdateRuleInput, done func(*PluginRule, error)) {
	mutate(s, pluginMutationKey("rule:update", id, ruleID), func() (*PluginRule, error) {
		return s.backend.UpdateRule(id, ruleID, input)
	}, nil, func(rule *PluginRule, err error) {
		if err == nil && rule == nil {
			err = errors.New("update rule returned no rule")
		}
		if err != nil {
			done(nil, err)
			return
		}
		rules := s.rulesOf(id)
		for index := range rules {
			if rules[index].ID == rule.ID {
				rules[index] = rule
			}
		}
		s.replaceRules(id, rules)
		done(rule, nil)
	})
}

// DeleteRule removes a rule.
func (s *PluginsStore) DeleteRule(id, ruleID string, done func(error)) {
	mutate(s, pluginMutationKey("rule:delete", id, ruleID), func() (struct{}, error) {
		return struct{}{}, s.backend.DeleteRule(id, ruleID)
	}, nil, func(_ struct{}, err error) {
		if err == nil {
			s.replaceRules(id, slices.DeleteFunc(s.rulesOf(id), func(rule *PluginRule) bool { return rule.ID == ruleID }))
		}
		done(err)
	})
}

// ReorderRules saves the rule order.
func (s *PluginsStore) ReorderRules(id string, ruleIDs []string, done func(error)) {
	previous := s.rulesOf(id)
	byID := map[string]*PluginRule{}
	for _, rule := range previous {
		byID[rule.ID] = rule
	}
	ordered := make([]*PluginRule, 0, len(ruleIDs))
	for index, ruleID := range ruleIDs {
		if rule := byID[ruleID]; rule != nil {
			copied := *rule
			copied.SortOrder = index
			ordered = append(ordered, &copied)
		}
	}
	s.replaceRules(id, ordered)
	mutate(s, pluginMutationKey("rule:reorder", id), func() (struct{}, error) {
		return struct{}{}, s.backend.ReorderRules(id, ruleIDs)
	}, nil, func(_ struct{}, err error) {
		if err != nil {
			s.replaceRules(id, previous)
		}
		done(err)
	})
}

// OpenPluginDirectory shows a plugin's directory.
func (s *PluginsStore) OpenPluginDirectory(id string, done func(error)) {
	runPlugin(s, "open-plugin", func() (struct{}, error) { return struct{}{}, s.backend.OpenPluginDirectory(id) }, func(_ struct{}, err error) { done(err) })
}

// OpenPluginsDirectory shows the root directory.
func (s *PluginsStore) OpenPluginsDirectory(done func(error)) {
	runPlugin(s, "open-root", func() (struct{}, error) { return struct{}{}, s.backend.OpenPluginsDirectory() }, func(_ struct{}, err error) { done(err) })
}
