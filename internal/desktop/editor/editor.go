// Package editor builds the request workspace: the tab strip above the traffic
// pages and the HTTP and WebSocket editors.
package editor

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/inspect"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	"github.com/josexy/flowlens/internal/desktop/shortcuts"
	"github.com/josexy/flowlens/internal/desktop/store"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	settingservice "github.com/josexy/flowlens/internal/services/setting_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

// Host is what the editors need from the rest of the application.
type Host struct {
	// ResolveFile reads the facts of a chosen file.
	ResolveFile func(path string) (*logic.RequestFileValue, error)
	// PickFile asks for a file; an empty path means canceled.
	PickFile func() (string, error)
	// PythonEnabled reports whether Python plugins are configured.
	PythonEnabled func() bool
	// ShowPythonHelp opens the plugin reference.
	ShowPythonHelp func()
	// SaveTab saves a request tab, asking where when it is new.
	SaveTab func(key string)
	// Dialogs, when set, builds dialogs of the tab strip.
	Dialogs func(c *ui.Context, theme nui.Theme)
	// Inspect is what the shared viewers need.
	Inspect inspect.Dependencies
}

// Editor holds the views of the workspace tabs.
type Editor struct {
	Data       *store.Store
	Host       Host
	Dispatcher *shortcuts.Dispatcher
	Notifier   *widgets.Notifier
	Confirm    *widgets.ConfirmDialog

	https     map[string]*httpPane
	sockets   map[string]*socketPane
	newMenu   nui.MenuState
	tabMenus  widgets.MenuStates
	tabScroll ui.ScrollState
	// revealed is the active tab last scrolled into view, and revealUntil
	// when bringing it into view gives up.
	revealed    string
	revealUntil time.Time
	// strip is where the tab strip showed in the last frame.
	strip ui.Rect
	drag  tabDrag
	// widths are the tabs' widths in the last frame, and dropped counts each
	// tab's drops, which key its element anew.
	widths    map[string]float32
	dropped   map[string]int
	clipboard *string
}

// New returns the editors of a window.
func New(data *store.Store, host Host, dispatcher *shortcuts.Dispatcher, notifier *widgets.Notifier, confirm *widgets.ConfirmDialog) *Editor {
	return &Editor{Data: data, Host: host, Dispatcher: dispatcher, Notifier: notifier, Confirm: confirm, https: map[string]*httpPane{}, sockets: map[string]*socketPane{}, widths: map[string]float32{}, dropped: map[string]int{}}
}

func (e *Editor) pythonEnabled() bool { return e.Host.PythonEnabled != nil && e.Host.PythonEnabled() }

func (e *Editor) prune() {
	workspace := e.Data.Workspace
	for key := range e.https {
		if workspace.FindTab(key) == nil {
			delete(e.https, key)
		}
	}
	for key := range e.sockets {
		if workspace.FindTab(key) == nil {
			delete(e.sockets, key)
		}
	}
	for key := range e.widths {
		if workspace.FindTab(key) == nil {
			delete(e.widths, key)
			delete(e.dropped, key)
		}
	}
}

// Build shows a request tab.
func (e *Editor) Build(c *ui.Context, theme nui.Theme, tab *logic.WorkspaceTab) {
	if e.clipboard != nil {
		c.WriteClipboard(*e.clipboard)
		e.clipboard = nil
	}
	switch {
	case tab.HTTP != nil:
		pane := e.https[tab.Key]
		if pane == nil {
			pane = newHTTPPane(e, tab)
			e.https[tab.Key] = pane
		}
		pane.build(c.Key(tab.Key), theme)
	case tab.WebSocket != nil:
		pane := e.sockets[tab.Key]
		if pane == nil {
			pane = newSocketPane(e, tab)
			e.sockets[tab.Key] = pane
		}
		pane.build(c.Key(tab.Key), theme)
	}
}

func (e *Editor) copy(text, success string) {
	e.clipboard = &text
	if success != "" {
		e.Notifier.Success(success)
	}
}

// pickFile asks for a file off the main thread and resolves it. fresh guards
// against an older pick finishing after a newer choice.
func (e *Editor) pickFile(job string, fresh func() bool, done func(*logic.RequestFileValue)) {
	if e.Host.PickFile == nil {
		return
	}
	runtime.Run(e.Data.Runtime.Jobs, "editor:pick:"+job, func(context.Context) (*logic.RequestFileValue, error) {
		path, err := e.Host.PickFile()
		if err != nil || strings.TrimSpace(path) == "" {
			return nil, err
		}
		return e.Host.ResolveFile(path)
	}, func(file *logic.RequestFileValue, err error) {
		if err != nil {
			e.Notifier.Error(err.Error())
			return
		}
		if file != nil && fresh() {
			done(file)
		}
	})
}

// useDroppedFile resolves the first of dropped paths.
func (e *Editor) useDroppedFile(job string, paths []string, fresh func() bool, done func(*logic.RequestFileValue)) {
	path := ""
	if len(paths) > 0 {
		path = strings.TrimSpace(paths[0])
	}
	if path == "" {
		e.Notifier.Error(locales.WorkspaceHttpRequestFilePathUnresolved())
		return
	}
	runtime.Run(e.Data.Runtime.Jobs, "editor:drop:"+job, func(context.Context) (*logic.RequestFileValue, error) {
		return e.Host.ResolveFile(path)
	}, func(file *logic.RequestFileValue, err error) {
		if err != nil {
			e.Notifier.Error(err.Error())
			return
		}
		if file != nil && fresh() {
			done(file)
		}
	})
}

func (e *Editor) declareShortcuts() {
	workspace := e.Data.Workspace
	e.Dispatcher.Handle(shortcuts.Handler{
		CommandID: shortcuts.AppSave,
		When:      func() bool { return e.Host.SaveTab != nil && workspace.IsDirty(workspace.ActiveKey) },
		Run:       func() { e.Host.SaveTab(workspace.ActiveKey) },
	})
	e.Dispatcher.Handle(shortcuts.Handler{CommandID: shortcuts.WorkspaceNewHTTPRequest, Run: func() { workspace.CreateHTTPRequestTab(logic.DraftNew, nil) }})
	e.Dispatcher.Handle(shortcuts.Handler{CommandID: shortcuts.WorkspaceNewWebSocket, Run: func() { workspace.CreateWebSocketClientTab(logic.DraftNew, nil) }})
	e.Dispatcher.Handle(shortcuts.Handler{
		CommandID: shortcuts.WorkspaceCloseTab,
		When:      func() bool { return workspace.ActiveTab().Closable },
		Run:       func() { e.closeWithPrompt(workspace.ActiveKey) },
	})
	several := func() bool { return len(workspace.Tabs) > 1 }
	e.Dispatcher.Handle(shortcuts.Handler{CommandID: shortcuts.WorkspaceNextTab, When: several, Run: workspace.ActivateNext})
	e.Dispatcher.Handle(shortcuts.Handler{CommandID: shortcuts.WorkspacePreviousTab, When: several, Run: workspace.ActivatePrevious})
}

var timeTitlePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$`)

// DisplayTitle returns the text a tab shows.
func DisplayTitle(tab *logic.WorkspaceTab) string {
	switch tab.Type {
	case logic.TabTypeCapture:
		return locales.MenuCapture()
	case logic.TabTypeHTTPRequest, logic.TabTypeWebSocketClient:
		if tab.Title == "" {
			return locales.WorkspaceTabBarUntitled()
		}
		return tab.Title
	}
	if timeTitlePattern.MatchString(tab.Title) {
		return tab.Title[5:]
	}
	return tab.Title
}

func tabIcon(kind string) *ui.SVG {
	switch kind {
	case logic.TabTypeCapture:
		return lucide.Radio()
	case logic.TabTypeHistory:
		return lucide.Archive()
	case logic.TabTypeHTTPRequest:
		return lucide.Braces()
	default:
		return lucide.MessagesSquare()
	}
}

// shortcutConfig returns the user's shortcut overrides, or nil.
func (e *Editor) shortcutConfig() *settingservice.ShortcutConfig {
	if e.Data.Settings != nil && e.Data.Settings.Current != nil {
		return e.Data.Settings.Current.Shortcuts
	}
	return nil
}

// menuShortcut returns the binding of a command as a menu item shows it, as
// 1.0.5's menus showed theirs, "" for none.
func (e *Editor) menuShortcut(commandID string) string {
	return strings.Join(e.kbds(commandID), "+")
}

func (e *Editor) kbds(commandID string) []string {
	command, ok := shortcuts.CommandByID(commandID)
	if !ok {
		return nil
	}
	resolved := shortcuts.ResolveCommand(command, e.shortcutConfig())
	if resolved.Binding == nil {
		return nil
	}
	return shortcuts.KbdKeys(resolved.Binding, shortcuts.IsMacOS())
}
