package plugins

import (
	"errors"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/pythonapi"
	"github.com/josexy/flowlens/internal/desktop/runtime/runtimetest"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	"github.com/josexy/flowlens/internal/pkg/database"
	"github.com/josexy/flowlens/internal/serviceruntime"
	pythonpluginservice "github.com/josexy/flowlens/internal/services/python_plugin_service"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// serviceBackend uses the real plugin service, except what needs Python.
type serviceBackend struct {
	*pythonpluginservice.PythonPluginService
	validate   func(string) (*store.Plugin, error)
	reload     func(string) (*store.Plugin, error)
	reorder    func([]string) error
	createFile func(string, string) (*store.Plugin, error)
	deleteFile func(string, string) (*store.Plugin, error)
	opened     []string
}

func (b *serviceBackend) CreatePluginFile(id, path string) (*store.Plugin, error) {
	if b.createFile != nil {
		return b.createFile(id, path)
	}
	return b.PythonPluginService.CreatePluginFile(id, path)
}

func (b *serviceBackend) DeletePluginFile(id, path string) (*store.Plugin, error) {
	if b.deleteFile != nil {
		return b.deleteFile(id, path)
	}
	return b.PythonPluginService.DeletePluginFile(id, path)
}

func (b *serviceBackend) ReorderPlugins(ids []string) error {
	if b.reorder != nil {
		return b.reorder(ids)
	}
	return b.PythonPluginService.ReorderPlugins(ids)
}

func (b *serviceBackend) ValidatePlugin(id string) (*store.Plugin, error) {
	if b.validate != nil {
		return b.validate(id)
	}
	return b.GetPlugin(id)
}

func (b *serviceBackend) ReloadPlugin(id string) (*store.Plugin, error) {
	if b.reload != nil {
		return b.reload(id)
	}
	return b.GetPlugin(id)
}

func (b *serviceBackend) GetRuntimeStatus() (*store.PluginRuntimeStatus, error) {
	return &store.PluginRuntimeStatus{Enabled: true, Ready: true}, nil
}

func (b *serviceBackend) OpenPluginsDirectory() error {
	b.opened = append(b.opened, "root")
	return nil
}

func (b *serviceBackend) OpenPluginDirectory(id string) error {
	b.opened = append(b.opened, id)
	return nil
}

type fixture struct {
	t        *testing.T
	backend  *serviceBackend
	data     *store.Store
	window   *runtimetest.Window
	tester   *ui.Tester
	notifier *widgets.Notifier
	dispatch *shortcuts.Dispatcher
	view     *View
	help     *Help
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	i18n.SetLocaleFunc(func() string { return "en" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	db, err := database.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	settings := settingservice.New(db)
	if err := settings.Load(); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	service, err := pythonpluginservice.New(db, settings)
	if err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("saving plugin files validates them with python3")
	}
	if _, err := service.ConfigureRuntime(settingservice.PythonPluginConfig{Enabled: true, InterpreterPath: python, HookTimeoutMs: 5000}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Shutdown() })
	backend := &serviceBackend{PythonPluginService: service}
	rt := store.NewRuntime()
	window := &runtimetest.Window{}
	rt.Dispatcher.SetMain(window)
	t.Cleanup(rt.Close)
	bus := serviceruntime.NewBus()
	service.SetEventBus(bus)
	data := store.New(rt, store.Dependencies{Settings: store.SettingsDependencies{Backend: settings}, Plugins: backend,
		PluginEvents: func(registry func(store.PluginRegistryEvent), status func(store.PluginStatusEvent)) func() {
			offRegistry := bus.On(pythonpluginservice.RegistryEventName, func(event serviceruntime.Event) {
				rt.Dispatcher.Post(func() { registry(event.Data.(store.PluginRegistryEvent)) })
			})
			offStatus := bus.On(pythonpluginservice.StatusEventName, func(event serviceruntime.Event) {
				rt.Dispatcher.Post(func() { status(event.Data.(store.PluginStatusEvent)) })
			})
			return func() { offRegistry(); offStatus() }
		},
	})
	t.Cleanup(data.Plugins.Cleanup)
	if err := data.Settings.Load(); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, backend: backend, data: data, window: window, notifier: &widgets.Notifier{}}
	f.dispatch = shortcuts.NewDispatcher(func() *settingservice.ShortcutConfig { return nil })
	f.view = New(data, f.dispatch, f.notifier)
	f.help = NewHelp(data.Workbench, f.notifier, func() string { return "en" })
	f.view.OpenHelp = data.Workbench.OpenPythonHelp
	f.tester = ui.NewTester(func(c *ui.Context) {
		theme := nui.App(c, data.Settings.Theme().Config)
		f.dispatch.Begin()
		ui.Row(c).Fill().Children(func() {
			ui.Box(c).Width(320).FillHeight().Shrink(0).Children(func() { f.view.Sidebar(c, theme) })
			ui.Box(c).Grow(1).Basis(0).MinWidth(0).FillHeight().Children(func() { f.view.Content(c, theme) })
		})
		f.help.Build(c, theme)
		f.notifier.Show(c, theme)
		nui.ToastViewport(c, theme, nui.ToastViewportOptions{Limit: 5})
		f.dispatch.Install(c)
	}, 1500, 900)
	f.tester.SetLocale("en")
	f.settle()
	return f
}

func (f *fixture) settle() {
	for range 6 {
		f.data.Runtime.Jobs.Wait()
		f.window.Drain()
		f.tester.Frame()
	}
}

func (f *fixture) click(label string) {
	f.t.Helper()
	if err := f.tester.Click(label); err != nil {
		f.t.Fatalf("click %q: %v\n%v", label, err, f.tester.Texts())
	}
	f.settle()
}

func (f *fixture) has(text string) bool { return f.tester.HasText(text) }

func (f *fixture) create(name, description string) *store.Plugin {
	f.t.Helper()
	var created *store.Plugin
	f.data.Plugins.CreatePlugin(name, description, func(plugin *store.Plugin, err error) {
		if err != nil {
			f.t.Error(err)
		}
		created = plugin
	})
	f.settle()
	if created == nil {
		f.t.Fatal("not created")
	}
	return created
}

func (f *fixture) center(label string) (float32, float32) {
	f.t.Helper()
	rect, ok := f.tester.Find(label)
	if !ok {
		f.t.Fatalf("no %q in %v", label, f.tester.Texts())
	}
	return rect.X + rect.W/2, rect.Y + rect.H/2
}

func (f *fixture) drag(label string, dy float32) {
	f.t.Helper()
	x, y := f.center(label)
	f.tester.Press(x, y)
	f.tester.Frame()
	const steps = 10
	for step := 1; step <= steps; step++ {
		f.tester.Move(x, y+dy*float32(step)/steps)
		f.tester.Frame()
	}
	f.tester.Release(x, y+dy)
	f.settle()
}

func TestCreateSearchEnableAndDirectory(t *testing.T) {
	f := newFixture(t)
	if !f.has(locales.PythonPluginsEmpty()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.PythonPluginsCreate())
	f.click(locales.PythonPluginsCreate() + " " + locales.PythonPluginsCreateTitle())
	if !f.has(locales.PythonPluginsNameRequired()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.view.create.name = "Alpha"
	f.settle()
	f.click(locales.PythonPluginsCreate() + " " + locales.PythonPluginsCreateTitle())
	if !f.has("Alpha") || f.data.Plugins.SelectedPlugin() == nil || f.data.Plugins.SelectedPlugin().Name != "Alpha" {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.create("Beta", "second one")
	f.view.search = "second"
	f.settle()
	if f.has("Alpha") || !f.has("Beta") {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.view.search = "nothing"
	f.settle()
	if !f.has(locales.PythonPluginsNoSearchResults()) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.view.search = ""
	f.settle()
	f.click(locales.PythonPluginsEnabled() + " Alpha")
	plugin, err := f.backend.GetPlugin(f.data.Plugins.Plugins[0].ID)
	if err != nil || !plugin.Enabled {
		t.Fatal(err, plugin)
	}
	f.click(locales.PythonPluginsOpenRootDirectory())
	if !slices.Contains(f.backend.opened, "root") {
		t.Fatal(f.backend.opened)
	}
	if !f.has(locales.PythonPluginsRuntimeReady()) {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestDragSavesPluginOrder(t *testing.T) {
	f := newFixture(t)
	first := f.create("First", "")
	second := f.create("Second", "")
	f.drag("drag First", pluginRowHeight*1.2)
	listed, err := f.backend.ListPlugins()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, plugin := range listed {
		ids = append(ids, plugin.ID)
	}
	if !slices.Equal(ids, []string{second.ID, first.ID}) {
		t.Fatalf("%v", ids)
	}
	if f.data.Plugins.Plugins[0].ID != second.ID {
		t.Fatal("the list follows")
	}
}

func TestPluginDragKeepsOrderUntilDropAndRestoresOnFailure(t *testing.T) {
	f := newFixture(t)
	first := f.create("First", "")
	second := f.create("Second", "")
	writes := 0
	f.backend.reorder = func(ids []string) error {
		writes++
		if !slices.Equal(ids, []string{second.ID, first.ID}) {
			t.Errorf("unexpected order %v", ids)
		}
		return errors.New("disk full")
	}
	x, y := f.center("drag First")
	f.tester.Press(x, y)
	f.tester.Move(x, y+pluginRowHeight*1.2)
	f.tester.Frame()
	if f.data.Plugins.Plugins[0].ID != first.ID || writes != 0 {
		t.Fatal("drag changed the store before drop")
	}
	f.tester.Key(0, ui.KeyEscape)
	f.tester.Release(x, y+pluginRowHeight*1.2)
	f.settle()
	if writes != 0 || f.data.Plugins.SelectedPluginID != second.ID {
		t.Fatal("canceling a drag saved or selected the dragged plugin")
	}
	f.drag("drag First", pluginRowHeight*1.2)
	if writes != 1 || f.data.Plugins.Plugins[0].ID != first.ID || !f.has(locales.PythonPluginsReorderFailed("disk full")) {
		t.Fatal("failed reorder did not restore the saved order", writes)
	}
	f.view.search = "First"
	f.settle()
	f.drag("drag First", pluginRowHeight*1.2)
	if writes != 1 {
		t.Fatal("filtered list reordered")
	}
}

func TestFilesCreateSaveRenameDeleteAndEscape(t *testing.T) {
	f := newFixture(t)
	plugin := f.create("Files", "")
	plugins := f.data.Plugins
	if plugins.SelectedFilePath != "main.py" {
		t.Fatalf("%q %v", plugins.SelectedFilePath, plugins.Files)
	}
	f.click(locales.PythonPluginsNewFile())
	f.view.fileModal.path = "../outside.py"
	f.settle()
	f.click(locales.PythonPluginsConfirm())
	if !f.view.fileModal.open || f.view.fileModal.err == "" || !f.has(f.view.fileModal.err) {
		t.Fatalf("escape rejected: %+v", f.view.fileModal)
	}
	f.view.fileModal = fileDialog{}
	f.settle()
	if slices.Contains(plugins.Files, "../outside.py") {
		t.Fatal(plugins.Files)
	}
	f.view.fileModal.show(false, "lib/util.py")
	f.settle()
	f.click(locales.PythonPluginsConfirm())
	if f.view.fileModal.open || f.view.fileModal.busy {
		t.Fatal("create file dialog did not close")
	}
	if plugins.SelectedFilePath != "lib/util.py" || !slices.Contains(plugins.Files, "lib/util.py") {
		t.Fatalf("%q %v", plugins.SelectedFilePath, plugins.Files)
	}
	if rows := fileTreeRows(plugins.Files); rows[0].key != "dir:lib" || !rows[0].directory {
		t.Fatalf("%+v", rows)
	}
	plugins.EditorContent = "x = 1\n"
	f.settle()
	if !plugins.EditorDirty() || !f.dispatch.Invoke(shortcuts.AppSave) {
		t.Fatal("dirty")
	}
	f.settle()
	if plugins.EditorDirty() {
		t.Fatal("saved")
	}
	saved, err := f.backend.ReadPluginFile(plugin.ID, "lib/util.py")
	if err != nil || saved.Content != "x = 1\n" {
		t.Fatal(err, saved)
	}
	plugins.EditorContent = "unsaved"
	f.settle()
	f.click(locales.PythonPluginsRenameFile())
	f.view.fileModal.path = "lib/renamed.py"
	f.settle()
	f.click(locales.PythonPluginsConfirm())
	if f.view.fileModal.open || f.view.fileModal.busy {
		t.Fatal("rename file dialog did not close")
	}
	if plugins.SelectedFilePath != "lib/renamed.py" || plugins.EditorContent != "unsaved" {
		t.Fatalf("draft follows the rename: %q %q", plugins.SelectedFilePath, plugins.EditorContent)
	}
	f.click(locales.PythonPluginsDeleteFile())
	if !f.has(locales.PythonPluginsDeleteFileConfirm("lib/renamed.py")) {
		t.Fatalf("%v", f.tester.Texts())
	}
	if !strings.Contains(strings.ToLower(locales.PythonPluginsDeleteFileConfirm("x")), "unsaved") {
		t.Fatal("the confirmation mentions unsaved content")
	}
	f.click(locales.PythonPluginsDelete())
	if f.view.deleting.file || f.view.deleting.fileBusy {
		t.Fatal("delete file dialog did not close")
	}
	if slices.Contains(plugins.Files, "lib/renamed.py") || plugins.SelectedFilePath != "main.py" {
		t.Fatalf("%q %v", plugins.SelectedFilePath, plugins.Files)
	}
}

func TestDraftsSurviveSwitchingPlugins(t *testing.T) {
	f := newFixture(t)
	first := f.create("First", "")
	f.data.Plugins.EditorContent = "first draft"
	f.settle()
	second := f.create("Second", "")
	if f.data.Plugins.SelectedPluginID != second.ID || f.data.Plugins.EditorContent == "first draft" {
		t.Fatal("second shown")
	}
	f.view.selectPlugin(first.ID)
	f.settle()
	if f.data.Plugins.EditorContent != "first draft" || !f.data.Plugins.EditorDirty() {
		t.Fatal(f.data.Plugins.EditorContent)
	}
}

func TestValidateSavesFirstAndFailureKeepsRevision(t *testing.T) {
	f := newFixture(t)
	plugin := f.create("Check", "")
	plugins := f.data.Plugins
	plugins.SelectFile("helpers.py", func(err error) {
		if err != nil {
			t.Error(err)
		}
	})
	f.settle()
	plugins.EditorContent = "changed = True\n"
	f.settle()
	f.backend.validate = func(id string) (*store.Plugin, error) {
		current, _ := f.backend.GetPlugin(id)
		copied := *current
		copied.ValidationStatus, copied.ActiveRevision = "valid", "abcdef1234567890"
		return &copied, nil
	}
	f.click(locales.PythonPluginsValidate())
	if plugins.EditorDirty() || !f.has(locales.PythonPluginsValidationSuccess()) || !f.has(locales.PythonPluginsValidationValid()) || !f.has(locales.PythonPluginsRevision("abcdef123456")) {
		t.Fatalf("%v", f.tester.Texts())
	}
	saved, _ := f.backend.ReadPluginFile(plugin.ID, "helpers.py")
	if saved.Content != "changed = True\n" {
		t.Fatal(saved.Content)
	}
	f.backend.validate = func(string) (*store.Plugin, error) { return nil, errors.New("syntax error on line 1") }
	f.click(locales.PythonPluginsValidate())
	if !f.has(locales.PythonPluginsValidationFailed("syntax error on line 1")) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.backend.reload = func(string) (*store.Plugin, error) { return nil, errors.New("reload boom") }
	f.click(locales.PythonPluginsReload())
	if !f.has(locales.PythonPluginsReloadFailed("reload boom")) {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestMetadataAndDeletePlugin(t *testing.T) {
	f := newFixture(t)
	plugin := f.create("Old name", "old")
	f.click(locales.PythonPluginsRenamePlugin())
	f.view.metadata.name = "New name"
	f.view.metadata.description = "new text"
	f.settle()
	f.click(locales.PythonPluginsSave() + " " + locales.PythonPluginsEditPlugin())
	stored, err := f.backend.GetPlugin(plugin.ID)
	if err != nil || stored.Name != "New name" || stored.Description != "new text" || f.view.metadata.open {
		t.Fatal(err, stored)
	}
	f.click(locales.PythonPluginsDeletePlugin())
	if !f.has(locales.PythonPluginsDeletePluginConfirm("New name")) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(locales.PythonPluginsDelete())
	if listed, _ := f.backend.ListPlugins(); len(listed) != 0 || !f.has(locales.PythonPluginsEmpty()) {
		t.Fatalf("%v", f.tester.Texts())
	}
}

func TestParamsRejectNonObjects(t *testing.T) {
	f := newFixture(t)
	plugin := f.create("Settings holder", "")
	plugins := f.data.Plugins
	f.click(locales.PythonPluginsParams())
	for _, value := range []string{"[1, 2]", "3", "not json"} {
		plugins.ParamsDraft = value
		f.settle()
		f.click(locales.PythonPluginsSaveParams())
		stored, _ := f.backend.GetPlugin(plugin.ID)
		if plugins.ParamsError == "" || !f.has(plugins.ParamsError) || stored.ParamsJSON != "{}" {
			t.Fatalf("%q: %q %q", value, plugins.ParamsError, stored.ParamsJSON)
		}
	}
	plugins.ParamsDraft = `{"b":1,"a":[2]}`
	f.settle()
	f.click(locales.PythonPluginsFormat())
	if plugins.ParamsDraft != "{\n  \"b\": 1,\n  \"a\": [\n    2\n  ]\n}" || plugins.ParamsError != "" {
		t.Fatalf("%q", plugins.ParamsDraft)
	}
	f.click(locales.PythonPluginsSaveParams())
	stored, _ := f.backend.GetPlugin(plugin.ID)
	if plugins.ParamsDirty() || !strings.Contains(stored.ParamsJSON, `"b"`) || !f.has(locales.PythonPluginsParamsSaved()) {
		t.Fatalf("%q", stored.ParamsJSON)
	}
	plugins.ParamsDraft = "[]"
	f.settle()
	f.click(locales.PythonPluginsFormat())
	if plugins.ParamsError != locales.PythonPluginsParamsObjectRequired() {
		t.Fatal(plugins.ParamsError)
	}
}

func TestRulesAddEditReorderDelete(t *testing.T) {
	f := newFixture(t)
	plugin := f.create("Rules", "")
	f.click(locales.PythonPluginsAddRule())
	f.click(locales.PythonPluginsAddRule())
	rules := func() []*store.PluginRule {
		stored, err := f.backend.GetPlugin(plugin.ID)
		if err != nil {
			t.Fatal(err)
		}
		return stored.Rules
	}
	if len(rules()) != 2 || rules()[0].Method != "*" {
		t.Fatalf("%+v", rules())
	}
	f.click("Method 1")
	f.tester.Key(ui.Ctrl, ui.KeyA)
	f.tester.Type("post")
	f.settle()
	f.tester.Key(0, ui.KeyEnter)
	f.settle()
	if rules()[0].Method != "POST" {
		t.Fatalf("%+v", rules()[0])
	}
	f.click("URL pattern 2")
	f.tester.Key(ui.Ctrl, ui.KeyA)
	f.tester.Type("https://api.example.com/*")
	f.settle()
	f.click("Method 1")
	if rules()[1].URLPattern != "https://api.example.com/*" {
		t.Fatalf("saved on leaving the field: %+v", rules()[1])
	}
	first := rules()[0].ID
	f.drag("drag rule 1", ruleRowHeight*1.2)
	if got := rules(); got[1].ID != first {
		t.Fatalf("%+v", got)
	}
	f.click(locales.PythonPluginsDeleteRule() + " 1")
	if len(rules()) != 1 {
		t.Fatalf("%+v", rules())
	}
}

func TestHelpSearchesAndCopiesPrompt(t *testing.T) {
	f := newFixture(t)
	f.click(locales.PythonPluginHelpTitle())
	// The help slides in over the window.
	f.tester.Advance(time.Second)
	f.settle()
	messages := pythonapi.MessagesFor("en")
	if !f.help.Visible() || !f.has(messages.Global) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(messages.CopyContext)
	if f.tester.Clipboard() != pythonapi.SystemPrompt(pythonapi.ScopeGlobal, "en") {
		t.Fatal("prompt")
	}
	f.click(messages.Reference)
	if f.help.tab != "reference" {
		t.Fatal(f.help.tab)
	}
	f.help.query = "write_file"
	f.settle()
	if !f.has("Body.write_file(path: str) -> FileDescriptor") && !strings.Contains(strings.Join(f.tester.Texts(), "\n"), "write_file") {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.help.query = "no such api at all"
	f.settle()
	if !f.has(messages.NoResults) {
		t.Fatalf("%v", f.tester.Texts())
	}
	f.click(messages.Close)
	if f.help.Visible() {
		t.Fatal("closed")
	}
}

func TestHelpClosesWhenItsContextChanges(t *testing.T) {
	f := newFixture(t)
	context := "a"
	f.help.Context = func() string { return context }
	f.data.Workbench.OpenPythonHelp(pythonapi.ScopeInline)
	f.tester.Advance(time.Second)
	f.settle()
	if !f.help.Visible() || !f.has(pythonapi.MessagesFor("en").Inline) {
		t.Fatalf("%v", f.tester.Texts())
	}
	context = "b"
	f.settle()
	if f.help.Visible() {
		t.Fatal("closes with its context")
	}
}

func TestPythonEditorOffersTheAPI(t *testing.T) {
	f := newFixture(t)
	f.create("Assist", "")
	state := f.view.python.State()
	if state.OnCompletion == nil || state.OnHover == nil {
		t.Fatal("assistance attached")
	}
	f.data.Plugins.EditorContent = "request.he"
	f.settle()
	f.view.python.State().SetSelections(selectionAt(len("request.he")))
	state.OnCompletion(editorRequest(len("request.he")))
	f.settle()
	if !f.has("headers") {
		t.Fatalf("%v", f.tester.Texts())
	}
}

// The help is a Slideover: a dialog at the window's right edge, closed by
// Escape as by its close button.
func TestHelpSlidesOverTheWindow(t *testing.T) {
	f := newFixture(t)
	f.click(locales.PythonPluginHelpTitle())
	f.tester.Advance(time.Second)
	f.settle()
	messages := pythonapi.MessagesFor("en")
	dialog, ok := f.tester.AccessByRole(ui.RoleDialog, messages.Title)
	if !ok {
		t.Fatalf("no help dialog in %v", f.tester.Texts())
	}
	if right := dialog.Bounds.X + dialog.Bounds.W; right < 1499 || dialog.Bounds.W > 421 {
		t.Fatalf("help at %v, want 420 wide at the right edge", dialog.Bounds)
	}
	f.tester.Key(0, ui.KeyEscape)
	f.tester.Advance(time.Second)
	f.settle()
	if f.help.Visible() {
		t.Fatal("Escape leaves the help open")
	}
	if _, ok := f.tester.AccessByRole(ui.RoleDialog, messages.Title); ok {
		t.Fatal("the help stays on show after closing")
	}
}
