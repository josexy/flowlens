package proxyservice

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	bodycache "github.com/josexy/flowlens/internal/pkg/body_cache"
	"github.com/josexy/flowlens/internal/pkg/fs"
	"github.com/josexy/mitmproxy-go/v2"
	"github.com/josexy/mitmproxy-go/v2/buf"
	http "github.com/josexy/xhttp"
)

type testCaptureFrame struct {
	direction mitmproxy.WSDirection
	msgType   int
	data      *buf.Buffer
}

func newTestCaptureFrame(direction mitmproxy.WSDirection, msgType int, payload []byte) *testCaptureFrame {
	data := buf.NewSize(len(payload))
	_, _ = data.Write(payload)
	return &testCaptureFrame{direction: direction, msgType: msgType, data: data}
}

func (f *testCaptureFrame) Direction() mitmproxy.WSDirection { return f.direction }
func (f *testCaptureFrame) MessageType() int                 { return f.msgType }
func (f *testCaptureFrame) DataBuffer() *buf.Buffer          { return f.data }

// Invoke and Release recycle the buffer as the real frame does, so capture
// that kept a reference to it would see the bytes change.
func (f *testCaptureFrame) Invoke() error { f.Release(); return nil }
func (f *testCaptureFrame) Release() {
	for i := range f.data.Bytes() {
		f.data.Bytes()[i] = 0xee
	}
}

type testCaptureFrameWatcher chan mitmproxy.WsFrame

func (w testCaptureFrameWatcher) Receive() <-chan mitmproxy.WsFrame { return w }

// captureWebSocketSession runs the capture interceptor over frames and returns
// the captured entry's ID.
func captureWebSocketSession(t *testing.T, svc *ProxyService, frames ...mitmproxy.WsFrame) uint64 {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "ws://spool.test/socket", nil)
	if err != nil {
		t.Fatal(err)
	}
	watcher := make(testCaptureFrameWatcher, len(frames))
	for _, frame := range frames {
		watcher <- frame
	}
	close(watcher)
	before := svc.trafficEntries.Len()
	svc.websocketInterceptor(nil)(newRawTCPMetadataContext(), req,
		&http.Response{StatusCode: http.StatusSwitchingProtocols, Proto: "HTTP/1.1"}, watcher)
	if svc.trafficEntries.Len() != before+1 {
		t.Fatal("WebSocket entry was not captured")
	}
	entries := svc.trafficEntries.Values()
	return entries[len(entries)-1].ID
}

// readFlushedHistoryBodies decodes every entry body of the current history
// file pair.
func readFlushedHistoryBodies(t *testing.T, svc *ProxyService) map[uint64]*TrafficBodyView {
	t.Helper()
	dir, err := getHistoryStoragePath()
	if err != nil {
		t.Fatal(err)
	}
	key := svc.CurrentHistoryKey()
	index, err := os.ReadFile(filepath.Join(dir, fs.GetHIdxFileName(key)))
	if err != nil {
		t.Fatalf("read history index: %v", err)
	}
	data, err := os.Open(filepath.Join(dir, fs.GetHBinFileName(key)))
	if err != nil {
		t.Fatalf("open history data: %v", err)
	}
	defer data.Close()
	count := binary.BigEndian.Uint32(index)
	bodies := make(map[uint64]*TrafficBodyView, count)
	for i := range count {
		record := index[4+i*16:]
		id := binary.BigEndian.Uint64(record)
		if _, err := data.Seek(int64(binary.BigEndian.Uint32(record[12:])), io.SeekStart); err != nil {
			t.Fatal(err)
		}
		body, err := DecodeTrafficBodyWithVersion(data, hbinVersionCurrent)
		if err != nil {
			t.Fatalf("decode body of %d: %v", id, err)
		}
		bodies[id] = body
	}
	return bodies
}

func TestLargeWebSocketPayloadsAreHeldOnDisk(t *testing.T) {
	setTestConfigDir(t)
	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache

	const large = 8 << 20
	var payloads [][]byte
	var frames []mitmproxy.WsFrame
	for i := range 3 {
		payload := make([]byte, large)
		for j := range payload {
			payload[j] = byte(i*7 + j*13)
		}
		payloads = append(payloads, payload)
		frames = append(frames, newTestCaptureFrame(mitmproxy.Receive, 2, payload))
	}
	small := []byte(`{"small":true}`)
	payloads = append(payloads, small)
	frames = append(frames, newTestCaptureFrame(mitmproxy.Send, 1, small))
	id := captureWebSocketSession(t, svc, frames...)

	value, ok := svc.trafficWsMsgs.Load(id)
	if !ok {
		t.Fatal("WebSocket messages were not captured")
	}
	wsMsgs := value.(*TrafficWsMsgs)
	wsMsgs.lock.RLock()
	inMemory := 0
	for i, message := range wsMsgs.Messages {
		inMemory += len(message.Data)
		if i < 3 && (message.spooled == nil || message.Data != "" || message.DataSize != large) {
			t.Errorf("message %d is not held on disk: spooled %v data %d size %d", i, message.spooled != nil, len(message.Data), message.DataSize)
		}
	}
	count := len(wsMsgs.Messages)
	wsMsgs.lock.RUnlock()
	if count != 4 || inMemory != len(small) {
		t.Fatalf("messages %d, payload bytes in memory %d, want 4 and %d", count, inMemory, len(small))
	}

	requirePayloads := func(name string, messages []*WebSocketMessage) {
		t.Helper()
		if len(messages) != len(payloads) {
			t.Fatalf("%s: %d messages, want %d", name, len(messages), len(payloads))
		}
		for i, message := range messages {
			if message.Data != string(payloads[i]) || message.DataSize != len(payloads[i]) {
				t.Fatalf("%s: message %d differs from the sent payload", name, i)
			}
		}
		if messages[0].MsgType != "binary" || messages[0].Direction != "receive" ||
			messages[3].MsgType != "text" || messages[3].Direction != "send" {
			t.Fatalf("%s: message metadata changed: %+v %+v", name, messages[0], messages[3])
		}
	}
	view, err := svc.GetTrafficBodyView(id)
	if err != nil {
		t.Fatalf("GetTrafficBodyView: %v", err)
	}
	requirePayloads("body view", view.WebSocketMessages)

	if err := svc.flushHistoryToDisk(true); err != nil {
		t.Fatalf("flushHistoryToDisk: %v", err)
	}
	requirePayloads("HBIN round trip", readFlushedHistoryBodies(t, svc)[id].WebSocketMessages)

	// HAR has no representation for WebSocket frames: the export must still
	// succeed with the handshake.
	harPath := filepath.Join(t.TempDir(), "spool.har")
	result, err := svc.ExportHAR(HARExportRequest{Path: harPath, TrafficIDs: []uint64{id}})
	if err != nil || result.Exported != 1 {
		t.Fatalf("ExportHAR = %+v, %v", result, err)
	}
	harBytes, err := os.ReadFile(harPath)
	if err != nil {
		t.Fatal(err)
	}
	var har struct {
		Log struct {
			Entries []struct {
				Request struct{ URL string } `json:"request"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(harBytes, &har); err != nil || len(har.Log.Entries) != 1 || har.Log.Entries[0].Request.URL != "ws://spool.test/socket" {
		t.Fatalf("HAR = %s, %v", bytes.TrimSpace(harBytes[:min(len(harBytes), 200)]), err)
	}

	svc.deleteTrafficEntry(id)
	if _, err := os.Stat(filepath.Join(cache.SessionDir(), fmt.Sprintf("%d_ws.body", id))); !os.IsNotExist(err) {
		t.Fatalf("WebSocket payload file should be removed with its entry, got %v", err)
	}
}

func TestLiveUpdateCarriesSpooledWebSocketPayload(t *testing.T) {
	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache
	payload := bytes.Repeat([]byte("live-"), 64<<10)
	var updates []TrafficLiveUpdate
	svc.emitTrafficLiveUpdateHook = func(update TrafficLiveUpdate) { updates = append(updates, update) }
	svc.SetLiveTrafficDetail(svc.nextID + 1)

	id := captureWebSocketSession(t, svc, newTestCaptureFrame(mitmproxy.Receive, 1, payload))
	if len(updates) != 1 || updates[0].TrafficID != id || updates[0].Message == nil {
		t.Fatalf("live updates = %+v", updates)
	}
	if message := updates[0].Message; message.Data != string(payload) || message.spooled != nil {
		t.Fatalf("live message has %d bytes, spooled %v", len(message.Data), message.spooled != nil)
	}
}

func TestClearTrafficRemovesWebSocketPayloadFiles(t *testing.T) {
	svc := newTestProxyService(t, nil)
	cache, err := bodycache.NewWithDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewWithDir: %v", err)
	}
	svc.bodyCache = cache
	setTestConfigDir(t)
	id := captureWebSocketSession(t, svc, newTestCaptureFrame(mitmproxy.Receive, 2, bytes.Repeat([]byte{7}, 64<<10)))
	path := filepath.Join(cache.SessionDir(), fmt.Sprintf("%d_ws.body", id))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("payload file: %v", err)
	}
	if err := svc.ClearTraffic(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("payload file should be removed when the capture is cleared, got %v", err)
	}
}
