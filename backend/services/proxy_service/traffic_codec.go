package proxyservice

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/josexy/flowlens/backend/pkg/compresspool"
)

const (
	hbinMagic                         = "PGHI"
	hbinVersionOldestSupported uint16 = 1
	hbinVersionCurrent         uint16 = 1
	hbinMaxHeaderFields               = 1 << 20
)

// History file layout
//
// All numeric fields are big-endian. Variable strings and byte slices use the
// shared helpers below:
//   - string: u32 byte length + UTF-8 bytes
//   - bytes:  u32 byte length + raw bytes
//   - optional structs: u8 present flag, followed by fields when present == 1
//
// Index file (.hidx):
//   - u32 entry_count
//   - repeated entry_count times:
//   - u64 traffic_id
//   - u32 header_offset, absolute byte offset in the matching .hbin file
//   - u32 body_offset, absolute byte offset in the matching .hbin file
//
// Binary file (.hbin):
//   - 4 bytes magic: "PGHI"
//   - u16 version
//   - string history_key
//   - string history_alias
//   - i64 created_at_unix_nano
//   - u32 entry_count
//   - repeated traffic entries. Each entry has two independently addressable
//     sections, and .hidx stores the offsets to both:
//   - entry header at header_offset:
//     hbinWriteEntryHeader fields: traffic metadata, request/response header fields,
//     optional request/response metrics, and optional traffic error. Bodies are
//     not stored in this section. Version 1 stores HTTP message metrics and
//     certificate validity as Unix microsecond timestamps.
//     The current layout appends an optional process block to metadata after
//     Certificate. For entries whose type is "tcp", the traffic error is
//     followed by an optional raw TCP tunnel block:
//   - u8 raw_tcp_present
//   - when raw_tcp_present == 1: string source, string host_port, u8 tls
//   - entry body at body_offset:
//   - u32 compressed_body_size
//   - zstd stream of hbinWriteEntryBody fields:
//   - u8 request_body_encoding, 0 = raw text marker, 1 = base64 view marker,
//     2 = captured body unavailable
//   - bytes request_body_raw_bytes
//   - u8 response_body_encoding, 0 = raw text marker, 1 = base64 view marker,
//     2 = captured body unavailable
//   - bytes response_body_raw_bytes
//   - u32 websocket_message_count
//   - repeated websocket messages
//   - u8 websocket_messages_truncated
//
// Body bytes are stored as the raw captured bytes. The encoding flags describe
// how the UI should render them when converting to TrafficBodyView; they do not
// mean the bytes in .hbin are base64-encoded.
func encodeHistoryMetadata(hbinFile io.Writer, md HistoryMetadata) error {
	if _, err := io.WriteString(hbinFile, hbinMagic); err != nil {
		return err
	}
	if err := binary.Write(hbinFile, binary.BigEndian, hbinVersionCurrent); err != nil {
		return err
	}
	if err := hbinWriteString(hbinFile, md.Key); err != nil {
		return err
	}
	if err := hbinWriteString(hbinFile, md.Alias); err != nil {
		return err
	}
	if err := binary.Write(hbinFile, binary.BigEndian, md.CreatedAt); err != nil {
		return err
	}
	return nil
}

func encodeTrafficEntry(hindexFile io.Writer, hbinFile io.WriteSeeker, te *TrafficEntry, bv *trafficBodyViewInner) error {
	return hbinWriteEntry(hindexFile, hbinFile, te, bv)
}

func DecodeHistoryMetadata(r io.Reader) (*HistoryMetadata, error) {
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, err
	}
	if string(magic) != hbinMagic {
		return nil, fmt.Errorf("hbin: invalid magic %q", magic)
	}
	var version uint16
	if err := binary.Read(r, binary.BigEndian, &version); err != nil {
		return nil, err
	}
	if !isSupportedHistoryFormatVersion(version) {
		return nil, fmt.Errorf("hbin: unsupported version %d", version)
	}
	key, err := hbinReadString(r)
	if err != nil {
		return nil, err
	}
	alias, err := hbinReadString(r)
	if err != nil {
		return nil, err
	}
	var createdAt int64
	if err := binary.Read(r, binary.BigEndian, &createdAt); err != nil {
		return nil, err
	}
	var entriesCount uint32
	if err := binary.Read(r, binary.BigEndian, &entriesCount); err != nil {
		return nil, err
	}
	return &HistoryMetadata{
		Key:           key,
		Alias:         alias,
		CreatedAt:     createdAt,
		Total:         int(entriesCount),
		FormatVersion: version,
	}, nil
}

func DecodeTrafficEntry(r io.Reader) (*TrafficEntry, error) {
	return DecodeTrafficEntryWithVersion(r, hbinVersionCurrent)
}

func DecodeTrafficEntryWithVersion(r io.Reader, formatVersion uint16) (*TrafficEntry, error) {
	if !isSupportedHistoryFormatVersion(formatVersion) {
		return nil, fmt.Errorf("hbin: unsupported version %d", formatVersion)
	}
	return hbinReadEntryHeader(r)
}

func isSupportedHistoryFormatVersion(version uint16) bool {
	return version >= hbinVersionOldestSupported && version <= hbinVersionCurrent
}

func convertToTrafficBodyView(bvi *trafficBodyViewInner) (*TrafficBodyView, error) {
	bv := &TrafficBodyView{
		RequestBodyEncoding:  bvi.RequestBodyEncoding,
		ResponseBodyEncoding: bvi.ResponseBodyEncoding,
		WebSocketMessages:    append([]*WebSocketMessage(nil), bvi.WebSocketMessages...),
		WsMsgsTruncated:      bvi.WsMsgsTruncated,
	}

	if bvi.RequestBodyReader != nil {
		reqBodyBytes, err := io.ReadAll(bvi.RequestBodyReader)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		bv.RequestBody, bv.RequestBodyEncoding = encodeTrafficBodyView(reqBodyBytes, bvi.RequestBodyEncoding)
	}
	if bvi.ResponseBodyReader != nil {
		rspBodyBytes, err := io.ReadAll(bvi.ResponseBodyReader)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
		bv.ResponseBody, bv.ResponseBodyEncoding = encodeTrafficBodyView(rspBodyBytes, bvi.ResponseBodyEncoding)
	}
	return bv, nil
}

func encodeTrafficBodyView(bodyBytes []byte, encoding string) (string, string) {
	if encoding == "base64" || !utf8.Valid(bodyBytes) {
		return base64.StdEncoding.EncodeToString(bodyBytes), "base64"
	}
	return bytes2String(bodyBytes), encoding
}

func DecodeTrafficBody(r io.Reader) (*TrafficBodyView, error) {
	bvi, err := hbinReadEntryBody(r)
	defer func() {
		bvi.closeReqBodyReaderSafely()
		bvi.closeRspBodyReaderSafely()
	}()
	if err != nil {
		return nil, err
	}
	return convertToTrafficBodyView(bvi)
}

func DecodeTrafficRequestBody(r io.Reader) ([]byte, string, error) {
	var size uint32
	if err := binary.Read(r, binary.BigEndian, &size); err != nil {
		return nil, "", err
	}
	dec, err := compresspool.AcquireZstdDecoder(io.LimitReader(r, int64(size)))
	if err != nil {
		return nil, "", fmt.Errorf("hbin: zstd decompress: %w", err)
	}
	defer func() {
		compresspool.ReleaseZstdDecoder(dec)
	}()
	var reqEnc uint8
	if err := binary.Read(dec, binary.BigEndian, &reqEnc); err != nil {
		return nil, "", err
	}
	var encoding string
	if reqEnc > 2 {
		return nil, "", fmt.Errorf("hbin: invalid request body encoding %d", reqEnc)
	}
	if reqEnc == 2 {
		return nil, "", errors.New("hbin: request body unavailable")
	}
	if reqEnc == 1 {
		encoding = "base64"
	}
	reqBodyBytes, err := hbinReadBytes(dec)
	if err != nil {
		return nil, "", err
	}
	return reqBodyBytes, encoding, nil
}

func hbinWriteEntry(hindexFile io.Writer, hbinFile io.WriteSeeker, te *TrafficEntry, bv *trafficBodyViewInner) error {
	offset, err := hbinFile.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	// size|id1|header_index1|body_index1|id2|header_index2|body_index2
	if err := binary.Write(hindexFile, binary.BigEndian, uint64(te.ID)); err != nil {
		return err
	}
	if err := binary.Write(hindexFile, binary.BigEndian, uint32(offset)); err != nil {
		return err
	}
	if err := hbinWriteEntryHeader(hbinFile, te); err != nil {
		return err
	}
	offset, err = hbinFile.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if err := binary.Write(hindexFile, binary.BigEndian, uint32(offset)); err != nil {
		return err
	}
	bodySizeOffset := offset
	if err := binary.Write(hbinFile, binary.BigEndian, uint32(0)); err != nil {
		return err
	}

	compressedBody := &hbinCountingWriter{w: hbinFile}
	encw, err := compresspool.AcquireZstdEncoder(compressedBody)
	if err != nil {
		return err
	}
	defer func() {
		compresspool.ReleaseZstdEncoder(encw)
	}()
	bodyView := bv
	if bodyView == nil {
		bodyView = &trafficBodyViewInner{}
	} else {
		clone := *bodyView
		bodyView = &clone
	}
	bodyView.RequestBodyUnavailable = bodyView.RequestBodyUnavailable || !harCurrentBodyAvailable(
		te.Request,
		bodyView.RequestBodyReader != nil,
	)
	bodyView.ResponseBodyUnavailable = bodyView.ResponseBodyUnavailable || !harCurrentBodyAvailable(
		te.Response,
		bodyView.ResponseBodyReader != nil,
	)
	if te.Type == "tcp" {
		bodyView = &trafficBodyViewInner{}
	}
	if err := hbinWriteEntryBody(encw, bodyView); err != nil {
		return err
	}
	if err := encw.Close(); err != nil {
		return err
	}
	if compressedBody.n > int64(^uint32(0)) {
		return fmt.Errorf("hbin: compressed body too large: %d", compressedBody.n)
	}
	endOffset, err := hbinFile.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := hbinFile.Seek(bodySizeOffset, io.SeekStart); err != nil {
		return err
	}
	if err := binary.Write(hbinFile, binary.BigEndian, uint32(compressedBody.n)); err != nil {
		return err
	}
	_, err = hbinFile.Seek(endOffset, io.SeekStart)
	return err
}

type hbinCountingWriter struct {
	w io.Writer
	n int64
}

func (w *hbinCountingWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}

func hbinReadEntryHeader(r io.Reader) (*TrafficEntry, error) {
	te := new(TrafficEntry)
	if err := binary.Read(r, binary.BigEndian, &te.ID); err != nil {
		return nil, err
	}
	var err error
	if te.Type, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if te.StartedAt, err = hbinReadTime(r); err != nil {
		return nil, err
	}
	if te.Method, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if te.URL, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if te.Host, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if te.Path, err = hbinReadString(r); err != nil {
		return nil, err
	}
	var statusCode int32
	if err := binary.Read(r, binary.BigEndian, &statusCode); err != nil {
		return nil, err
	}
	te.StatusCode = int(statusCode)
	if te.Status, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if te.Metadata, err = hbinReadEntryMetadata(r); err != nil {
		return nil, err
	}
	if te.Request, err = hbinReadOptHTTPMessage(r); err != nil {
		return nil, err
	}
	if te.Response, err = hbinReadOptHTTPMessage(r); err != nil {
		return nil, err
	}
	if te.Error, err = hbinReadOptTrafficError(r); err != nil {
		return nil, err
	}
	if te.Type == "tcp" {
		if te.RawTCP, err = hbinReadOptRawTCPTunnelInfo(r); err != nil {
			return nil, err
		}
	}
	return te, nil
}

func hbinReadEntryMetadata(r io.Reader) (*Metadata, error) {
	var present uint8
	if err := binary.Read(r, binary.BigEndian, &present); err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	md := new(Metadata)
	var err error
	if md.LocalSourceAddr, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if md.LocalDestinationAddr, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if md.RemoteSourceAddr, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if md.RemoteDestinationAddr, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if md.LocalConnectionEstablishedAt, err = hbinReadTime(r); err != nil {
		return nil, err
	}
	if md.RemoteConnectionEstablishedAt, err = hbinReadTime(r); err != nil {
		return nil, err
	}
	if md.RequestProcessedAt, err = hbinReadTime(r); err != nil {
		return nil, err
	}
	if md.SSLHandshakeCompletedAt, err = hbinReadTime(r); err != nil {
		return nil, err
	}
	if md.TLS, err = hbinReadOptTLSState(r); err != nil {
		return nil, err
	}
	if md.Certificate, err = hbinReadOptServerCertificate(r); err != nil {
		return nil, err
	}
	if md.Process, err = hbinReadOptProcessInfo(r); err != nil {
		return nil, err
	}
	return md, nil
}

func hbinWriteEntryHeader(w io.Writer, te *TrafficEntry) error {
	if err := binary.Write(w, binary.BigEndian, te.ID); err != nil {
		return err
	}
	if err := hbinWriteString(w, te.Type); err != nil {
		return err
	}
	if err := hbinWriteTime(w, te.StartedAt); err != nil {
		return err
	}
	if err := hbinWriteString(w, te.Method); err != nil {
		return err
	}
	if err := hbinWriteString(w, te.URL); err != nil {
		return err
	}
	if err := hbinWriteString(w, te.Host); err != nil {
		return err
	}
	if err := hbinWriteString(w, te.Path); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, int32(te.StatusCode)); err != nil {
		return err
	}
	if err := hbinWriteString(w, te.Status); err != nil {
		return err
	}
	if err := hbinWriteEntryMetadata(w, te.Metadata); err != nil {
		return err
	}
	if err := hbinWriteOptHTTPMessage(w, te.Request); err != nil {
		return err
	}
	if err := hbinWriteOptHTTPMessage(w, te.Response); err != nil {
		return err
	}
	if err := hbinWriteOptTrafficError(w, te.Error); err != nil {
		return err
	}
	if te.Type == "tcp" {
		return hbinWriteOptRawTCPTunnelInfo(w, te.RawTCP)
	}
	return nil
}

func hbinWriteOptRawTCPTunnelInfo(w io.Writer, info *RawTCPTunnelInfo) error {
	if info == nil {
		return binary.Write(w, binary.BigEndian, uint8(0))
	}
	if err := binary.Write(w, binary.BigEndian, uint8(1)); err != nil {
		return err
	}
	if err := hbinWriteString(w, string(info.Source)); err != nil {
		return err
	}
	if err := hbinWriteString(w, info.HostPort); err != nil {
		return err
	}
	var tls uint8
	if info.TLS {
		tls = 1
	}
	return binary.Write(w, binary.BigEndian, tls)
}

func hbinReadOptRawTCPTunnelInfo(r io.Reader) (*RawTCPTunnelInfo, error) {
	var present uint8
	if err := binary.Read(r, binary.BigEndian, &present); err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	source, err := hbinReadString(r)
	if err != nil {
		return nil, err
	}
	hostPort, err := hbinReadString(r)
	if err != nil {
		return nil, err
	}
	var tls uint8
	if err := binary.Read(r, binary.BigEndian, &tls); err != nil {
		return nil, err
	}
	return &RawTCPTunnelInfo{
		Source:   RawTCPTunnelSource(source),
		HostPort: hostPort,
		TLS:      tls == 1,
	}, nil
}

func hbinWriteEntryMetadata(w io.Writer, md *Metadata) error {
	if md == nil {
		return binary.Write(w, binary.BigEndian, uint8(0))
	}
	if err := binary.Write(w, binary.BigEndian, uint8(1)); err != nil {
		return err
	}
	for _, s := range []string{
		md.LocalSourceAddr, md.LocalDestinationAddr,
		md.RemoteSourceAddr, md.RemoteDestinationAddr,
	} {
		if err := hbinWriteString(w, s); err != nil {
			return err
		}
	}
	for _, t := range []time.Time{
		md.LocalConnectionEstablishedAt, md.RemoteConnectionEstablishedAt,
		md.RequestProcessedAt, md.SSLHandshakeCompletedAt,
	} {
		if err := hbinWriteTime(w, t); err != nil {
			return err
		}
	}
	if err := hbinWriteOptTLSState(w, md.TLS); err != nil {
		return err
	}
	if err := hbinWriteOptServerCertificate(w, md.Certificate); err != nil {
		return err
	}
	return hbinWriteOptProcessInfo(w, md.Process)
}

func hbinWriteOptProcessInfo(w io.Writer, process *ProcessInfo) error {
	if process == nil {
		return binary.Write(w, binary.BigEndian, uint8(0))
	}
	if err := binary.Write(w, binary.BigEndian, uint8(1)); err != nil {
		return err
	}
	if err := hbinWriteString(w, string(process.Status)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, process.PID); err != nil {
		return err
	}
	for _, value := range []string{
		process.DisplayName,
		process.ProcessName,
		process.ExecutablePath,
		process.AppID,
		process.IconKey,
		process.Source,
		process.IdentityConfidence,
		process.UnavailableReason,
	} {
		if err := hbinWriteString(w, value); err != nil {
			return err
		}
	}
	return nil
}

func hbinReadOptProcessInfo(r io.Reader) (*ProcessInfo, error) {
	var present uint8
	if err := binary.Read(r, binary.BigEndian, &present); err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	status, err := hbinReadString(r)
	if err != nil {
		return nil, err
	}
	process := &ProcessInfo{Status: ProcessStatus(status)}
	if err := binary.Read(r, binary.BigEndian, &process.PID); err != nil {
		return nil, err
	}
	fields := []*string{
		&process.DisplayName,
		&process.ProcessName,
		&process.ExecutablePath,
		&process.AppID,
		&process.IconKey,
		&process.Source,
		&process.IdentityConfidence,
		&process.UnavailableReason,
	}
	for _, field := range fields {
		*field, err = hbinReadString(r)
		if err != nil {
			return nil, err
		}
	}
	return process, nil
}

func hbinWriteEntryBody(w io.Writer, bv *trafficBodyViewInner) error {
	// encode request body
	reqEnc := uint8(0)
	if bv.RequestBodyUnavailable {
		reqEnc = 2
	} else if bv.RequestBodyEncoding == "base64" {
		reqEnc = 1
	}
	if err := binary.Write(w, binary.BigEndian, reqEnc); err != nil {
		return err
	}
	if err := hbinWriteFromReader(w, bv.RequestBodyReader, bv.RequestBodySize); err != nil {
		return err
	}
	// encode response body
	rspEnc := uint8(0)
	if bv.ResponseBodyUnavailable {
		rspEnc = 2
	} else if bv.ResponseBodyEncoding == "base64" {
		rspEnc = 1
	}
	if err := binary.Write(w, binary.BigEndian, rspEnc); err != nil {
		return err
	}
	if err := hbinWriteFromReader(w, bv.ResponseBodyReader, bv.ResponseBodySize); err != nil {
		return err
	}
	// encode WebSocket messages
	if err := binary.Write(w, binary.BigEndian, uint32(len(bv.WebSocketMessages))); err != nil {
		return err
	}
	for _, msg := range bv.WebSocketMessages {
		if err := hbinWriteWebSocketMessage(w, msg); err != nil {
			return err
		}
	}
	var wsTruncated uint8
	if bv.WsMsgsTruncated {
		wsTruncated = 1
	}
	if err := binary.Write(w, binary.BigEndian, wsTruncated); err != nil {
		return err
	}
	return nil
}

func hbinReadEntryBody(r io.Reader) (bv *trafficBodyViewInner, err error) {
	var size uint32
	if err = binary.Read(r, binary.BigEndian, &size); err != nil {
		err = fmt.Errorf("hbin: read body size: %w", err)
		return
	}
	dec, err := compresspool.AcquireZstdDecoder(io.LimitReader(r, int64(size)))
	if err != nil {
		return nil, fmt.Errorf("hbin: zstd decompress: %w", err)
	}
	defer func() {
		compresspool.ReleaseZstdDecoder(dec)
	}()
	bv = new(trafficBodyViewInner)
	// decode request body
	var reqEnc uint8
	if err = binary.Read(dec, binary.BigEndian, &reqEnc); err != nil {
		err = fmt.Errorf("hbin: read request encoding: %w", err)
		return
	}
	if reqEnc > 2 {
		err = fmt.Errorf("hbin: invalid request body encoding %d", reqEnc)
		return
	}
	if reqEnc == 1 {
		bv.RequestBodyEncoding = "base64"
	} else if reqEnc == 2 {
		bv.RequestBodyUnavailable = true
	}
	reqBodyReader, reqBodySize, err := hbinReadBytesAsReader(dec)
	if err != nil {
		err = fmt.Errorf("hbin: read request body: %w", err)
		return
	}
	if reqBodyReader != nil {
		bv.RequestBodySize = reqBodySize
		bv.RequestBodyReader = io.NopCloser(reqBodyReader)
	}
	// decode response body
	var rspEnc uint8
	if err = binary.Read(dec, binary.BigEndian, &rspEnc); err != nil {
		err = fmt.Errorf("hbin: read response encoding: %w", err)
		return
	}
	if rspEnc > 2 {
		err = fmt.Errorf("hbin: invalid response body encoding %d", rspEnc)
		return
	}
	if rspEnc == 1 {
		bv.ResponseBodyEncoding = "base64"
	} else if rspEnc == 2 {
		bv.ResponseBodyUnavailable = true
	}
	rspBodyReader, rspBodySize, err := hbinReadBytesAsReader(dec)
	if err != nil {
		err = fmt.Errorf("hbin: read response body: %w", err)
		return
	}
	if rspBodyReader != nil {
		bv.ResponseBodySize = rspBodySize
		bv.ResponseBodyReader = io.NopCloser(rspBodyReader)
	}
	// decode WebSocket messages
	var wsMsgCount uint32
	if err = binary.Read(dec, binary.BigEndian, &wsMsgCount); err != nil {
		err = fmt.Errorf("hbin: read WebSocket message count: %w", err)
		return
	}
	if wsMsgCount > 0 {
		bv.WebSocketMessages = make([]*WebSocketMessage, 0, wsMsgCount)
		for i := uint32(0); i < wsMsgCount; i++ {
			msg, err := hbinReadWebSocketMessage(dec)
			if err != nil {
				err = fmt.Errorf("hbin: read WebSocket message #%d: %w", i+1, err)
				return nil, err
			}
			bv.WebSocketMessages = append(bv.WebSocketMessages, msg)
		}
	}
	var wsTruncated uint8
	if err = binary.Read(dec, binary.BigEndian, &wsTruncated); err != nil {
		err = fmt.Errorf("hbin: read WebSocket messages truncated flag: %w", err)
		return
	}
	bv.WsMsgsTruncated = wsTruncated == 1
	return
}

func hbinWriteOptHTTPMessage(w io.Writer, msg *HTTPMessage) error {
	if msg == nil {
		return binary.Write(w, binary.BigEndian, uint8(0))
	}
	if err := binary.Write(w, binary.BigEndian, uint8(1)); err != nil {
		return err
	}
	if err := hbinWriteString(w, msg.Proto); err != nil {
		return err
	}
	if err := hbinWriteHeaderFields(
		w,
		msg.HeaderFields,
		msg.HeadersTruncated,
		msg.HeaderOrderUnavailable,
	); err != nil {
		return err
	}
	if err := hbinWriteHeaderFields(
		w,
		msg.TrailerFields,
		msg.TrailersTruncated,
		msg.TrailerOrderUnavailable,
	); err != nil {
		return err
	}
	return hbinWriteOptHTTPMessageMetrics(w, msg.Metrics)
}

func hbinReadOptHTTPMessage(r io.Reader) (*HTTPMessage, error) {
	var present uint8
	if err := binary.Read(r, binary.BigEndian, &present); err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	proto, err := hbinReadString(r)
	if err != nil {
		return nil, err
	}
	headerFields, headersTruncated, headerOrderUnavailable, err := hbinReadHeaderFields(r)
	if err != nil {
		return nil, err
	}
	trailerFields, trailersTruncated, trailerOrderUnavailable, err := hbinReadHeaderFields(r)
	if err != nil {
		return nil, err
	}
	metrics, err := hbinReadOptHTTPMessageMetrics(r)
	if err != nil {
		return nil, err
	}
	return &HTTPMessage{
		Proto:                   proto,
		HeaderFields:            headerFields,
		HeadersTruncated:        headersTruncated,
		HeaderOrderUnavailable:  headerOrderUnavailable,
		TrailerFields:           trailerFields,
		TrailersTruncated:       trailersTruncated,
		TrailerOrderUnavailable: trailerOrderUnavailable,
		Metrics:                 metrics,
	}, nil
}

func hbinWriteOptHTTPMessageMetrics(w io.Writer, metrics *HTTPMessageMetrics) error {
	if metrics == nil {
		return binary.Write(w, binary.BigEndian, uint8(0))
	}
	if err := binary.Write(w, binary.BigEndian, uint8(1)); err != nil {
		return err
	}
	for _, value := range []int64{
		metrics.StartedAtMicros,
		metrics.EndedAtMicros,
		metrics.HeaderSize,
		metrics.BodySize,
	} {
		if err := binary.Write(w, binary.BigEndian, value); err != nil {
			return err
		}
	}
	return hbinWriteString(w, string(metrics.State))
}

func hbinReadOptHTTPMessageMetrics(r io.Reader) (*HTTPMessageMetrics, error) {
	var present uint8
	if err := binary.Read(r, binary.BigEndian, &present); err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	metrics := new(HTTPMessageMetrics)
	for _, target := range []*int64{
		&metrics.StartedAtMicros,
		&metrics.EndedAtMicros,
		&metrics.HeaderSize,
		&metrics.BodySize,
	} {
		if err := binary.Read(r, binary.BigEndian, target); err != nil {
			return nil, err
		}
	}
	state, err := hbinReadString(r)
	if err != nil {
		return nil, err
	}
	metrics.State = HTTPMessageState(state)
	return metrics, nil
}

func hbinWriteHeaderFields(
	w io.Writer,
	fields []HTTPHeaderField,
	truncated bool,
	orderUnavailable bool,
) error {
	present := uint8(0)
	if fields != nil {
		present = 1
	}
	if err := binary.Write(w, binary.BigEndian, present); err != nil {
		return err
	}
	if fields != nil {
		if len(fields) > hbinMaxHeaderFields {
			return fmt.Errorf("hbin: too many header fields: %d", len(fields))
		}
		if err := binary.Write(w, binary.BigEndian, uint32(len(fields))); err != nil {
			return err
		}
		for _, field := range fields {
			if err := hbinWriteString(w, field.Name); err != nil {
				return err
			}
			if err := hbinWriteString(w, field.Value); err != nil {
				return err
			}
		}
	}
	truncatedValue := uint8(0)
	if truncated {
		truncatedValue = 1
	}
	if err := binary.Write(w, binary.BigEndian, truncatedValue); err != nil {
		return err
	}
	orderUnavailableValue := uint8(0)
	if orderUnavailable {
		orderUnavailableValue = 1
	}
	return binary.Write(w, binary.BigEndian, orderUnavailableValue)
}

func hbinReadHeaderFields(r io.Reader) ([]HTTPHeaderField, bool, bool, error) {
	var present uint8
	if err := binary.Read(r, binary.BigEndian, &present); err != nil {
		return nil, false, false, err
	}
	if present > 1 {
		return nil, false, false, fmt.Errorf("hbin: invalid header fields presence %d", present)
	}
	var fields []HTTPHeaderField
	if present == 1 {
		var count uint32
		if err := binary.Read(r, binary.BigEndian, &count); err != nil {
			return nil, false, false, err
		}
		if count > hbinMaxHeaderFields {
			return nil, false, false, fmt.Errorf("hbin: too many header fields: %d", count)
		}
		fields = make([]HTTPHeaderField, int(count))
		for index := range fields {
			name, err := hbinReadString(r)
			if err != nil {
				return nil, false, false, err
			}
			value, err := hbinReadString(r)
			if err != nil {
				return nil, false, false, err
			}
			fields[index] = HTTPHeaderField{Name: name, Value: value}
		}
	}
	var truncated uint8
	if err := binary.Read(r, binary.BigEndian, &truncated); err != nil {
		return nil, false, false, err
	}
	if truncated > 1 {
		return nil, false, false, fmt.Errorf("hbin: invalid header fields truncated value %d", truncated)
	}
	var orderUnavailable uint8
	if err := binary.Read(r, binary.BigEndian, &orderUnavailable); err != nil {
		return nil, false, false, err
	}
	if orderUnavailable > 1 {
		return nil, false, false, fmt.Errorf(
			"hbin: invalid header order unavailable value %d",
			orderUnavailable,
		)
	}
	return fields, truncated == 1, orderUnavailable == 1, nil
}

func hbinWriteOptTrafficError(w io.Writer, e *TrafficError) error {
	if e == nil {
		return binary.Write(w, binary.BigEndian, uint8(0))
	}
	if err := binary.Write(w, binary.BigEndian, uint8(1)); err != nil {
		return err
	}
	if err := hbinWriteTime(w, e.Timestamp); err != nil {
		return err
	}
	return hbinWriteString(w, e.Error)
}

func hbinReadOptTrafficError(r io.Reader) (*TrafficError, error) {
	var present uint8
	if err := binary.Read(r, binary.BigEndian, &present); err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	t, err := hbinReadTime(r)
	if err != nil {
		return nil, err
	}
	msg, err := hbinReadString(r)
	if err != nil {
		return nil, err
	}
	return &TrafficError{Timestamp: t, Error: msg}, nil
}

func hbinWriteWebSocketMessage(w io.Writer, msg *WebSocketMessage) error {
	dir := uint8(0) // "send"
	if msg.Direction == "receive" {
		dir = 1
	}
	if err := binary.Write(w, binary.BigEndian, dir); err != nil {
		return err
	}
	mt := uint8(0) // "text"
	if msg.MsgType == "binary" {
		mt = 1
	}
	if err := binary.Write(w, binary.BigEndian, mt); err != nil {
		return err
	}
	if err := hbinWriteBytes(w, string2Bytes(msg.Data)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, int32(msg.DataSize)); err != nil {
		return err
	}
	return hbinWriteOptTrafficError(w, msg.Error)
}

func hbinReadWebSocketMessage(r io.Reader) (*WebSocketMessage, error) {
	var dir uint8
	if err := binary.Read(r, binary.BigEndian, &dir); err != nil {
		return nil, err
	}
	var mt uint8
	if err := binary.Read(r, binary.BigEndian, &mt); err != nil {
		return nil, err
	}
	dataBytes, err := hbinReadBytes(r)
	if err != nil {
		return nil, err
	}
	var dataSize int32
	if err := binary.Read(r, binary.BigEndian, &dataSize); err != nil {
		return nil, err
	}
	e, err := hbinReadOptTrafficError(r)
	if err != nil {
		return nil, err
	}
	msg := &WebSocketMessage{
		Data:     string(dataBytes),
		DataSize: int(dataSize),
		Error:    e,
	}
	if dir == 0 {
		msg.Direction = "send"
	} else {
		msg.Direction = "receive"
	}
	if mt&1 == 0 {
		msg.MsgType = "text"
	} else {
		msg.MsgType = "binary"
	}
	return msg, nil
}

// hbinWriteString writes u32(len) + UTF-8 bytes.
func hbinWriteString(w io.Writer, s string) error {
	b := string2Bytes(s)
	if err := binary.Write(w, binary.BigEndian, uint32(len(b))); err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	_, err := w.Write(b)
	return err
}

// hbinReadString reads a string written by hbinWriteString.
func hbinReadString(r io.Reader) (string, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return "", err
	}
	if length == 0 {
		return "", nil
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// hbinWriteBytes writes u32(len) + raw bytes.
func hbinWriteBytes(w io.Writer, b []byte) error {
	if err := binary.Write(w, binary.BigEndian, uint32(len(b))); err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	_, err := w.Write(b)
	return err
}

func hbinWriteFromReader(w io.Writer, r io.Reader, size int64) error {
	if r == nil || size < 0 {
		size = 0
	}
	if err := binary.Write(w, binary.BigEndian, uint32(size)); err != nil {
		return err
	}
	if size == 0 {
		return nil
	}
	_, err := io.Copy(w, r)
	return err
}

// hbinReadBytes reads a byte slice written by hbinWriteBytes.
func hbinReadBytes(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}
	if length == 0 {
		return nil, nil
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func hbinReadBytesAsReader(r io.Reader) (io.Reader, int64, error) {
	data, err := hbinReadBytes(r)
	if err != nil {
		return nil, 0, err
	}
	return bytes.NewReader(data), int64(len(data)), nil
}

// hbinWriteTime writes a time.Time as i64 Unix nanoseconds.
func hbinWriteTime(w io.Writer, t time.Time) error {
	return binary.Write(w, binary.BigEndian, t.UnixNano())
}

// hbinReadTime reads a time.Time from i64 Unix nanoseconds.
func hbinReadTime(r io.Reader) (time.Time, error) {
	var ns int64
	if err := binary.Read(r, binary.BigEndian, &ns); err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, ns).UTC(), nil
}

func string2Bytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func bytes2String(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// hbinWriteStringSlice writes u16(count) + each string via hbinWriteString.
func hbinWriteStringSlice(w io.Writer, ss []string) error {
	if err := binary.Write(w, binary.BigEndian, uint16(len(ss))); err != nil {
		return err
	}
	for _, s := range ss {
		if err := hbinWriteString(w, s); err != nil {
			return err
		}
	}
	return nil
}

// hbinReadStringSlice reads a string slice written by hbinWriteStringSlice.
func hbinReadStringSlice(r io.Reader) ([]string, error) {
	var count uint16
	if err := binary.Read(r, binary.BigEndian, &count); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	ss := make([]string, 0, count)
	for i := uint16(0); i < count; i++ {
		s, err := hbinReadString(r)
		if err != nil {
			return nil, err
		}
		ss = append(ss, s)
	}
	return ss, nil
}

func hbinWriteOptTLSState(w io.Writer, t *TLSState) error {
	if t == nil {
		return binary.Write(w, binary.BigEndian, uint8(0))
	}
	if err := binary.Write(w, binary.BigEndian, uint8(1)); err != nil {
		return err
	}
	if err := hbinWriteString(w, t.ServerName); err != nil {
		return err
	}
	if err := hbinWriteStringSlice(w, t.SupportedALPN); err != nil {
		return err
	}
	if err := hbinWriteStringSlice(w, t.SupportedVersion); err != nil {
		return err
	}
	if err := hbinWriteStringSlice(w, t.SupportedCipherSuites); err != nil {
		return err
	}
	if err := hbinWriteString(w, t.SelectedALPN); err != nil {
		return err
	}
	if err := hbinWriteString(w, t.SelectedVersion); err != nil {
		return err
	}
	return hbinWriteString(w, t.SelectedCipherSuite)
}

func hbinReadOptTLSState(r io.Reader) (*TLSState, error) {
	var present uint8
	if err := binary.Read(r, binary.BigEndian, &present); err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	t := new(TLSState)
	var err error
	if t.ServerName, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if t.SupportedALPN, err = hbinReadStringSlice(r); err != nil {
		return nil, err
	}
	if t.SupportedVersion, err = hbinReadStringSlice(r); err != nil {
		return nil, err
	}
	if t.SupportedCipherSuites, err = hbinReadStringSlice(r); err != nil {
		return nil, err
	}
	if t.SelectedALPN, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if t.SelectedVersion, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if t.SelectedCipherSuite, err = hbinReadString(r); err != nil {
		return nil, err
	}
	return t, nil
}

func hbinWriteOptServerCertificate(w io.Writer, c *ServerCertificate) error {
	if c == nil {
		return binary.Write(w, binary.BigEndian, uint8(0))
	}
	if err := binary.Write(w, binary.BigEndian, uint8(1)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, int32(c.Version)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, c.NotBeforeMicros); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, c.NotAfterMicros); err != nil {
		return err
	}
	for _, s := range []string{
		c.SerialNumber, c.SignatureAlgorithm,
		c.Sha1Fingerprint, c.Sha256Fingerprint,
	} {
		if err := hbinWriteString(w, s); err != nil {
			return err
		}
	}
	if err := hbinWriteOptPkixName(w, c.Subject); err != nil {
		return err
	}
	if err := hbinWriteOptPkixName(w, c.Issuer); err != nil {
		return err
	}
	if err := hbinWriteStringSlice(w, c.DNSNames); err != nil {
		return err
	}
	return hbinWriteStringSlice(w, c.IPAddresses)
}

func hbinReadOptServerCertificate(r io.Reader) (*ServerCertificate, error) {
	var present uint8
	if err := binary.Read(r, binary.BigEndian, &present); err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	c := new(ServerCertificate)
	var version int32
	if err := binary.Read(r, binary.BigEndian, &version); err != nil {
		return nil, err
	}
	c.Version = int(version)
	var err error
	if err := binary.Read(r, binary.BigEndian, &c.NotBeforeMicros); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.BigEndian, &c.NotAfterMicros); err != nil {
		return nil, err
	}
	if c.SerialNumber, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if c.SignatureAlgorithm, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if c.Sha1Fingerprint, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if c.Sha256Fingerprint, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if c.Subject, err = hbinReadOptPkixName(r); err != nil {
		return nil, err
	}
	if c.Issuer, err = hbinReadOptPkixName(r); err != nil {
		return nil, err
	}
	if c.DNSNames, err = hbinReadStringSlice(r); err != nil {
		return nil, err
	}
	if c.IPAddresses, err = hbinReadStringSlice(r); err != nil {
		return nil, err
	}
	return c, nil
}

func hbinWriteOptPkixName(w io.Writer, n *PkixName) error {
	if n == nil {
		return binary.Write(w, binary.BigEndian, uint8(0))
	}
	if err := binary.Write(w, binary.BigEndian, uint8(1)); err != nil {
		return err
	}
	for _, ss := range [][]string{
		n.Country, n.Organization, n.OrganizationalUnit,
		n.Locality, n.Province,
		n.StreetAddress, n.PostalCode,
	} {
		if err := hbinWriteStringSlice(w, ss); err != nil {
			return err
		}
	}
	if err := hbinWriteString(w, n.SerialNumber); err != nil {
		return err
	}
	return hbinWriteString(w, n.CommonName)
}

func hbinReadOptPkixName(r io.Reader) (*PkixName, error) {
	var present uint8
	if err := binary.Read(r, binary.BigEndian, &present); err != nil {
		return nil, err
	}
	if present == 0 {
		return nil, nil
	}
	n := new(PkixName)
	var err error
	for _, dst := range []*[]string{
		&n.Country, &n.Organization, &n.OrganizationalUnit,
		&n.Locality, &n.Province,
		&n.StreetAddress, &n.PostalCode,
	} {
		if *dst, err = hbinReadStringSlice(r); err != nil {
			return nil, err
		}
	}
	if n.SerialNumber, err = hbinReadString(r); err != nil {
		return nil, err
	}
	if n.CommonName, err = hbinReadString(r); err != nil {
		return nil, err
	}
	return n, nil
}
