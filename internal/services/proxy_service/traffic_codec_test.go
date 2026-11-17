package proxyservice

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

var (
	zstdEncoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
)

func TestHistoryCodecRoundTripPreservesWsMsgsTruncated(t *testing.T) {
	t.Parallel()

	reqData := "req"
	rspData := "rsp_test"
	rawBinary := "\x00\x41\xff"
	original := &trafficBodyViewInner{
		RequestBodyReader:  io.NopCloser(strings.NewReader(reqData)),
		ResponseBodyReader: io.NopCloser(strings.NewReader(rspData)),
		RequestBodySize:    int64(len(reqData)),
		ResponseBodySize:   int64(len(rspData)),
		RequestBodyBinary:  true,
		ResponseBodyBinary: false,
		WebSocketMessages: []*WebSocketMessage{
			{Direction: "send", MsgType: "text", Data: "hello", DataSize: 5},
			{Direction: "receive", MsgType: "binary", Data: rawBinary, DataSize: 3},
		},
		WsMsgsTruncated: true,
	}

	var inner bytes.Buffer
	if err := hbinWriteEntryBody(&inner, original); err != nil {
		t.Fatalf("hbinWriteEntryBody: %v", err)
	}
	t.Logf("dump: \n%s\n", hex.Dump(inner.Bytes()))
	compressed := zstdEncoder.EncodeAll(inner.Bytes(), nil)
	var outer bytes.Buffer
	if err := binary.Write(&outer, binary.BigEndian, uint32(len(compressed))); err != nil {
		t.Fatalf("write size: %v", err)
	}
	if _, err := outer.Write(compressed); err != nil {
		t.Fatalf("write compressed body: %v", err)
	}

	decoded, err := hbinReadEntryBody(bytes.NewReader(outer.Bytes()), hbinVersionCurrent)
	if err != nil {
		t.Fatalf("hbinReadEntryBody: %v", err)
	}
	if !decoded.RequestBodyBinary {
		t.Fatal("RequestBodyBinary should survive round-trip")
	}
	if decoded.ResponseBodyBinary {
		t.Fatal("ResponseBodyBinary should survive round-trip")
	}
	if decoded.RequestBodySize != 3 {
		t.Fatalf("RequestBodySize should survive round-trip, got %d", decoded.RequestBodySize)
	}
	if decoded.ResponseBodySize != 8 {
		t.Fatalf("ResponseBodySize should survive round-trip, got %d", decoded.ResponseBodySize)
	}
	if !decoded.WsMsgsTruncated {
		t.Fatal("WsMsgsTruncated should survive round-trip")
	}
	if len(decoded.WebSocketMessages) != 2 || decoded.WebSocketMessages[0].Data != "hello" {
		t.Fatalf("decoded websocket messages = %+v", decoded.WebSocketMessages)
	}
	binaryMsg := decoded.WebSocketMessages[1]
	if binaryMsg.MsgType != "binary" {
		t.Fatalf("binary websocket msgType = %q, want binary", binaryMsg.MsgType)
	}
	if binaryMsg.Data != rawBinary {
		t.Fatalf("binary websocket data = %q, want %q", binaryMsg.Data, rawBinary)
	}
	if binaryMsg.DataSize != 3 {
		t.Fatalf("binary websocket data size = %d, want 3", binaryMsg.DataSize)
	}
}

// hbinEntryBodyRecord compresses an entry body the way hbinWriteEntry stores
// it, so the read paths can be exercised without a full history file.
func hbinEntryBodyRecord(t *testing.T, body *trafficBodyViewInner) []byte {
	t.Helper()
	var inner bytes.Buffer
	if err := hbinWriteEntryBody(&inner, body); err != nil {
		t.Fatalf("hbinWriteEntryBody: %v", err)
	}
	compressed := zstdEncoder.EncodeAll(inner.Bytes(), nil)
	var outer bytes.Buffer
	if err := binary.Write(&outer, binary.BigEndian, uint32(len(compressed))); err != nil {
		t.Fatalf("write size: %v", err)
	}
	outer.Write(compressed)
	return outer.Bytes()
}

func allByteValues() string {
	value := make([]byte, 256)
	for index := range value {
		value[index] = byte(index)
	}
	return string(value)
}

func TestHistoryCodecV3RoundTripsRawBinaryWebSocketPayload(t *testing.T) {
	t.Parallel()

	payload := allByteValues()
	record := hbinEntryBodyRecord(t, &trafficBodyViewInner{
		WebSocketMessages: []*WebSocketMessage{
			{Direction: "receive", MsgType: "binary", Data: payload, DataSize: len(payload)},
		},
	})

	view, err := DecodeTrafficBodyWithVersion(bytes.NewReader(record), 3)
	if err != nil {
		t.Fatalf("DecodeTrafficBodyWithVersion: %v", err)
	}
	if len(view.WebSocketMessages) != 1 {
		t.Fatalf("decoded websocket messages = %+v", view.WebSocketMessages)
	}
	msg := view.WebSocketMessages[0]
	if msg.MsgType != "binary" || msg.Data != payload || msg.DataSize != 256 {
		t.Fatalf("binary websocket message = type %q size %d data %x", msg.MsgType, msg.DataSize, msg.Data)
	}
}

func TestHistoryCodecLegacyBinaryWebSocketPayload(t *testing.T) {
	t.Parallel()

	payload := allByteValues()
	for _, test := range []struct {
		name   string
		stored string
		want   string
	}{
		// v1 and v2 wrote binary payloads as standard Base64 text.
		{name: "base64 is decoded", stored: base64.StdEncoding.EncodeToString([]byte(payload)), want: payload},
		{name: "damaged base64 keeps stored bytes", stored: "%%not base64%%", want: "%%not base64%%"},
	} {
		for _, version := range []uint16{1, 2} {
			t.Run(fmt.Sprintf("%s v%d", test.name, version), func(t *testing.T) {
				t.Parallel()
				record := hbinEntryBodyRecord(t, &trafficBodyViewInner{
					WebSocketMessages: []*WebSocketMessage{
						{Direction: "send", MsgType: "text", Data: "aGVsbG8=", DataSize: 8},
						{Direction: "receive", MsgType: "binary", Data: test.stored, DataSize: len(test.want)},
					},
				})
				view, err := DecodeTrafficBodyWithVersion(bytes.NewReader(record), version)
				if err != nil {
					t.Fatalf("DecodeTrafficBodyWithVersion: %v", err)
				}
				if len(view.WebSocketMessages) != 2 {
					t.Fatalf("decoded websocket messages = %+v", view.WebSocketMessages)
				}
				if text := view.WebSocketMessages[0].Data; text != "aGVsbG8=" {
					t.Fatalf("text message = %q, want it unchanged", text)
				}
				if got := view.WebSocketMessages[1].Data; got != test.want {
					t.Fatalf("binary message = %x, want %x", got, test.want)
				}
			})
		}
	}
}

func TestHistoryFormatVersionSupport(t *testing.T) {
	t.Parallel()

	for version, want := range map[uint16]bool{0: false, 1: true, 2: true, 3: true, 4: false} {
		if got := isSupportedHistoryFormatVersion(version); got != want {
			t.Errorf("isSupportedHistoryFormatVersion(%d) = %t, want %t", version, got, want)
		}
	}
	if _, err := DecodeTrafficBodyWithVersion(bytes.NewReader(hbinEntryBodyRecord(t, &trafficBodyViewInner{})), 4); err == nil {
		t.Fatal("DecodeTrafficBodyWithVersion accepted version 4")
	}
}

func TestHbinWriteEntryRoundTripWithDecodeFunctions(t *testing.T) {
	t.Parallel()

	entries := []struct {
		entry    *TrafficEntry
		body     *trafficBodyViewInner
		wantBody *TrafficBodyView
	}{
		{
			entry: &TrafficEntry{
				ID:         101,
				Type:       "https",
				StartedAt:  time.Date(2026, 5, 7, 9, 30, 0, 123456789, time.UTC),
				Method:     "POST",
				URL:        "https://example.com/api/login",
				Host:       "example.com",
				Path:       "/api/login",
				StatusCode: 201,
				Status:     "201 Created",
				Metadata: &Metadata{
					LocalSourceAddr:               "127.0.0.1:52345",
					LocalDestinationAddr:          "127.0.0.1:8080",
					RemoteSourceAddr:              "10.0.0.5:443",
					RemoteDestinationAddr:         "10.0.0.8:53742",
					LocalConnectionEstablishedAt:  time.Date(2026, 5, 7, 9, 30, 0, 0, time.UTC),
					RemoteConnectionEstablishedAt: time.Date(2026, 5, 7, 9, 30, 0, 1000, time.UTC),
					RequestProcessedAt:            time.Date(2026, 5, 7, 9, 30, 0, 2000, time.UTC),
					SSLHandshakeCompletedAt:       time.Date(2026, 5, 7, 9, 30, 0, 3000, time.UTC),
					TLS: &TLSState{
						ServerName:            "example.com",
						SupportedALPN:         []string{"h2", "http/1.1"},
						SupportedVersion:      []string{"TLS 1.3", "TLS 1.2"},
						SupportedCipherSuites: []string{"TLS_AES_128_GCM_SHA256"},
						SelectedALPN:          "h2",
						SelectedVersion:       "TLS 1.3",
						SelectedCipherSuite:   "TLS_AES_128_GCM_SHA256",
					},
					Certificate: &ServerCertificate{
						Version:            3,
						NotBeforeMicros:    time.Date(2026, 1, 1, 0, 0, 0, 123456000, time.UTC).UnixMicro(),
						NotAfterMicros:     time.Date(2027, 1, 1, 0, 0, 0, 654321000, time.UTC).UnixMicro(),
						SerialNumber:       "01ABCD",
						SignatureAlgorithm: "SHA256-RSA",
						Sha1Fingerprint:    "sha1",
						Sha256Fingerprint:  "sha256",
						Subject: &PkixName{
							Country:            []string{"US"},
							Organization:       []string{"Example Inc"},
							OrganizationalUnit: []string{"Engineering"},
							Locality:           []string{"Seattle"},
							Province:           []string{"WA"},
							StreetAddress:      []string{"1 Example Way"},
							PostalCode:         []string{"98101"},
							SerialNumber:       "subject-1",
							CommonName:         "example.com",
						},
						Issuer: &PkixName{
							Country:      []string{"US"},
							Organization: []string{"Example CA"},
							CommonName:   "Example Root CA",
						},
						DNSNames:    []string{"example.com", "api.example.com"},
						IPAddresses: []string{"10.0.0.5"},
					},
				},
				Request: &HTTPMessage{
					Proto: "HTTP/2.0",
					HeaderFields: []HTTPHeaderField{
						{Name: ":method", Value: "POST"},
						{Name: ":authority", Value: "example.com"},
						{Name: "x-test", Value: "a"},
						{Name: "content-type", Value: "application/json"},
						{Name: "x-test", Value: "b"},
					},
					Metrics: &HTTPMessageMetrics{
						StartedAtMicros: 1_778_145_400_123_456,
						EndedAtMicros:   1_778_145_400_124_040,
						HeaderSize:      107,
						BodySize:        42,
						State:           HTTPMessageStateCompleted,
					},
				},
				Response: &HTTPMessage{
					Proto: "HTTP/2.0",
					TrailerFields: []HTTPHeaderField{
						{Name: "grpc-status", Value: "0"},
						{Name: "x-trace", Value: "trace-1"},
						{Name: "grpc-message", Value: ""},
						{Name: "x-trace", Value: "trace-2"},
					},
					HeaderFields: []HTTPHeaderField{
						{Name: ":status", Value: "201"},
						{Name: "content-length", Value: "42"},
					},
					HeadersTruncated:        true,
					HeaderOrderUnavailable:  true,
					TrailersTruncated:       true,
					TrailerOrderUnavailable: true,
					Metrics: &HTTPMessageMetrics{
						StartedAtMicros: 1_778_145_400_850_000,
						EndedAtMicros:   1_778_145_400_857_542,
						HeaderSize:      531,
						BodySize:        425,
						State:           HTTPMessageStateCompleted,
					},
				},
				Error: &TrafficError{
					Timestamp: time.Date(2026, 5, 7, 9, 30, 1, 0, time.UTC),
					Error:     "upstream timeout",
				},
			},
			body: &trafficBodyViewInner{
				RequestBodyReader:  io.NopCloser(bytes.NewReader([]byte{0x00, 0x01, 0x02, 0xff})),
				ResponseBodyReader: io.NopCloser(strings.NewReader(`{"ok":true}`)),
				RequestBodySize:    4,
				ResponseBodySize:   int64(len(`{"ok":true}`)),
				RequestBodyBinary:  true,
				ResponseBodyBinary: false,
				WebSocketMessages: []*WebSocketMessage{
					{Direction: "send", MsgType: "text", Data: "hello", DataSize: 5},
					{Direction: "receive", MsgType: "binary", Data: "\xde\xad\xbe\xef", DataSize: 4},
				},
				WsMsgsTruncated: true,
			},
			wantBody: &TrafficBodyView{
				RequestBody:        "\x00\x01\x02\xff",
				ResponseBody:       `{"ok":true}`,
				RequestBodyBinary:  true,
				ResponseBodyBinary: false,
				WebSocketMessages: []*WebSocketMessage{
					{Direction: "send", MsgType: "text", Data: "hello", DataSize: 5},
					{Direction: "receive", MsgType: "binary", Data: "\xde\xad\xbe\xef", DataSize: 4},
				},
				WsMsgsTruncated: true,
			},
		},
		{
			entry: &TrafficEntry{
				ID:         202,
				Type:       "http",
				StartedAt:  time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC),
				Method:     "GET",
				URL:        "http://example.net/ping",
				Host:       "example.net",
				Path:       "/ping",
				StatusCode: 204,
				Status:     "204 No Content",
			},
			body: &trafficBodyViewInner{
				RequestBodyReader:  io.NopCloser(strings.NewReader("")),
				ResponseBodyReader: io.NopCloser(strings.NewReader("")),
				RequestBodySize:    0,
				ResponseBodySize:   0,
			},
			wantBody: &TrafficBodyView{
				RequestBody:  "",
				ResponseBody: "",
			},
		},
		{
			entry: &TrafficEntry{
				ID:        203,
				Type:      "tcp",
				StartedAt: time.Date(2026, 5, 7, 10, 5, 0, 0, time.UTC),
				Method:    "CONNECT",
				URL:       "tcp://[2001:db8::10]:8443",
				Host:      "[2001:db8::10]:8443",
				Request: &HTTPMessage{
					Proto: "HTTP/1.1",
					HeaderFields: []HTTPHeaderField{{
						Name: "Proxy-Authorization", Value: "Basic fixture",
					}},
				},
				RawTCP: &RawTCPTunnelInfo{
					Source:   RawTCPTunnelSource("http_connect"),
					HostPort: "[2001:db8::10]:8443",
					TLS:      true,
				},
			},
			body: &trafficBodyViewInner{
				RequestBodyReader: io.NopCloser(strings.NewReader("must not be persisted")),
				RequestBodySize:   int64(len("must not be persisted")),
				WebSocketMessages: []*WebSocketMessage{{Direction: "send", MsgType: "text", Data: "ignored", DataSize: 7}},
				WsMsgsTruncated:   true,
			},
			wantBody: &TrafficBodyView{},
		},
	}

	tmpFile, err := os.CreateTemp(t.TempDir(), "history-*.hbin")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer tmpFile.Close()

	var hindex bytes.Buffer
	for _, tc := range entries {
		if err := hbinWriteEntry(&hindex, tmpFile, tc.entry, tc.body); err != nil {
			t.Fatalf("hbinWriteEntry(%d): %v", tc.entry.ID, err)
		}
	}

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek start: %v", err)
	}
	hbinBytes, err := io.ReadAll(tmpFile)
	if err != nil {
		t.Fatalf("ReadAll hbin file: %v", err)
	}

	type entryIndex struct {
		id          uint64
		headerIndex uint32
		bodyIndex   uint32
	}
	gotIndexes := make([]entryIndex, 0, len(entries))
	indexReader := bytes.NewReader(hindex.Bytes())
	for range entries {
		var idx entryIndex
		if err := binary.Read(indexReader, binary.BigEndian, &idx.id); err != nil {
			t.Fatalf("read index id: %v", err)
		}
		if err := binary.Read(indexReader, binary.BigEndian, &idx.headerIndex); err != nil {
			t.Fatalf("read index header: %v", err)
		}
		if err := binary.Read(indexReader, binary.BigEndian, &idx.bodyIndex); err != nil {
			t.Fatalf("read index body: %v", err)
		}
		gotIndexes = append(gotIndexes, idx)
	}

	for i, idx := range gotIndexes {
		want := entries[i]
		if idx.id != want.entry.ID {
			t.Fatalf("index[%d] id = %d, want %d", i, idx.id, want.entry.ID)
		}
		if idx.bodyIndex <= idx.headerIndex {
			t.Fatalf("index[%d] bodyIndex = %d, want > headerIndex %d", i, idx.bodyIndex, idx.headerIndex)
		}
		if int(idx.headerIndex) >= len(hbinBytes) {
			t.Fatalf("index[%d] headerIndex = %d out of range %d", i, idx.headerIndex, len(hbinBytes))
		}
		if int(idx.bodyIndex) >= len(hbinBytes) {
			t.Fatalf("index[%d] bodyIndex = %d out of range %d", i, idx.bodyIndex, len(hbinBytes))
		}

		decodedEntry, err := DecodeTrafficEntry(bytes.NewReader(hbinBytes[idx.headerIndex:]))
		if err != nil {
			t.Fatalf("DecodeTrafficEntry(%d): %v", want.entry.ID, err)
		}
		if !reflect.DeepEqual(decodedEntry, want.entry) {
			t.Fatalf("decoded entry mismatch for id %d:\n got: %#v\nwant: %#v", want.entry.ID, decodedEntry, want.entry)
		}

		decodedBody, err := DecodeTrafficBodyWithVersion(bytes.NewReader(hbinBytes[idx.bodyIndex:]), hbinVersionCurrent)
		if err != nil {
			t.Fatalf("DecodeTrafficBodyWithVersion(%d): %v", want.entry.ID, err)
		}
		if !reflect.DeepEqual(decodedBody, want.wantBody) {
			t.Fatalf("decoded body mismatch for id %d:\n got: %#v\nwant: %#v", want.entry.ID, decodedBody, want.wantBody)
		}
	}
}

func TestDecodeTrafficRequestBodyReturnsRawBytes(t *testing.T) {
	t.Parallel()

	payload := []byte{0x00, 0x01, 0x02, 0xff}
	entry := &TrafficEntry{ID: 303, Type: "http", Method: "POST"}
	body := &trafficBodyViewInner{
		RequestBodyReader: io.NopCloser(bytes.NewReader(payload)),
		RequestBodySize:   int64(len(payload)),
		RequestBodyBinary: true,
	}

	tmpFile, err := os.CreateTemp(t.TempDir(), "history-*.hbin")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer tmpFile.Close()

	var hindex bytes.Buffer
	if err := hbinWriteEntry(&hindex, tmpFile, entry, body); err != nil {
		t.Fatalf("hbinWriteEntry: %v", err)
	}

	indexReader := bytes.NewReader(hindex.Bytes())
	var id uint64
	var headerIndex uint32
	var bodyIndex uint32
	if err := binary.Read(indexReader, binary.BigEndian, &id); err != nil {
		t.Fatalf("read index id: %v", err)
	}
	if err := binary.Read(indexReader, binary.BigEndian, &headerIndex); err != nil {
		t.Fatalf("read index header: %v", err)
	}
	if err := binary.Read(indexReader, binary.BigEndian, &bodyIndex); err != nil {
		t.Fatalf("read index body: %v", err)
	}
	if id != entry.ID {
		t.Fatalf("index id = %d, want %d", id, entry.ID)
	}
	if bodyIndex <= headerIndex {
		t.Fatalf("bodyIndex = %d, want > headerIndex %d", bodyIndex, headerIndex)
	}

	if _, err := tmpFile.Seek(int64(bodyIndex), io.SeekStart); err != nil {
		t.Fatalf("seek body: %v", err)
	}
	got, binaryBody, err := DecodeTrafficRequestBody(tmpFile, hbinVersionCurrent)
	if err != nil {
		t.Fatalf("DecodeTrafficRequestBody: %v", err)
	}
	if !binaryBody {
		t.Fatal("binary marker = false, want true")
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("request body = %v, want %v", got, payload)
	}
}

func TestHistoryMetadataUsesCurrentV3(t *testing.T) {
	want := HistoryMetadata{
		Key:           "current-v3",
		Alias:         "Current V3",
		CreatedAt:     1_785_000_000_000,
		FormatVersion: 3,
	}
	var encoded bytes.Buffer
	if err := encodeHistoryMetadata(&encoded, want); err != nil {
		t.Fatalf("encodeHistoryMetadata: %v", err)
	}
	if err := binary.Write(&encoded, binary.BigEndian, uint32(0)); err != nil {
		t.Fatalf("write entry count: %v", err)
	}
	if got := binary.BigEndian.Uint16(encoded.Bytes()[len(hbinMagic):]); got != 3 {
		t.Fatalf("encoded version = %d, want 3", got)
	}
	if got := binary.BigEndian.Uint32(encoded.Bytes()[len(hbinMagic)+2:]); got != uint32(len(want.Key)) {
		t.Fatalf("encoded key length = %d, want %d", got, len(want.Key))
	}
	metadata, err := DecodeHistoryMetadata(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatalf("DecodeHistoryMetadata: %v", err)
	}
	if !reflect.DeepEqual(metadata, &want) {
		t.Fatalf("decoded metadata = %+v, want %+v", metadata, want)
	}
}

func TestCurrentProcessInfoRoundTrip(t *testing.T) {
	statuses := []ProcessStatus{
		ProcessStatusPending,
		ProcessStatusResolved,
		ProcessStatusRemote,
		ProcessStatusNotFound,
		ProcessStatusPermissionDenied,
		ProcessStatusUnsupported,
		ProcessStatusAmbiguous,
	}
	for index, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			entry := &TrafficEntry{
				ID:        uint64(8000 + index),
				Type:      "https",
				StartedAt: time.Date(2026, 7, 23, 12, index, 0, 0, time.UTC),
				Metadata: &Metadata{Process: &ProcessInfo{
					Status:             status,
					PID:                uint32(9000 + index),
					DisplayName:        "Fixture Application",
					ProcessName:        "fixture-process",
					ExecutablePath:     "/opt/fixture/bin/process",
					AppID:              "org.flowlens.Fixture",
					IconKey:            strings.Repeat("a", 64),
					Source:             "linux_inet_diag",
					IdentityConfidence: "exact",
					UnavailableReason:  "metadata_denied",
				}},
			}
			var encoded bytes.Buffer
			if err := hbinWriteEntryHeader(&encoded, entry); err != nil {
				t.Fatalf("hbinWriteEntryHeader: %v", err)
			}
			decoded, err := DecodeTrafficEntry(bytes.NewReader(encoded.Bytes()))
			if err != nil {
				t.Fatalf("DecodeTrafficEntry: %v", err)
			}
			if !reflect.DeepEqual(decoded.Metadata.Process, entry.Metadata.Process) {
				t.Fatalf("process round trip:\n got: %#v\nwant: %#v", decoded.Metadata.Process, entry.Metadata.Process)
			}
		})
	}
}

func TestDecodeHistoryRejectsUnknownV4(t *testing.T) {
	var encoded bytes.Buffer
	encoded.WriteString(hbinMagic)
	if err := binary.Write(&encoded, binary.BigEndian, uint16(4)); err != nil {
		t.Fatalf("write version: %v", err)
	}
	metadata, err := DecodeHistoryMetadata(bytes.NewReader(encoded.Bytes()))
	if err == nil || !strings.Contains(err.Error(), "unsupported version 4") {
		t.Fatalf("DecodeHistoryMetadata error = %v", err)
	}
	if metadata != nil {
		t.Fatalf("unknown-version metadata = %+v, want nil", metadata)
	}
}

func TestDecodeTrafficEntryRejectsUnknownV4(t *testing.T) {
	entry, err := DecodeTrafficEntryWithVersion(bytes.NewReader(nil), 4)
	if err == nil || !strings.Contains(err.Error(), "unsupported version 4") {
		t.Fatalf("DecodeTrafficEntryWithVersion error = %v", err)
	}
	if entry != nil {
		t.Fatalf("unknown-version entry = %+v, want nil", entry)
	}
}

func TestHbinBodyMarkerPreservesUnavailableCapturedPayload(t *testing.T) {
	entry := &TrafficEntry{
		ID:   404,
		Type: "https",
		Request: &HTTPMessage{Metrics: &HTTPMessageMetrics{
			StartedAtMicros: 1, EndedAtMicros: 2, HeaderSize: 0, BodySize: 0,
			State: HTTPMessageStateCompleted,
		}},
		Response: &HTTPMessage{Metrics: &HTTPMessageMetrics{
			StartedAtMicros: 3, EndedAtMicros: 4, HeaderSize: 0, BodySize: 5,
			State: HTTPMessageStateCompleted,
		}},
	}
	file, err := os.CreateTemp(t.TempDir(), "missing-body-*.hbin")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var index bytes.Buffer
	if err := hbinWriteEntry(&index, file, entry, &trafficBodyViewInner{}); err != nil {
		t.Fatalf("hbinWriteEntry: %v", err)
	}

	var id uint64
	var headerOffset, bodyOffset uint32
	if err := binary.Read(&index, binary.BigEndian, &id); err != nil {
		t.Fatal(err)
	}
	if err := binary.Read(&index, binary.BigEndian, &headerOffset); err != nil {
		t.Fatal(err)
	}
	if err := binary.Read(&index, binary.BigEndian, &bodyOffset); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(int64(bodyOffset), io.SeekStart); err != nil {
		t.Fatal(err)
	}
	requestBody, responseBody, err := DecodeTrafficHARBodies(file, hbinVersionCurrent)
	if err != nil {
		t.Fatalf("DecodeTrafficHARBodies: %v", err)
	}
	if !requestBody.Available {
		t.Fatal("completed empty request body should be available")
	}
	if responseBody.Available || len(responseBody.Data) != 0 {
		t.Fatalf("missing response body = %+v", responseBody)
	}
}
