package editor

import (
	"testing"

	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/theme"
	"github.com/josexy/flowlens/locales"
)

func (f *fixture) setDark(dark bool) {
	mode := theme.ModeLight
	if dark {
		mode = theme.ModeDark
	}
	if err := f.data.Settings.SetThemeMode(mode); err != nil {
		f.t.Fatal(err)
	}
	f.settle()
}

func TestRenderRequestEditor(t *testing.T) {
	f := newFake(t)
	f.tester.SetSize(rendertest.Width, rendertest.Height)
	tab := f.newHTTP()
	tab.HTTP.URL = "https://www.bilibili.com/?spm=333"
	tab.HTTP.Headers = []logic.EditableRow{
		{Enabled: true, Key: "accept", Value: "text/html,application/xhtml+xml"},
		{Enabled: true, Key: "bad name", Value: "1"},
		{Enabled: false, Key: "x-debug", Value: ""},
		{Enabled: true},
	}
	f.settle()
	rendertest.ShootThemes(t, f.tester, "request-editor", f.setDark)
	tab.HTTP.RequestBodyType = logic.BodyTypeJSON
	tab.HTTP.RequestBodyText = `{"sample":true}`
	f.click(locales.WorkspaceHttpRequestRequestBody())
	rendertest.ShootThemes(t, f.tester, "request-editor-body", f.setDark)
}

func TestRenderPythonConsole(t *testing.T) {
	f, state, _ := consoleFixture(t,
		consoleEntry(1, logic.CurrentRequestScriptPluginID, "stdout", "info", "request prepared for https://example.com/api"),
		consoleEntry(2, "Auth signer", "stdout", "debug", "signature computed"),
		consoleEntry(3, "Auth signer", "stdout", "warning", "token expires in 30 s"),
		consoleEntry(4, "Rate limiter", "stderr", "", "Traceback (most recent call last):\n  File \"plugin.py\", line 3\nValueError: limit"),
	)
	f.tester.SetSize(rendertest.Width, rendertest.Height)
	state.ScriptExecutionID = "render"
	f.settle()
	rendertest.ShootThemes(t, f.tester, "python-console", f.setDark)
	f.click("Traceback (most recent call last): File \"plugin.py\", line 3 ValueError: limit")
	f.tester.Advance(2e9)
	f.settle()
	rendertest.ShootThemes(t, f.tester, "python-console-detail", f.setDark)
}
