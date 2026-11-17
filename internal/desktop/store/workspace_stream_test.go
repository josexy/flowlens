package store

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/josexy/flowlens/internal/desktop/logic"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

func TestStreamBodySnapshotsSurviveAppendAndNextResponse(t *testing.T) {
	for _, test := range []struct {
		name   string
		binary bool
		value  string
	}{
		{name: "binary", binary: true, value: "\x00\xff\x80A"},
		{name: "text", value: "🙂"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newWorkspaceHarness(t)
			key := h.store.CreateHTTPRequestTab(logic.DraftNew, nil)
			state := h.store.FindTab(key).HTTP
			state.Response = &logic.HTTPResponseView{Kind: logic.ResponseKindSuccess, Body: test.value, BodyBinary: test.binary}
			if !h.store.RegisterHTTPStream(key, "first") {
				t.Fatal("stream not registered")
			}
			want := test.value
			snapshots := []string{state.Response.Body}
			wants := []string{strings.Clone(want)}
			// Alternate small appends and growth. Consumers may keep any of the
			// published strings for saving, copying, or background rendering.
			for _, count := range []int{1, 32, 4096, 2, 65536, 3} {
				data := strings.Repeat(test.value, count)
				h.emit(eventHTTPRequest, chunk("first", int64(len(want)), data))
				h.clock.tick(streamFlushInterval)
				want += data
				if state.Response.Body != want {
					t.Fatalf("body after append of %d bytes differs", len(data))
				}
				snapshots = append(snapshots, state.Response.Body)
				wants = append(wants, strings.Clone(want))
			}
			h.emit(eventHTTPRequest, terminal("first", "complete", int64(len(want))))
			h.clock.tick(streamFlushInterval)
			if state.IsStreaming || state.Response.Body != want {
				t.Fatal("completion lost the response body or left the stream active")
			}

			// Reusing the tab must create independent storage for the next body.
			state.Response = &logic.HTTPResponseView{Kind: logic.ResponseKindSuccess, Body: "next:", BodyBinary: test.binary}
			if !h.store.RegisterHTTPStream(key, "second") {
				t.Fatal("second stream not registered")
			}
			h.emit(eventHTTPRequest, chunk("second", 5, test.value), terminal("second", "complete", int64(5+len(test.value))))
			h.clock.tick(streamFlushInterval)
			if state.Response.Body != "next:"+test.value {
				t.Fatalf("second response = %q", state.Response.Body)
			}
			for index, snapshot := range snapshots {
				if snapshot != wants[index] {
					t.Fatalf("published snapshot %d changed", index)
				}
			}
		})
	}
}

func TestStreamTextBodyUsesWireOffsetsAfterDecoding(t *testing.T) {
	h := newWorkspaceHarness(t)
	state := h.streamTab("text")
	for _, step := range []struct {
		offset int64
		data   string
		want   string
	}{
		{offset: 0, data: "\xef\xbb", want: ""},
		{offset: 2, data: "\xbfA\xe4", want: "A"},
		{offset: 5, data: "\xb8\xad\xff", want: "A中�"},
		{offset: 4, data: "中\xffB", want: "A中�B"}, // Overlap contributes only B.
		{offset: 4, data: "中\xffB", want: "A中�B"}, // Duplicate contributes nothing.
		{offset: 9, data: "文", want: "A中�B文"},
	} {
		h.emit(eventHTTPRequest, chunk("text", step.offset, step.data))
		h.clock.tick(streamFlushInterval)
		if state.Response.Body != step.want {
			t.Fatalf("chunk at %d: body = %q, want %q", step.offset, state.Response.Body, step.want)
		}
	}
}

func TestHTTPStreamBodyAllocationBudget(t *testing.T) {
	for _, binary := range []bool{true, false} {
		t.Run(fmt.Sprintf("binary=%t", binary), func(t *testing.T) {
			const size = 8 << 20
			result := testing.Benchmark(func(b *testing.B) {
				benchmarkHTTPStreamAppend(b, binary, size)
			})
			// Allow buffer growth, text decoding, and event bookkeeping. Rebuilding
			// the entire string on every 64 KiB flush needs over 60 times the body.
			const limit = 12 * size
			if allocated := result.AllocedBytesPerOp(); allocated > limit {
				t.Fatalf("8 MiB stream allocated %d bytes, budget is %d: %s", allocated, limit, result.MemString())
			}
		})
	}
}

func BenchmarkHTTPStreamAppend(b *testing.B) {
	for _, binary := range []bool{true, false} {
		for _, mib := range []int{4, 8, 16, 32} {
			b.Run(fmt.Sprintf("binary=%t/%dMiB", binary, mib), func(b *testing.B) {
				benchmarkHTTPStreamAppend(b, binary, mib<<20)
			})
		}
	}
}

func benchmarkHTTPStreamAppend(b *testing.B, binary bool, size int) {
	const chunkSize = 64 << 10
	pattern := []byte("🙂")
	if binary {
		pattern = []byte{0, 0xff, 0x80, 'A'}
	}
	payload := bytes.Repeat(pattern, chunkSize/len(pattern))
	b.ReportAllocs()
	b.SetBytes(int64(size))
	var body string
	for b.Loop() {
		response := &logic.HTTPResponseView{BodyBinary: binary}
		stream := &httpStreamRuntime{sessionID: "bench", state: &logic.HTTPEditorState{Response: response}, decoder: &utf8StreamDecoder{}}
		workspace := &WorkspaceStore{streams: map[string]*httpStreamRuntime{"bench": stream}}
		// Flush every chunk separately: batching the entire response into one
		// flush would hide repeated copying of the already-published prefix.
		for offset := int64(0); offset < int64(size); offset += chunkSize {
			stream.pending = []proxyservice.HTTPRequestStreamEvent{{SessionID: "bench", EventType: "chunk", Offset: &offset, Chunk: payload}}
			workspace.flushStream(stream)
		}
		body = response.Body
	}
	if len(body) != size {
		b.Fatalf("body size = %d, want %d", len(body), size)
	}
}
