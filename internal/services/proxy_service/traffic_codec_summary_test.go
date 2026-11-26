package proxyservice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func summaryCodecFixture(payload string) *TrafficEntry {
	name := &PkixName{
		Country: []string{payload}, Organization: []string{payload, ""}, OrganizationalUnit: []string{payload},
		Locality: []string{payload}, Province: []string{payload}, StreetAddress: []string{payload}, PostalCode: []string{payload},
		SerialNumber: payload, CommonName: payload,
	}
	return &TrafficEntry{
		ID: 42, Type: "https", StartedAt: time.Unix(1, 1234).UTC(), Method: "POST", URL: "https://example.test/a", Host: "example.test", Path: "/a", StatusCode: 200, Status: "OK",
		Metadata: &Metadata{
			LocalSourceAddr: "127.0.0.1:1234", LocalDestinationAddr: "127.0.0.1:9000", RemoteSourceAddr: "127.0.0.1:5555", RemoteDestinationAddr: "127.0.0.1:443",
			TLS:               &TLSState{ServerName: payload, SupportedALPN: []string{payload, "h2"}, SupportedVersion: []string{payload}, SupportedCipherSuites: []string{payload}, SelectedALPN: "h2", SelectedVersion: payload, SelectedCipherSuite: payload},
			Certificate:       &ServerCertificate{Version: 3, NotBeforeMicros: 1_000_001, NotAfterMicros: 2_000_002, SerialNumber: payload, SignatureAlgorithm: payload, Sha1Fingerprint: payload, Sha256Fingerprint: payload, Subject: name, Issuer: name, DNSNames: []string{payload, ""}, IPAddresses: []string{payload}},
			Process:           &ProcessInfo{Status: ProcessStatusResolved, PID: 1234, DisplayName: "fixture", ExecutablePath: "/fixture", IconKey: "icon"},
			ConnectionTimings: &HTTPConnectionTimings{DNSStartedAtMicros: -1, DNSEndedAtMicros: -1, ConnectStartedAtMicros: 100, ConnectEndedAtMicros: 200, TLSStartedAtMicros: 200, TLSEndedAtMicros: 300},
		},
		Request: &HTTPMessage{
			Proto: "HTTP/2.0", HeaderFields: []HTTPHeaderField{{Name: "Cookie", Value: payload}, {Name: payload, Value: payload}, {Name: "Content-typX", Value: payload}, {Name: "cOnTeNt-TyPe", Value: "application/json"}, {Name: "Content-Type", Value: payload}},
			TrailerFields:    []HTTPHeaderField{{Name: "Content-Type", Value: payload}, {Name: payload, Value: payload}},
			HeadersTruncated: true, TrailerOrderUnavailable: true,
			Metrics: &HTTPMessageMetrics{StartedAtMicros: 1234, EndedAtMicros: -1, HeaderSize: -1, BodySize: 0, State: HTTPMessageStateFailed},
		},
		Response: &HTTPMessage{
			Proto: "HTTP/1.1", HeaderFields: []HTTPHeaderField{{Name: "Set-Cookie", Value: payload}, {Name: "content-type", Value: ""}, {Name: "Content-Type", Value: payload}, {Name: "X-Custom", Value: payload}},
			TrailerFields:     []HTTPHeaderField{{Name: "X-Trailer", Value: payload}},
			TrailersTruncated: true, HeaderOrderUnavailable: true,
			Metrics: &HTTPMessageMetrics{StartedAtMicros: -1, EndedAtMicros: -1, HeaderSize: -1, BodySize: -1, State: HTTPMessageStateFailed},
		},
		Error: &TrafficError{Timestamp: time.Unix(2, 5678).UTC(), Error: "fixture error"},
	}
}

func encodeSummaryFixture(t testing.TB, entry *TrafficEntry, version uint16) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := hbinWriteEntryHeader(&encoded, entry); err != nil {
		t.Fatal(err)
	}
	if version == 1 {
		var timings bytes.Buffer
		var value *HTTPConnectionTimings
		if entry.Metadata != nil {
			value = entry.Metadata.ConnectionTimings
		}
		if err := hbinWriteConnectionTimings(&timings, value); err != nil {
			t.Fatal(err)
		}
		encoded.Truncate(encoded.Len() - timings.Len())
	}
	return encoded.Bytes()
}

func TestHBINSummaryPreservesRecordFields(t *testing.T) {
	for _, version := range []uint16{1, 2, 3} {
		for _, kind := range []string{"https", "tcp", "empty", "missing"} {
			t.Run(fmt.Sprintf("v%d/%s", version, kind), func(t *testing.T) {
				entry := summaryCodecFixture("payload")
				switch kind {
				case "tcp":
					entry.Type = "tcp"
					entry.RawTCP = &RawTCPTunnelInfo{Source: RawTCPTunnelSourceSOCKS5, HostPort: "example.test:443", TLS: true}
				case "empty":
					entry.Metadata = &Metadata{TLS: &TLSState{}, Certificate: &ServerCertificate{Subject: &PkixName{}, Issuer: &PkixName{}}}
					entry.Request, entry.Response = &HTTPMessage{HeaderFields: []HTTPHeaderField{}, TrailerFields: []HTTPHeaderField{}}, &HTTPMessage{}
				case "missing":
					entry = &TrafficEntry{}
				}
				encoded := encodeSummaryFixture(t, entry, version)
				reader := bytes.NewReader(append(encoded, 0xfa, 0xfb))
				summary, err := DecodeTrafficEntrySummaryWithVersion(reader, version)
				if err != nil || reader.Len() != 2 {
					t.Fatalf("summary error=%v, remaining=%d", err, reader.Len())
				}
				full, err := DecodeTrafficEntryWithVersion(bytes.NewReader(encoded), version)
				if err != nil {
					t.Fatal(err)
				}
				if version == 1 && entry.Metadata != nil {
					entry.Metadata.ConnectionTimings = nil
				}
				if !reflect.DeepEqual(full, entry) {
					t.Fatal("complete decoding changed the original record")
				}
				// Check exactly which fields were omitted, then restore just those
				// fields to compare every remaining value with the complete decode.
				for _, pair := range [][2]*HTTPMessage{{summary.Request, full.Request}, {summary.Response, full.Response}} {
					if pair[0] == nil || pair[1] == nil {
						continue
					}
					var want []HTTPHeaderField
					for _, field := range pair[1].HeaderFields {
						if strings.EqualFold(field.Name, "Content-Type") {
							want = []HTTPHeaderField{field}
							break
						}
					}
					if !reflect.DeepEqual(pair[0].HeaderFields, want) || pair[0].TrailerFields != nil {
						t.Fatal("summary did not keep exactly the first Content-Type")
					}
					pair[0].HeaderFields, pair[0].TrailerFields = pair[1].HeaderFields, pair[1].TrailerFields
				}
				if summary.Metadata != nil && full.Metadata != nil {
					var tls *TLSState
					if full.Type == "tcp" && full.Metadata.TLS != nil {
						tls = &TLSState{SelectedALPN: full.Metadata.TLS.SelectedALPN}
					}
					if summary.Metadata.Certificate != nil || !reflect.DeepEqual(summary.Metadata.TLS, tls) {
						t.Fatal("summary retained certificate or unused TLS details")
					}
					summary.Metadata.Certificate, summary.Metadata.TLS = full.Metadata.Certificate, full.Metadata.TLS
				}
				if !reflect.DeepEqual(summary, full) {
					t.Fatal("summary changed other fields")
				}
			})
		}
	}
	for _, version := range []uint16{0, 4} {
		if _, err := DecodeTrafficEntrySummaryWithVersion(bytes.NewReader(nil), version); err == nil || !strings.Contains(err.Error(), "unsupported version") {
			t.Fatalf("unsupported version %d: %v", version, err)
		}
	}
}

func TestHBINSummaryRejectsTruncatedRecords(t *testing.T) {
	for _, version := range []uint16{1, 2, 3} {
		entry := summaryCodecFixture("fixture payload")
		entry.Type = "tcp"
		entry.RawTCP = &RawTCPTunnelInfo{Source: RawTCPTunnelSourceHTTPConnect, HostPort: "example.test:443", TLS: true}
		encoded := encodeSummaryFixture(t, entry, version)
		for end := 0; end < len(encoded); end++ {
			_, fullErr := DecodeTrafficEntryWithVersion(bytes.NewReader(encoded[:end]), version)
			_, summaryErr := DecodeTrafficEntrySummaryWithVersion(bufio.NewReader(bytes.NewReader(encoded[:end])), version)
			if fullErr == nil || summaryErr == nil || fullErr.Error() != summaryErr.Error() {
				t.Fatalf("v%d truncated at %d/%d: full=%v, summary=%v", version, end, len(encoded), fullErr, summaryErr)
			}
		}
	}
}

func TestHBINSummaryValidatesDiscardedHeaders(t *testing.T) {
	for _, mode := range []hbinHeaderReadMode{hbinHeadersAll, hbinHeadersContentType, hbinHeadersNone} {
		for _, encoded := range [][]byte{
			{2},                   // Invalid presence.
			{1, 0, 0x10, 0, 1},    // Above the field-count limit.
			{0, 2, 0},             // Invalid truncation flag.
			{1, 0, 0, 0, 0, 0, 2}, // Invalid order flag after an empty array.
		} {
			if _, _, _, err := hbinReadHeaderFields(bytes.NewReader(encoded), mode); err == nil {
				t.Fatalf("mode %d accepted %x", mode, encoded)
			}
		}
	}
}

func TestHBINSkipBoundsAndReaderErrors(t *testing.T) {
	// A corrupt 4 GiB length must not allocate 4 GiB or silently seek past EOF.
	var encoded bytes.Buffer
	_ = binary.Write(&encoded, binary.BigEndian, ^uint32(0))
	encoded.WriteString("short")
	if _, err := hbinReadStringValue(&encoded, false); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("huge truncated string: %v", err)
	}
	reader := io.MultiReader(strings.NewReader("prefix"), hbinSummaryErrorReader{err: context.Canceled})
	if err := hbinSkipBytes(reader, 1<<20); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

type hbinSummaryErrorReader struct{ err error }

func (r hbinSummaryErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestHBINSummaryAllocationDoesNotScaleWithSkippedPayload(t *testing.T) {
	measure := func(payload string, summary bool) uint64 {
		encoded := encodeSummaryFixture(t, summaryCodecFixture(payload), 3)
		decode := DecodeTrafficEntryWithVersion
		if summary {
			decode = DecodeTrafficEntrySummaryWithVersion
		}
		reader := bufio.NewReader(bytes.NewReader(nil))
		source := bytes.NewReader(encoded)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		const runs = 8
		for range runs {
			source.Reset(encoded)
			reader.Reset(source)
			entry, err := decode(reader, 3)
			if err != nil {
				t.Fatal(err)
			}
			runtime.KeepAlive(entry)
		}
		runtime.ReadMemStats(&after)
		return (after.TotalAlloc - before.TotalAlloc) / runs
	}
	small := measure("x", true)
	payload := strings.Repeat("x", 64<<10)
	large, full := measure(payload, true), measure(payload, false)
	t.Logf("allocation per record: summary small=%d B, summary large=%d B, full large=%d B", small, large, full)
	if large > small+16<<10 || large > 64<<10 || full < 1<<20 {
		t.Fatal("summary allocation scales with discarded payloads")
	}
}

func BenchmarkHistoryEntryDecode(b *testing.B) {
	for _, payloadSize := range []int{4096, 65536} {
		encoded := encodeSummaryFixture(b, summaryCodecFixture(strings.Repeat("x", payloadSize)), 3)
		for _, summary := range []bool{false, true} {
			b.Run(fmt.Sprintf("payload=%d/summary=%t", payloadSize, summary), func(b *testing.B) {
				decode := DecodeTrafficEntryWithVersion
				if summary {
					decode = DecodeTrafficEntrySummaryWithVersion
				}
				source := bytes.NewReader(encoded)
				reader := bufio.NewReader(source)
				b.ReportAllocs()
				b.SetBytes(int64(len(encoded)))
				b.ResetTimer()
				for b.Loop() {
					source.Reset(encoded)
					reader.Reset(source)
					entry, err := decode(reader, 3)
					if err != nil {
						b.Fatal(err)
					}
					runtime.KeepAlive(entry)
				}
			})
		}
	}
}
