package logic

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func parseSSE(chunks ...string) ([]*SSEEvent, *SSEParser, []SSEMessageDetail) {
	parser := &SSEParser{}
	var events []*SSEEvent
	for _, chunk := range chunks {
		events = append(events, parser.Append(chunk)...)
	}
	body := strings.Join(chunks, "")
	details := make([]SSEMessageDetail, len(events))
	for i, event := range events {
		details[i] = SSEEventDetail(body, *event)
	}
	return events, parser, details
}

func TestSSEBoundariesPreserveEvents(t *testing.T) {
	body := "\xef\xbb\xbf: ping\r\nid: control\r\nretry: 1500\r\n\r\n" +
		"event: server\rdata: 中文😀: hello\rdata:  indented\rid: 1\r\r" +
		"data: next\n\n" +
		"event:\r\ndata\r\ndata:\r\nid:\r\n\r\n" +
		"event: unfinished\ndata: tail"
	_, parser, expected := parseSSE(body)
	summary := [][3]string{}
	for _, item := range expected {
		summary = append(summary, [3]string{item.EventType, item.LastEventID, item.Data})
	}
	want := [][3]string{{"server", "1", "中文😀: hello\n indented"}, {"message", "1", "next"}, {"message", "", "\n"}}
	if !reflect.DeepEqual(summary, want) {
		t.Fatalf("%q", summary)
	}
	if !parser.HasPendingEvent() {
		t.Fatal("pending")
	}
	for index := 0; index <= len(body); index++ {
		_, _, details := parseSSE(body[:index], body[index:])
		if !reflect.DeepEqual(details, expected) {
			t.Fatalf("boundary %d: %+v", index, details)
		}
	}
	characters := make([]string, len(body))
	for i := 0; i < len(body); i++ {
		characters[i] = body[i : i+1]
	}
	if _, _, details := parseSSE(characters...); !reflect.DeepEqual(details, expected) {
		t.Fatal("byte at a time")
	}
}

func TestSSEControlFields(t *testing.T) {
	_, parser, details := parseSSE(": heartbeat\n\nid: control\nretry: 123\nevent: ignored\n\n" +
		"DATA: ignored\nx-extra: field\n\ndata:\n\n" +
		"data: second\nid: control\n\n" +
		"id: bad\x00id\nevent: custom\nevent: final\ndata: third\n\n" +
		"id\n\ndata: reset\n\n")
	type row struct {
		sequence       int
		kind, id, data string
	}
	var got []row
	for _, item := range details {
		got = append(got, row{item.Sequence, item.EventType, item.LastEventID, item.Data})
	}
	want := []row{{1, "message", "control", ""}, {2, "message", "control", "second"}, {3, "final", "control", "third"}, {4, "message", "", "reset"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	if parser.HasPendingEvent() {
		t.Fatal("pending")
	}
}

func TestSSECRDispatch(t *testing.T) {
	parser := &SSEParser{}
	first := parser.Append("data: one\r\r")[0]
	if first.RawEnd != len("data: one\r\r") || parser.HasPendingEvent() {
		t.Fatalf("%+v", first)
	}
	second := parser.Append("\ndata: two\r\n\r\n")[0]
	body := "data: one\r\r\ndata: two\r\n\r\n"
	if got := SSEEventDetail(body, *first).Raw; got != "data: one\r\r\n" {
		t.Fatalf("%q", got)
	}
	if got := SSEEventDetail(body, *second).Raw; got != "data: two\r\n\r\n" {
		t.Fatalf("%q", got)
	}
}

func TestSSEUnfinishedBlocks(t *testing.T) {
	for _, tail := range []string{"data: tail", "data: tail\n", "event: x\nid: y\ndata: tail\r\n"} {
		parser := &SSEParser{}
		if len(parser.Append(tail)) != 0 || !parser.HasPendingEvent() {
			t.Fatalf("%q", tail)
		}
		end := "\n\n"
		if strings.HasSuffix(tail, "\n") {
			end = "\n"
		}
		if len(parser.Append(end)) != 1 || parser.HasPendingEvent() {
			t.Fatalf("%q completion", tail)
		}
	}
	if _, parser, _ := parseSSE("\xef\xbb\xbf"); parser.HasPendingEvent() {
		t.Fatal("BOM alone")
	}
}

func TestSSEPreviews(t *testing.T) {
	data := strings.Repeat("😀", 1000) + "\n" + strings.Repeat("中", 1024*1024)
	body := "data: " + strings.ReplaceAll(data, "\n", "\ndata: ") + "\n\n"
	events, _, _ := parseSSE(body)
	if utf8.RuneCountInString(events[0].Preview) != 160 || events[0].Preview != strings.Repeat("😀", 159)+"…" {
		t.Fatal(events[0].Preview)
	}
	if SSEEventDetail(body, *events[0]).Data != data {
		t.Fatal("data")
	}
	if got, _, _ := parseSSE("data: a\ndata: b\n\n"); got[0].Preview != "a ↵ b" {
		t.Fatal(got[0].Preview)
	}
	if got, _, _ := parseSSE("data: " + strings.Repeat("x", 160) + "\n\n"); len(got[0].Preview) != 160 {
		t.Fatal(len(got[0].Preview))
	}
}

func TestIsEventStream(t *testing.T) {
	if !IsEventStream("TEXT/EVENT-STREAM; charset=utf-8") || IsEventStream("text/plain") {
		t.Fatal("type")
	}
}
