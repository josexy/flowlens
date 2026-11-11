package editor

import (
	"image/color"
	"slices"
	"testing"
	"time"

	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go"
)

// consoleFixture shows the console of an HTTP tab holding entries.
func consoleFixture(t *testing.T, entries ...logic.PluginLogEntry) (*fixture, *logic.HTTPEditorState, *consoleView) {
	t.Helper()
	f := newFake(t)
	f.tester.SetSize(1600, 1000)
	tab := f.newHTTP()
	state := tab.HTTP
	state.InlineScriptEnabled = true
	state.ActiveResponseTab = logic.ResponseTabConsole
	state.ScriptConsoleEntries = entries
	f.settle()
	return f, state, f.editor.https[tab.Key].response.console
}

func consoleEntry(id uint64, plugin, stream, level, message string) logic.PluginLogEntry {
	return logic.PluginLogEntry{EventID: id, PluginID: plugin, PluginName: plugin, Stream: stream, Level: level, Message: message, Timestamp: 1_700_000_000_000_000 + int64(id)}
}

func TestPythonConsoleColumns(t *testing.T) {
	f, _, view := consoleFixture(t, consoleEntry(1, "alpha", "stdout", "info", "hello"))
	for _, title := range []string{locales.WorkspaceHttpRequestConsoleColumnTime(), locales.WorkspaceHttpRequestConsoleColumnType(), locales.WorkspaceHttpRequestConsoleColumnSource(), locales.WorkspaceHttpRequestConsoleColumnOutput()} {
		if !f.tester.HasText(title) {
			t.Fatalf("no %q column in %v", title, f.tester.Texts())
		}
	}
	// Dragging the Source column's edge to 600 px stops at 420.
	header, ok := f.tester.Find(locales.WorkspaceHttpRequestConsoleColumnSource())
	if !ok {
		t.Fatal("Source header")
	}
	output, ok := f.tester.Find(locales.WorkspaceHttpRequestConsoleColumnOutput())
	if !ok {
		t.Fatal("Output header")
	}
	// The Source cell ends where the Output cell, padded as Source's, begins.
	edge, y := output.X-10-1, header.Y+header.H/2
	f.tester.Press(edge, y)
	f.tester.Frame()
	for x := edge; x <= edge+480; x += 40 {
		f.tester.Move(x, y)
		f.tester.Frame()
	}
	f.tester.Release(edge+480, y)
	f.settle()
	if got := view.list.Columns.Widths[consoleColumnSource]; got != 420 {
		t.Fatalf("Source is %v wide, want 420 (widths %v)", got, view.list.Columns.Widths)
	}
}

func TestPythonConsoleRows(t *testing.T) {
	f, _, view := consoleFixture(t,
		consoleEntry(1, "alpha", "stderr", "", "first line\nsecond line\nthird line"),
		consoleEntry(2, "beta", "stdout", "warning", "plain"),
	)
	first, ok := f.tester.Find("alpha")
	if !ok {
		t.Fatalf("no row in %v", f.tester.Texts())
	}
	second, ok := f.tester.Find("beta")
	if !ok {
		t.Fatal("second row")
	}
	if height := second.Y - first.Y; height != consoleRowHeight {
		t.Fatalf("rows are %v apart, want %v", height, consoleRowHeight)
	}
	for _, text := range []string{
		locales.WorkspaceHttpRequestConsoleTypeStderr(), locales.WorkspaceHttpRequestConsoleLevelInfo(),
		"first line second line third line", locales.WorkspaceHttpRequestConsoleLines("3"),
		locales.WorkspaceHttpRequestConsoleTypeStdout(), locales.WorkspaceHttpRequestConsoleLevelWarning(),
	} {
		if !f.tester.HasText(text) {
			t.Fatalf("missing %q in %v", text, f.tester.Texts())
		}
	}
	if logic.PythonLogTone("stderr", "") != logic.PythonToneError || consoleTone(logic.PythonLogTone("stderr", "")) != nui.Error {
		t.Fatal("a stderr entry without a level is not in the error tone")
	}
	if _, ok := f.tester.Access(locales.WorkspaceHttpRequestConsoleWrap()); ok {
		t.Fatal("the list has a wrap button")
	}

	// Opening an entry.
	f.click("plain")
	if !view.open || view.detail == nil || view.detail.EventID != 2 {
		t.Fatalf("detail %+v open %v", view.detail, view.open)
	}
}

// pixel returns the color at x, y of the tester's next frame.
func pixel(f *fixture, x, y float32) color.RGBA {
	image := f.tester.Image()
	return image.RGBAAt(int(x), int(y))
}

func TestPythonConsoleStatus(t *testing.T) {
	f, state, _ := consoleFixture(t)
	state.IsSending = true
	f.settle()
	running, ok := f.tester.Find(locales.WorkspaceHttpRequestConsoleRunning())
	if !ok {
		t.Fatalf("no running status in %v", f.tester.Texts())
	}
	dotX, dotY := running.X-8-4, running.Y+running.H/2
	start := pixel(f, dotX, dotY)
	f.tester.Advance(time.Second)
	f.tester.Frame()
	half := pixel(f, dotX, dotY)
	if start == half {
		t.Fatalf("the dot does not pulse: %v", start)
	}

	state.IsSending = false
	state.ScriptConsoleEntries = []logic.PluginLogEntry{
		consoleEntry(1, "a", "stdout", "info", "1"), consoleEntry(2, "a", "stdout", "info", "2"), consoleEntry(3, "a", "stdout", "info", "3"),
		consoleEntry(4, "a", "stdout", "info", "4"), consoleEntry(5, "a", "stdout", "info", "5"),
	}
	f.settle()
	done, ok := f.tester.Find(locales.WorkspaceHttpRequestConsoleEntries("5"))
	if !ok {
		t.Fatalf("no entry count in %v", f.tester.Texts())
	}
	still := pixel(f, done.X-8-4, done.Y+done.H/2)
	f.tester.Advance(time.Second)
	f.tester.Frame()
	if later := pixel(f, done.X-8-4, done.Y+done.H/2); later != still {
		t.Fatalf("the dot pulses after the send: %v then %v", still, later)
	}
}

func TestPythonConsoleEntryDetail(t *testing.T) {
	long := "a long single line that goes on and on and on and on and on and on and on and on and on and on and on and on and on and on and on"
	f, _, view := consoleFixture(t, consoleEntry(1, logic.CurrentRequestScriptPluginID, "stdout", "info", long))
	f.click(logic.PythonLogPreview(long, consolePreviewLength))
	f.tester.Advance(time.Second)
	f.settle()
	for _, text := range []string{
		logic.FormatUnixMicrosLocal(view.detail.Timestamp),
		locales.WorkspaceHttpRequestConsoleTypeStdout() + " · " + locales.WorkspaceHttpRequestConsoleLevelInfo(),
		locales.WorkspaceHttpRequestConsoleCurrentScript(),
	} {
		if !f.tester.HasText(text) {
			t.Fatalf("missing %q in %v", text, f.tester.Texts())
		}
	}
	if view.view.Text() != long || view.view.WordWrap() {
		t.Fatalf("detail %q wrap %v", view.view.Text(), view.view.WordWrap())
	}
	f.click(locales.WorkspaceHttpRequestConsoleWrap())
	if info, ok := f.tester.Access(locales.WorkspaceHttpRequestConsoleWrap()); !ok || !info.Checked || !view.view.WordWrap() {
		t.Fatalf("wrap %+v", info)
	}
	f.click(locales.WorkspaceHttpRequestConsoleCopyEntry())
	if got := f.tester.Clipboard(); got != long {
		t.Fatalf("clipboard %q", got)
	}
}

func TestPythonConsoleCopyAndSaveFormat(t *testing.T) {
	entries := []logic.PluginLogEntry{
		consoleEntry(1, logic.CurrentRequestScriptPluginID, "stdout", "info", "hello"),
		consoleEntry(2, "Plugin B", "", "warning", "careful"),
	}
	f, _, _ := consoleFixture(t, entries...)
	want := "[" + logic.FormatUnixMicrosLocal(entries[0].Timestamp) + "] [" + locales.WorkspaceHttpRequestConsoleCurrentScript() + "] [stdout] hello\n" +
		"[" + logic.FormatUnixMicrosLocal(entries[1].Timestamp) + "] [Plugin B] [warning] careful"
	f.click(locales.WorkspaceHttpRequestConsoleCopyAll())
	if got := f.tester.Clipboard(); got != want {
		t.Fatalf("clipboard %q, want %q", got, want)
	}
	f.click(locales.WorkspaceHttpRequestConsoleSave())
	f.click("hello")
	f.tester.Advance(time.Second)
	f.settle()
	f.click(locales.WorkspaceHttpRequestConsoleSaveEntry())
	if got, names := f.pickedNames(), []string{consoleFilename, consoleEntryFilename}; !slices.Equal(got, names) {
		t.Fatalf("suggested %v, want %v", got, names)
	}
	if got := f.savedBodies(); !slices.Equal(got, []string{want, "hello"}) {
		t.Fatalf("saved %q", got)
	}
}

func TestClearingTheConsoleClosesItsDetail(t *testing.T) {
	f, state, view := consoleFixture(t, consoleEntry(1, "a", "stdout", "info", "first"))
	f.click("first")
	if !view.open {
		t.Fatal("detail")
	}
	// Clear, as the console's button does.
	view.clear()
	f.settle()
	if view.open || view.detail != nil || len(state.ScriptConsoleEntries) != 0 {
		t.Fatalf("open %v detail %+v entries %d", view.open, view.detail, len(state.ScriptConsoleEntries))
	}

	// A new send replaces the entries.
	state.ScriptConsoleEntries = []logic.PluginLogEntry{consoleEntry(2, "a", "stdout", "info", "second")}
	f.settle()
	f.click("second")
	if !view.open {
		t.Fatal("detail")
	}
	state.ScriptConsoleEntries = []logic.PluginLogEntry{consoleEntry(3, "a", "stdout", "info", "third")}
	f.settle()
	if view.open || view.detail != nil {
		t.Fatal("the detail outlived its entry")
	}
}
