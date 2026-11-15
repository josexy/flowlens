package logic

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

func TestRawHTTP1RequestKeepsEscapedTargetsAndFields(t *testing.T) {
	got := FormatRawHTTPRequest(RawHTTPRequestInput{
		Method: "GET", URL: "https://example.test/a%2Fb?q=hello%20world&empty=#fragment",
		Host: "example.test", Protocol: "HTTP/1.1",
		HeaderFields: []HeaderField{{Name: "X-Repeat", Value: "one"}, {Name: "x-empty"}, {Name: "X-Repeat", Value: "two"}},
	})
	want := strings.Join([]string{"GET /a%2Fb?q=hello%20world&empty= HTTP/1.1", "X-Repeat: one", "x-empty: ", "X-Repeat: two", "", ""}, "\r\n")
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestParseURLQueryDecodesAndKeepsOrder(t *testing.T) {
	got := ParseURLQuery("https://example.test/search?lang=zh-CN&tag=first&tag=second&message=hello+world&path=a%2Fb#section")
	want := ParsedURLQuery{
		RawQuery: "lang=zh-CN&tag=first&tag=second&message=hello+world&path=a%2Fb",
		Fields: []QueryParameterField{
			{"lang", "zh-CN"}, {"tag", "first"}, {"tag", "second"}, {"message", "hello world"}, {"path", "a/b"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseURLQueryEdgeCases(t *testing.T) {
	got := ParseURLQuery("/items?flag&empty=&=unnamed&bad=%ZZ")
	want := ParsedURLQuery{
		RawQuery: "flag&empty=&=unnamed&bad=%ZZ",
		Fields:   []QueryParameterField{{"flag", ""}, {"empty", ""}, {"", "unnamed"}, {"bad", "%ZZ"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := ParseURLQuery("/items#ignored?not-a-query"); got.RawQuery != "" || len(got.Fields) != 0 {
		t.Fatalf("no query: %+v", got)
	}
}

func TestParseHighlightedURL(t *testing.T) {
	got := ParseHighlightedURL(" https://example.test:8443/a/b?x=1&y#frag ")
	if got.Scheme != "https://" || got.Host != "example.test:8443" || got.Path != "/a/b" || !got.HasQuery || got.Hash != "#frag" || len(got.QueryItems) != 2 || got.QueryItems[1].HasEquals {
		t.Fatalf("got %+v", got)
	}
	if got := ParseHighlightedURL("  "); !reflect.DeepEqual(got, ParsedHighlightedURL{}) {
		t.Fatalf("empty: %+v", got)
	}
}

func TestRawHTTP1ConnectUsesAuthorityTarget(t *testing.T) {
	got := FormatRawHTTPRequest(RawHTTPRequestInput{Method: "CONNECT", URL: "https://tunnel.example:8443/ignored?q=1", Host: "fallback.example:443", Protocol: "HTTP/1.1"})
	if got != "CONNECT tunnel.example:8443 HTTP/1.1\r\n\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRawHTTP1ResponseKeepsReasonPhrase(t *testing.T) {
	got := FormatRawHTTPResponse(RawHTTPResponseInput{Status: "418 I am a teapot", StatusCode: 418, Protocol: "HTTP/1.1", HeaderFields: []HeaderField{{Name: "Content-Length", Value: "0"}}})
	if got != "HTTP/1.1 418 I am a teapot\r\nContent-Length: 0\r\n\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRawHTTP2RequestConvertsPseudoHeaders(t *testing.T) {
	got := FormatRawHTTPRequest(RawHTTPRequestInput{
		Method: "POST", URL: "https://fallback.test/ignored", Protocol: "HTTP/2.0",
		HeaderFields: []HeaderField{
			{Name: ":method", Value: "GET"}, {Name: ":authority", Value: "api.example.test"}, {Name: ":scheme", Value: "https"},
			{Name: ":path", Value: "/v1/items?q=a%2Fb"}, {Name: "X-Repeat", Value: "one"}, {Name: ":protocol", Value: "websocket"}, {Name: "X-Repeat", Value: "two"},
		},
	})
	want := strings.Join([]string{"GET /v1/items?q=a%2Fb HTTP/2.0", "host: api.example.test", "X-Repeat: one", "protocol: websocket", "X-Repeat: two", "", ""}, "\r\n")
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestRawHTTP2ResponseUsesStatusPseudoHeader(t *testing.T) {
	got := FormatRawHTTPResponse(RawHTTPResponseInput{
		Status: "200 OK", StatusCode: 200, Protocol: "HTTP/2.0",
		HeaderFields: []HeaderField{{Name: ":status", Value: "204"}, {Name: "content-type", Value: "text/plain"}},
	})
	if got != "HTTP/2.0 204\r\ncontent-type: text/plain\r\n\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRawHTTPBodiesAppendOnlyWhenAvailable(t *testing.T) {
	input := RawHTTPRequestInput{Method: "POST", URL: "https://example.test/submit", Protocol: "HTTP/1.1", HeaderFields: []HeaderField{{Name: "Fallback-Header"}}}
	head := FormatRawHTTPRequest(input)
	if head != "POST /submit HTTP/1.1\r\nFallback-Header: \r\n\r\n" {
		t.Fatalf("head %q", head)
	}
	text := input
	text.Body = new("hello\nworld")
	if got := FormatRawHTTPRequest(text); got != head+"hello\nworld" {
		t.Fatalf("text %q", got)
	}
	binary := input
	binary.Body, binary.BodyEncoding = new("AAEC"), "base64"
	if got := FormatRawHTTPRequest(binary); got != head+RawHTTPBinaryBody {
		t.Fatalf("binary %q", got)
	}
	none := input
	none.HeaderFields = nil
	if got := FormatRawHTTPRequest(none); got != "POST /submit HTTP/1.1\r\n\r\n" {
		t.Fatalf("no fields %q", got)
	}
}

func TestWebSocketDisplayMessageKeepsPayload(t *testing.T) {
	created := time.UnixMilli(123)
	got := ToWebSocketDisplayMessage(&proxyservice.WebSocketMessage{Direction: "receive", MsgType: "binary", Data: "AAEC", DataSize: 32 << 20}, "large", created)
	want := WebSocketDisplayMessage{ID: "large", Direction: "receive", MsgType: "binary", Data: "AAEC", DataSize: 32 << 20, CreatedAt: created}
	if got != want {
		t.Fatalf("got %+v", got)
	}
	if NormalizeWebSocketDirection("") != "send" || NormalizeWebSocketMsgType("x") != "text" {
		t.Fatal("defaults")
	}
}

func TestTimestampsUseLocalMicroseconds(t *testing.T) {
	local := time.Date(2026, 8, 12, 17, 56, 1, 341_480_000, time.Local)
	if got := FormatTimeLocal(local.UTC()); got != "2026-08-12 17:56:01.341480" {
		t.Fatalf("got %q", got)
	}
	if got := FormatTimeLocal(time.Time{}); got != UnknownValue {
		t.Fatalf("zero: %q", got)
	}
	if got := FormatUnixMicrosLocal(local.UnixMicro()); got != "2026-08-12 17:56:01.341480" {
		t.Fatalf("micros: %q", got)
	}
	if got := FormatUnixMicrosLocal(-1); got != UnknownValue {
		t.Fatalf("negative: %q", got)
	}
}

func TestDurationsKeepMicrosecondsBelowOneMillisecond(t *testing.T) {
	for _, test := range []struct {
		start, end int64
		want       string
	}{
		{1_000_000, 1_000_000, "0 μs"}, {1_000_000, 1_000_125, "125 μs"}, {1_000_000, 1_000_584, "584 μs"},
		{1_000_000, 1_001_000, "1 ms"}, {1_000_000, 1_007_542, "8 ms"}, {1_000_000, 1_735_936, "736 ms"},
		{-1, 1_000_000, UnknownValue}, {2_000_000, 1_000_000, UnknownValue},
	} {
		if got := FormatDurationMicros(test.start, test.end); got != test.want {
			t.Errorf("%d-%d: got %q, want %q", test.start, test.end, got, test.want)
		}
	}
}

func TestByteSizesUseCompactBinaryUnits(t *testing.T) {
	if got := FormatFileSize(0); got != "0 B" {
		t.Fatal(got)
	}
	if got := FormatFileSize(109); got != "109 B" {
		t.Fatal(got)
	}
	if got := FormatFileSize(1063); got != "1.04 KB" {
		t.Fatal(got)
	}
	if got := FormatFileSizeWith(1024, FileSizeOptions{Precision: 2}); got != "1.00 KB" {
		t.Fatal(got)
	}
	if got := FormatFileSizeWith(-1, FileSizeOptions{UnknownValue: "-"}); got != "-" {
		t.Fatal(got)
	}
	if got := FormatFileSize(-1); got != UnknownValue {
		t.Fatal(got)
	}
	if got := FormatFileSize(1024 * 1024); got != "1 MB" {
		t.Fatal(got)
	}
}

func TestMessageSizesReuseBackendHeaderSize(t *testing.T) {
	request := SummarizeHTTPMessageSize(107, 0, false)
	response := SummarizeHTTPMessageSize(531, 425, false)
	if request != (HTTPMessageSizeSummary{107, 0, 107}) || response != (HTTPMessageSizeSummary{531, 425, 956}) {
		t.Fatalf("%+v %+v", request, response)
	}
	if got := SumKnownByteSizes(request.Total, response.Total); got != 1063 {
		t.Fatal(got)
	}
	if got := SummarizeHTTPMessageSize(107, 0, true); got != (HTTPMessageSizeSummary{UnknownSize, 0, UnknownSize}) {
		t.Fatalf("%+v", got)
	}
	if got := SummarizeHTTPMessageSize(-1, -1, false); got != (HTTPMessageSizeSummary{UnknownSize, UnknownSize, UnknownSize}) {
		t.Fatalf("%+v", got)
	}
}

func TestRequestHeaderConversionKeepsOrderCaseAndDuplicates(t *testing.T) {
	rows := []EditableRow{
		{" X-Last ", "last-1", true}, {"x-first", "first", true}, {"x-last", "last-2", true},
		{"ignored", "disabled", false}, {"   ", "blank", true},
	}
	fields := EditableRowsToHeaderFields(rows)
	want := []HeaderField{{Name: "X-Last", Value: "last-1"}, {Name: "x-first", Value: "first"}, {Name: "x-last", Value: "last-2"}}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields %+v", fields)
	}
	back := HeaderFieldsToEditableRows(fields)
	if len(back) != 3 || back[0] != (EditableRow{"X-Last", "last-1", true}) {
		t.Fatalf("rows %+v", back)
	}
	if got := HeaderFieldsToEditableRows(nil); len(got) != 0 {
		t.Fatal("nil")
	}
}

func TestEditableRequestHeadersExcludePseudoHeaders(t *testing.T) {
	got := EditableHeaderFieldsToRows([]HeaderField{{Name: ":method", Value: "GET"}, {Name: ":status", Value: "200"}, {Name: "X-Test", Value: "one"}})
	if !reflect.DeepEqual(got, []EditableRow{{"X-Test", "one", true}}) {
		t.Fatalf("got %+v", got)
	}
}

func TestRequestHeaderValidation(t *testing.T) {
	for _, test := range []struct {
		rows     []EditableRow
		protocol RequestProtocol
		want     string
	}{
		{[]EditableRow{{"X-Valid", "one", true}, {":status", "200", true}}, ProtocolAuto, ":status"},
		{[]EditableRow{{"Bad Header", "one", true}}, ProtocolAuto, "Bad Header"},
		{[]EditableRow{{":method", "GET", false}}, ProtocolAuto, ""},
		{[]EditableRow{{"Connection", "keep-alive", true}}, ProtocolHTTP2, "Connection"},
		{[]EditableRow{{"Connection", "keep-alive", true}}, ProtocolHTTP1, ""},
	} {
		got, found := FindInvalidRequestHeaderName(test.rows, test.protocol)
		if got != test.want || found != (test.want != "") {
			t.Errorf("%+v: got %q, %v", test.rows, got, found)
		}
	}
}

func TestHeaderJSONHandlesPrototypeLikeNames(t *testing.T) {
	var parsed map[string]any
	text := FormatHeaderFieldsAsJSON([]HeaderField{{Name: "__proto__", Value: "first"}, {Name: "__proto__", Value: "second"}, {Name: "constructor", Value: "value"}})
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed["__proto__"], []any{"first", "second"}) || parsed["constructor"] != "value" {
		t.Fatalf("got %v", parsed)
	}
	if FormatHeaderFieldsAsJSON(nil) != "{}" {
		t.Fatal("empty")
	}
}

func TestOrderedHeaderHelpersKeepInterleavingAndCase(t *testing.T) {
	source := []HeaderField{{Name: "A", Value: "first"}, {Name: "B", Value: "middle"}, {Name: "A", Value: "last"}, {Name: "a", Value: "lowercase"}}
	if got := FormatHeaderFieldsAsText(source); got != "A: first\nB: middle\nA: last\na: lowercase" {
		t.Fatalf("got %q", got)
	}
	want := "{\n  \"A\": [\n    \"first\",\n    \"last\"\n  ],\n  \"B\": \"middle\",\n  \"a\": \"lowercase\"\n}"
	if got := FormatHeaderFieldsAsJSON(source); got != want {
		t.Fatalf("got %s", got)
	}
}

func TestHeaderSortingIsStable(t *testing.T) {
	source := []HeaderField{{Name: "B", Value: "first-b"}, {Name: "a", Value: "first-a"}, {Name: "A", Value: "second-a"}, {Name: "b", Value: "second-b"}}
	values := func(fields []HeaderField) []string {
		out := make([]string, len(fields))
		for i, f := range fields {
			out[i] = f.Value
		}
		return out
	}
	if got := values(SortHeaderFields(source, HeaderSortAsc)); !reflect.DeepEqual(got, []string{"first-a", "second-a", "first-b", "second-b"}) {
		t.Fatalf("asc %v", got)
	}
	if got := values(SortHeaderFields(source, HeaderSortDesc)); !reflect.DeepEqual(got, []string{"first-b", "second-b", "first-a", "second-a"}) {
		t.Fatalf("desc %v", got)
	}
	if got := values(SortHeaderFields(source, HeaderSortDefault)); got[0] != "first-b" {
		t.Fatalf("default %v", got)
	}
}

func TestWireOrderAvailability(t *testing.T) {
	if !HasWireOrder([]HeaderField{}, false) {
		t.Fatal("empty captured list has wire order")
	}
	if HasWireOrder(nil, false) || HasWireOrder([]HeaderField{{Name: "F"}}, true) {
		t.Fatal("missing or normalized fields have no wire order")
	}
}

func TestFirstHeaderValueIgnoresCase(t *testing.T) {
	fields := []HeaderField{{Name: "Content-Type", Value: "application/json"}, {Name: "X-Test", Value: "one"}, {Name: "X-Test", Value: "two"}}
	if got, ok := FirstHeaderValue(fields, "content-type"); !ok || got != "application/json" {
		t.Fatal(got)
	}
	if _, ok := FirstHeaderValue(fields, "missing"); ok {
		t.Fatal("missing found")
	}
}

func TestCookieHelpersUseOrderedFields(t *testing.T) {
	record := RequestCookiesRecord([]HeaderField{{Name: "Cookie", Value: "first=one; second=two"}, {Name: "cookie", Value: "third=three"}})
	want := HeaderRecord{{"first", []string{"one"}}, {"second", []string{"two"}}, {"third", []string{"three"}}}
	if !reflect.DeepEqual(record, want) {
		t.Fatalf("record %+v", record)
	}
	cookies := ParseResponseCookies([]HeaderField{{Name: "Set-Cookie", Value: "session=abc; HttpOnly"}, {Name: "set-cookie", Value: "session=def; Path=/"}})
	if len(cookies) != 2 || cookies[0].Value != "abc" || !reflect.DeepEqual(cookies[0].Attributes, []CookieAttribute{{"HttpOnly", "", false}}) ||
		cookies[1].Header.Name != "set-cookie" || !reflect.DeepEqual(cookies[1].Attributes, []CookieAttribute{{"Path", "/", true}}) {
		t.Fatalf("cookies %+v", cookies)
	}
}

func TestResponseCookiesKeepRawTextAndAttributes(t *testing.T) {
	raw := "session.Value=abc==; domain=.example.test; Path=/; path=/next; Secure; HttpOnly=; SameSite=None; Partitioned; Extension=a=b; Expires=Wed, 21 Oct 2015 07:28:00 GMT"
	cookies := ParseResponseCookies([]HeaderField{{Name: "Set-Cookie", Value: raw}, {Name: "set-cookie", Value: "session.Value="}, {Name: "Set-Cookie", Value: `session#2="quoted=="`}})
	if len(cookies) != 3 || cookies[0].Name != "session.Value" || cookies[0].Value != "abc==" || cookies[0].Header.Value != raw {
		t.Fatalf("cookies %+v", cookies)
	}
	want := []CookieAttribute{
		{"domain", ".example.test", true}, {"Path", "/", true}, {"path", "/next", true}, {"Secure", "", false}, {"HttpOnly", "", true},
		{"SameSite", "None", true}, {"Partitioned", "", false}, {"Extension", "a=b", true}, {"Expires", "Wed, 21 Oct 2015 07:28:00 GMT", true},
	}
	if !reflect.DeepEqual(cookies[0].Attributes, want) {
		t.Fatalf("attributes %+v", cookies[0].Attributes)
	}
	if cookies[1].Value != "" || len(cookies[1].Attributes) != 0 || cookies[2].Name != "session#2" || cookies[2].Value != `"quoted=="` {
		t.Fatalf("cookies %+v", cookies[1:])
	}
}

func TestCookieHeaderCopyingKeepsCaseAndValues(t *testing.T) {
	fields := []HeaderField{{Name: "Set-Cookie", Value: "a=1; Secure"}, {Name: "set-cookie", Value: "a=2; Path=/next"}, {Name: "Content-Type", Value: "text/plain"}, {Name: "Set-Cookie", Value: "b=;  HttpOnly "}}
	headers := CookieHeaderFields(fields, ResponseCookieHeader)
	if !reflect.DeepEqual(headers, []HeaderField{fields[0], fields[1], fields[3]}) {
		t.Fatalf("headers %+v", headers)
	}
	if got := FormatHeaderFieldsAsText(headers); got != "Set-Cookie: a=1; Secure\nset-cookie: a=2; Path=/next\nSet-Cookie: b=;  HttpOnly " {
		t.Fatalf("text %q", got)
	}
}

func TestMalformedCookiesStayInspectable(t *testing.T) {
	if len(ParseResponseCookies(nil)) != 0 || len(ParseResponseCookies([]HeaderField{{Name: "Cookie", Value: "a=1"}})) != 0 {
		t.Fatal("no cookies expected")
	}
	cookies := ParseResponseCookies([]HeaderField{{Name: "Set-Cookie"}, {Name: "Set-Cookie", Value: "name-only; =orphan; ; Flag"}})
	if len(cookies) != 2 || cookies[0].Header.Value != "" || cookies[0].HasEquals || cookies[1].HasEquals {
		t.Fatalf("cookies %+v", cookies)
	}
	if !reflect.DeepEqual(cookies[1].Attributes, []CookieAttribute{{"", "orphan", true}, {"Flag", "", false}}) {
		t.Fatalf("attributes %+v", cookies[1].Attributes)
	}
}

func TestRequestCookieCount(t *testing.T) {
	headers := []EditableRow{{"Cookie", "first=one; second=two", true}, {"cookie", "disabled=three", false}, {"X-Test", "ignored", true}}
	if got := CountRequestCookieRows(headers); got != 2 {
		t.Fatal(got)
	}
	if got := CountRequestCookieRows(nil); got != 0 {
		t.Fatal(got)
	}
}

func TestReplaceRequestCookieHeadersKeepsPositionAndSpelling(t *testing.T) {
	headers := []EditableRow{{"A", "1", true}, {"cookie", "old=1", true}, {"B", "2", true}, {"Cookie", "older=2", false}}
	got := ReplaceRequestCookieHeaders(headers, []EditableRow{{"x", "1", true}, {"y", "2", false}, {"  ", "skip", true}})
	want := []EditableRow{{"A", "1", true}, {"cookie", "x=1", true}, {"cookie", "y=2", false}, {"B", "2", true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	got = ReplaceRequestCookieHeaders([]EditableRow{{"A", "1", true}}, []EditableRow{{"x", "1", true}})
	if !reflect.DeepEqual(got, []EditableRow{{"A", "1", true}, {"Cookie", "x=1", true}}) {
		t.Fatalf("appended %+v", got)
	}
}

func TestProtocolConversionChangesOnlyRouteRows(t *testing.T) {
	rows := []EditableRow{{"X-First", "1", true}, {"Host", "old.test", true}, {"X-Second", "2", false}}
	http2 := ConvertRequestRouteHeaders(rows, ProtocolHTTP2, "https://new.test/a?q=1")
	if !reflect.DeepEqual(http2, []EditableRow{rows[0], rows[2]}) {
		t.Fatalf("http2 %+v", http2)
	}
	http1 := ConvertRequestRouteHeaders(http2, ProtocolHTTP1, "http://back.test/")
	if !reflect.DeepEqual(http1, []EditableRow{{"Host", "back.test", true}, rows[0], rows[2]}) {
		t.Fatalf("http1 %+v", http1)
	}
}

func TestSynchronizeRouteHeadersFollowsURL(t *testing.T) {
	rows := []EditableRow{{"A", "1", true}, {"host", "old", true}, {"HOST", "dup", true}, {":path", "/", true}}
	got := SynchronizeRequestRouteHeaders(rows, ProtocolHTTP1, "https://Example.test:443/x")
	if !reflect.DeepEqual(got, []EditableRow{{"A", "1", true}, {"host", "example.test", true}}) {
		t.Fatalf("got %+v", got)
	}
	got = SynchronizeRequestRouteHeaders([]EditableRow{{"A", "1", true}}, ProtocolHTTP1, "http://b.test:8080/")
	if !reflect.DeepEqual(got, []EditableRow{{"Host", "b.test:8080", true}, {"A", "1", true}}) {
		t.Fatalf("inserted %+v", got)
	}
	got = SynchronizeRequestRouteHeaders(rows, ProtocolHTTP2, "https://a.test/")
	if !reflect.DeepEqual(got, []EditableRow{{"A", "1", true}}) {
		t.Fatalf("http2 %+v", got)
	}
}

func TestRequestProtocolInheritanceRequiresCompleteBlock(t *testing.T) {
	if NormalizeRequestProtocol("") != ProtocolAuto {
		t.Fatal("default")
	}
	for _, test := range []struct {
		message *proxyservice.HTTPMessage
		want    RequestProtocol
	}{
		{&proxyservice.HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []HeaderField{}}, ProtocolHTTP2},
		{&proxyservice.HTTPMessage{Proto: "HTTP/1.1", HeaderFields: []HeaderField{{Name: "Host", Value: "example.test"}}}, ProtocolHTTP1},
		{&proxyservice.HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []HeaderField{}, HeadersTruncated: true}, ProtocolAuto},
		{&proxyservice.HTTPMessage{Proto: "HTTP/2.0", HeaderFields: []HeaderField{}, HeaderOrderUnavailable: true}, ProtocolAuto},
		{&proxyservice.HTTPMessage{Proto: "HTTP/1.1"}, ProtocolAuto},
		{nil, ProtocolAuto},
	} {
		if got := InferRequestProtocol(test.message); got != test.want {
			t.Errorf("%+v: got %q", test.message, got)
		}
	}
}
