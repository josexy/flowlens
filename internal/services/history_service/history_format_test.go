package historyservice

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/josexy/flowlens/internal/pkg/fs"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
	"github.com/klauspost/compress/zstd"
)

func TestHistoryServiceLoadsV1AndSkipsUnsupportedVersions(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	writeHistoryWithVersion(t, historyDir, "v4-future", 4)
	writeHistoryWithVersion(t, historyDir, "v1-current", 1)
	writeHistoryWithVersion(t, historyDir, "v0-invalid", 0)
	writeRawTCPHistory(t, historyDir, "v1-raw-tcp")

	service := New(nil, nil)
	if err := service.initializeHistoryIndexMap(); err != nil {
		t.Fatalf("initializeHistoryIndexMap: %v", err)
	}

	service.mu.RLock()
	futureIndex := service.indexMap["v4-future"]
	currentIndex := service.indexMap["v1-current"]
	invalidIndex := service.indexMap["v0-invalid"]
	rawTCPIndex := service.indexMap["v1-raw-tcp"]
	service.mu.RUnlock()
	if futureIndex != nil || invalidIndex != nil || currentIndex == nil || rawTCPIndex == nil {
		t.Fatalf(
			"history indexes = future %+v, invalid %+v, current %+v, raw TCP %+v",
			futureIndex,
			invalidIndex,
			currentIndex,
			rawTCPIndex,
		)
	}
	if currentIndex.formatVersion != 1 || rawTCPIndex.formatVersion != 1 {
		t.Fatalf("format versions = current %d, raw TCP %d", currentIndex.formatVersion, rawTCPIndex.formatVersion)
	}
	currentMetadata, err := loadHistoryMetadata(historyDir, "v1-current")
	if err != nil {
		t.Fatalf("loadHistoryMetadata current: %v", err)
	}
	if currentMetadata.FormatVersion != 1 {
		t.Fatalf("metadata format version = %d, want 1", currentMetadata.FormatVersion)
	}
	for _, key := range []string{"v4-future", "v0-invalid"} {
		if !fs.PathExists(filepath.Join(historyDir, fs.GetHBinFileName(key))) ||
			!fs.PathExists(filepath.Join(historyDir, fs.GetHIdxFileName(key))) {
			t.Fatalf("unsupported %s history files should remain on disk", key)
		}
	}
	currentEntries, err := service.GetHistory("v1-current")
	if err != nil {
		t.Fatalf("GetHistory current: %v", err)
	}
	if len(currentEntries) != 1 || currentEntries[0].Metadata == nil || currentEntries[0].Metadata.Process == nil ||
		currentEntries[0].Metadata.Process.IconKey != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("current entries = %+v", currentEntries)
	}
	if currentEntries[0].Request == nil || currentEntries[0].Request.Metrics == nil {
		t.Fatalf("current request metrics = %+v", currentEntries[0].Request)
	}
	if got := currentEntries[0].Request.Metrics; got.StartedAtMicros != 1_786_540_561_341_480 ||
		got.EndedAtMicros != 1_786_540_561_342_064 || got.HeaderSize != 48 || got.BodySize != 0 ||
		got.State != proxyservice.HTTPMessageStateCompleted {
		t.Fatalf("current request metrics = %+v", got)
	}
	rawTCPEntries, err := service.GetHistory("v1-raw-tcp")
	if err != nil {
		t.Fatalf("GetHistory raw TCP: %v", err)
	}
	if len(rawTCPEntries) != 1 || rawTCPEntries[0].Type != "tcp" || rawTCPEntries[0].RawTCP == nil {
		t.Fatalf("raw TCP entries = %+v", rawTCPEntries)
	}
	if got := rawTCPEntries[0].RawTCP; got.Source != proxyservice.RawTCPTunnelSource("socks5") ||
		got.HostPort != "db.internal.example:5432" || got.TLS {
		t.Fatalf("raw TCP info = %+v", got)
	}
	rawTCPBody, err := service.GetHistoryTrafficBodyView("v1-raw-tcp", 9001)
	if err != nil {
		t.Fatalf("GetHistoryTrafficBodyView raw TCP: %v", err)
	}
	if rawTCPBody.RequestBody != "" || rawTCPBody.ResponseBody != "" || len(rawTCPBody.WebSocketMessages) != 0 {
		t.Fatalf("raw TCP body = %+v, want empty", rawTCPBody)
	}
}

func writeHistoryWithVersion(t *testing.T, directory, key string, version uint16) {
	t.Helper()
	writeHistoryFixture(t, directory, key, version, "https", nil)
}

// binaryWebSocketBodyRecord is an uncompressed entry body with empty HTTP
// bodies and one received binary WebSocket message whose payload bytes are
// stored exactly as given.
func binaryWebSocketBodyRecord(stored []byte, dataSize int32) []byte {
	var body bytes.Buffer
	for range 2 {
		_ = binary.Write(&body, binary.BigEndian, uint8(0))  // text display
		_ = binary.Write(&body, binary.BigEndian, uint32(0)) // empty body
	}
	_ = binary.Write(&body, binary.BigEndian, uint32(1))
	_ = binary.Write(&body, binary.BigEndian, uint8(1)) // receive
	_ = binary.Write(&body, binary.BigEndian, uint8(1)) // binary
	_ = binary.Write(&body, binary.BigEndian, uint32(len(stored)))
	body.Write(stored)
	_ = binary.Write(&body, binary.BigEndian, dataSize)
	_ = binary.Write(&body, binary.BigEndian, uint8(0)) // no error
	_ = binary.Write(&body, binary.BigEndian, uint8(0)) // not truncated
	return body.Bytes()
}

func TestHistoryBinaryWebSocketMessageMatchesAcrossV2AndV3(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	payload := make([]byte, 256)
	for index := range payload {
		payload[index] = byte(index)
	}
	// v2 stored binary payloads as standard Base64 text; v3 stores them raw.
	legacy := []byte(base64.StdEncoding.EncodeToString(payload))
	writeHistoryFixtureWithBody(t, historyDir, "ws-v2", 2, "https", nil, binaryWebSocketBodyRecord(legacy, int32(len(payload))))
	writeHistoryFixtureWithBody(t, historyDir, "ws-v3", 3, "https", nil, binaryWebSocketBodyRecord(payload, int32(len(payload))))

	service := New(nil, nil)
	if err := service.initializeHistoryIndexMap(); err != nil {
		t.Fatalf("initializeHistoryIndexMap: %v", err)
	}
	views := map[string]*proxyservice.TrafficBodyView{}
	for _, key := range []string{"ws-v2", "ws-v3"} {
		view, err := service.GetHistoryTrafficBodyView(key, 9001)
		if err != nil {
			t.Fatalf("GetHistoryTrafficBodyView %s: %v", key, err)
		}
		if len(view.WebSocketMessages) != 1 || view.WebSocketMessages[0].MsgType != "binary" {
			t.Fatalf("%s messages = %+v", key, view.WebSocketMessages)
		}
		views[key] = view
	}
	v2, v3 := views["ws-v2"].WebSocketMessages[0], views["ws-v3"].WebSocketMessages[0]
	if v2.Data != v3.Data || v2.DataSize != v3.DataSize {
		t.Fatalf("v2 message = %d bytes (size %d), v3 message = %d bytes (size %d)", len(v2.Data), v2.DataSize, len(v3.Data), v3.DataSize)
	}
	if v3.Data != string(payload) {
		t.Fatalf("v3 message = %x, want %x", v3.Data, payload)
	}
}

func TestHistoryServiceLoadsV2ConnectionTimingsAndBody(t *testing.T) {
	historyDir := setupHistoryTestStorage(t)
	writeHistoryWithVersion(t, historyDir, "v2-timings", 2)
	service := New(nil, nil)
	if err := service.initializeHistoryIndexMap(); err != nil {
		t.Fatal(err)
	}
	entries, err := service.GetHistory("v2-timings")
	if err != nil || len(entries) != 1 {
		t.Fatalf("history entries = %+v, error = %v", entries, err)
	}
	timings := entries[0].Metadata.ConnectionTimings
	if timings == nil || timings.DNSStartedAtMicros != 1000001 || timings.DNSEndedAtMicros != 1000123 || timings.TLSEndedAtMicros != -1 {
		t.Fatalf("connection timings = %+v", timings)
	}
	body, err := service.GetHistoryTrafficBodyView("v2-timings", 9001)
	if err != nil || body.RequestBody != "" || body.ResponseBody != "" {
		t.Fatalf("body = %+v, error = %v", body, err)
	}
	var har bytes.Buffer
	_, err = proxyservice.WriteHAR(&har, "test", []proxyservice.HARExportEntry{{Entry: entries[0]}})
	if err != nil || !bytes.Contains(har.Bytes(), []byte(`"dnsStartedAtMicros":1000001`)) {
		t.Fatalf("history HAR lost connection timings: %v", err)
	}
}

func writeRawTCPHistory(t *testing.T, directory, key string) {
	t.Helper()
	writeHistoryFixture(t, directory, key, 1, "tcp", &proxyservice.RawTCPTunnelInfo{
		Source:   proxyservice.RawTCPTunnelSource("socks5"),
		HostPort: "db.internal.example:5432",
		TLS:      false,
	})
}

func writeHistoryFixture(
	t *testing.T,
	directory string,
	key string,
	version uint16,
	trafficType string,
	rawTCP *proxyservice.RawTCPTunnelInfo,
) {
	t.Helper()
	writeHistoryFixtureWithBody(t, directory, key, version, trafficType, rawTCP, nil)
}

// writeHistoryFixtureWithBody writes a one-entry history whose entry body is
// bodyPayload before compression. A nil payload writes empty bodies and no
// WebSocket messages.
func writeHistoryFixtureWithBody(
	t *testing.T,
	directory string,
	key string,
	version uint16,
	trafficType string,
	rawTCP *proxyservice.RawTCPTunnelInfo,
	bodyPayload []byte,
) {
	t.Helper()
	var hbin bytes.Buffer
	hbin.WriteString("PGHI")
	writeHistoryValue(t, &hbin, version)
	writeHistoryString(t, &hbin, key)
	writeHistoryString(t, &hbin, "Current capture")
	writeHistoryValue(t, &hbin, int64(1_785_000_000_000))
	writeHistoryValue(t, &hbin, uint32(1))
	headerOffset := uint32(hbin.Len())

	writeHistoryValue(t, &hbin, uint64(9001))
	writeHistoryString(t, &hbin, trafficType)
	writeHistoryValue(t, &hbin, time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC).UnixNano())
	method, requestURL, host, path := "GET", "https://current.example/process", "current.example", "/process"
	statusCode, status := int32(200), "200 OK"
	if trafficType == "tcp" {
		method, requestURL, host, path = "", "tcp://db.internal.example:5432", "db.internal.example:5432", ""
		statusCode, status = 0, ""
	}
	for _, value := range []string{method, requestURL, host, path} {
		writeHistoryString(t, &hbin, value)
	}
	writeHistoryValue(t, &hbin, statusCode)
	writeHistoryString(t, &hbin, status)
	writeHistoryValue(t, &hbin, uint8(1))
	for _, value := range []string{"127.0.0.1:43121", "127.0.0.1:8080", "203.0.113.20:443", "192.0.2.30:53001"} {
		writeHistoryString(t, &hbin, value)
	}
	for index := range 4 {
		writeHistoryValue(t, &hbin, time.Date(2026, 7, 23, 12, 0, 0, index, time.UTC).UnixNano())
	}
	writeHistoryValue(t, &hbin, uint8(0)) // TLS
	writeHistoryValue(t, &hbin, uint8(0)) // certificate
	writeHistoryValue(t, &hbin, uint8(1)) // process
	writeHistoryString(t, &hbin, "resolved")
	writeHistoryValue(t, &hbin, uint32(4242))
	for _, value := range []string{
		"FlowLens Fixture", "fixture-process", "/opt/fixture/bin/process", "org.flowlens.Fixture",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"linux_inet_diag", "exact", "",
	} {
		writeHistoryString(t, &hbin, value)
	}
	if trafficType == "tcp" {
		writeHistoryValue(t, &hbin, uint8(0)) // request
	} else {
		writeHistoryValue(t, &hbin, uint8(1))
		writeHistoryString(t, &hbin, "HTTP/2.0")
		writeHistoryHeaderFields(t, &hbin, []proxyservice.HTTPHeaderField{
			{Name: ":method", Value: "GET"},
			{Name: ":authority", Value: "current.example"},
		})
		writeHistoryHeaderFields(t, &hbin, nil)
		writeHistoryValue(t, &hbin, uint8(1))
		for _, value := range []int64{1_786_540_561_341_480, 1_786_540_561_342_064, 48, 0} {
			writeHistoryValue(t, &hbin, value)
		}
		writeHistoryString(t, &hbin, string(proxyservice.HTTPMessageStateCompleted))
	}
	writeHistoryValue(t, &hbin, uint8(0)) // response
	writeHistoryValue(t, &hbin, uint8(0)) // traffic error
	if trafficType == "tcp" {
		if rawTCP == nil {
			writeHistoryValue(t, &hbin, uint8(0))
		} else {
			writeHistoryValue(t, &hbin, uint8(1))
			writeHistoryString(t, &hbin, string(rawTCP.Source))
			writeHistoryString(t, &hbin, rawTCP.HostPort)
			var tls uint8
			if rawTCP.TLS {
				tls = 1
			}
			writeHistoryValue(t, &hbin, tls)
		}
	}
	if version >= 2 {
		writeHistoryValue(t, &hbin, uint8(1))
		writeHistoryValue(t, &hbin, proxyservice.HTTPConnectionTimings{DNSStartedAtMicros: 1000001, DNSEndedAtMicros: 1000123, ConnectStartedAtMicros: 1000123, ConnectEndedAtMicros: 1000456, TLSStartedAtMicros: -1, TLSEndedAtMicros: -1})
	}
	bodyOffset := uint32(hbin.Len())
	if bodyPayload == nil {
		bodyPayload = make([]byte, 15)
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	compressed := encoder.EncodeAll(bodyPayload, nil)
	encoder.Close()
	writeHistoryValue(t, &hbin, uint32(len(compressed)))
	hbin.Write(compressed)

	var hidx bytes.Buffer
	for _, value := range []any{uint32(1), uint64(9001), headerOffset, bodyOffset} {
		writeHistoryValue(t, &hidx, value)
	}
	if err := os.WriteFile(filepath.Join(directory, fs.GetHBinFileName(key)), hbin.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile hbin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, fs.GetHIdxFileName(key)), hidx.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile hidx: %v", err)
	}
}

func writeHistoryValue(t *testing.T, writer io.Writer, value any) {
	t.Helper()
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		t.Fatalf("binary.Write: %v", err)
	}
}

func writeHistoryString(t *testing.T, writer io.Writer, value string) {
	t.Helper()
	writeHistoryValue(t, writer, uint32(len(value)))
	if _, err := io.WriteString(writer, value); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
}

func writeHistoryHeaderFields(t *testing.T, writer io.Writer, fields []proxyservice.HTTPHeaderField) {
	t.Helper()
	if fields == nil {
		writeHistoryValue(t, writer, uint8(0))
	} else {
		writeHistoryValue(t, writer, uint8(1))
		writeHistoryValue(t, writer, uint32(len(fields)))
		for _, field := range fields {
			writeHistoryString(t, writer, field.Name)
			writeHistoryString(t, writer, field.Value)
		}
	}
	writeHistoryValue(t, writer, uint8(0)) // truncated
	writeHistoryValue(t, writer, uint8(0)) // order unavailable
}
