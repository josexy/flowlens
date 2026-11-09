package logic

import (
	"reflect"
	"slices"
	"testing"
	"time"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

func categoryEntry(id uint64, host string, process *proxyservice.ProcessInfo) *TrafficEntry {
	if process == nil {
		process = &proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusResolved, AppID: "com.example.browser", DisplayName: "Browser", ProcessName: "browser.exe", ExecutablePath: `C:\Apps\Browser\browser.exe`}
	}
	return &TrafficEntry{ID: id, Type: "https", Host: host, URL: "https://" + host + "/", Path: "/", Metadata: &proxyservice.Metadata{Process: process}}
}

func appProcess(id, name, path string) *proxyservice.ProcessInfo {
	return &proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusResolved, AppID: id, DisplayName: name, ProcessName: name + ".exe", ExecutablePath: path}
}

func TestProcessSummaryGroupsByIdentity(t *testing.T) {
	summary := BuildProcessSummary([]*TrafficEntry{
		categoryEntry(1, "one.test", nil),
		categoryEntry(2, "two.test", nil),
		categoryEntry(3, "three.test", &proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusResolved, ExecutablePath: `C:\First\tool.exe`, DisplayName: "Tool", ProcessName: "tool.exe"}),
		categoryEntry(4, "four.test", &proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusResolved, ExecutablePath: `C:\Second\tool.exe`, DisplayName: "Tool", ProcessName: "tool.exe"}),
		categoryEntry(5, "pending.test", &proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusPending}),
	}, "Unidentified process", false)
	if len(summary) != 4 {
		t.Fatalf("%+v", summary)
	}
	browser := slices.IndexFunc(summary, func(item ProcessSummaryItem) bool { return item.ProcessKey == "app:com.example.browser" })
	if summary[browser].Count != 2 {
		t.Fatal("count")
	}
	last := summary[len(summary)-1]
	if last.Kind != ProcessUnavailable || last.Count != 1 {
		t.Fatalf("%+v", last)
	}
}

func TestProcessSummaryKeepsSelectedEmptyUnavailable(t *testing.T) {
	summary := BuildProcessSummary([]*TrafficEntry{categoryEntry(1, "one.test", nil)}, "Unidentified process", true)
	if last := summary[len(summary)-1]; last.ProcessKey != ProcessCategoryUnavailableKey || last.Count != 0 {
		t.Fatalf("%+v", last)
	}
}

func TestProcessSearchMatchesVisibleNames(t *testing.T) {
	summary := BuildProcessSummary([]*TrafficEntry{categoryEntry(1, "one.test", nil), categoryEntry(2, "p.test", &proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusPending})}, "Unidentified process", false)
	for query, want := range map[string]int{"Browser": 1, "browser.exe": 1, "Unidentified": 1, "com.example.browser": 0, `C:\Apps\Browser`: 0} {
		if got := len(FilterProcessSummary(summary, query)); got != want {
			t.Errorf("%q: %d", query, got)
		}
	}
}

func hostNames(items []HostSummaryItem) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Host
	}
	return names
}

func TestFacetsNarrowOnlyByTheOppositeFacet(t *testing.T) {
	entries := []*TrafficEntry{
		categoryEntry(1, "api.example.com", nil), categoryEntry(2, "cdn.example.com", nil),
		categoryEntry(3, "api.example.com", appProcess("com.example.terminal", "Terminal", `C:\Terminal`)),
		categoryEntry(4, "admin.example.com", appProcess("com.example.editor", "Editor", `C:\Editor`)),
	}
	hosts, processes := BuildCategoryFacets(entries, map[string]bool{"api.example.com": true}, nil, "x")
	if !slices.Equal(hostNames(hosts), []string{"admin.example.com", "api.example.com", "cdn.example.com"}) {
		t.Fatal(hostNames(hosts))
	}
	labels := []string{}
	for _, item := range processes {
		labels = append(labels, item.Label)
	}
	if !slices.Equal(labels, []string{"Browser", "Terminal"}) {
		t.Fatal(labels)
	}
	hosts, processes = BuildCategoryFacets(entries, nil, map[string]bool{"app:com.example.browser": true}, "x")
	if !slices.Equal(hostNames(hosts), []string{"api.example.com", "cdn.example.com"}) || len(processes) != 3 {
		t.Fatalf("%v %v", hostNames(hosts), processes)
	}
}

func TestFacetsRetainSelectedZeroResultItems(t *testing.T) {
	entries := []*TrafficEntry{categoryEntry(1, "api.example.com", nil), categoryEntry(2, "admin.example.com", appProcess("com.example.editor", "Editor", `C:\E`))}
	hosts, processes := BuildCategoryFacets(entries, map[string]bool{"api.example.com": true}, map[string]bool{"app:com.example.editor": true}, "x")
	if !reflect.DeepEqual(hosts, []HostSummaryItem{{"admin.example.com", 1}, {"api.example.com", 0}}) {
		t.Fatalf("%+v", hosts)
	}
	if processes[0].ProcessKey != "app:com.example.browser" || processes[0].Count != 1 || processes[1].ProcessKey != "app:com.example.editor" || processes[1].Count != 0 {
		t.Fatalf("%+v", processes)
	}
}

func TestCategoryFiltersORWithinAndANDAcross(t *testing.T) {
	browser := categoryEntry(1, "api.example.com", nil)
	editor := categoryEntry(2, "cdn.example.com", appProcess("com.example.editor", "Editor", "/opt/editor"))
	if !TrafficMatchesCategoryFilters(browser, map[string]bool{"api.example.com": true, "other": true}, map[string]bool{"app:com.example.browser": true, "app:other": true}) {
		t.Fatal("or")
	}
	if TrafficMatchesCategoryFilters(editor, map[string]bool{"api.example.com": true}, map[string]bool{"app:com.example.editor": true}) {
		t.Fatal("and")
	}
	pending := categoryEntry(3, "a", &proxyservice.ProcessInfo{Status: proxyservice.ProcessStatusPending})
	if !TrafficMatchesCategoryFilters(pending, nil, map[string]bool{ProcessCategoryUnavailableKey: true}) {
		t.Fatal("unavailable")
	}
}

func TestStructureTreeGroupsPaths(t *testing.T) {
	a := &TrafficEntry{ID: 1, Type: "https", Host: "h.test", URL: "https://h.test/api/users", Path: "/api/users"}
	b := &TrafficEntry{ID: 2, Type: "https", Host: "h.test", URL: "https://h.test/api/items", Path: "/api/items"}
	root := &TrafficEntry{ID: 3, Type: "https", Host: "h.test", URL: "https://h.test/", Path: "/"}
	tcp := &TrafficEntry{ID: 4, Type: "tcp", Host: "h.test:443", RawTCP: &proxyservice.RawTCPTunnelInfo{HostPort: "h.test:443", TLS: true}}
	roots := BuildStructureTree([]*TrafficEntry{a, b, root, tcp})
	if len(roots) != 1 || roots[0].Label != "h.test" || len(roots[0].EntryIDs) != 4 {
		t.Fatalf("%+v", roots)
	}
	labels := []string{}
	for _, child := range roots[0].Children {
		labels = append(labels, child.Label)
	}
	if !slices.Equal(labels, []string{"/", "api", "TCP-TLS"}) {
		t.Fatal(labels)
	}
	api := roots[0].Children[1]
	if api.Type != StructureSegment || len(api.Children) != 2 || api.Children[0].Label != "items" || api.Children[0].Type != StructureLeaf || api.Depth != 1 {
		t.Fatalf("%+v", api)
	}
	if got := FilterStructureByHost(roots, "OTHER"); len(got) != 0 {
		t.Fatal("filter")
	}
}

func TestHostSummaryFilter(t *testing.T) {
	items := BuildHostSummary([]*TrafficEntry{categoryEntry(1, "b.test", nil), categoryEntry(2, "a.test", nil), categoryEntry(3, "a.test", nil)})
	if !reflect.DeepEqual(items, []HostSummaryItem{{"a.test", 2}, {"b.test", 1}}) {
		t.Fatalf("%+v", items)
	}
	if got := FilterHostSummary(items, " B."); len(got) != 1 || got[0].Host != "b.test" {
		t.Fatalf("%+v", got)
	}
}

func TestHARExportableFormats(t *testing.T) {
	for version, want := range map[int]bool{1: true, 2: true, 0: false, 3: false} {
		if IsHARExportableHistoryFormat(version) != want {
			t.Errorf("%d", version)
		}
	}
}

func TestUniqueHARImportPaths(t *testing.T) {
	if got := UniqueHARImportPaths([]string{`C:\A.har`, "c:/a.har", "C:/b.har", ""}, true); !slices.Equal(got, []string{`C:\A.har`, "C:/b.har"}) {
		t.Fatal(got)
	}
	if got := UniqueHARImportPaths([]string{"/tmp/A.har", "/tmp/a.har", "/tmp/A.har"}, false); !slices.Equal(got, []string{"/tmp/A.har", "/tmp/a.har"}) {
		t.Fatal(got)
	}
	if HARImportFileName(`C:\x\a.har`) != "a.har" || HARImportFileName("/x/b.har") != "b.har" {
		t.Fatal("name")
	}
}

func TestCanExportTraffic(t *testing.T) {
	completed := &proxyservice.HTTPMessageMetrics{State: proxyservice.HTTPMessageStateCompleted}
	entry := &TrafficEntry{Type: "https", Method: "GET", StatusCode: 200,
		Request:  &proxyservice.HTTPMessage{Proto: "HTTP/1.1", Metrics: completed},
		Response: &proxyservice.HTTPMessage{Proto: "HTTP/1.1"},
	}
	for format, want := range map[TrafficExportFormat]bool{
		ExportRequest: true, ExportRequestHeaders: true, ExportRequestBody: true, ExportResponseHeaders: true,
		ExportResponseBody: false, ExportRequestResponse: false, ExportCSV: true, ExportHAR: true,
	} {
		if got := CanExportTraffic(entry, format); got != want {
			t.Errorf("%s: %v", format, got)
		}
	}
	raw := rawTCPEntry()
	if CanExportTraffic(raw, ExportHAR) || !CanExportTraffic(raw, ExportCSV) {
		t.Fatal("raw TCP")
	}
}

func TestExportNames(t *testing.T) {
	entry := withContentType(tableEntry(1, 200), "application/vnd.x+json")
	for format, want := range map[TrafficExportFormat]string{
		ExportCSV: ".csv", ExportHAR: ".har", ExportRequestHeaders: ".txt", ExportRequest: ".http", ExportResponseBody: ".json", ExportRequestBody: ".bin",
	} {
		if got := TrafficExportExtension(format, entry); got != want {
			t.Errorf("%s: %s", format, got)
		}
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := MakeHARFilename("a:b. ", now); got != "a-b.har" {
		t.Fatal(got)
	}
	if got := MakeHARFilename("", now); got != "flowlens-20260102-030405.har" {
		t.Fatal(got)
	}
	if EnsureHARExtension("/x/a") != "/x/a.har" || EnsureHARExtension("/x/a.HAR") != "/x/a.HAR" {
		t.Fatal("har extension")
	}
	if EnsureFileExtension("/x.y/a", ".csv") != "/x.y/a.csv" || EnsureFileExtension("/x/a.txt", ".csv") != "/x/a.txt" {
		t.Fatal("extension")
	}
}
