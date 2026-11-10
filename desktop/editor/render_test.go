package editor

import (
	"testing"

	"github.com/josexy/flowlens/desktop/internal/rendertest"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/desktop/theme"
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
}
