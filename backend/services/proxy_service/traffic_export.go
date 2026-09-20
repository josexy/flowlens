package proxyservice

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type TrafficExportKind string

const (
	TrafficExportRequestMessage  TrafficExportKind = "request"
	TrafficExportRequestHeaders  TrafficExportKind = "request-headers"
	TrafficExportRequestBody     TrafficExportKind = "request-body"
	TrafficExportResponseMessage TrafficExportKind = "response"
	TrafficExportResponseHeaders TrafficExportKind = "response-headers"
	TrafficExportResponseBody    TrafficExportKind = "response-body"
	TrafficExportExchange        TrafficExportKind = "request-response"
	TrafficExportCSV             TrafficExportKind = "csv"
)

type TrafficExportRequest struct {
	CaptureGeneration *uint64           `json:"captureGeneration,omitempty"`
	Kind              TrafficExportKind `json:"kind"`
	Path              string            `json:"path"`
	TargetType        string            `json:"targetType"`
	TrafficIDs        []uint64          `json:"trafficIds,omitempty"`
}

type TrafficExportResult struct {
	Path            string `json:"path"`
	Exported        int    `json:"exported"`
	Skipped         int    `json:"skipped"`
	HeadersDegraded bool   `json:"headersDegraded"`
}

func (kind TrafficExportKind) sides() (request, response, body, headers bool) {
	switch kind {
	case TrafficExportRequestMessage:
		return true, false, true, true
	case TrafficExportRequestHeaders:
		return true, false, false, true
	case TrafficExportRequestBody:
		return true, false, true, false
	case TrafficExportResponseMessage:
		return false, true, true, true
	case TrafficExportResponseHeaders:
		return false, true, false, true
	case TrafficExportResponseBody:
		return false, true, true, false
	case TrafficExportExchange:
		return true, true, true, true
	default:
		return false, false, false, false
	}
}

func validateTrafficExport(request TrafficExportRequest, count int) error {
	requestSide, responseSide, _, _ := request.Kind.sides()
	if !requestSide && !responseSide && request.Kind != TrafficExportCSV {
		return errors.New("export: unsupported format")
	}
	if strings.TrimSpace(request.Path) == "" || !filepath.IsAbs(request.Path) {
		return errors.New("export: an absolute output path is required")
	}
	if request.TargetType != "file" && request.TargetType != "directory" {
		return errors.New("export: unsupported target type")
	}
	if request.Kind == TrafficExportCSV && request.TargetType != "file" {
		return errors.New("export: CSV requires a file target")
	}
	if request.Kind != TrafficExportCSV && request.TargetType == "file" && (count != 1 || len(request.TrafficIDs) == 0) {
		return errors.New("export: a single file requires one selected entry")
	}
	return nil
}

func WriteTrafficExport(
	ctx context.Context,
	request TrafficExportRequest,
	count int,
	entryAt func(int) (*TrafficEntry, error),
	loadBodies func(*TrafficEntry, string) (HARBody, HARBody, error),
) (TrafficExportResult, error) {
	result := TrafficExportResult{}
	if err := validateTrafficExport(request, count); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var temporary, target string
	var file *os.File
	var output *bufio.Writer
	var csvOutput *csv.Writer
	var err error
	if request.TargetType == "directory" {
		temporary, err = os.MkdirTemp(request.Path, ".flowlens-export-*")
		if err != nil {
			return result, err
		}
		target = filepath.Join(request.Path, strings.TrimPrefix(filepath.Base(temporary), "."))
	} else {
		target = filepath.Clean(request.Path)
		file, err = os.CreateTemp(filepath.Dir(target), ".flowlens-export-*.tmp")
		if err != nil {
			return result, err
		}
		temporary = file.Name()
		output = bufio.NewWriterSize(file, 64*1024)
	}
	defer func() {
		if file != nil {
			_ = file.Close()
		}
		if temporary != "" {
			_ = os.RemoveAll(temporary)
		}
	}()
	if request.Kind == TrafficExportCSV {
		if _, err = output.WriteString("\xef\xbb\xbf"); err != nil {
			return result, err
		}
		csvOutput = csv.NewWriter(output)
		csvOutput.UseCRLF = true
		if err = csvOutput.Write([]string{"id", "method", "host", "path", "process", "status", "type", "destination", "protocol", "durationMicros", "sizeBytes"}); err != nil {
			return result, err
		}
	}
	spoolDirectory := filepath.Dir(temporary)
	if request.TargetType == "directory" {
		spoolDirectory = temporary
	}
	for index := 0; index < count; index++ {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		entry, readErr := entryAt(index)
		if readErr != nil {
			return result, readErr
		}
		if entry == nil {
			result.Skipped++
			continue
		}
		if csvOutput != nil {
			if err = csvOutput.Write(trafficCSVRow(entry)); err != nil {
				return result, err
			}
			result.Exported++
			continue
		}
		if !trafficExportEligible(entry, request.Kind) {
			result.Skipped++
			continue
		}
		var singleOutput io.Writer
		if output != nil {
			singleOutput = output
		}
		written, writeErr := writeTrafficExportEntry(ctx, request.Kind, entry, index, temporary, singleOutput, spoolDirectory, loadBodies, &result)
		if writeErr != nil {
			return result, writeErr
		}
		if written {
			result.Exported++
		} else {
			result.Skipped++
		}
	}
	if csvOutput != nil {
		csvOutput.Flush()
		if err = csvOutput.Error(); err != nil {
			return result, err
		}
	}
	if result.Exported == 0 {
		return result, nil
	}
	if file != nil {
		if err = output.Flush(); err != nil {
			return result, err
		}
		if err = file.Sync(); err != nil {
			return result, err
		}
		if err = file.Close(); err != nil {
			return result, err
		}
		file = nil
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if request.TargetType == "directory" {
		if _, statErr := os.Lstat(target); !os.IsNotExist(statErr) {
			return result, fmt.Errorf("export: output directory already exists or cannot be inspected: %s", target)
		}
		err = os.Rename(temporary, target)
	} else {
		err = atomicReplaceHARFile(temporary, target)
	}
	if err != nil {
		return result, err
	}
	temporary = ""
	result.Path = target
	return result, nil
}

func trafficExportEligible(entry *TrafficEntry, kind TrafficExportKind) bool {
	if !harSupports(entry) {
		return false
	}
	requestSide, responseSide, body, _ := kind.sides()
	messages := []*HTTPMessage{}
	if requestSide {
		if entry.Request == nil || logicalHTTPRequestStartLine(entry) == "" {
			return false
		}
		messages = append(messages, entry.Request)
	}
	if responseSide {
		if entry.Response == nil || logicalHTTPResponseStartLine(entry) == "" {
			return false
		}
		messages = append(messages, entry.Response)
	}
	if body {
		for _, message := range messages {
			if message.Metrics == nil || message.Metrics.State != HTTPMessageStateCompleted {
				return false
			}
		}
	}
	return true
}

func writeTrafficExportEntry(
	ctx context.Context, kind TrafficExportKind, entry *TrafficEntry, index int,
	directory string, singleOutput io.Writer, spoolDirectory string,
	loadBodies func(*TrafficEntry, string) (HARBody, HARBody, error), result *TrafficExportResult,
) (bool, error) {
	requestSide, responseSide, body, headers := kind.sides()
	var requestBody, responseBody HARBody
	if body {
		var err error
		requestBody, responseBody, err = loadBodies(entry, spoolDirectory)
		defer requestBody.close()
		defer responseBody.close()
		if err != nil {
			return false, err
		}
		if requestSide && !requestBody.Available || responseSide && !responseBody.Available {
			return false, nil
		}
	}
	var file *os.File
	var buffered *bufio.Writer
	if singleOutput == nil {
		filename := fmt.Sprintf("%06d-%d-%s%s", index+1, entry.ID, kind, trafficExportExtension(kind, entry))
		var err error
		file, err = os.OpenFile(filepath.Join(directory, filename), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return false, err
		}
		defer file.Close()
		buffered = bufio.NewWriterSize(file, 64*1024)
		singleOutput = buffered
	}
	output := trafficExportContextWriter{ctx: ctx, output: singleOutput}
	writeSide := func(message *HTTPMessage, payload HARBody, request bool) error {
		if headers {
			if message.HeadersTruncated || message.HeaderOrderUnavailable || message.HeaderFields == nil {
				result.HeadersDegraded = true
			}
			if err := writeTrafficHTTPHead(output, entry, request); err != nil {
				return err
			}
		}
		if body {
			if payload.Reader != nil {
				_, err := io.CopyN(output, payload.Reader, payload.Size)
				return err
			}
			_, err := output.Write(payload.Data)
			return err
		}
		return nil
	}
	if requestSide {
		if err := writeSide(entry.Request, requestBody, true); err != nil {
			return false, err
		}
	}
	if requestSide && responseSide {
		if _, err := io.WriteString(output, "\r\n\r\n"); err != nil {
			return false, err
		}
	}
	if responseSide {
		if err := writeSide(entry.Response, responseBody, false); err != nil {
			return false, err
		}
	}
	if file != nil {
		if err := buffered.Flush(); err != nil {
			return false, err
		}
		if err := file.Sync(); err != nil {
			return false, err
		}
		if err := file.Close(); err != nil {
			return false, err
		}
	}
	return true, nil
}

type trafficExportContextWriter struct {
	ctx    context.Context
	output io.Writer
}

func (writer trafficExportContextWriter) Write(data []byte) (int, error) {
	if err := writer.ctx.Err(); err != nil {
		return 0, err
	}
	return writer.output.Write(data)
}

func writeTrafficHTTPHead(output io.Writer, entry *TrafficEntry, request bool) error {
	message := entry.Response
	var startLine string
	if request {
		message = entry.Request
		startLine = logicalHTTPRequestStartLine(entry)
	} else {
		startLine = logicalHTTPResponseStartLine(entry)
	}
	writeLine := func(line string) error {
		_, err := io.WriteString(output, string([]rune(line))+"\r\n")
		return err
	}
	if err := writeLine(startLine); err != nil {
		return err
	}
	http2 := logicalHeaderProtocol(message.Proto) == "HTTP/2.0"
	if http2 {
		for _, field := range message.HeaderFields {
			if strings.EqualFold(field.Name, ":authority") {
				if err := writeLine("host: " + field.Value); err != nil {
					return err
				}
			}
		}
	}
	for _, field := range message.HeaderFields {
		name := field.Name
		if http2 {
			lower := strings.ToLower(name)
			if lower == ":authority" || lower == ":scheme" || request && (lower == ":method" || lower == ":path") || !request && lower == ":status" {
				continue
			}
			name = strings.TrimPrefix(name, ":")
		}
		if err := writeLine(name + ": " + field.Value); err != nil {
			return err
		}
	}
	return writeLine("")
}

func trafficExportExtension(kind TrafficExportKind, entry *TrafficEntry) string {
	_, responseSide, body, headers := kind.sides()
	if headers {
		if body {
			return ".http"
		}
		return ".txt"
	}
	message := entry.Request
	if responseSide {
		message = entry.Response
	}
	contentType, _, _ := mime.ParseMediaType(firstHeaderFieldValue(message.HeaderFields, "Content-Type"))
	switch {
	case contentType == "application/json" || strings.HasSuffix(contentType, "+json"):
		return ".json"
	case contentType == "text/plain":
		return ".txt"
	case contentType == "text/html":
		return ".html"
	case contentType == "image/svg+xml":
		return ".svg"
	case contentType == "text/xml" || contentType == "application/xml" || strings.HasSuffix(contentType, "+xml"):
		return ".xml"
	case contentType == "image/jpeg":
		return ".jpg"
	case contentType == "image/png":
		return ".png"
	case contentType == "image/gif":
		return ".gif"
	case contentType == "image/webp":
		return ".webp"
	case contentType == "application/pdf":
		return ".pdf"
	case contentType == "application/zip":
		return ".zip"
	default:
		return ".bin"
	}
}

func trafficCSVRow(entry *TrafficEntry) []string {
	process, destination, protocol := "", "", ""
	host, path, method := entry.Host, entry.Path, entry.Method
	kind := strings.ToUpper(entry.Type)
	status := ""
	if entry.Error != nil {
		status = "ERR"
	} else if entry.StatusCode > 0 {
		status = strconv.Itoa(entry.StatusCode)
	}
	if entry.Metadata != nil {
		destination = entry.Metadata.RemoteDestinationAddr
		if address, _, err := net.SplitHostPort(destination); err == nil {
			destination = address
		}
		if info := entry.Metadata.Process; info != nil {
			process = string(info.Status)
			if info.Status == ProcessStatusResolved {
				process = info.DisplayName
				if process == "" {
					process = info.ProcessName
				}
				if process == "" && info.PID > 0 {
					process = "PID " + strconv.FormatUint(uint64(info.PID), 10)
				}
			}
		}
	}
	if entry.Response != nil {
		protocol = entry.Response.Proto
	}
	if protocol == "" && entry.Request != nil {
		protocol = entry.Request.Proto
	}
	duration, size := int64(-1), int64(-1)
	if entry.Request != nil && entry.Response != nil && entry.Request.Metrics != nil && entry.Response.Metrics != nil {
		requestMetrics, responseMetrics := entry.Request.Metrics, entry.Response.Metrics
		if requestMetrics.StartedAtMicros >= 0 && responseMetrics.EndedAtMicros >= requestMetrics.StartedAtMicros {
			duration = responseMetrics.EndedAtMicros - requestMetrics.StartedAtMicros
		}
		if !entry.Request.HeadersTruncated && !entry.Response.HeadersTruncated && requestMetrics.HeaderSize >= 0 && requestMetrics.BodySize >= 0 && responseMetrics.HeaderSize >= 0 && responseMetrics.BodySize >= 0 {
			size = requestMetrics.HeaderSize + requestMetrics.BodySize + responseMetrics.HeaderSize + responseMetrics.BodySize
		}
	}
	if entry.Type == "tcp" {
		method, path, protocol = "", "", "TCP"
		if entry.RawTCP != nil {
			host = entry.RawTCP.HostPort
			if entry.RawTCP.TLS {
				kind, protocol = "TCP/TLS", "TLS"
			}
		}
		if entry.Metadata != nil && entry.Metadata.TLS != nil && entry.Metadata.TLS.SelectedALPN != "" {
			protocol = entry.Metadata.TLS.SelectedALPN
		}
	}
	return []string{strconv.FormatUint(entry.ID, 10), method, host, path, process, status, kind, destination, protocol, strconv.FormatInt(duration, 10), strconv.FormatInt(size, 10)}
}
