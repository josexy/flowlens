package editor

import (
	"fmt"
	"image/color"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/flowlens/desktop/logic"
	"github.com/josexy/flowlens/locales"
	nui "github.com/josexy/nuxt-ui-go/ui"
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
	if got := view.table.List.Columns.Widths[consoleColumnSource]; got != 420 {
		t.Fatalf("Source is %v wide, want 420 (widths %v)", got, view.table.List.Columns.Widths)
	}
}

func TestPythonConsoleVirtualizesAndRefreshesRollingEntries(t *testing.T) {
	entries := make([]logic.PluginLogEntry, 2000)
	for index := range entries {
		entries[index] = consoleEntry(uint64(index+1), "alpha", "stdout", "info", fmt.Sprintf("message-%d", index+1))
	}
	f, state, view := consoleFixture(t, entries...)
	checkVirtualized := func() {
		t.Helper()
		if cells := view.table.CellBuilds; cells == 0 || cells > 50*len(consoleColumnIDs) {
			t.Fatalf("built %d cells for a 2000-row console", cells)
		}
	}
	checkVirtualized()
	if !f.tester.HasText("message-2000") {
		t.Fatal("console did not follow the latest entry")
	}
	view.table.List.ScrollTo(0, ui.Start)
	f.settle()
	if !f.tester.HasText("message-1") {
		t.Fatal("console did not scroll to its first entry")
	}
	checkVirtualized()

	// A full console replaces its oldest rows while its count stays constant.
	view.table.List.ScrollToEnd()
	f.settle()
	state.ScriptConsoleEntries = logic.AppendPythonLogBatch(state.ScriptConsoleEntries, []logic.PluginLogEntry{
		consoleEntry(2001, "alpha", "stdout", "info", "replacement-message"),
	}, len(entries))
	f.settle()
	if !f.tester.HasText("replacement-message") {
		t.Fatal("console did not follow the replacement entry")
	}
	if view.table.Row(0).EventID != 2 || view.table.Len() != len(entries) {
		t.Fatal("the table retained the old console snapshot")
	}
	checkVirtualized()
	f.click("replacement-message")
	if view.detail == nil || view.detail.EventID != 2001 {
		t.Fatalf("opened the wrong entry after rotation: %+v", view.detail)
	}
}

func TestPythonConsoleHeaderDividerIsOnePixel(t *testing.T) {
	f, _, _ := consoleFixture(t, consoleEntry(1, "alpha", "stdout", "info", "hello"))
	f.tester.SetSize(1200, 500)
	const scale = 1.5
	f.tester.SetScale(scale)
	for _, dark := range []bool{false, true} {
		f.setDark(dark)
		palette := f.data.Settings.Theme().Config.Resolve(dark, ui.Preferences{})
		border, background := palette.BorderAccented, palette.Background
		line := color.RGBA{R: border.R, G: border.G, B: border.B, A: border.A}
		blank := color.RGBA{R: background.R, G: background.G, B: background.B, A: background.A}
		for _, zoom := range []float32{1, 1.19} {
			f.tester.SetZoom(zoom)
			header, ok := f.tester.AccessByRole(ui.RoleColumnHeader, locales.WorkspaceHttpRequestConsoleColumnTime())
			if !ok {
				t.Fatal("console time header is missing")
			}
			x := int((header.Bounds.X + header.Bounds.W/2) * scale)
			y := int(math.Round(float64((header.Bounds.Y + header.Bounds.H) * scale)))
			frame := f.tester.Image()
			got := [3]color.RGBA{frame.RGBAAt(x, y-1), frame.RGBAAt(x, y), frame.RGBAAt(x, y+1)}
			if want := [3]color.RGBA{blank, line, blank}; got != want {
				t.Fatalf("dark=%v zoom=%v: divider and adjacent pixels are %v, want %v", dark, zoom, got, want)
			}
		}
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

func TestPythonConsoleRowsOpenByKeyboard(t *testing.T) {
	f, _, view := consoleFixture(t,
		consoleEntry(1, "alpha", "stdout", "info", "one"),
		consoleEntry(2, "beta", "stdout", "info", "two"),
		consoleEntry(3, "gamma", "stdout", "info", "three"),
	)
	open := func(want uint64) {
		t.Helper()
		f.tester.Advance(time.Second)
		f.settle()
		if !view.open || view.detail == nil || view.detail.EventID != want {
			t.Fatalf("open %v detail %+v, want entry %d", view.open, view.detail, want)
		}
	}
	close := func() {
		t.Helper()
		f.tester.Key(0, ui.KeyEscape)
		f.tester.Advance(time.Second)
		f.settle()
		if view.open {
			t.Fatal("Escape left the detail open")
		}
	}
	f.click("alpha")
	open(1)
	// Closing gives the focus back to the table, whose keys move from the
	// row opened, and Enter opens the row they reach.
	close()
	f.tester.Key(0, ui.KeyDown)
	f.settle()
	if view.open {
		t.Fatal("a key opened an entry")
	}
	f.tester.Key(0, ui.KeyEnter)
	open(2)
	close()
	// A click on the row opened before opens it again.
	f.click("alpha")
	open(1)
	close()
	// A click on the header opens nothing.
	f.click(locales.WorkspaceHttpRequestConsoleColumnTime())
	if view.open {
		t.Fatal("a click on the header opened an entry")
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
