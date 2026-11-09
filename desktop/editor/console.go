package editor

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/runtime"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
	"github.com/josexy/nuxt-ui-go/icons/lucide"
)

const consolePreviewLength = 240

type consoleView struct {
	editor *Editor
	key    string
	state  *logic.HTTPEditorState
	list   ui.ListState
	detail *logic.PluginLogEntry
	open   bool
	wrap   bool
	saving bool
}

func newConsoleView(e *Editor, key string, state *logic.HTTPEditorState) *consoleView {
	view := &consoleView{editor: e, key: key, state: state}
	view.list.FollowEnd = true
	return view
}

func consoleSource(entry *logic.PluginLogEntry) string {
	switch {
	case entry.PluginID == logic.CurrentRequestScriptPluginID:
		return locales.WorkspaceHttpRequestConsoleCurrentScript()
	case entry.PluginName != "":
		return entry.PluginName
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

func consoleEntryText(entry *logic.PluginLogEntry) string {
	return fmt.Sprintf("[%s] %s %s %s\n%s", logic.FormatUnixMicrosLocal(entry.Timestamp), consoleSource(entry), consoleStreamLabel(entry.Stream), consoleLevelLabel(entry.Level), entry.Message)
}

func (v *consoleView) allText() string {
	parts := make([]string, len(v.state.ScriptConsoleEntries))
	for index := range v.state.ScriptConsoleEntries {
		parts[index] = consoleEntryText(&v.state.ScriptConsoleEntries[index])
	}
	return strings.Join(parts, "\n\n")
}

func (v *consoleView) save(filename, text string) {
	deps := v.editor.Host.Inspect
	if deps.PickSavePath == nil || deps.SaveBody == nil || v.saving {
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

func (v *consoleView) build(c *ui.Context, theme nui.Theme) {
	entries := v.state.ScriptConsoleEntries
	running := v.state.IsSending
	ui.Column(c.Key("console")).Fill().MinHeight(0).Children(func() {
		ui.Row(c.Key("console-head")).FillWidth().Shrink(0).AlignItems(ui.Center).Justify(ui.SpaceBetween).Padding(6, 10).Children(func() {
			status := locales.WorkspaceHttpRequestConsoleEntries(strconv.Itoa(len(entries)))
			if running {
				status = locales.WorkspaceHttpRequestConsoleRunning()
			}
			ui.Text(c, status).FontSize(12).TextColor(theme.Muted)
			ui.Row(c).Gap(4).Children(func() {
				color := nui.Neutral
				if v.wrap {
					color = nui.Primary
				}
				wrap := nui.Button(c.Key("wrap"), theme, "", nui.ButtonOptions{Variant: nui.Ghost, Color: color, Size: nui.SM, Square: true, Leading: lucide.TextWrap(), AccessibleLabel: locales.WorkspaceHttpRequestConsoleWrap()})
				wrap.OnClick(func() { v.wrap = !v.wrap })
				nui.Tooltip(c, theme, wrap, locales.WorkspaceHttpRequestConsoleWrap(), nui.TooltipOptions{})
				empty := len(entries) == 0
				iconAction(c, theme, "copy-all", lucide.Copy(), locales.WorkspaceHttpRequestConsoleCopyAll(), empty, func() { v.editor.copy(v.allText(), locales.WorkspaceHttpRequestConsoleCopied()) })
				iconAction(c, theme, "save-all", lucide.Download(), locales.WorkspaceHttpRequestConsoleSave(), empty, func() {
					v.save(fmt.Sprintf("flowlens-console-%s.log", time.Now().Format("20060102-150405")), v.allText())
				})
				iconAction(c, theme, "clear", lucide.Trash(), locales.WorkspaceHttpRequestConsoleClear(), empty, func() { v.editor.Data.Workspace.ClearScriptConsole(v.key) })
			})
		})
		if len(entries) == 0 {
			title := locales.WorkspaceHttpRequestConsoleEmpty()
			if running {
				title = locales.WorkspaceHttpRequestConsoleWaitingOutput()
			}
			emptyPane(c, theme, lucide.Terminal(), title, "")
			return
		}
		ui.List(c.Key("console-list"), &v.list, len(entries), func(index int) {
			if index < len(entries) {
				v.row(c, theme, index, &entries[index])
			}
		}).Fill().MinHeight(0)
	})
	v.detailModal(c, theme)
}

func toneColor(theme nui.Theme, tone string) ui.Color {
	switch tone {
	case logic.PythonToneError:
		return theme.Error
	case logic.PythonToneWarning:
		return theme.Warning
	case logic.PythonToneNeutral:
		return theme.Muted
	default:
		return theme.Info
	}
}

func (v *consoleView) row(c *ui.Context, theme nui.Theme, index int, entry *logic.PluginLogEntry) {
	preview := logic.PythonLogPreview(entry.Message, consolePreviewLength)
	line := ui.Row(c.Key(entry.EventID)).FillWidth().AlignItems(ui.Center).Gap(8).PaddingX(10).MinHeight(28).Role(ui.RoleButton).Label(preview)
	line.BorderWidth(0, 0, 1, 0).BorderColor(theme.BorderMuted)
	line.OnClick(func() {
		chosen := *entry
		v.detail, v.open = &chosen, true
	})
	line.Children(func() {
		ui.Text(c, logic.FormatUnixMicrosLocal(entry.Timestamp)).Width(152).Shrink(0).SingleLine().FontSize(12).TextColor(theme.Muted)
		ui.Text(c, consoleStreamLabel(entry.Stream)+" "+consoleLevelLabel(entry.Level)).Width(96).Shrink(0).SingleLine().FontSize(12).TextColor(toneColor(theme, logic.PythonLogTone(entry.Stream, entry.Level)))
		ui.Text(c, consoleSource(entry)).Width(120).Shrink(0).SingleLine().FontSize(12).TextColor(theme.Toned)
		message := ui.Text(c, preview).Grow(1).Basis(0).MinWidth(0).FontSize(12).Font(theme.MonoFont).TextColor(theme.Text)
		if v.wrap {
			message.PaddingY(4)
		} else {
			message.SingleLine()
		}
		if count := logic.PythonLogLineCount(entry.Message); count > 1 {
			ui.Text(c, locales.WorkspaceHttpRequestConsoleLines(strconv.Itoa(count))).Shrink(0).FontSize(11).TextColor(theme.Muted)
		}
	})
}

func (v *consoleView) detailModal(c *ui.Context, theme nui.Theme) {
	if v.detail == nil {
		return
	}
	entry := v.detail
	nui.ModalWithOptions(c.Key("console-detail"), theme, &v.open, nui.ModalOptions{Title: locales.WorkspaceHttpRequestConsoleEntryDetail(), Width: nui.Set[float32](720), Footer: func(c *ui.Context, palette nui.Theme) {
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			nui.Button(c.Key("copy"), palette, locales.WorkspaceHttpRequestConsoleCopyEntry(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral}).
				OnClick(func() { v.editor.copy(entry.Message, locales.WorkspaceHttpRequestConsoleEntryCopied()) })
			nui.Button(c.Key("save"), palette, locales.WorkspaceHttpRequestConsoleSaveEntry(), nui.ButtonOptions{Variant: nui.Outline, Color: nui.Neutral}).
				OnClick(func() { v.save(fmt.Sprintf("flowlens-console-entry-%d.log", entry.EventID), entry.Message) })
		})
	}}, func(palette nui.Theme) {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, consoleSource(entry)+" · "+consoleStreamLabel(entry.Stream)+" · "+consoleLevelLabel(entry.Level)).FontSize(12).TextColor(palette.Muted)
			ui.Text(c, logic.FormatUnixMicrosLocal(entry.Timestamp)).FontSize(12).TextColor(palette.Muted)
			ui.Scroll(c.Key("message")).FillWidth().MaxHeight(420).Children(func() {
				ui.Text(c, entry.Message).FontSize(13).Font(palette.MonoFont).TextColor(palette.Text).Selectable().FillWidth()
			})
		})
	})
}
