package proxyservice

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bodycache "github.com/josexy/flowlens/backend/pkg/body_cache"
	settingservice "github.com/josexy/flowlens/backend/services/setting_service"
)

func TestGetTrafficBodyViewPreservesMultipartRequestBytesForRecovery(t *testing.T) {
	t.Parallel()

	fileBytes := []byte{0x00, 0xff, 0x10, 0x41, 0x42}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("title", "hello"); err != nil {
		t.Fatalf("WriteField title: %v", err)
	}
	filePart, err := writer.CreateFormFile("upload", "raw.bin")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err = filePart.Write(fileBytes); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	entry := &TrafficEntry{
		ID:   100,
		Type: "http",
		Request: &HTTPMessage{
			HeaderFields: []HTTPHeaderField{{
				Name: "Content-Type", Value: writer.FormDataContentType(),
			}},
		},
	}
	svc.storeTrafficEntry(entry)
	svc.trafficBodies.Store(entry.ID, &TrafficBodies{
		requestBody: testCapturedBodyFromBytes(t, entry.ID, bodycache.KindRequest, body.Bytes()),
	})

	bodyView, err := svc.GetTrafficBodyView(entry.ID)
	if err != nil {
		t.Fatalf("GetTrafficBodyView returned error: %v", err)
	}
	if bodyView.RequestBodyEncoding != "base64" {
		t.Fatalf("RequestBodyEncoding = %q, want base64", bodyView.RequestBodyEncoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(bodyView.RequestBody)
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}
	if !bytes.Equal(decoded, body.Bytes()) {
		t.Fatal("decoded multipart bytes do not match original request body")
	}

	result, err := svc.RecoverRequestBodyForEditing("http://example.com/upload", entry.Request.HeaderFields, *bodyView)
	if err != nil {
		t.Fatalf("RecoverRequestBodyForEditing returned error: %v", err)
	}
	if result.BodyType != SendRequestBodyTypeFormData {
		t.Fatalf("BodyType = %q, want %q", result.BodyType, SendRequestBodyTypeFormData)
	}
	if len(result.FormData) != 2 {
		t.Fatalf("FormData length = %d, want 2", len(result.FormData))
	}
	if got := result.FormData[1]; got.ItemType != "file" || got.File == nil {
		t.Fatalf("file part = %+v, want recovered file", got)
	}
	assertFileBytes(t, result.FormData[1].File.Path, fileBytes)
}

func TestRecoverRequestBodyForEditingMultipartTextAndFile(t *testing.T) {
	t.Parallel()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("title", "hello"); err != nil {
		t.Fatalf("WriteField title: %v", err)
	}
	filePart, err := writer.CreateFormFile("upload", "note.txt")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err = filePart.Write([]byte("file-content")); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	textPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": []string{`form-data; name="tail"`},
	})
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if _, err = textPart.Write([]byte("last")); err != nil {
		t.Fatalf("write tail part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	result, err := svc.RecoverRequestBodyForEditing(
		"http://example.com/upload",
		testHeaderFields(map[string][]string{"Content-Type": {writer.FormDataContentType()}}),
		TrafficBodyView{RequestBody: body.String()},
	)
	if err != nil {
		t.Fatalf("RecoverRequestBodyForEditing returned error: %v", err)
	}

	if result.BodyType != SendRequestBodyTypeFormData {
		t.Fatalf("BodyType = %q, want %q", result.BodyType, SendRequestBodyTypeFormData)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none", result.Warnings)
	}
	if len(result.FormData) != 3 {
		t.Fatalf("FormData length = %d, want 3", len(result.FormData))
	}

	if got := result.FormData[0]; got.Name != "title" || got.ItemType != "text" || got.Value != "hello" {
		t.Fatalf("first part = %+v, want text title=hello", got)
	}
	if got := result.FormData[1]; got.Name != "upload" || got.ItemType != "file" || got.File == nil {
		t.Fatalf("second part = %+v, want file upload", got)
	}
	if got := result.FormData[2]; got.Name != "tail" || got.ItemType != "text" || got.Value != "last" {
		t.Fatalf("third part = %+v, want text tail=last", got)
	}

	recoveredFile := result.FormData[1].File
	if recoveredFile.Name != "note.txt" {
		t.Fatalf("recovered file name = %q, want note.txt", recoveredFile.Name)
	}
	assertFileBytes(t, recoveredFile.Path, []byte("file-content"))
	assertRequestDraftCachePath(t, recoveredFile.Path)
}

func TestRecoverRequestBodyForEditingBase64BinaryFile(t *testing.T) {
	t.Parallel()

	bodyBytes := []byte{0x00, 0x01, 0x02, 0x03}
	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	result, err := svc.RecoverRequestBodyForEditing(
		"http://example.com/files/archive.bin",
		testHeaderFields(map[string][]string{"Content-Type": {"application/octet-stream"}}),
		TrafficBodyView{
			RequestBody:         base64.StdEncoding.EncodeToString(bodyBytes),
			RequestBodyEncoding: "base64",
		},
	)
	if err != nil {
		t.Fatalf("RecoverRequestBodyForEditing returned error: %v", err)
	}

	if result.BodyType != SendRequestBodyTypeFile {
		t.Fatalf("BodyType = %q, want %q", result.BodyType, SendRequestBodyTypeFile)
	}
	if result.File == nil {
		t.Fatal("File = nil, want recovered file")
	}
	if result.File.Name != "archive.bin" {
		t.Fatalf("recovered file name = %q, want archive.bin", result.File.Name)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none", result.Warnings)
	}
	assertFileBytes(t, result.File.Path, bodyBytes)
	assertRequestDraftCachePath(t, result.File.Path)
}

func TestRecoverRequestBodyForEditingTreatsNonBinaryFileLikeUploadAsFile(t *testing.T) {
	t.Parallel()

	bodyBytes := []byte("plain file payload")
	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	result, err := svc.RecoverRequestBodyForEditing(
		"http://example.com/uploads/readme.log",
		testHeaderFields(map[string][]string{"Content-Type": {"text/markdown"}}),
		TrafficBodyView{
			RequestBody: string(bodyBytes),
		},
	)
	if err != nil {
		t.Fatalf("RecoverRequestBodyForEditing returned error: %v", err)
	}

	if result.BodyType != SendRequestBodyTypeFile {
		t.Fatalf("BodyType = %q, want %q", result.BodyType, SendRequestBodyTypeFile)
	}
	if result.File == nil {
		t.Fatal("File = nil, want recovered file")
	}
	if result.File.Name != "readme.log" {
		t.Fatalf("recovered file name = %q, want readme.log", result.File.Name)
	}
	if result.Text != "" {
		t.Fatalf("Text = %q, want empty for file recovery", result.Text)
	}
	assertFileBytes(t, result.File.Path, bodyBytes)
	assertRequestDraftCachePath(t, result.File.Path)
}

func TestRecoverRequestBodyForEditingURLEncoded(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	result, err := svc.RecoverRequestBodyForEditing(
		"http://example.com/form",
		testHeaderFields(map[string][]string{"Content-Type": {"application/x-www-form-urlencoded; charset=utf-8"}}),
		TrafficBodyView{
			RequestBody: "first=one&space=a+value&encoded=a%26b&empty=&bare",
		},
	)
	if err != nil {
		t.Fatalf("RecoverRequestBodyForEditing returned error: %v", err)
	}

	if result.BodyType != SendRequestBodyTypeURLEncoded {
		t.Fatalf("BodyType = %q, want %q", result.BodyType, SendRequestBodyTypeURLEncoded)
	}
	if result.Text != "" {
		t.Fatalf("Text = %q, want empty", result.Text)
	}
	if len(result.URLEncoded) != 5 {
		t.Fatalf("URLEncoded length = %d, want 5", len(result.URLEncoded))
	}

	want := []SendRequestURLEncodedItem{
		{Enabled: true, Name: "first", Value: "one"},
		{Enabled: true, Name: "space", Value: "a value"},
		{Enabled: true, Name: "encoded", Value: "a&b"},
		{Enabled: true, Name: "empty", Value: ""},
		{Enabled: true, Name: "bare", Value: ""},
	}
	for index, item := range want {
		got := result.URLEncoded[index]
		if got == nil || *got != item {
			t.Fatalf("URLEncoded[%d] = %+v, want %+v", index, got, item)
		}
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none", result.Warnings)
	}
}

func TestRecoverRequestBodyForEditingInvalidBase64FallsBackWithWarning(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	result, err := svc.RecoverRequestBodyForEditing(
		"http://example.com/upload.bin",
		testHeaderFields(map[string][]string{"Content-Type": {"application/octet-stream"}}),
		TrafficBodyView{
			RequestBody:         "%%%not-base64%%%",
			RequestBodyEncoding: "base64",
		},
	)
	if err != nil {
		t.Fatalf("RecoverRequestBodyForEditing returned error: %v", err)
	}

	if result.BodyType != SendRequestBodyTypeFile {
		t.Fatalf("BodyType = %q, want %q", result.BodyType, SendRequestBodyTypeFile)
	}
	if result.File != nil {
		t.Fatalf("File = %+v, want nil when decode fails", result.File)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("Warnings = empty, want decode warning")
	}
}

func TestRecoverRequestBodyForEditingTruncatedMultipartWarnsAndKeepsPartialData(t *testing.T) {
	t.Parallel()

	body := "--demo\r\nContent-Disposition: form-data; name=\"field\"\r\n\r\nvalue\r\n--demo"
	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	result, err := svc.RecoverRequestBodyForEditing(
		"http://example.com/upload",
		testHeaderFields(map[string][]string{"Content-Type": {"multipart/form-data; boundary=demo"}}),
		TrafficBodyView{RequestBody: body},
	)
	if err != nil {
		t.Fatalf("RecoverRequestBodyForEditing returned error: %v", err)
	}

	if result.BodyType != SendRequestBodyTypeFormData {
		t.Fatalf("BodyType = %q, want %q", result.BodyType, SendRequestBodyTypeFormData)
	}
	if len(result.FormData) != 1 {
		t.Fatalf("FormData length = %d, want 1 partial part", len(result.FormData))
	}
	if got := result.FormData[0]; got.Name != "field" || got.Value != "value" {
		t.Fatalf("partial part = %+v, want field=value", got)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("Warnings = empty, want multipart parse warning")
	}
}

func TestRecoverRequestBodyForEditingMissingBoundaryWarns(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	result, err := svc.RecoverRequestBodyForEditing(
		"http://example.com/upload",
		testHeaderFields(map[string][]string{"Content-Type": {"multipart/form-data"}}),
		TrafficBodyView{RequestBody: "ignored"},
	)
	if err != nil {
		t.Fatalf("RecoverRequestBodyForEditing returned error: %v", err)
	}

	if result.BodyType != SendRequestBodyTypeFormData {
		t.Fatalf("BodyType = %q, want %q", result.BodyType, SendRequestBodyTypeFormData)
	}
	if len(result.FormData) != 0 {
		t.Fatalf("FormData length = %d, want 0", len(result.FormData))
	}
	if len(result.Warnings) == 0 {
		t.Fatal("Warnings = empty, want boundary warning")
	}
}

func TestRecoverRequestBodyForEditingEmptyBodyWithoutContentTypeUsesNone(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	result, err := svc.RecoverRequestBodyForEditing(
		"http://example.com/items",
		nil,
		TrafficBodyView{},
	)
	if err != nil {
		t.Fatalf("RecoverRequestBodyForEditing returned error: %v", err)
	}
	if result.BodyType != SendRequestBodyTypeNone {
		t.Fatalf("BodyType = %q, want %q", result.BodyType, SendRequestBodyTypeNone)
	}
	if result.Text != "" {
		t.Fatalf("Text = %q, want empty", result.Text)
	}
}

func TestRecoverRequestBodyForEditingBodyWithoutContentTypeUsesText(t *testing.T) {
	t.Parallel()

	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	result, err := svc.RecoverRequestBodyForEditing(
		"http://example.com/items",
		nil,
		TrafficBodyView{RequestBody: "plain body"},
	)
	if err != nil {
		t.Fatalf("RecoverRequestBodyForEditing returned error: %v", err)
	}
	if result.BodyType != SendRequestBodyTypeText {
		t.Fatalf("BodyType = %q, want %q", result.BodyType, SendRequestBodyTypeText)
	}
	if result.Text != "plain body" {
		t.Fatalf("Text = %q, want plain body", result.Text)
	}
}

func TestRecoverRequestBodyForEditingBase64BodyWithoutContentTypeUsesText(t *testing.T) {
	t.Parallel()

	bodyBytes := []byte{0x00, 0x01, 0x02, 0x03}
	svc := newTestProxyService(t, &settingservice.ProxyConfig{})
	result, err := svc.RecoverRequestBodyForEditing(
		"http://example.com/items",
		nil,
		TrafficBodyView{
			RequestBody:         base64.StdEncoding.EncodeToString(bodyBytes),
			RequestBodyEncoding: "base64",
		},
	)
	if err != nil {
		t.Fatalf("RecoverRequestBodyForEditing returned error: %v", err)
	}
	if result.BodyType != SendRequestBodyTypeText {
		t.Fatalf("BodyType = %q, want %q", result.BodyType, SendRequestBodyTypeText)
	}
	if result.Text != bytes2String(bodyBytes) {
		t.Fatalf("Text = %q, want decoded body string", result.Text)
	}
	if result.File != nil {
		t.Fatalf("File = %+v, want nil", result.File)
	}
}

func TestGetRequestDraftCacheStoragePathCreatesStableDirectory(t *testing.T) {
	t.Parallel()

	first, err := getRequestDraftCacheStoragePath()
	if err != nil {
		t.Fatalf("getRequestDraftCacheStoragePath first call: %v", err)
	}
	second, err := getRequestDraftCacheStoragePath()
	if err != nil {
		t.Fatalf("getRequestDraftCacheStoragePath second call: %v", err)
	}

	if first != second {
		t.Fatalf("request draft cache path mismatch: %q vs %q", first, second)
	}
	info, err := os.Stat(first)
	if err != nil {
		t.Fatalf("stat request draft cache path: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("request draft cache path %q is not a directory", first)
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("file %q content mismatch", path)
	}
}

func assertRequestDraftCachePath(t *testing.T, path string) {
	t.Helper()

	cacheDir, err := getRequestDraftCacheStoragePath()
	if err != nil {
		t.Fatalf("getRequestDraftCacheStoragePath: %v", err)
	}
	rel, err := filepath.Rel(cacheDir, path)
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("path %q is not under request draft cache %q", path, cacheDir)
	}
}
