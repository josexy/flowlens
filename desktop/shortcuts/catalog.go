package shortcuts

import (
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
	"github.com/josexy/flowlens/locales"
)

// Command is one keyboard command.
type Command struct {
	ID       string
	Category func() string
	Label    func() string
	// DefaultBinding is nil for commands without a default key.
	DefaultBinding *Binding
	DefaultScope   settingservice.ShortcutScope
	// AllowInModal lets the command run while a dialog is open.
	AllowInModal bool
	// AllowInEditable lets the command run while a text field has the focus.
	AllowInEditable bool
	GlobalCapable   bool
}

func bind(key string, modifiers ...settingservice.ShortcutModifier) *Binding {
	return &Binding{Key: key, Modifiers: modifiers}
}

// Command identifiers.
const (
	AppSave                  = "app.save"
	AppOpenSettings          = "app.openSettings"
	AppShowMainWindow        = "app.showMainWindow"
	CaptureToggleProxy       = "capture.toggleProxy"
	WorkbenchCapture         = "workbench.capture"
	WorkbenchCategory        = "workbench.category"
	WorkbenchAPICollection   = "workbench.apiCollection"
	WorkbenchRewriteRules    = "workbench.rewriteRules"
	WorkbenchPythonPlugins   = "workbench.pythonPlugins"
	WorkbenchMemStats        = "workbench.memstats"
	WorkspaceNewHTTPRequest  = "workspace.newHttpRequest"
	WorkspaceNewWebSocket    = "workspace.newWebSocketRequest"
	WorkspaceCloseTab        = "workspace.closeTab"
	WorkspaceNextTab         = "workspace.nextTab"
	WorkspacePreviousTab     = "workspace.previousTab"
	RequestEditorSend        = "requestEditor.send"
	CaptureFocusFilter       = "capture.focusFilter"
	CaptureToggleSystemProxy = "capture.toggleSystemProxy"
	CaptureClearTraffic      = "capture.clearTraffic"
)

// Catalog lists every command in the order Settings shows them.
var Catalog = []Command{
	{ID: AppSave, Category: locales.ShortcutsCategoriesApplication, Label: locales.ShortcutsCommandsAppSave, DefaultBinding: bind("s", modifierPrimary), DefaultScope: settingservice.ShortcutScopeApplication, AllowInEditable: true},
	{ID: AppOpenSettings, Category: locales.ShortcutsCategoriesApplication, Label: locales.ShortcutsCommandsAppOpenSettings, DefaultBinding: bind(",", modifierPrimary), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: AppShowMainWindow, Category: locales.ShortcutsCategoriesApplication, Label: locales.ShortcutsCommandsAppShowMainWindow, DefaultScope: settingservice.ShortcutScopeGlobal, AllowInModal: true, AllowInEditable: true, GlobalCapable: true},
	{ID: CaptureToggleProxy, Category: locales.ShortcutsCategoriesCapture, Label: locales.ShortcutsCommandsCaptureToggleProxy, DefaultScope: settingservice.ShortcutScopeApplication, GlobalCapable: true},
	{ID: WorkbenchCapture, Category: locales.ShortcutsCategoriesWorkbench, Label: locales.ShortcutsCommandsWorkbenchCapture, DefaultBinding: bind("1", modifierPrimary), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: WorkbenchCategory, Category: locales.ShortcutsCategoriesWorkbench, Label: locales.ShortcutsCommandsWorkbenchCategory, DefaultBinding: bind("2", modifierPrimary), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: WorkbenchAPICollection, Category: locales.ShortcutsCategoriesWorkbench, Label: locales.ShortcutsCommandsWorkbenchApiCollection, DefaultBinding: bind("3", modifierPrimary), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: WorkbenchRewriteRules, Category: locales.ShortcutsCategoriesWorkbench, Label: locales.ShortcutsCommandsWorkbenchRewriteRules, DefaultBinding: bind("4", modifierPrimary), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: WorkbenchPythonPlugins, Category: locales.ShortcutsCategoriesWorkbench, Label: locales.ShortcutsCommandsWorkbenchPythonPlugins, DefaultBinding: bind("5", modifierPrimary), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: WorkbenchMemStats, Category: locales.ShortcutsCategoriesWorkbench, Label: locales.ShortcutsCommandsWorkbenchMemstats, DefaultBinding: bind("6", modifierPrimary), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: WorkspaceNewHTTPRequest, Category: locales.ShortcutsCategoriesWorkspace, Label: locales.ShortcutsCommandsWorkspaceNewHttpRequest, DefaultBinding: bind("n", modifierPrimary), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: WorkspaceNewWebSocket, Category: locales.ShortcutsCategoriesWorkspace, Label: locales.ShortcutsCommandsWorkspaceNewWebSocketRequest, DefaultBinding: bind("n", modifierPrimary, modifierShift), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: WorkspaceCloseTab, Category: locales.ShortcutsCategoriesWorkspace, Label: locales.ShortcutsCommandsWorkspaceCloseTab, DefaultBinding: bind("w", modifierPrimary), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: WorkspaceNextTab, Category: locales.ShortcutsCategoriesWorkspace, Label: locales.ShortcutsCommandsWorkspaceNextTab, DefaultBinding: bind("Tab", modifierControl), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: WorkspacePreviousTab, Category: locales.ShortcutsCategoriesWorkspace, Label: locales.ShortcutsCommandsWorkspacePreviousTab, DefaultBinding: bind("Tab", modifierControl, modifierShift), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: RequestEditorSend, Category: locales.ShortcutsCategoriesRequestEditor, Label: locales.ShortcutsCommandsRequestEditorSend, DefaultBinding: bind("Enter", modifierPrimary), DefaultScope: settingservice.ShortcutScopeApplication, AllowInEditable: true},
	{ID: CaptureFocusFilter, Category: locales.ShortcutsCategoriesCapture, Label: locales.ShortcutsCommandsCaptureFocusFilter, DefaultBinding: bind("f", modifierPrimary), DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: CaptureToggleSystemProxy, Category: locales.ShortcutsCategoriesCapture, Label: locales.ShortcutsCommandsCaptureToggleSystemProxy, DefaultScope: settingservice.ShortcutScopeApplication},
	{ID: CaptureClearTraffic, Category: locales.ShortcutsCategoriesCapture, Label: locales.ShortcutsCommandsCaptureClearTraffic, DefaultScope: settingservice.ShortcutScopeApplication},
}

// CommandByID finds a command.
func CommandByID(id string) (Command, bool) {
	for _, command := range Catalog {
		if command.ID == id {
			return command, true
		}
	}
	return Command{}, false
}

// ResolvedCommand is a command with the user's configuration applied.
type ResolvedCommand struct {
	Command Command
	Binding *Binding
	Scope   settingservice.ShortcutScope
}

// ResolveCommand applies a saved override. An override with no binding
// disables the command.
func ResolveCommand(command Command, config *settingservice.ShortcutConfig) ResolvedCommand {
	resolved := ResolvedCommand{Command: command, Binding: command.DefaultBinding, Scope: command.DefaultScope}
	if config == nil {
		return resolved
	}
	override, ok := config.Overrides[command.ID]
	if !ok {
		return resolved
	}
	resolved.Binding = NormalizeBinding(override.Binding)
	if override.Scope == settingservice.ShortcutScopeGlobal && command.GlobalCapable {
		resolved.Scope = settingservice.ShortcutScopeGlobal
	}
	return resolved
}
