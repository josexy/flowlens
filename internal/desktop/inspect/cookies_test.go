package inspect

import (
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/josexy/flowlens/locales"
)

func cookieFixture(t *testing.T, setCookies ...string) *fixture {
	t.Helper()
	entry := httpEntry()
	entry.Response.HeaderFields = nil
	for _, value := range setCookies {
		entry.Response.HeaderFields = append(entry.Response.HeaderFields, logic.HeaderField{Name: "Set-Cookie", Value: value})
	}
	f := newFixture(t, entry, &proxyservice.TrafficBodyView{})
	f.panel.responseTab = tabResponseCookies
	f.settle()
	return f
}

func (f *fixture) openAttributes(name, count string) {
	f.t.Helper()
	f.click(locales.DetailCookieViewAttributes(count, name))
}

func (f *fixture) count(text string) int {
	n := 0
	for _, shown := range f.tester.Texts() {
		if shown == text {
			n++
		}
	}
	return n
}

func TestCookieAttributesFlagAndEmptyValue(t *testing.T) {
	f := cookieFixture(t, "sid=abc; Secure; Domain=")
	f.has("sid")
	f.has("abc")
	f.openAttributes("sid", "2")
	f.has(locales.DetailCookieAttributeCount("2"))
	f.has("Secure")
	f.has("Domain")
	if f.count(locales.DetailCookieEmptyValue()) != 1 {
		t.Fatalf("want one Empty value, got %v", f.tester.Texts())
	}
	// Secure, a flag, shows a check mark rather than text.
	for _, text := range f.tester.Texts() {
		if text == "true" || text == "Secure=" {
			t.Fatalf("flag shown as text %q", text)
		}
	}
	cookie := logic.ParseResponseCookies([]logic.HeaderField{{Name: "Set-Cookie", Value: "sid=abc; Secure; Domain="}})[0]
	if got := []attributeShown{cookieAttributeShown(cookie.Attributes[0]), cookieAttributeShown(cookie.Attributes[1])}; got[0] != attributeFlag || got[1] != attributeEmpty {
		t.Fatalf("attributes shown as %v", got)
	}
	f.has(locales.DetailCookieRawHeader())
	f.has("Set-Cookie: sid=abc; Secure; Domain=")
}

func TestCookieWithoutAttributes(t *testing.T) {
	f := cookieFixture(t, "plain=1")
	f.openAttributes("plain", "0")
	f.has(locales.DetailCookieNoAttributes())
	f.has(locales.DetailCookieAttributeCount("0"))
}

func TestCookieRawHeaderCopies(t *testing.T) {
	f := cookieFixture(t, "a=1; Path=/", "b=2; HttpOnly")
	f.openAttributes("b", "1")
	// The pane's copy button has the same label, as in 1.0.5: click the
	// popover's, at the end of its Raw Set-Cookie row.
	raw, _ := f.tester.Find(locales.DetailCookieRawHeader())
	head, _ := f.tester.Find(locales.DetailCookieAttributeCount("1"))
	f.tester.ClickAt(head.X+head.W-12, raw.Y+raw.H/2)
	f.settle()
	if got := f.tester.Clipboard(); got != "Set-Cookie: b=2; HttpOnly" {
		t.Fatalf("copied %q", got)
	}
}

func TestCookieValueOpensReadOnlyEditor(t *testing.T) {
	f := cookieFixture(t, "token=abcdef; Path=/")
	value, _ := f.tester.Find("abcdef")
	f.tester.ClickAt(value.X+2, value.Y+value.H/2)
	f.settle()
	if editor, ok := f.tester.Access("cell-editor"); !ok || editor.Value != "abcdef" {
		t.Fatalf("editor %+v", editor)
	}
}
