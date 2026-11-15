package editor

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	apptheme "github.com/josexy/flowlens/internal/desktop/theme"
	"github.com/josexy/flowlens/internal/desktop/widgets"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
	nui "github.com/josexy/nuxt-ui-go/ui"
)

const consolePreviewLength = 240

// consoleRowHeight is 1.0.5's console row: 40 px, with the cells' padding.
const consoleRowHeight = 40

// File names 1.0.5 suggested for the console and for one entry.
const (
	consoleFilename      = "flowlens-python-console.log"
	consoleEntryFilename = "flowlens-python-console-entry.log"
)

// Console columns, with 1.0.5's starting widths and bounds. Output takes
// the room the others leave.
const (
	consoleColumnTime   = "time"
	consoleColumnType   = "type"
	consoleColumnSource = "source"
	consoleColumnOutput = "output"
)

// consoleColumnIDs are the console's columns in the order columns
// gives them.
var consoleColumnIDs = []string{consoleColumnTime, consoleColumnType, consoleColumnSource, consoleColumnOutput}

func (v *consoleView) columns() []nui.TableColumn[logic.PluginLogEntry] {
	columns := []nui.TableColumn[logic.PluginLogEntry]{
		{ID: consoleColumnTime, Title: locales.WorkspaceHttpRequestConsoleColumnTime(), Width: 152, MinWidth: 120, MaxWidth: 360},
		{ID: consoleColumnType, Title: locales.WorkspaceHttpRequestConsoleColumnType(), Width: 96, MinWidth: 80, MaxWidth: 260},
		{ID: consoleColumnSource, Title: locales.WorkspaceHttpRequestConsoleColumnSource(), Width: 120, MinWidth: 96, MaxWidth: 420},
		{ID: consoleColumnOutput, Title: locales.WorkspaceHttpRequestConsoleColumnOutput(), MinWidth: 240, MaxWidth: 900},
	}
	for index := range columns {
		columns[index].Cell = func(c *ui.Context, theme nui.Theme, entry logic.PluginLogEntry, _ bool) {
			v.cell(c, theme, &entry, index)
		}
	}
	return columns
}

type consoleView struct {
	editor *Editor
	key    string
	state  *logic.HTTPEditorState
	// table keeps the column widths while the tab is open; they are not
	// saved, as in 1.0.5.
	table nui.TableState[logic.PluginLogEntry]
	// rows opens an entry on a click or Enter, as 1.0.5's row buttons did.
	rows   *widgets.RowActivator
	detail *logic.PluginLogEntry
	open   bool
	wrap   widgets.DetailWrap
	view   *widgets.CodeView
	saving bool
}

func newConsoleView(e *Editor, key string, state *logic.HTTPEditorState) *consoleView {
	view := &consoleView{editor: e, key: key, state: state, view: widgets.NewCodeView("plaintext", true)}
	view.table.List.FollowEnd = true
	view.rows = widgets.NewRowActivator(&view.table.List, &view.table.Selection)
	return view
}

func consoleSource(entry *logic.PluginLogEntry) string {
	switch {
	case entry.PluginID == logic.CurrentRequestScriptPluginID:
		return locales.WorkspaceHttpRequestConsoleCurrentScript()
	case strings.TrimSpace(entry.PluginName) != "":
		return strings.TrimSpace(entry.PluginName)
	case entry.PluginID != "":
		return entry.PluginID
	default:
		return locales.WorkspaceHttpRequestConsoleUnknownPlugin()
	}
}

func consoleLevelLabel(level string) string {
	switch logic.PythonLogLevelKey(level) {
	case "debug":
		return locales.WorkspaceHttpRequestConsoleLevelDebug()
	case "warning":
		return locales.WorkspaceHttpRequestConsoleLevelWarning()
	case "error":
		return locales.WorkspaceHttpRequestConsoleLevelError()
	default:
		return locales.WorkspaceHttpRequestConsoleLevelInfo()
	}
}

func consoleStreamLabel(stream string) string {
	if logic.PythonLogStreamKey(stream) == "stderr" {
		return locales.WorkspaceHttpRequestConsoleTypeStderr()
	}
	return locales.WorkspaceHttpRequestConsoleTypeStdout()
}

func consoleStreamIcon(stream string) *ui.SVG {
	if logic.PythonLogStreamKey(stream) == "stderr" {
		return lucide.TriangleAlert()
	}
	return lucide.Terminal()
}

// consoleTone returns the badge color of a tone, as 1.0.5's tone classes:
// neutral, the accent, warning and error.
func consoleTone(tone string) nui.Color {
	switch tone {
	case logic.PythonToneError:
		return nui.Error
	case logic.PythonToneWarning:
		return nui.Warning
	case logic.PythonToneNeutral:
		return nui.Neutral
	default:
		return nui.Primary
	}
}

// consolePill is an entry's level, or its stream and level, in a round
// pill of its tone.
func consolePill(c *ui.Context, theme nui.Theme, entry *logic.PluginLogEntry, label string, icon *ui.SVG) ui.Element {
	return nui.Badge(c, theme, label, nui.BadgeOptions{
		Color: consoleTone(logic.PythonLogTone(entry.Stream, entry.Level)), Variant: nui.Soft, Size: nui.SM, Leading: icon,
		Style: func(_ nui.Slot, element ui.Element) { element.Radius(999) },
	}).MinWidth(0)
}

func (v *consoleView) allText() string {
	return logic.PythonConsoleText(v.state.ScriptConsoleEntries, consoleSource)
}

func (v *consoleView) save(filename, text string) {
	deps := v.editor.Host.Inspect
	if deps.PickSavePath == nil || deps.SaveBody == nil || v.saving || text == "" {
		return
	}
	v.saving = true
	runtime.Run(deps.Runtime.Jobs, "editor:console-save:"+v.key, func(context.Context) (struct{}, error) {
		path, err := deps.PickSavePath(filename)
		if err != nil || strings.TrimSpace(path) == "" {
			return struct{}{}, err
		}
		return struct{}{}, deps.SaveBody(proxyservice.SaveBodyToFileRequest{Path: path, Body: text, ContentType: "text/plain; charset=utf-8"})
	}, func(_ struct{}, err error) {
		v.saving = false
		if err != nil {
			v.editor.Notifier.Error(locales.WorkspaceHttpRequestConsoleSaveFailed(err.Error()))
		}
	})
}

// clear empties the console and closes the entry detail.
func (v *consoleView) clear() {
	v.open, v.detail = false, nil
	v.editor.Data.Workspace.ClearScriptConsole(v.key)
}

// closeGoneDetail closes the detail of an entry no longer in the console, as
// after Clear or a new send.
func (v *consoleView) closeGoneDetail(entries []logic.PluginLogEntry) {
	if v.detail == nil {
		return
	}
	for index := range entries {
		if entries[index].EventID == v.detail.EventID {
			return
		}
	}
	v.open, v.detail = false, nil
}

func (v *consoleView) build(c *ui.Context, theme nui.Theme) {
	entries := v.state.ScriptConsoleEntries
	// Console events are immutable. Count and endpoint IDs also detect a
	// bounded console replacing its oldest entries without changing length.
	if len(entries) != v.table.Len() || len(entries) > 0 && (entries[0].EventID != v.table.Row(0).EventID || entries[len(entries)-1].EventID != v.table.Row(v.table.Len()-1).EventID) {
		v.table.SetRows(entries, func(entry logic.PluginLogEntry) string { return strconv.FormatUint(entry.EventID, 10) })
	}
	running := v.state.IsSending
	v.closeGoneDetail(entries)
	ui.Column(c.Key("console")).Fill().MinHeight(0).Children(func() {
		widgets.PaneToolbar(c.Key("console-head"), func() {
			v.status(c, theme, running, len(entries))
		}, func() {
			empty := len(entries) == 0
			iconAction(c, theme, "copy-all", lucide.Copy(), locales.WorkspaceHttpRequestConsoleCopyAll(), empty, func() { v.editor.copy(v.allText(), locales.WorkspaceHttpRequestConsoleCopied()) })
			iconAction(c, theme, "save-all", lucide.Download(), locales.WorkspaceHttpRequestConsoleSave(), empty, func() { v.save(consoleFilename, v.allText()) })
			iconAction(c, theme, "clear", lucide.Trash(), locales.WorkspaceHttpRequestConsoleClear(), empty, v.clear)
		})
		if len(entries) == 0 {
			title := locales.WorkspaceHttpRequestConsoleEmpty()
			if running {
				title = locales.WorkspaceHttpRequestConsoleWaitingOutput()
			}
			emptyPane(c, theme, lucide.Terminal(), title, "")
			return
		}
		widgets.PaneContent(c, func() {
			table := widgets.Table(c.Key("console-table"), theme, &v.table, v.columns(), nui.TableOptions{
				Label: locales.WorkspaceHttpRequestConsole(),
			}).Border(1, theme.Border)
			v.rows.Attach(table)
			if row, ok := v.rows.Activated(table, len(entries)); ok {
				chosen := entries[row]
				v.detail, v.open = &chosen, true
			}
		})
	})
	v.detailDialog(c, theme)
}

// status shows a dot, pulsing in the accent color while a send runs, and the
// console's state.
func (v *consoleView) status(c *ui.Context, theme nui.Theme, running bool, count int) {
	ui.Row(c).MinWidth(0).AlignItems(ui.Center).Gap(8).Children(func() {
		dot := ui.Box(c.Key("status-dot")).Size(8, 8).Shrink(0).Radius(4).Role(ui.RoleNone)
		text := locales.WorkspaceHttpRequestConsoleEntries(strconv.Itoa(count))
		if running {
			text = locales.WorkspaceHttpRequestConsoleRunning()
			dot.Background(theme.Primary)
			if !theme.Motion.Disabled {
				// Tailwind's animate-pulse: half opacity halfway through 2 s.
				ease := ui.CubicBezier(0.4, 0, 0.6, 1)
				opacity := dot.Keyframes("pulse", []ui.Keyframe{{At: 0, Value: 1}, {At: time.Second, Value: 0.5, Ease: ease}, {At: 2 * time.Second, Value: 1, Ease: ease}}, ui.KeyframeOptions{Repeat: ui.RepeatForever, NonMotion: true})
				dot.Opacity(opacity)
			}
		} else {
			dot.Background(theme.Muted)
		}
		ui.Text(c, text).SingleLine().Shrink(1).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Muted)
	})
}

// cell builds a console row's column on one line, 40 px high with the
// cell's padding.
func (v *consoleView) cell(c *ui.Context, theme nui.Theme, entry *logic.PluginLogEntry, col int) {
	ui.Row(c).FillWidth().MinWidth(0).Height(consoleRowHeight - 12).AlignItems(ui.Center).Gap(6).Children(func() {
		switch consoleColumnIDs[col] {
		case consoleColumnTime:
			ui.Text(c, logic.FormatUnixMicrosLocal(entry.Timestamp)).SingleLine().MinWidth(0).FontSize(apptheme.TextSize(c, 14)).FontFeatures("tnum").TextColor(theme.Toned)
		case consoleColumnType:
			ui.Icon(c, consoleStreamIcon(entry.Stream)).Size(14, 14).Shrink(0).TextColor(theme.Text).Label(consoleStreamLabel(entry.Stream))
			consolePill(c, theme, entry, consoleLevelLabel(entry.Level), nil)
		case consoleColumnSource:
			ui.Text(c, consoleSource(entry)).SingleLine().MinWidth(0).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Toned)
		default:
			ui.Text(c, logic.PythonLogPreview(entry.Message, consolePreviewLength)).SingleLine().Shrink(1).MinWidth(0).FontSize(apptheme.TextSize(c, 14)).TextColor(theme.Text)
			if count := logic.PythonLogLineCount(entry.Message); count > 1 {
				ui.Text(c, locales.WorkspaceHttpRequestConsoleLines(strconv.Itoa(count))).SingleLine().Shrink(0).FontSize(apptheme.TextSize(c, 12)).TextColor(theme.Muted)
			}
		}
	})
}

// detailDialog shows an entry as 1.0.5 did: its time, a pill of its stream
// and level, and its source, above its message.
func (v *consoleView) detailDialog(c *ui.Context, theme nui.Theme) {
	if v.detail == nil {
		v.open = false
	}
	entry := v.detail
	widgets.DetailDialog(c.Key("console-detail"), theme, &v.open, widgets.DetailDialogOptions{
		Title: locales.WorkspaceHttpRequestConsoleEntryDetail(),
		Header: func(c *ui.Context, palette nui.Theme) {
			ui.Text(c, logic.FormatUnixMicrosLocal(entry.Timestamp)).Shrink(0).FontSize(apptheme.TextSize(c, 14)).FontWeight(500).TextColor(palette.Toned)
			consolePill(c, palette, entry, consoleStreamLabel(entry.Stream)+" · "+consoleLevelLabel(entry.Level), consoleStreamIcon(entry.Stream)).Shrink(0)
			ui.Text(c, consoleSource(entry)).SingleLine().MinWidth(0).FontSize(apptheme.TextSize(c, 14)).TextColor(palette.Muted)
		},
		Actions: []widgets.DetailAction{
			v.wrap.Action(locales.WorkspaceHttpRequestConsoleWrap()),
			{Key: "copy", Icon: lucide.Copy(), Label: locales.WorkspaceHttpRequestConsoleCopyEntry(), Run: func() {
				v.editor.copy(entry.Message, locales.WorkspaceHttpRequestConsoleEntryCopied())
			}},
			{Key: "save", Icon: lucide.Download(), Label: locales.WorkspaceHttpRequestConsoleSaveEntry(), Disabled: v.saving, Run: func() { v.save(consoleEntryFilename, entry.Message) }},
		},
		Wrap: &v.wrap,
		Content: func(c *ui.Context, palette nui.Theme) {
			if v.view.Text() != entry.Message {
				v.view.SetText(entry.Message)
			}
			v.view.SetWordWrap(v.wrap.On())
			v.view.BuildUnframed(c, ui.Font{Family: palette.Font, Size: apptheme.TextSize(c, 14)})
		},
	})
	if !v.open {
		v.detail = nil
	}
}
