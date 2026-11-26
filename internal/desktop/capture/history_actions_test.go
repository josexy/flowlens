package capture

import (
	"strings"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
	historyservice "github.com/josexy/flowlens/internal/services/history_service"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

func TestHistoryRowActionsResolveCompleteRecords(t *testing.T) {
	f := newHistoryFixture(t)
	summary := f.history.entries["k1"][0]
	full := *summary
	full.Request = &proxyservice.HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []proxyservice.HTTPHeaderField{{Name: "X-Repeat", Value: "one"}, {Name: "x-repeat", Value: "two"}, {Name: "X-Empty", Value: ""}}}
	body := &proxyservice.TrafficBodyView{RequestBody: "payload"}
	f.history.details = map[uint64]historyservice.HistoryDetail{summary.ID: {Entry: &full, Body: body}}
	f.click("session one")
	table := f.workspace.HistoryTable
	table.copyCurl([]*logic.TrafficEntry{summary})
	f.settle()
	copied := f.tester.Clipboard()
	if !strings.Contains(copied, "X-Repeat: one") || !strings.Contains(copied, "x-repeat: two") || !strings.Contains(copied, "X-Empty: ") || !strings.Contains(copied, "payload") {
		t.Fatalf("cURL lost stored data: %s", copied)
	}
	var opened *logic.TrafficEntry
	table.deps.OpenEditor = func(entry *logic.TrafficEntry, got *proxyservice.TrafficBodyView, _ bool, key string, done func(error)) {
		opened = entry
		if got != body || key != "k1" {
			t.Fatal("editor lost body or source")
		}
		done(nil)
	}
	table.editEntries([]*logic.TrafficEntry{summary})
	f.settle()
	if opened != &full || summary.Request != nil {
		t.Fatal("action used or modified lightweight row")
	}
}
