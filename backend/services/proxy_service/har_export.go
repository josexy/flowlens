package proxyservice

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/josexy/flowlens/backend/pkg/compresspool"
)

const (
	harVersion     = "1.2"
	harCreatorName = "FlowLens"
)

var errHARExportAborted = errors.New("har: export aborted")

var harExportMu sync.Mutex

// AcquireHARExport serializes HAR exports across current capture and history
// services. This prevents two large exports from multiplying body-spool memory
// and disk pressure. The returned release function must be deferred.
func AcquireHARExport() func() {
	harExportMu.Lock()
	return harExportMu.Unlock
}

// HARBody is the decoded body captured by FlowLens. Encoding is either empty
// or "base64" and describes how the body must be represented to text-only
// consumers. Available distinguishes a captured empty body from a missing body
// cache file.
type HARBody struct {
	Data      []byte
	Reader    io.ReadCloser
	Size      int64
	Encoding  string
	Available bool
}

func (b *HARBody) close() {
	if b != nil && b.Reader != nil {
		_ = b.Reader.Close()
		b.Reader = nil
	}
}

func (b HARBody) size() int64 {
	if b.Reader != nil || b.Size != 0 {
		return b.Size
	}
	return int64(len(b.Data))
}

func (b HARBody) hasData() bool {
	return b.size() > 0
}

// HARExportEntry combines a traffic snapshot with the separately stored body
// bytes needed to produce one HAR entry.
type HARExportEntry struct {
	Entry        *TrafficEntry
	RequestBody  HARBody
	ResponseBody HARBody
}

// HARWriteResult reports how much of an export request was represented.
// MissingBodies counts individual request/response bodies, not traffic rows.
type HARWriteResult struct {
	Exported      int `json:"exported"`
	Skipped       int `json:"skipped"`
	MissingBodies int `json:"missingBodies"`
}

// DecodeTrafficHARBodies reads the decoded request and response payloads from
// an HBIN entry body. The returned data is raw bytes; Encoding only guides the
// HAR text/base64 representation.
func DecodeTrafficHARBodies(r io.Reader) (HARBody, HARBody, error) {
	bvi, err := hbinReadEntryBody(r)
	if err != nil {
		return HARBody{}, HARBody{}, err
	}
	defer bvi.closeReqBodyReaderSafely()
	defer bvi.closeRspBodyReaderSafely()

	requestBody, err := readHARBody(
		bvi.RequestBodyReader,
		bvi.RequestBodyEncoding,
		!bvi.RequestBodyUnavailable,
	)
	if err != nil {
		return HARBody{}, HARBody{}, fmt.Errorf("har: read request body: %w", err)
	}
	responseBody, err := readHARBody(
		bvi.ResponseBodyReader,
		bvi.ResponseBodyEncoding,
		!bvi.ResponseBodyUnavailable,
	)
	if err != nil {
		return HARBody{}, HARBody{}, fmt.Errorf("har: read response body: %w", err)
	}
	return requestBody, responseBody, nil
}

const harBodyMemorySpoolLimit = 1 << 20

// DecodeTrafficHARBodyReaders decodes only the request and response bodies from
// an HBIN body record. Large decoded bodies are spooled to temporary files and
// WebSocket frames that follow the handshake bodies are deliberately left
// undecoded because HAR does not represent them. The returned readers are
// owned by HARFileWriter.WriteEntry and are closed there.
func DecodeTrafficHARBodyReaders(r io.Reader, temporaryDir string) (HARBody, HARBody, error) {
	var compressedSize uint32
	if err := binary.Read(r, binary.BigEndian, &compressedSize); err != nil {
		return HARBody{}, HARBody{}, fmt.Errorf("hbin: read body size: %w", err)
	}
	decoder, err := compresspool.AcquireZstdDecoder(io.LimitReader(r, int64(compressedSize)))
	if err != nil {
		return HARBody{}, HARBody{}, fmt.Errorf("hbin: zstd decompress: %w", err)
	}
	defer compresspool.ReleaseZstdDecoder(decoder)

	request, err := decodeHARBodyReader(decoder, temporaryDir, "request")
	if err != nil {
		return HARBody{}, HARBody{}, err
	}
	response, err := decodeHARBodyReader(decoder, temporaryDir, "response")
	if err != nil {
		request.close()
		return HARBody{}, HARBody{}, err
	}
	return request, response, nil
}

func decodeHARBodyReader(r io.Reader, temporaryDir, side string) (HARBody, error) {
	var encodingValue uint8
	if err := binary.Read(r, binary.BigEndian, &encodingValue); err != nil {
		return HARBody{}, fmt.Errorf("hbin: read %s encoding: %w", side, err)
	}
	if encodingValue > 2 {
		return HARBody{}, fmt.Errorf("hbin: invalid %s body encoding %d", side, encodingValue)
	}
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return HARBody{}, fmt.Errorf("hbin: read %s body size: %w", side, err)
	}
	body := HARBody{Size: int64(length), Available: encodingValue != 2}
	if encodingValue == 1 {
		body.Encoding = "base64"
	}
	if length == 0 {
		return body, nil
	}
	reader, validUTF8, err := spoolHARBody(r, int64(length), temporaryDir)
	if err != nil {
		return HARBody{}, fmt.Errorf("hbin: read %s body: %w", side, err)
	}
	if body.Encoding == "" && !validUTF8 {
		body.Encoding = "base64"
	}
	body.Reader = reader
	return body, nil
}

func spoolHARBody(r io.Reader, size int64, temporaryDir string) (io.ReadCloser, bool, error) {
	if size <= harBodyMemorySpoolLimit {
		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, false, err
		}
		return io.NopCloser(bytes.NewReader(data)), utf8.Valid(data), nil
	}
	file, err := os.CreateTemp(temporaryDir, ".flowlens-har-body-*.tmp")
	if err != nil {
		return nil, false, err
	}
	path := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(path)
	}
	if _, err := io.CopyN(file, r, size); err != nil {
		cleanup()
		return nil, false, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, false, err
	}
	validUTF8, err := readerContainsValidUTF8(file)
	if err != nil {
		cleanup()
		return nil, false, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, false, err
	}
	return &removeOnCloseHARBody{File: file, path: path}, validUTF8, nil
}

func readerContainsValidUTF8(r io.Reader) (bool, error) {
	buffer := make([]byte, 32*1024+utf8.UTFMax)
	carry := 0
	for {
		n, err := r.Read(buffer[carry : len(buffer)-utf8.UTFMax])
		total := carry + n
		validEnd := total
		if err == nil && total > 0 {
			start := total - 1
			for start > 0 && !utf8.RuneStart(buffer[start]) {
				start--
			}
			if !utf8.FullRune(buffer[start:total]) {
				validEnd = start
			}
		}
		if !utf8.Valid(buffer[:validEnd]) {
			return false, nil
		}
		carry = copy(buffer, buffer[validEnd:total])
		if err != nil {
			if err != io.EOF {
				return false, err
			}
			return carry == 0, nil
		}
	}
}

type removeOnCloseHARBody struct {
	*os.File
	path string
}

func (r *removeOnCloseHARBody) Close() error {
	closeErr := r.File.Close()
	removeErr := os.Remove(r.path)
	if closeErr != nil {
		return closeErr
	}
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	return nil
}

func readHARBody(reader io.Reader, encoding string, available bool) (HARBody, error) {
	body := HARBody{Encoding: encoding, Available: available}
	if reader == nil {
		return body, nil
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return HARBody{}, err
	}
	body.Data = data
	return body, nil
}

// HARFileWriter incrementally writes one HAR entry at a time. The destination
// path is only replaced after Close successfully writes and syncs the complete
// document. A failed WriteEntry or an Abort removes the temporary file.
type HARFileWriter struct {
	target      string
	temporary   string
	file        *os.File
	output      *bufio.Writer
	result      HARWriteResult
	connections harConnectionIDs
	finished    bool
	err         error
}

type harEnvelope struct {
	Log harLog `json:"log"`
}

type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Entries []harEntry `json:"entries"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            int64       `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}    `json:"cache"`
	Timings         harTimings  `json:"timings"`
	ServerIPAddress string      `json:"serverIPAddress,omitempty"`
	Connection      string      `json:"connection,omitempty"`
	Comment         string      `json:"comment"`
	CTime           *int64      `json:"_ctime,omitempty"`
	STime           *int64      `json:"_stime,omitempty"`
	ServerAddress   string      `json:"_serverAddress,omitempty"`
	ServerFamily    *int        `json:"_serverAddressFamily,omitempty"`
	ServerPort      *int        `json:"_serverPort,omitempty"`
	ClientAddress   string      `json:"_clientAddress,omitempty"`
	ClientFamily    *int        `json:"_clientAddressFamily,omitempty"`
	ClientPort      *int        `json:"_clientPort,omitempty"`
	App             *harApp     `json:"_app,omitempty"`
	Error           string      `json:"_error,omitempty"`
}

type harRequest struct {
	Method         string         `json:"method"`
	URL            string         `json:"url"`
	HTTPVersion    string         `json:"httpVersion"`
	Cookies        []harCookie    `json:"cookies"`
	Headers        []harNameValue `json:"headers"`
	QueryString    []harNameValue `json:"queryString"`
	PostData       *harPostData   `json:"postData,omitempty"`
	HeadersSize    int64          `json:"headersSize"`
	BodySize       int64          `json:"bodySize"`
	Status         string         `json:"_status"`
	StartTimestamp int64          `json:"_startTimestamp"`
	EndTimestamp   int64          `json:"_endTimestamp"`
}

type harResponse struct {
	Status         int            `json:"status"`
	StatusText     string         `json:"statusText"`
	HTTPVersion    string         `json:"httpVersion"`
	Cookies        []harCookie    `json:"cookies"`
	Headers        []harNameValue `json:"headers"`
	Content        harContent     `json:"content"`
	RedirectURL    string         `json:"redirectURL"`
	HeadersSize    int64          `json:"headersSize"`
	BodySize       int64          `json:"bodySize"`
	StatusValue    string         `json:"_status"`
	StartTimestamp int64          `json:"_startTimestamp"`
	EndTimestamp   int64          `json:"_endTimestamp"`
}

type harContent struct {
	Size        int64   `json:"size"`
	Compression *int64  `json:"compression,omitempty"`
	MimeType    string  `json:"mimeType"`
	Text        *string `json:"text,omitempty"`
	Encoding    string  `json:"encoding,omitempty"`
}

type harPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Encoding string `json:"_encoding,omitempty"`
}

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harCookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Path     string `json:"path,omitempty"`
	Domain   string `json:"domain,omitempty"`
	Expires  string `json:"expires,omitempty"`
	HTTPOnly bool   `json:"httpOnly,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
}

type harTimings struct {
	Send    int64 `json:"send"`
	Wait    int64 `json:"wait"`
	Receive int64 `json:"receive"`
}

type harApp struct {
	Name string `json:"name,omitempty"`
	ID   string `json:"id,omitempty"`
	Path string `json:"path,omitempty"`
	PID  uint32 `json:"pid,omitempty"`
}

type harConnectionIDs struct {
	local map[string]int
}

// WriteHAR writes compact HAR 1.2 JSON using FlowLens's logical size profile.
// It does not close w.
func WriteHAR(w io.Writer, creatorVersion string, inputs []HARExportEntry) (HARWriteResult, error) {
	if w == nil {
		return HARWriteResult{}, errors.New("har: nil writer")
	}
	if creatorVersion == "" {
		creatorVersion = "dev"
	}

	output := bufio.NewWriterSize(w, 64*1024)
	if err := writeHARDocumentHeader(output, creatorVersion); err != nil {
		return HARWriteResult{}, fmt.Errorf("har: encode header: %w", err)
	}
	result := HARWriteResult{}
	connections := harConnectionIDs{local: make(map[string]int)}
	for _, input := range inputs {
		if err := writeHARInput(output, input, &connections, &result); err != nil {
			return HARWriteResult{}, fmt.Errorf("har: encode entry: %w", err)
		}
	}
	if _, err := io.WriteString(output, "]}}\n"); err != nil {
		return HARWriteResult{}, fmt.Errorf("har: encode footer: %w", err)
	}
	if err := output.Flush(); err != nil {
		return HARWriteResult{}, fmt.Errorf("har: flush: %w", err)
	}
	return result, nil
}

// WriteHARFile writes through a same-directory temporary file and atomically
// replaces path. The destination is left untouched if encoding or syncing
// fails.
func WriteHARFile(path string, creatorVersion string, inputs []HARExportEntry) (HARWriteResult, error) {
	writer, err := NewHARFileWriter(path, creatorVersion)
	if err != nil {
		return HARWriteResult{}, err
	}
	defer func() {
		_ = writer.Abort()
	}()
	for _, input := range inputs {
		if err := writer.WriteEntry(input); err != nil {
			return HARWriteResult{}, err
		}
	}
	return writer.Close()
}

// NewHARFileWriter starts an incremental HAR export in a same-directory
// temporary file. Call Close to commit it or Abort to discard it.
func NewHARFileWriter(path, creatorVersion string) (*HARFileWriter, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("har: empty output path")
	}
	path = filepath.Clean(path)
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return nil, fmt.Errorf("har: create temporary file: %w", err)
	}
	w := &HARFileWriter{
		target:    path,
		temporary: tmp.Name(),
		file:      tmp,
		output:    bufio.NewWriterSize(tmp, 64*1024),
		connections: harConnectionIDs{
			local: make(map[string]int),
		},
	}
	if creatorVersion == "" {
		creatorVersion = "dev"
	}
	if err := writeHARDocumentHeader(w.output, creatorVersion); err != nil {
		_ = w.fail(fmt.Errorf("har: write document header: %w", err))
		return nil, w.err
	}
	return w, nil
}

// WriteEntry appends one supported HTTP(S) or WebSocket handshake. Unsupported
// records, including raw TCP tunnels, are counted and skipped without emitting
// a JSON array item.
func (w *HARFileWriter) WriteEntry(input HARExportEntry) error {
	if w == nil {
		return errors.New("har: nil file writer")
	}
	if w.finished {
		if w.err != nil {
			return w.err
		}
		return errors.New("har: file writer is closed")
	}
	if err := writeHARInput(w.output, input, &w.connections, &w.result); err != nil {
		return w.fail(fmt.Errorf("har: write entry: %w", err))
	}
	if err := w.output.Flush(); err != nil {
		return w.fail(fmt.Errorf("har: flush entry: %w", err))
	}
	return nil
}

// Close completes, syncs, and atomically commits the HAR file.
func (w *HARFileWriter) Close() (HARWriteResult, error) {
	if w == nil {
		return HARWriteResult{}, errors.New("har: nil file writer")
	}
	if w.finished {
		return w.result, w.err
	}
	if _, err := io.WriteString(w.output, "]}}\n"); err != nil {
		return w.result, w.fail(fmt.Errorf("har: write document footer: %w", err))
	}
	if err := w.output.Flush(); err != nil {
		return w.result, w.fail(fmt.Errorf("har: flush document: %w", err))
	}
	if err := w.file.Sync(); err != nil {
		return w.result, w.fail(fmt.Errorf("har: sync temporary file: %w", err))
	}
	if err := w.file.Close(); err != nil {
		w.file = nil
		return w.result, w.fail(fmt.Errorf("har: close temporary file: %w", err))
	}
	w.file = nil
	if err := atomicReplaceHARFile(w.temporary, w.target); err != nil {
		return w.result, w.fail(fmt.Errorf("har: replace destination: %w", err))
	}
	w.temporary = ""
	w.finished = true
	return w.result, nil
}

// Abort discards an unfinished export. It is safe to defer immediately after
// NewHARFileWriter; after a successful Close it is a no-op.
func (w *HARFileWriter) Abort() error {
	if w == nil || w.finished {
		return nil
	}
	var cleanupErr error
	if w.file != nil {
		cleanupErr = w.file.Close()
		w.file = nil
	}
	if w.temporary != "" {
		if err := os.Remove(w.temporary); err != nil && !errors.Is(err, os.ErrNotExist) && cleanupErr == nil {
			cleanupErr = err
		}
		w.temporary = ""
	}
	w.finished = true
	w.err = errHARExportAborted
	if cleanupErr != nil {
		w.err = fmt.Errorf("har: abort cleanup: %w", cleanupErr)
		return w.err
	}
	return nil
}

func (w *HARFileWriter) fail(err error) error {
	if w.err == nil {
		w.err = err
	}
	if w.finished {
		return w.err
	}
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	if w.temporary != "" {
		_ = os.Remove(w.temporary)
		w.temporary = ""
	}
	w.finished = true
	return w.err
}

func writeHARDocumentHeader(w io.Writer, creatorVersion string) error {
	creatorJSON, err := json.Marshal(harCreator{Name: harCreatorName, Version: creatorVersion})
	if err != nil {
		return err
	}
	_, err = io.WriteString(
		w,
		`{"log":{"version":"`+harVersion+`","creator":`+string(creatorJSON)+`,"entries":[`,
	)
	return err
}

func writeHARInput(
	w io.Writer,
	input HARExportEntry,
	connections *harConnectionIDs,
	result *HARWriteResult,
) error {
	defer input.RequestBody.close()
	defer input.ResponseBody.close()
	if !harSupports(input.Entry) {
		result.Skipped++
		return nil
	}
	entry, missing := makeHAREntry(input, connections)
	if result.Exported != 0 {
		if _, err := io.WriteString(w, ","); err != nil {
			return err
		}
	}
	if err := writeHAREntryJSON(w, entry, input.RequestBody, input.ResponseBody); err != nil {
		return err
	}
	result.Exported++
	result.MissingBodies += missing
	return nil
}

func writeHAREntryJSON(w io.Writer, entry harEntry, requestBody, responseBody HARBody) error {
	if _, err := io.WriteString(w, "{"); err != nil {
		return err
	}
	first := true
	if err := writeHARJSONField(w, &first, "startedDateTime", entry.StartedDateTime); err != nil {
		return err
	}
	if err := writeHARJSONField(w, &first, "time", entry.Time); err != nil {
		return err
	}
	if err := writeHARJSONFieldName(w, &first, "request"); err != nil {
		return err
	}
	if err := writeHARRequestJSON(w, entry.Request, requestBody); err != nil {
		return err
	}
	if err := writeHARJSONFieldName(w, &first, "response"); err != nil {
		return err
	}
	if err := writeHARResponseJSON(w, entry.Response, responseBody); err != nil {
		return err
	}
	for _, field := range []struct {
		name  string
		value any
	}{
		{"cache", entry.Cache},
		{"timings", entry.Timings},
		{"comment", entry.Comment},
	} {
		if err := writeHARJSONField(w, &first, field.name, field.value); err != nil {
			return err
		}
	}
	for _, field := range []struct {
		name    string
		value   any
		present bool
	}{
		{"serverIPAddress", entry.ServerIPAddress, entry.ServerIPAddress != ""},
		{"connection", entry.Connection, entry.Connection != ""},
		{"_ctime", entry.CTime, entry.CTime != nil},
		{"_stime", entry.STime, entry.STime != nil},
		{"_serverAddress", entry.ServerAddress, entry.ServerAddress != ""},
		{"_serverAddressFamily", entry.ServerFamily, entry.ServerFamily != nil},
		{"_serverPort", entry.ServerPort, entry.ServerPort != nil},
		{"_clientAddress", entry.ClientAddress, entry.ClientAddress != ""},
		{"_clientAddressFamily", entry.ClientFamily, entry.ClientFamily != nil},
		{"_clientPort", entry.ClientPort, entry.ClientPort != nil},
		{"_app", entry.App, entry.App != nil},
		{"_error", entry.Error, entry.Error != ""},
	} {
		if field.present {
			if err := writeHARJSONField(w, &first, field.name, field.value); err != nil {
				return err
			}
		}
	}
	_, err := io.WriteString(w, "}")
	return err
}

func writeHARRequestJSON(w io.Writer, request harRequest, body HARBody) error {
	if _, err := io.WriteString(w, "{"); err != nil {
		return err
	}
	first := true
	for _, field := range []struct {
		name  string
		value any
	}{
		{"method", request.Method},
		{"url", request.URL},
		{"httpVersion", request.HTTPVersion},
		{"cookies", request.Cookies},
		{"headers", request.Headers},
		{"queryString", request.QueryString},
	} {
		if err := writeHARJSONField(w, &first, field.name, field.value); err != nil {
			return err
		}
	}
	if body.Available && (body.hasData() || request.BodySize > 0) {
		if err := writeHARJSONFieldName(w, &first, "postData"); err != nil {
			return err
		}
		if err := writeHARBodyObject(
			w,
			firstHARHeaderValue(request.Headers, "Content-Type"),
			body,
			"_encoding",
		); err != nil {
			return err
		}
	}
	for _, field := range []struct {
		name  string
		value any
	}{
		{"headersSize", request.HeadersSize},
		{"bodySize", request.BodySize},
		{"_status", request.Status},
		{"_startTimestamp", request.StartTimestamp},
		{"_endTimestamp", request.EndTimestamp},
	} {
		if err := writeHARJSONField(w, &first, field.name, field.value); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "}")
	return err
}

func writeHARResponseJSON(w io.Writer, response harResponse, body HARBody) error {
	if _, err := io.WriteString(w, "{"); err != nil {
		return err
	}
	first := true
	for _, field := range []struct {
		name  string
		value any
	}{
		{"status", response.Status},
		{"statusText", response.StatusText},
		{"httpVersion", response.HTTPVersion},
		{"cookies", response.Cookies},
		{"headers", response.Headers},
	} {
		if err := writeHARJSONField(w, &first, field.name, field.value); err != nil {
			return err
		}
	}
	if err := writeHARJSONFieldName(w, &first, "content"); err != nil {
		return err
	}
	if err := writeHARContentJSON(w, response.Content, body); err != nil {
		return err
	}
	for _, field := range []struct {
		name  string
		value any
	}{
		{"redirectURL", response.RedirectURL},
		{"headersSize", response.HeadersSize},
		{"bodySize", response.BodySize},
		{"_status", response.StatusValue},
		{"_startTimestamp", response.StartTimestamp},
		{"_endTimestamp", response.EndTimestamp},
	} {
		if err := writeHARJSONField(w, &first, field.name, field.value); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "}")
	return err
}

func writeHARContentJSON(w io.Writer, content harContent, body HARBody) error {
	if _, err := io.WriteString(w, "{"); err != nil {
		return err
	}
	first := true
	if err := writeHARJSONField(w, &first, "size", content.Size); err != nil {
		return err
	}
	if content.Compression != nil {
		if err := writeHARJSONField(w, &first, "compression", content.Compression); err != nil {
			return err
		}
	}
	if err := writeHARJSONField(w, &first, "mimeType", content.MimeType); err != nil {
		return err
	}
	if body.Available {
		if err := writeHARBodyField(w, &first, "text", body); err != nil {
			return err
		}
		if encoding := harBodyEncoding(body); encoding != "" {
			if err := writeHARJSONField(w, &first, "encoding", encoding); err != nil {
				return err
			}
		}
	}
	_, err := io.WriteString(w, "}")
	return err
}

func writeHARBodyObject(w io.Writer, mimeType string, body HARBody, encodingName string) error {
	if _, err := io.WriteString(w, "{"); err != nil {
		return err
	}
	first := true
	if err := writeHARJSONField(w, &first, "mimeType", mimeType); err != nil {
		return err
	}
	if err := writeHARBodyField(w, &first, "text", body); err != nil {
		return err
	}
	if encoding := harBodyEncoding(body); encoding != "" {
		if err := writeHARJSONField(w, &first, encodingName, encoding); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "}")
	return err
}

func writeHARBodyField(w io.Writer, first *bool, name string, body HARBody) error {
	if err := writeHARJSONFieldName(w, first, name); err != nil {
		return err
	}
	if _, err := io.WriteString(w, `"`); err != nil {
		return err
	}
	reader := io.Reader(bytes.NewReader(body.Data))
	if body.Reader != nil {
		reader = body.Reader
	}
	var err error
	if harBodyEncoding(body) == "base64" {
		encoder := base64.NewEncoder(base64.StdEncoding, w)
		_, err = io.CopyBuffer(encoder, reader, make([]byte, 32*1024))
		if closeErr := encoder.Close(); err == nil {
			err = closeErr
		}
	} else {
		err = writeEscapedHARText(w, reader)
	}
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, `"`)
	return err
}

func harBodyEncoding(body HARBody) string {
	if body.Encoding == "base64" || body.Reader == nil && !utf8.Valid(body.Data) {
		return "base64"
	}
	return ""
}

func writeEscapedHARText(w io.Writer, r io.Reader) error {
	buffer := make([]byte, 32*1024+utf8.UTFMax)
	carry := 0
	for {
		n, readErr := r.Read(buffer[carry : len(buffer)-utf8.UTFMax])
		total := carry + n
		processEnd := total
		if readErr == nil && total > 0 {
			start := total - 1
			for start > 0 && !utf8.RuneStart(buffer[start]) {
				start--
			}
			if !utf8.FullRune(buffer[start:total]) {
				processEnd = start
			}
		}
		if err := writeEscapedHARTextChunk(w, buffer[:processEnd]); err != nil {
			return err
		}
		carry = copy(buffer, buffer[processEnd:total])
		if readErr != nil {
			if readErr != io.EOF {
				return readErr
			}
			if carry > 0 {
				return writeEscapedHARTextChunk(w, buffer[:carry])
			}
			return nil
		}
	}
}

func writeEscapedHARTextChunk(w io.Writer, data []byte) error {
	start := 0
	for index := 0; index < len(data); {
		value := data[index]
		if value >= utf8.RuneSelf {
			runeValue, size := utf8.DecodeRune(data[index:])
			if runeValue == utf8.RuneError && size == 1 {
				if err := writeHAREscapedSpan(w, data, start, index, `\ufffd`); err != nil {
					return err
				}
				index++
				start = index
				continue
			}
			if runeValue == '\u2028' || runeValue == '\u2029' {
				escape := `\u2028`
				if runeValue == '\u2029' {
					escape = `\u2029`
				}
				if err := writeHAREscapedSpan(w, data, start, index, escape); err != nil {
					return err
				}
				index += size
				start = index
				continue
			}
			index += size
			continue
		}

		escape := ""
		switch value {
		case '\\', '"':
			escape = `\` + string(value)
		case '\b':
			escape = `\b`
		case '\f':
			escape = `\f`
		case '\n':
			escape = `\n`
		case '\r':
			escape = `\r`
		case '\t':
			escape = `\t`
		case '<':
			escape = `\u003c`
		case '>':
			escape = `\u003e`
		case '&':
			escape = `\u0026`
		default:
			if value < 0x20 {
				const hexadecimal = "0123456789abcdef"
				escape = `\u00` + string([]byte{hexadecimal[value>>4], hexadecimal[value&0x0f]})
			}
		}
		if escape == "" {
			index++
			continue
		}
		if err := writeHAREscapedSpan(w, data, start, index, escape); err != nil {
			return err
		}
		index++
		start = index
	}
	if start < len(data) {
		_, err := w.Write(data[start:])
		return err
	}
	return nil
}

func writeHAREscapedSpan(w io.Writer, data []byte, start, end int, escape string) error {
	if start < end {
		if _, err := w.Write(data[start:end]); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, escape)
	return err
}

func writeHARJSONField(w io.Writer, first *bool, name string, value any) error {
	if err := writeHARJSONFieldName(w, first, name); err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = w.Write(encoded)
	return err
}

func writeHARJSONFieldName(w io.Writer, first *bool, name string) error {
	separator := `"` + name + `":`
	if !*first {
		separator = `,"` + name + `":`
	}
	*first = false
	_, err := io.WriteString(w, separator)
	return err
}

func harSupports(entry *TrafficEntry) bool {
	if entry == nil {
		return false
	}
	switch strings.ToLower(entry.Type) {
	case "http", "https", "ws", "wss":
		return true
	default:
		return false
	}
}

func makeHAREntry(input HARExportEntry, connections *harConnectionIDs) (harEntry, int) {
	traffic := input.Entry
	request := traffic.Request
	response := traffic.Response
	reqStart, _ := harMessageTimestamps(request)
	_, rspEnd := harMessageTimestamps(response)

	entry := harEntry{
		StartedDateTime: harStartedDateTime(traffic, reqStart),
		Time:            harTotalTime(reqStart, rspEnd),
		Request:         makeHARRequest(traffic, request, input.RequestBody),
		Response:        makeHARResponse(traffic, response, input.ResponseBody),
		Timings:         harTimings{Send: -1, Wait: -1, Receive: -1},
		Comment:         "",
	}
	if traffic.Error != nil {
		entry.Error = traffic.Error.Error
	}
	fillHARConnection(&entry, traffic.Metadata, connections)
	fillHARApp(&entry, traffic.Metadata)

	missing := 0
	if harBodyExpected(request) && !input.RequestBody.Available {
		missing++
	}
	if harBodyExpected(response) && !input.ResponseBody.Available {
		missing++
	}
	return entry, missing
}

func makeHARRequest(traffic *TrafficEntry, message *HTTPMessage, body HARBody) harRequest {
	start, end := harMessageTimestamps(message)
	fields := harHeaders(message)
	request := harRequest{
		Method:         traffic.Method,
		URL:            traffic.URL,
		HTTPVersion:    harHTTPVersion(message, nil),
		Cookies:        harRequestCookies(fields),
		Headers:        fields,
		QueryString:    harQueryString(traffic.URL),
		HeadersSize:    harHeaderSize(message),
		BodySize:       harBodySize(message, body),
		Status:         harMessageStatus(message, traffic.Error != nil),
		StartTimestamp: start,
		EndTimestamp:   end,
	}
	return request
}

func makeHARResponse(traffic *TrafficEntry, message *HTTPMessage, body HARBody) harResponse {
	start, end := harMessageTimestamps(message)
	fields := harHeaders(message)
	bodySize := harBodySize(message, body)
	content := harContent{
		Size:     -1,
		MimeType: firstHeaderFieldValue(messageFields(message), "Content-Type"),
	}
	if body.Available {
		content.Size = body.size()
		if harMessageCompleted(message) && bodySize >= 0 {
			compression := content.Size - bodySize
			content.Compression = &compression
		}
	}

	return harResponse{
		Status:         traffic.StatusCode,
		StatusText:     harStatusText(traffic.StatusCode, traffic.Status, harHTTPVersion(message, traffic.Request)),
		HTTPVersion:    harHTTPVersion(message, traffic.Request),
		Cookies:        harResponseCookies(fields),
		Headers:        fields,
		Content:        content,
		RedirectURL:    firstHARHeaderValue(fields, "Location"),
		HeadersSize:    harHeaderSize(message),
		BodySize:       bodySize,
		StatusValue:    harMessageStatus(message, traffic.Error != nil),
		StartTimestamp: start,
		EndTimestamp:   end,
	}
}

func harHeaders(message *HTTPMessage) []harNameValue {
	fields := messageFields(message)
	headers := make([]harNameValue, len(fields))
	for i, field := range fields {
		headers[i] = harNameValue{Name: field.Name, Value: field.Value}
	}
	return headers
}

func messageFields(message *HTTPMessage) []HTTPHeaderField {
	if message == nil {
		return nil
	}
	return message.HeaderFields
}

func harHeaderSize(message *HTTPMessage) int64 {
	if message == nil || message.HeadersTruncated {
		return -1
	}
	if message.Metrics != nil {
		return message.Metrics.HeaderSize
	}
	return logicalHARHeaderSize(message.HeaderFields)
}

func logicalHARHeaderSize(fields []HTTPHeaderField) int64 {
	var size int64
	for _, field := range fields {
		size += int64(len(field.Name) + len(field.Value) + len(": ") + len("\r\n"))
	}
	return size
}

func harBodySize(message *HTTPMessage, body HARBody) int64 {
	if message == nil {
		return -1
	}
	if !harMessageCompleted(message) {
		return -1
	}
	if message.Metrics != nil {
		return message.Metrics.BodySize
	}
	if body.Available {
		return body.size()
	}
	return -1
}

func harBodyExpected(message *HTTPMessage) bool {
	// Only count a missing payload when capture observed at least one entity
	// byte. A synthetic failed/canceled response has BodySize == -1, which is
	// unknown rather than evidence that a cache payload went missing.
	return message != nil && message.Metrics != nil && message.Metrics.BodySize > 0
}

func harMessageTimestamps(message *HTTPMessage) (int64, int64) {
	if message == nil || message.Metrics == nil {
		return -1, -1
	}
	return message.Metrics.StartedAtMicros, message.Metrics.EndedAtMicros
}

func harMessageStatus(message *HTTPMessage, failed bool) string {
	if message != nil && message.Metrics != nil {
		if state := string(message.Metrics.State); state != "" {
			return state
		}
		if message.Metrics.EndedAtMicros >= 0 {
			return "completed"
		}
	}
	if failed {
		return "failed"
	}
	return "pending"
}

func harMessageCompleted(message *HTTPMessage) bool {
	return harMessageStatus(message, false) == "completed"
}

func harStartedDateTime(traffic *TrafficEntry, requestStart int64) string {
	var started time.Time
	if requestStart >= 0 {
		started = time.UnixMicro(requestStart)
	} else if traffic != nil {
		started = traffic.StartedAt
	}
	if started.IsZero() {
		started = time.Unix(0, 0)
	}
	return started.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

func harTotalTime(requestStart, responseEnd int64) int64 {
	if requestStart < 0 || responseEnd < requestStart {
		return -1
	}
	return (responseEnd - requestStart + 500) / 1000
}

func harHTTPVersion(message, fallback *HTTPMessage) string {
	proto := ""
	if message != nil {
		proto = message.Proto
	}
	if proto == "" && fallback != nil {
		proto = fallback.Proto
	}
	switch strings.ToLower(proto) {
	case "h2", "http/2":
		return "HTTP/2.0"
	case "h3", "http/3":
		return "HTTP/3.0"
	default:
		return proto
	}
}

func harStatusText(code int, status, version string) string {
	if version == "HTTP/2.0" || version == "HTTP/3.0" {
		return ""
	}
	prefix := strconv.Itoa(code)
	if after, ok := strings.CutPrefix(status, prefix); ok {
		return strings.TrimSpace(after)
	}
	if status != "" {
		return status
	}
	return stdhttp.StatusText(code)
}

func harQueryString(rawURL string) []harNameValue {
	result := make([]harNameValue, 0)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.RawQuery == "" {
		return result
	}
	for pair := range strings.SplitSeq(parsed.RawQuery, "&") {
		name, value, _ := strings.Cut(pair, "=")
		result = append(result, harNameValue{
			Name:  harQueryUnescape(name),
			Value: harQueryUnescape(value),
		})
	}
	return result
}

func harQueryUnescape(value string) string {
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

func harRequestCookies(headers []harNameValue) []harCookie {
	cookies := make([]harCookie, 0)
	for _, header := range headers {
		if !strings.EqualFold(header.Name, "Cookie") {
			continue
		}
		for item := range strings.SplitSeq(header.Value, ";") {
			name, value, found := strings.Cut(strings.TrimSpace(item), "=")
			if !found || name == "" {
				continue
			}
			cookies = append(cookies, harCookie{Name: name, Value: value})
		}
	}
	return cookies
}

func harResponseCookies(headers []harNameValue) []harCookie {
	cookies := make([]harCookie, 0)
	for _, header := range headers {
		if !strings.EqualFold(header.Name, "Set-Cookie") {
			continue
		}
		cookie, err := stdhttp.ParseSetCookie(header.Value)
		if err != nil {
			name, value, found := strings.Cut(strings.SplitN(header.Value, ";", 2)[0], "=")
			if found && strings.TrimSpace(name) != "" {
				cookies = append(cookies, harCookie{Name: strings.TrimSpace(name), Value: strings.TrimSpace(value)})
			}
			continue
		}
		expires := ""
		if !cookie.Expires.IsZero() {
			expires = cookie.Expires.UTC().Format(time.RFC3339)
		}
		cookies = append(cookies, harCookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Path:     cookie.Path,
			Domain:   cookie.Domain,
			Expires:  expires,
			HTTPOnly: cookie.HttpOnly,
			Secure:   cookie.Secure,
		})
	}
	return cookies
}

func firstHARHeaderValue(headers []harNameValue, name string) string {
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) {
			return header.Value
		}
	}
	return ""
}

func fillHARConnection(entry *harEntry, metadata *Metadata, ids *harConnectionIDs) {
	if metadata == nil {
		return
	}
	clientHost, clientPort, clientOK := harAddress(metadata.LocalSourceAddr)
	serverHost, serverPort, serverOK := harAddress(metadata.RemoteDestinationAddr)
	if clientOK {
		entry.ClientAddress = clientHost
		entry.ClientPort = &clientPort
		family := harAddressFamily(clientHost)
		entry.ClientFamily = &family
	}
	if serverOK {
		entry.ServerAddress = serverHost
		entry.ServerPort = &serverPort
		family := harAddressFamily(serverHost)
		entry.ServerFamily = &family
		if net.ParseIP(serverHost) != nil {
			entry.ServerIPAddress = serverHost
		}
	}

	if key := harConnectionKey(metadata.LocalSourceAddr, metadata.LocalDestinationAddr, metadata.LocalConnectionEstablishedAt); key != "" {
		cid, ok := ids.local[key]
		if !ok {
			cid = len(ids.local) + 1
			ids.local[key] = cid
		}
		entry.Connection = strconv.Itoa(cid)
		if !metadata.LocalConnectionEstablishedAt.IsZero() {
			ctime := metadata.LocalConnectionEstablishedAt.UnixMilli()
			entry.CTime = &ctime
		}
	}
	if !metadata.RemoteConnectionEstablishedAt.IsZero() {
		stime := metadata.RemoteConnectionEstablishedAt.UnixMilli()
		entry.STime = &stime
	}
}

func harConnectionKey(source, destination string, established time.Time) string {
	if source == "" && destination == "" && established.IsZero() {
		return ""
	}
	return source + "\x00" + destination + "\x00" + strconv.FormatInt(established.UnixNano(), 10)
}

func harAddress(value string) (string, int, bool) {
	if value == "" {
		return "", 0, false
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return "", 0, false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 {
		return "", 0, false
	}
	return host, port, true
}

func harAddressFamily(host string) int {
	ip := net.ParseIP(host)
	if ip != nil && ip.To4() == nil {
		return 1
	}
	return 0
}

func fillHARApp(entry *harEntry, metadata *Metadata) {
	if metadata == nil || metadata.Process == nil {
		return
	}
	process := metadata.Process
	app := &harApp{
		Name: process.DisplayName,
		ID:   process.AppID,
		Path: process.ExecutablePath,
		PID:  process.PID,
	}
	if app.Name == "" {
		app.Name = process.ProcessName
	}
	if app.ID == "" {
		app.ID = process.ProcessName
	}
	if app.Name == "" && app.ID == "" && app.Path == "" && app.PID == 0 {
		return
	}
	entry.App = app
}
