package inspect

import (
	"runtime"
	"strings"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
)

func TestBodyTextIsPreparedOnlyOnDemand(t *testing.T) {
	v := NewBodyViewer(Dependencies{}, true)
	v.active = logic.BodyTabHex
	content := BodyContent{Identity: 1, ContentType: "application/json", Body: "{\"a\":1}\r\n"}
	v.set(content)
	if v.rawReady || v.formattedReady || v.raw.Text() != "" || v.formatted.Text() != "" {
		t.Fatal("Hex prepared hidden text buffers")
	}
	v.raw.SetWordWrap(true)
	v.active = logic.BodyTabRaw
	v.set(content)
	if v.copyable() != content.Body || v.formattedReady {
		t.Fatal("Raw changed original bytes or prepared Formatted")
	}
	if saved, _ := v.saveTarget(); saved != content.Body {
		t.Fatal("Raw save changed line endings")
	}
	v.active = logic.BodyTabFormatted
	v.set(content)
	want := strings.ReplaceAll(logic.FormatBody(content.Body, logic.BodyJSON), "\r\n", "\n")
	if v.copyable() != want {
		t.Fatal("formatted copy differs")
	}
	if saved, _ := v.saveTarget(); saved != want {
		t.Fatal("formatted save differs")
	}
	v.Reset()
	if !v.raw.WordWrap() || v.raw.Text() != "" || v.formatted.Text() != "" {
		t.Fatal("reset retained data or lost wrap preference")
	}
}

func TestLargeJSONNeverPreparesHiddenFormatting(t *testing.T) {
	v := NewBodyViewer(Dependencies{}, false)
	v.active = logic.BodyTabFormatted
	body := `{"data":"` + strings.Repeat("x", logic.LargeTextThreshold) + `"}`
	v.set(BodyContent{Identity: 1, Body: body, ContentType: "application/json"})
	if v.Active() != logic.BodyTabRaw || v.formattedReady || v.formatted.Text() != "" {
		t.Fatal("large JSON retained formatted text")
	}
	if v.raw.Text() != body || v.copyable() != body {
		t.Fatal("large Raw changed")
	}
}

func TestRepeatedBodyReplacementReleasesOldText(t *testing.T) {
	heap := func() uint64 {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	v := NewBodyViewer(Dependencies{}, false)
	baseline := heap()
	for i := 1; i <= 20; i++ {
		v.set(BodyContent{Identity: uint64(i), Body: strings.Repeat(string(rune('a'+i)), 1<<20), ContentType: "text/plain"})
	}
	loaded := heap()
	v.set(BodyContent{Identity: 21, Body: "tiny", ContentType: "text/plain"})
	released := heap()
	t.Logf("20 x 1 MiB responses: live delta %.2f MiB; after tiny response %.2f MiB", float64(int64(loaded)-int64(baseline))/(1<<20), float64(int64(released)-int64(baseline))/(1<<20))
	if int64(loaded)-int64(baseline) > 5<<20 || int64(released)-int64(baseline) > 2<<20 {
		t.Fatal("obsolete responses remain reachable")
	}
	runtime.KeepAlive(v)
}
