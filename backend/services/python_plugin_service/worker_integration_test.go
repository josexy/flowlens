package pythonpluginservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const integrationPluginSource = `from flowlens import *
import os
import time

def onRequest(context, request):
    action = context.params.get("action", "")
    if action == "sleep":
        time.sleep(10)
    if action == "short_sleep":
        time.sleep(0.15)
    if action == "crash":
        os._exit(23)
    if action == "corrupt":
        os.write(1, b"\xff\xff\xff\xff")
    if action == "exception":
        raise RuntimeError("request exploded")
    if action == "invalid":
        return "not a request"
    print("stdout from plugin")
    context.log.warning("warning from context")
    request.headers.add("X-Plugin", context.params.get("header", "yes"))
    if request.body.kind == "json":
        request.body.value["plugin"] = True
    context.shared["request"] = "complete"
    return request

def onResponse(context, response):
    response.headers.set("X-Response-Plugin", context.shared.get("request", "missing"))
    context.shared["response"] = "complete"
    return response
`

const firstPhaseAPIPluginSource = `from flowlens import *

def onRequest(context, request):
    untouched = Request("GET", "https://example.com/path?", Headers(), None)
    assert untouched.url == "https://example.com/path?"
    assert untouched.port == 443
    untouched.url = "http://example.net/other?a=1&a=2"
    assert untouched.path == "/other"
    assert untouched.port == 80
    assert untouched.queries.get_all("a") == ["1", "2"]
    untouched.queries = [QueryField("next", "value")]
    assert untouched.url == "http://example.net/other?next=value"

    assert request.url == "https://example.com:8443/api/items?tag=one&tag=two&empty=&encoded=a%2Fb"
    assert request.scheme == "https"
    assert request.host == "example.com"
    assert request.port == 8443
    assert request.path == "/api/items"
    assert request.queries.get("tag") == "one"
    assert request.queries.get_all("tag") == ["one", "two"]
    assert [(field.name, field.value) for field in request.queries] == [
        ("tag", "one"),
        ("tag", "two"),
        ("empty", ""),
        ("encoded", "a/b"),
    ]
    assert request.content_type == "application/json; charset=utf-8"

    request.queries.set("tag", "three")
    request.queries.add("tag", "four")
    request.queries.remove("empty")
    request.queries.add("space", "a b")
    request.path = "/v2/items"
    try:
        request.content_type = "application/problem+json"
    except AttributeError:
        pass
    else:
        raise AssertionError("request.content_type must be read-only")
    assert request.url == "https://example.com:8443/v2/items?tag=three&encoded=a%2Fb&tag=four&space=a+b"
    return request

def onResponse(context, response):
    assert response.protocol == "HTTP/2.0"
    assert response.status_text == "200 OK"
    assert response.content_type == "text/plain; charset=utf-8"

    request = response.request
    assert request.url == "https://example.com:8443/v2/items?tag=three&encoded=a%2Fb&tag=four&space=a+b"
    assert request.path == "/v2/items"
    assert request.queries.get_all("tag") == ["three", "four"]
    assert request.content_type == "application/json; charset=utf-8"
    assert request.body.kind == "file"
    assert request.body.value.name == "request.bin"
    assert request.body.value.size == context.params["request_size"]
    assert not hasattr(request.body.value, "path")
    request.body.write_file(context.params["snapshot_copy"])

    response.code = 404
    assert response.status_text == "404 Not Found"
    try:
        response.content_type = "application/problem+json"
    except AttributeError:
        pass
    else:
        raise AssertionError("response.content_type must be read-only")
    response.headers.set("Content-Type", "application/problem+json")
    assert response.content_type == "application/problem+json"
    return response
`

const multipartPartFilenamePluginSource = `from flowlens import *

def onRequest(context, request):
    descriptor = FileDescriptor.from_file(context.params["upload_path"])
    request.body = Body("multipart", [
        MultipartPart(
            "upload",
            file=descriptor,
            filename="renamed.bin",
        ),
    ])
    return request

def onResponse(context, response):
    request = response.request
    part = request.body.value[0]
    assert part.filename == "renamed.bin"
    assert part.file.name == "upload.txt"
    assert not hasattr(part.file, "path")
    return response
`

const bodyAPIPluginSource = `from flowlens import *
import inspect
import os

TRANSPORT_INLINE_LIMIT = 4 * 1024 * 1024
REMOVED_BODY_API = (
    "payload",
    "of",
    "streaming",
    "changed",
    "none",
    "text",
    "textFromFile",
    "binary",
    "file",
    "multiparts",
    "writeFile",
    "jsonify",
    "__len__",
    "__iter__",
    "__getitem__",
    "__setitem__",
    "__add__",
    "__radd__",
    "__str__",
    "storage",
    "fileDescriptor",
    "size",
    "is_file_backed",
    "isFileBacked",
    "read_bytes",
    "read_text",
    "read_json",
    "set_bytes",
    "set_text",
    "set_json",
    "set_file",
    "open",
    "iter_bytes",
    "iterBytes",
    "replace",
    "clear",
    "isNone",
    "isText",
    "isBinary",
    "isMultipart",
    "replace_from_file",
)

def expect_error(callback, error_type):
    try:
        callback()
    except error_type:
        return
    raise AssertionError("expected {}".format(error_type.__name__))

def onRequest(context, request):
    action = context.params.get("action", "")
    body = request.body
    if action == "inspect_inline":
        constructor_parameters = inspect.signature(Body).parameters
        assert tuple(constructor_parameters) == ("kind", "value")
        assert body.value == {"value": 1}
        for name in REMOVED_BODY_API:
            assert name not in Body.__dict__, name
        expect_error(lambda: setattr(body, "kind", "text"), AttributeError)
        expect_error(lambda: setattr(body, "value", "text"), AttributeError)
        assert not body._changed
        request.headers.set("X-Body-Check", "inline")
    elif action == "inspect_file":
        assert body.value == "file-backed text"
        for name in REMOVED_BODY_API:
            assert name not in Body.__dict__, name
        assert not body._changed
        request.headers.set("X-Body-Check", "file")
    elif action == "inspect_file_json":
        assert body.value == {"file": True}
        request.headers.set("X-Body-Check", "file-json")
    elif action == "materialize_binary":
        assert len(body.value) == context.params["expectedSize"]
        assert body.value[0] == 0
        request.headers.set("X-Body-Check", "materialized")
    elif action == "structured_value":
        assert body.value[0].name == "a"
        if body.kind == "urlencoded":
            body.value[0] = URLEncodedField("a", "1")
        elif body.kind == "multipart":
            body.value[0] = MultipartPart("a", "1")
        else:
            raise AssertionError("expected a structured Body")
        assert body.value[0].value == "1"
        request.headers.set("X-Body-Check", "structured")
    elif action == "file_descriptor_from_file":
        descriptor = FileDescriptor.from_file(context.params["source"])
        assert descriptor.path == context.params["source"]
        assert descriptor.name == os.path.basename(context.params["source"])
        assert descriptor.size == context.params["expectedSize"]
        assert descriptor.read_only
        assert "from_file" not in MultipartPart.__dict__
        request.body = Body(
            "multipart",
            [MultipartPart("upload", file=descriptor)],
        )
    elif action == "file_descriptor_from_file_errors":
        expect_error(lambda: FileDescriptor.from_file("relative.bin"), ValueError)
        expect_error(lambda: FileDescriptor.from_file(context.params["directory"]), ValueError)
        symlink = context.params.get("symlink", "")
        if symlink:
            expect_error(lambda: FileDescriptor.from_file(symlink), ValueError)
        request.headers.set("X-Body-Check", "multipart-file-errors")
    elif action == "assign_file_body":
        source = context.params["source"]
        descriptor = FileDescriptor.from_file(source)
        request.body = Body("file", descriptor)
        assert request.body.kind == "file"
        assert request.body.value is not descriptor
        assert request.body.value.path != source
        assert request.body.value.name == descriptor.name
        assert request.body.value.size == descriptor.size
        os.remove(source)
    elif action == "file_body_errors":
        expect_error(lambda: Body("file", "not a descriptor"), TypeError)
        request.headers.set("X-Body-Check", "file-body-errors")
    elif action == "unavailable_errors":
        expect_error(lambda: body.value, ValueError)
        expect_error(lambda: body.write_file(context.params["destination"]), ValueError)
        expect_error(lambda: setattr(request, "body", "replacement"), ValueError)
        request.headers.set("X-Body-Check", "unavailable")
    elif action == "json_assignment":
        request.body = {"answer": 42}
    elif action == "assign_request_file_kind":
        request.body = Body(
            context.params["kind"],
            FileDescriptor.from_file(context.params["source"]),
        )
        os.remove(context.params["source"])
        assert not os.path.exists(context.params["source"])
    elif action == "assign_none":
        request.body = None
    elif action == "assign_text":
        request.body = "assigned text"
    elif action == "assign_binary":
        request.body = b"assigned bytes"
    elif action == "assign_json_object":
        request.body = {"assigned": True}
    elif action == "assign_json_array":
        request.body = [1, "two", False]
    elif action == "assign_body":
        request.body = Body("text", "Body instance")
    elif action == "assign_xml_body":
        request.body = Body("xml", "<root>Body instance</root>")
    elif action == "reject_assignment":
        expect_error(lambda: setattr(request, "body", object()), TypeError)
        request.headers.set("X-Body-Check", "assignment-error")
    elif action == "oversized_binary":
        request.body = b"x" * (TRANSPORT_INLINE_LIMIT + 1)
        assert request.body.kind == "binary"
    elif action == "oversized_text":
        request.body = "文" * ((TRANSPORT_INLINE_LIMIT // 3) + 1)
        assert request.body.kind == "text"
    elif action == "oversized_json":
        request.body = {"data": "j" * TRANSPORT_INLINE_LIMIT}
        assert request.body.kind == "json"
    elif action == "exact_limit_binary":
        request.body = b"e" * TRANSPORT_INLINE_LIMIT
    elif action == "limit_plus_one_binary":
        request.body = b"e" * (TRANSPORT_INLINE_LIMIT + 1)
    elif action == "assign_text_file":
        source = context.params["source"]
        request.body = Body("text", FileDescriptor.from_file(source))
        assert request.body.kind == "text"
        assert request.body.value == context.params["expected"]
        os.remove(source)
    elif action == "write_file":
        body.write_file(context.params["destination"])
        assert os.path.getsize(context.params["destination"]) == context.params["expectedSize"]
    elif action == "large_value_materialization":
        value = body.value
        assert len(value.encode("utf-8")) == context.params["expectedSize"]
        request.body = value.replace("L", "R", 1)
    elif action == "file_errors":
        expect_error(lambda: FileDescriptor.from_file("relative.bin"), ValueError)
        expect_error(lambda: body.write_file("relative-output.bin"), ValueError)
        descriptor = FileDescriptor.from_file(context.params["source"])
        expect_error(lambda: Body("multipart", descriptor), TypeError)
        request.headers.set("X-Body-Check", "file-errors")
    return request

def onResponse(context, response):
    action = context.params.get("action", "")
    if action == "assign_response_file_kind":
        source = context.params["source"]
        response.body = Body(
            context.params["kind"],
            FileDescriptor.from_file(source),
        )
        os.remove(source)
    elif action == "reject_response_file_kind":
        expect_error(
            lambda: setattr(
                response,
                "body",
                Body("file", FileDescriptor.from_file(context.params["source"])),
            ),
            ValueError,
        )
        response.headers.set("X-Body-Check", "response-file-error")
    elif action == "assign_response_json":
        response.body = {"response": True}
    return response
`

func TestWorkerInvokesSDKCapturesLogsAndCarriesShared(t *testing.T) {
	var logMu sync.Mutex
	logs := make([]WorkerLog, 0)
	pool, manager, plugin := newPythonWorkerHarness(t, 2, func(entry WorkerLog) {
		logMu.Lock()
		logs = append(logs, entry)
		logMu.Unlock()
	})
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, integrationPluginSource)

	requestContext := integrationContext(map[string]any{"header": "one"}, map[string]any{})
	requestValue := map[string]any{
		"method": "POST", "url": "https://example.com/api",
		"headers": []map[string]string{{"name": "X-Duplicate", "value": "one"}, {"name": "X-Duplicate", "value": "two"}},
		"body":    map[string]any{"kind": "json", "value": map[string]any{"before": true}},
	}
	requestResult, err := pool.Invoke(context.Background(), InvokeRequest{
		ExecutionID: "execution-request",
		PluginID:    plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: manager.revisionPath(plugin.ID, plugin.ActiveRevision), Hook: "onRequest",
		Context: requestContext, Value: requestValue,
	})
	if err != nil {
		t.Fatalf("invoke onRequest: %v", err)
	}
	if requestResult.Blocked || !requestResult.Transformed {
		t.Fatalf("request result = %+v", requestResult)
	}
	var transformedRequest struct {
		Headers []map[string]string `json:"headers"`
		Body    struct {
			Kind  string         `json:"kind"`
			Value map[string]any `json:"value"`
		} `json:"body"`
	}
	if err := json.Unmarshal(requestResult.Value, &transformedRequest); err != nil {
		t.Fatalf("decode transformed request: %v", err)
	}
	if len(transformedRequest.Headers) != 3 || transformedRequest.Headers[0]["value"] != "one" || transformedRequest.Headers[1]["value"] != "two" || transformedRequest.Headers[2]["name"] != "X-Plugin" {
		t.Fatalf("transformed headers = %#v", transformedRequest.Headers)
	}
	if transformedRequest.Body.Kind != "json" || transformedRequest.Body.Value["plugin"] != true {
		t.Fatalf("transformed body = %+v", transformedRequest.Body)
	}

	var shared map[string]any
	if err := json.Unmarshal(requestResult.Shared, &shared); err != nil {
		t.Fatalf("decode shared: %v", err)
	}
	responseContext := integrationContext(map[string]any{}, shared)
	responseValue := map[string]any{
		"code": 200, "headers": []map[string]string{}, "trailers": []map[string]string{},
		"body":    map[string]any{"kind": "text", "value": "response"},
		"request": requestValue,
	}
	responseResult, err := pool.Invoke(context.Background(), InvokeRequest{
		ExecutionID: "execution-response",
		PluginID:    plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: manager.revisionPath(plugin.ID, plugin.ActiveRevision), Hook: "onResponse",
		Context: responseContext, Value: responseValue,
	})
	if err != nil {
		t.Fatalf("invoke onResponse: %v", err)
	}
	if !strings.Contains(string(responseResult.Value), "X-Response-Plugin") || !strings.Contains(string(responseResult.Shared), "response") {
		t.Fatalf("response result value=%s shared=%s", responseResult.Value, responseResult.Shared)
	}

	logMu.Lock()
	defer logMu.Unlock()
	joined := ""
	for _, entry := range logs {
		joined += entry.Message + "\n"
		if (strings.Contains(entry.Message, "stdout from plugin") || strings.Contains(entry.Message, "warning from context")) && entry.ExecutionID != "execution-request" {
			t.Fatalf("request log execution ID = %q", entry.ExecutionID)
		}
	}
	if !strings.Contains(joined, "stdout from plugin") || !strings.Contains(joined, "warning from context") {
		t.Fatalf("captured logs = %#v", logs)
	}
}

func TestWorkerFirstPhaseSDKSurfaceAndSemanticRequestSnapshot(t *testing.T) {
	pool, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, firstPhaseAPIPluginSource)
	revisionPath := manager.revisionPath(plugin.ID, plugin.ActiveRevision)

	requestBody := []byte("file-backed request snapshot")
	requestPath := filepath.Join(t.TempDir(), "request.bin")
	if err := os.WriteFile(requestPath, requestBody, 0o600); err != nil {
		t.Fatal(err)
	}
	requestResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onRequest", OutputDirectory: t.TempDir(),
		Context: integrationContext(map[string]any{}, map[string]any{}),
		Value: map[string]any{
			"method": "POST",
			"url":    "https://example.com:8443/api/items?tag=one&tag=two&empty=&encoded=a%2Fb",
			"headers": []map[string]string{{
				"name": "Content-Type", "value": "application/json; charset=utf-8",
			}},
			"body": fileBodyWireValue("file", requestPath, int64(len(requestBody))),
		},
	})
	if err != nil {
		t.Fatalf("invoke first-phase onRequest: %v", err)
	}
	if requestResult.Blocked || !requestResult.Transformed {
		t.Fatalf("first-phase request result = %+v", requestResult)
	}
	var requestValue map[string]any
	if err := json.Unmarshal(requestResult.Value, &requestValue); err != nil {
		t.Fatalf("decode first-phase request: %v", err)
	}
	if requestValue["url"] != "https://example.com:8443/v2/items?tag=three&encoded=a%2Fb&tag=four&space=a+b" {
		t.Fatalf("first-phase request URL = %q", requestValue["url"])
	}

	snapshotCopy := filepath.Join(t.TempDir(), "snapshot-copy.bin")
	responseResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onResponse", OutputDirectory: t.TempDir(),
		Context: integrationContext(map[string]any{
			"request_size": len(requestBody), "snapshot_copy": snapshotCopy,
		}, map[string]any{}),
		Value: map[string]any{
			"code": 200, "protocol": "HTTP/2.0", "statusText": "200 OK",
			"headers": []map[string]string{{
				"name": "Content-Type", "value": "text/plain; charset=utf-8",
			}},
			"trailers": []map[string]string{},
			"body":     map[string]any{"kind": "text", "value": "response"},
			"request":  requestValue,
		},
	})
	if err != nil {
		t.Fatalf("invoke first-phase onResponse: %v", err)
	}
	if responseResult.Blocked || !responseResult.Transformed {
		t.Fatalf("first-phase response result = %+v", responseResult)
	}
	var responseValue struct {
		Code       int                 `json:"code"`
		Protocol   string              `json:"protocol"`
		StatusText string              `json:"statusText"`
		Headers    []map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(responseResult.Value, &responseValue); err != nil {
		t.Fatalf("decode first-phase response: %v", err)
	}
	if responseValue.Code != 404 || responseValue.Protocol != "HTTP/2.0" || responseValue.StatusText != "404 Not Found" {
		t.Fatalf("first-phase response metadata = %+v", responseValue)
	}
	if len(responseValue.Headers) != 1 || responseValue.Headers[0]["value"] != "application/problem+json" {
		t.Fatalf("first-phase response headers = %#v", responseValue.Headers)
	}
	copied, err := os.ReadFile(snapshotCopy)
	if err != nil || !bytes.Equal(copied, requestBody) {
		t.Fatalf("semantic snapshot copy = %q err=%v", copied, err)
	}
}

func TestWorkerMultipartPartFilename(t *testing.T) {
	pool, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, multipartPartFilenamePluginSource)
	revisionPath := manager.revisionPath(plugin.ID, plugin.ActiveRevision)

	uploadPath := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(uploadPath, []byte("upload body"), 0o600); err != nil {
		t.Fatal(err)
	}
	requestResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onRequest", OutputDirectory: t.TempDir(),
		Context: integrationContext(map[string]any{"upload_path": uploadPath}, map[string]any{}),
		Value: map[string]any{
			"method": "POST", "url": "https://example.com/upload",
			"headers": []map[string]string{},
			"body":    map[string]any{"kind": "none", "value": nil},
		},
	})
	if err != nil {
		t.Fatalf("invoke second-phase onRequest: %v", err)
	}
	if requestResult.Blocked || !requestResult.Transformed || !requestResult.BodyChanged {
		t.Fatalf("second-phase request result = %+v", requestResult)
	}
	var requestValue struct {
		Method  string              `json:"method"`
		URL     string              `json:"url"`
		Headers []map[string]string `json:"headers"`
		Body    struct {
			Kind  string `json:"kind"`
			Items []struct {
				Filename string         `json:"filename"`
				File     map[string]any `json:"file"`
			} `json:"items"`
		} `json:"body"`
	}
	if err := json.Unmarshal(requestResult.Value, &requestValue); err != nil {
		t.Fatalf("decode second-phase request: %v", err)
	}
	if requestValue.Body.Kind != "multipart" || len(requestValue.Body.Items) != 1 {
		t.Fatalf("second-phase multipart body = %+v", requestValue.Body)
	}
	part := requestValue.Body.Items[0]
	if part.Filename != "renamed.bin" {
		t.Fatalf("second-phase multipart part = %+v", part)
	}

	var requestMap map[string]any
	if err := json.Unmarshal(requestResult.Value, &requestMap); err != nil {
		t.Fatal(err)
	}
	responseResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onResponse", OutputDirectory: t.TempDir(),
		Context: integrationContext(map[string]any{}, map[string]any{}),
		Value: map[string]any{
			"code": 204, "protocol": "HTTP/2.0", "statusText": "204 No Content",
			"headers": []map[string]string{}, "trailers": []map[string]string{},
			"body": map[string]any{"kind": "none", "value": nil}, "request": requestMap,
		},
	})
	if err != nil {
		t.Fatalf("invoke second-phase onResponse: %v", err)
	}
	if responseResult.Blocked || responseResult.Transformed {
		t.Fatalf("second-phase response result = %+v", responseResult)
	}
}

func TestWorkerBodyAPISupportsInlineAndFileBackedValues(t *testing.T) {
	pool, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, bodyAPIPluginSource)
	revisionPath := manager.revisionPath(plugin.ID, plugin.ActiveRevision)
	outputDirectory := t.TempDir()

	invoke := func(action string, body map[string]any, params map[string]any) InvokeResult {
		t.Helper()
		params["action"] = action
		result, err := pool.Invoke(context.Background(), InvokeRequest{
			PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
			Path: revisionPath, Hook: "onRequest", OutputDirectory: outputDirectory,
			Context: integrationContext(params, map[string]any{}),
			Value: map[string]any{
				"method": "POST", "url": "https://example.com/",
				"headers": []map[string]string{}, "body": body,
			},
		})
		if err != nil {
			t.Fatalf("invoke action %q: %v", action, err)
		}
		return result
	}
	decodeBody := func(result InvokeResult) map[string]any {
		t.Helper()
		var transformed struct {
			Body map[string]any `json:"body"`
		}
		if err := json.Unmarshal(result.Value, &transformed); err != nil {
			t.Fatal(err)
		}
		return transformed.Body
	}

	inlineResult := invoke("inspect_inline", map[string]any{
		"kind": "json", "value": map[string]any{"value": 1},
	}, map[string]any{})
	if inlineResult.BodyChanged || !strings.Contains(string(inlineResult.Value), "X-Body-Check") {
		t.Fatalf("inline result = %+v value=%s", inlineResult, inlineResult.Value)
	}

	filePath := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(filePath, []byte("file-backed text"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileResult := invoke("inspect_file", fileBodyWireValue("text", filePath, int64(len("file-backed text"))), map[string]any{})
	if fileResult.BodyChanged || !strings.Contains(string(fileResult.Value), "X-Body-Check") {
		t.Fatalf("file result = %+v value=%s", fileResult, fileResult.Value)
	}
	jsonFilePath := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(jsonFilePath, []byte(`{"file":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	jsonFileResult := invoke("inspect_file_json", fileBodyWireValue("json", jsonFilePath, 13), map[string]any{})
	if jsonFileResult.BodyChanged || !strings.Contains(string(jsonFileResult.Value), "X-Body-Check") {
		t.Fatalf("file JSON result = %+v value=%s", jsonFileResult, jsonFileResult.Value)
	}

	textFromFileSource := filepath.Join(t.TempDir(), "body-source.txt")
	textFromFileBytes := []byte("FlowLens text 世界")
	if err := os.WriteFile(textFromFileSource, textFromFileBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	textFromFileResult := invoke("assign_text_file", map[string]any{
		"kind": "none", "value": nil,
	}, map[string]any{"source": textFromFileSource, "expected": string(textFromFileBytes)})
	if _, err := os.Stat(textFromFileSource); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source file still exists after Body assignment: %v", err)
	}
	textFromFileBody := decodeBody(textFromFileResult)
	textDescriptor, ok := textFromFileBody["file"].(map[string]any)
	if !ok || textFromFileBody["kind"] != "text" || textFromFileBody["storage"] != "file" {
		t.Fatalf("descriptor-backed text Body = %#v", textFromFileBody)
	}
	managedTextBytes, err := os.ReadFile(textDescriptor["path"].(string))
	if err != nil || !bytes.Equal(managedTextBytes, textFromFileBytes) {
		t.Fatalf("descriptor-backed text managed bytes differ: len=%d err=%v", len(managedTextBytes), err)
	}

	writeFileDestination := filepath.Join(t.TempDir(), "written.bin")
	writeFileBytes := []byte{0x00, 0x7f, 0xff, 0x42, 0x99}
	writeFileResult := invoke("write_file", map[string]any{
		"kind": "binary", "base64": base64.StdEncoding.EncodeToString(writeFileBytes),
	}, map[string]any{"destination": writeFileDestination, "expectedSize": len(writeFileBytes)})
	if writeFileResult.BodyChanged {
		t.Fatalf("write_file() changed Body: %+v", writeFileResult)
	}
	writtenBytes, err := os.ReadFile(writeFileDestination)
	if err != nil || !bytes.Equal(writtenBytes, writeFileBytes) {
		t.Fatalf("write_file() bytes differ: bytes=%v err=%v", writtenBytes, err)
	}

	largePath := filepath.Join(t.TempDir(), "large.bin")
	largeFile, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := largeFile.Truncate(maxInlineHTTPRequestPluginBodyBytes + 1); err != nil {
		_ = largeFile.Close()
		t.Fatal(err)
	}
	if err := largeFile.Close(); err != nil {
		t.Fatal(err)
	}
	limitResult := invoke("materialize_binary", fileBodyWireValue("binary", largePath, maxInlineHTTPRequestPluginBodyBytes+1), map[string]any{
		"expectedSize": maxInlineHTTPRequestPluginBodyBytes + 1,
	})
	if limitResult.BodyChanged {
		t.Fatalf("materializing binary Body changed it: %+v", limitResult)
	}

	largeTextPath := filepath.Join(t.TempDir(), "large-text.txt")
	largeTextBytes := append(bytes.Repeat([]byte("L"), int(maxInlineHTTPRequestPluginBodyBytes)), '!')
	if err := os.WriteFile(largeTextPath, largeTextBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	largeMaterializationResult := invoke(
		"large_value_materialization",
		fileBodyWireValue("text", largeTextPath, int64(len(largeTextBytes))),
		map[string]any{"expectedSize": len(largeTextBytes)},
	)
	if !largeMaterializationResult.BodyChanged || !largeMaterializationResult.Transformed {
		t.Fatalf("large Body materialization result = %+v", largeMaterializationResult)
	}
	largeMaterializedBody := decodeBody(largeMaterializationResult)
	if largeMaterializedBody["storage"] != "file" {
		t.Fatalf("large materialized Body = %#v, want file-backed wire", largeMaterializedBody)
	}
	largeDescriptor, ok := largeMaterializedBody["file"].(map[string]any)
	if !ok {
		t.Fatalf("large materialized Body descriptor = %#v", largeMaterializedBody["file"])
	}
	materializedBytes, err := os.ReadFile(largeDescriptor["path"].(string))
	wantMaterializedBytes := append([]byte(nil), largeTextBytes...)
	wantMaterializedBytes[0] = 'R'
	if err != nil || !bytes.Equal(materializedBytes, wantMaterializedBytes) {
		t.Fatalf("large materialized Body bytes differ: len=%d err=%v", len(materializedBytes), err)
	}

	structuredResult := invoke("structured_value", map[string]any{
		"kind": "urlencoded", "items": []map[string]any{{"enabled": true, "name": "a", "value": "1"}},
	}, map[string]any{})
	if structuredResult.BodyChanged {
		t.Fatalf("structured value changed body: %+v", structuredResult)
	}

	unavailableResult := invoke("unavailable_errors", map[string]any{
		"kind": "unavailable", "value": nil, "streaming": true,
	}, map[string]any{"destination": filepath.Join(t.TempDir(), "unavailable.bin")})
	if unavailableResult.BodyChanged {
		t.Fatalf("unavailable checks changed body: %+v", unavailableResult)
	}

	fileErrorSource := filepath.Join(t.TempDir(), "file-error-source.bin")
	if err := os.WriteFile(fileErrorSource, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileErrorResult := invoke("file_errors", map[string]any{
		"kind": "none", "value": nil,
	}, map[string]any{"source": fileErrorSource})
	if fileErrorResult.BodyChanged || !strings.Contains(string(fileErrorResult.Value), "X-Body-Check") {
		t.Fatalf("file error result = %+v value=%s", fileErrorResult, fileErrorResult.Value)
	}

}

func TestWorkerBodyAssignmentsProduceExpectedWireValues(t *testing.T) {
	pool, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, bodyAPIPluginSource)
	revisionPath := manager.revisionPath(plugin.ID, plugin.ActiveRevision)
	outputDirectory := t.TempDir()

	invokeWithBody := func(action string, body map[string]any, params map[string]any) map[string]any {
		t.Helper()
		params["action"] = action
		requestValue := minimalRequestWire()
		requestValue["body"] = body
		result, err := pool.Invoke(context.Background(), InvokeRequest{
			PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
			Path: revisionPath, Hook: "onRequest", OutputDirectory: outputDirectory,
			Context: integrationContext(params, map[string]any{}), Value: requestValue,
		})
		if err != nil {
			t.Fatalf("invoke action %q: %v", action, err)
		}
		if !result.BodyChanged || !result.Transformed {
			t.Fatalf("action %q result = %+v", action, result)
		}
		var value struct {
			Body map[string]any `json:"body"`
		}
		if err := json.Unmarshal(result.Value, &value); err != nil {
			t.Fatal(err)
		}
		return value.Body
	}
	invoke := func(action string, params map[string]any) map[string]any {
		t.Helper()
		return invokeWithBody(action, map[string]any{"kind": "none", "value": nil}, params)
	}

	jsonBody := invoke("json_assignment", map[string]any{})
	if jsonBody["kind"] != "json" {
		t.Fatalf("JSON assignment wire = %#v", jsonBody)
	}
	jsonValue, ok := jsonBody["value"].(map[string]any)
	if !ok || jsonValue["answer"] != float64(42) {
		t.Fatalf("JSON assignment value = %#v", jsonBody["value"])
	}

	assignmentTests := []struct {
		action   string
		wantKind string
		wantText string
	}{
		{action: "assign_none", wantKind: "none"},
		{action: "assign_text", wantKind: "text", wantText: "assigned text"},
		{action: "assign_binary", wantKind: "binary"},
		{action: "assign_json_object", wantKind: "json"},
		{action: "assign_json_array", wantKind: "json"},
		{action: "assign_body", wantKind: "text", wantText: "Body instance"},
		{action: "assign_xml_body", wantKind: "xml", wantText: "<root>Body instance</root>"},
	}
	for _, test := range assignmentTests {
		t.Run(test.action, func(t *testing.T) {
			body := invokeWithBody(test.action, map[string]any{
				"kind": "text", "value": "initial",
			}, map[string]any{})
			if body["kind"] != test.wantKind {
				t.Fatalf("assigned Body = %#v, want kind %q", body, test.wantKind)
			}
			if test.wantText != "" && body["value"] != test.wantText {
				t.Fatalf("assigned Body = %#v, want value %q", body, test.wantText)
			}
			switch test.action {
			case "assign_binary":
				value, err := base64.StdEncoding.DecodeString(body["base64"].(string))
				if err != nil || string(value) != "assigned bytes" {
					t.Fatalf("assigned binary = %#v bytes=%q err=%v", body, value, err)
				}
			case "assign_json_object":
				value, ok := body["value"].(map[string]any)
				if !ok || value["assigned"] != true {
					t.Fatalf("assigned JSON object = %#v", body)
				}
			case "assign_json_array":
				value, ok := body["value"].([]any)
				if !ok || len(value) != 3 || value[1] != "two" {
					t.Fatalf("assigned JSON array = %#v", body)
				}
			}
		})
	}

	assertFileBackedBody := func(action string, body map[string]any, want []byte) map[string]any {
		t.Helper()
		if body["storage"] != "file" {
			t.Fatalf("%s storage = %#v, want file", action, body)
		}
		if _, ok := body["base64"]; ok {
			t.Fatalf("%s leaked Base64 into Worker result: %#v", action, body)
		}
		if _, ok := body["value"]; ok {
			t.Fatalf("%s leaked inline value into Worker result: %#v", action, body)
		}
		descriptor, ok := body["file"].(map[string]any)
		if !ok {
			t.Fatalf("%s descriptor = %#v", action, body["file"])
		}
		got, err := os.ReadFile(descriptor["path"].(string))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s bytes differ: len=%d want=%d err=%v", action, len(got), len(want), err)
		}
		return descriptor
	}

	oversizedBinaryBytes := bytes.Repeat([]byte("x"), int(maxInlineHTTPRequestPluginBodyBytes+1))
	oversizedBinaryBody := invokeWithBody("oversized_binary", map[string]any{
		"kind": "none", "value": nil,
	}, map[string]any{})
	assertFileBackedBody("oversized_binary", oversizedBinaryBody, oversizedBinaryBytes)

	oversizedTextValue := strings.Repeat("文", int(maxInlineHTTPRequestPluginBodyBytes/3)+1)
	oversizedTextBody := invokeWithBody("oversized_text", map[string]any{
		"kind": "none", "value": nil,
	}, map[string]any{})
	assertFileBackedBody("oversized_text", oversizedTextBody, []byte(oversizedTextValue))

	oversizedJSONValue := map[string]any{"data": strings.Repeat("j", int(maxInlineHTTPRequestPluginBodyBytes))}
	oversizedJSONBytes, err := json.Marshal(oversizedJSONValue)
	if err != nil {
		t.Fatal(err)
	}
	oversizedJSONBody := invokeWithBody("oversized_json", map[string]any{
		"kind": "none", "value": nil,
	}, map[string]any{})
	assertFileBackedBody("oversized_json", oversizedJSONBody, oversizedJSONBytes)

	exactLimitBody := invokeWithBody("exact_limit_binary", map[string]any{
		"kind": "none", "value": nil,
	}, map[string]any{})
	if exactLimitBody["storage"] == "file" || exactLimitBody["file"] != nil {
		t.Fatalf("exact-limit Body was file-backed: %#v", exactLimitBody)
	}
	exactLimitBytes, err := base64.StdEncoding.DecodeString(exactLimitBody["base64"].(string))
	if err != nil || len(exactLimitBytes) != int(maxInlineHTTPRequestPluginBodyBytes) {
		t.Fatalf("exact-limit bytes=%d err=%v", len(exactLimitBytes), err)
	}

	limitPlusOneBytes := bytes.Repeat([]byte("e"), int(maxInlineHTTPRequestPluginBodyBytes+1))
	limitPlusOneBody := invokeWithBody("limit_plus_one_binary", map[string]any{
		"kind": "none", "value": nil,
	}, map[string]any{})
	assertFileBackedBody("limit_plus_one_binary", limitPlusOneBody, limitPlusOneBytes)

	rejectedAssignmentRequest := minimalRequestWire()
	rejectedAssignmentResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onRequest", OutputDirectory: outputDirectory,
		Context: integrationContext(map[string]any{
			"action": "reject_assignment",
		}, map[string]any{}),
		Value: rejectedAssignmentRequest,
	})
	if err != nil {
		t.Fatalf("invoke rejected assignment: %v", err)
	}
	if rejectedAssignmentResult.BodyChanged || !rejectedAssignmentResult.Transformed {
		t.Fatalf("rejected assignment result = %+v", rejectedAssignmentResult)
	}

	sourcePath := filepath.Join(t.TempDir(), "source.bin")
	want := []byte("staged body bytes")
	if err := os.WriteFile(sourcePath, want, 0o600); err != nil {
		t.Fatal(err)
	}
	fileBody := invoke("assign_request_file_kind", map[string]any{
		"source": sourcePath, "kind": "binary",
	})
	if _, err := os.Stat(sourcePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source file still exists: %v", err)
	}
	if fileBody["kind"] != "binary" || fileBody["storage"] != "file" {
		t.Fatalf("descriptor-backed binary wire = %#v", fileBody)
	}
	descriptor, ok := fileBody["file"].(map[string]any)
	if !ok {
		t.Fatalf("file descriptor = %#v", fileBody["file"])
	}
	stagedPath, _ := descriptor["path"].(string)
	stagedBytes, err := os.ReadFile(stagedPath)
	if err != nil || string(stagedBytes) != string(want) {
		t.Fatalf("staged file path=%q bytes=%q err=%v", stagedPath, stagedBytes, err)
	}
	contained, err := filepath.Rel(outputDirectory, stagedPath)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
		t.Fatalf("staged path %q is outside %q", stagedPath, outputDirectory)
	}
	for _, test := range []struct {
		kind string
		body []byte
	}{
		{kind: "json", body: []byte(`{"from":"file"}`)},
		{kind: "xml", body: []byte(`<root>file</root>`)},
	} {
		t.Run("request_"+test.kind+"_file", func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "request."+test.kind)
			if err := os.WriteFile(source, test.body, 0o600); err != nil {
				t.Fatal(err)
			}
			body := invoke("assign_request_file_kind", map[string]any{
				"source": source, "kind": test.kind,
			})
			if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s request source still exists: %v", test.kind, err)
			}
			if body["kind"] != test.kind {
				t.Fatalf("%s request Body = %#v", test.kind, body)
			}
			assertFileBackedBody("request_"+test.kind+"_file", body, test.body)
		})
	}

	responseSource := filepath.Join(t.TempDir(), "response-source.txt")
	if err := os.WriteFile(responseSource, []byte("response file"), 0o600); err != nil {
		t.Fatal(err)
	}
	responseResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onResponse", OutputDirectory: outputDirectory,
		Context: integrationContext(map[string]any{
			"action": "assign_response_file_kind", "source": responseSource, "kind": "text",
		}, map[string]any{}),
		Value: map[string]any{
			"code": 200, "headers": []map[string]string{}, "trailers": []map[string]string{},
			"body":    map[string]any{"kind": "text", "value": "original"},
			"request": minimalRequestWire(),
		},
	})
	if err != nil {
		t.Fatalf("invoke descriptor-backed text response: %v", err)
	}
	if !responseResult.BodyChanged || !responseResult.Transformed {
		t.Fatalf("descriptor-backed text response result = %+v", responseResult)
	}
	if _, err := os.Stat(responseSource); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("response source still exists: %v", err)
	}
	var responseValue struct {
		Body map[string]any `json:"body"`
	}
	if err := json.Unmarshal(responseResult.Value, &responseValue); err != nil {
		t.Fatal(err)
	}
	if responseValue.Body["kind"] != "text" || responseValue.Body["storage"] != "file" {
		t.Fatalf("descriptor-backed text response wire = %#v", responseValue.Body)
	}

	responseXMLSource := filepath.Join(t.TempDir(), "response.xml")
	if err := os.WriteFile(responseXMLSource, []byte("<root/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	xmlResponseFileResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onResponse", OutputDirectory: outputDirectory,
		Context: integrationContext(map[string]any{
			"action": "assign_response_file_kind", "source": responseXMLSource, "kind": "xml",
		}, map[string]any{}),
		Value: map[string]any{
			"code": 200, "headers": []map[string]string{}, "trailers": []map[string]string{},
			"body": map[string]any{"kind": "text", "value": "original"}, "request": minimalRequestWire(),
		},
	})
	if err != nil {
		t.Fatalf("invoke response XML file: %v", err)
	}
	if !xmlResponseFileResult.BodyChanged || !xmlResponseFileResult.Transformed {
		t.Fatalf("response XML file result = %+v", xmlResponseFileResult)
	}
	var xmlResponseValue struct {
		Body map[string]any `json:"body"`
	}
	if err := json.Unmarshal(xmlResponseFileResult.Value, &xmlResponseValue); err != nil {
		t.Fatal(err)
	}
	if xmlResponseValue.Body["kind"] != "xml" || xmlResponseValue.Body["storage"] != "file" {
		t.Fatalf("response XML file wire = %#v", xmlResponseValue.Body)
	}
	for _, test := range []struct {
		kind string
		body []byte
	}{
		{kind: "json", body: []byte(`{"response":"file"}`)},
		{kind: "binary", body: []byte{0x00, 0xff, 0x42}},
	} {
		t.Run("response_"+test.kind+"_file", func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "response."+test.kind)
			if err := os.WriteFile(source, test.body, 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := pool.Invoke(context.Background(), InvokeRequest{
				PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
				Path: revisionPath, Hook: "onResponse", OutputDirectory: outputDirectory,
				Context: integrationContext(map[string]any{
					"action": "assign_response_file_kind", "source": source, "kind": test.kind,
				}, map[string]any{}),
				Value: map[string]any{
					"code": 200, "headers": []map[string]string{}, "trailers": []map[string]string{},
					"body": map[string]any{"kind": "text", "value": "original"}, "request": minimalRequestWire(),
				},
			})
			if err != nil {
				t.Fatalf("invoke descriptor-backed %s response: %v", test.kind, err)
			}
			if !result.BodyChanged || !result.Transformed {
				t.Fatalf("descriptor-backed %s response result = %+v", test.kind, result)
			}
			if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s response source still exists: %v", test.kind, err)
			}
			var value struct {
				Body map[string]any `json:"body"`
			}
			if err := json.Unmarshal(result.Value, &value); err != nil {
				t.Fatal(err)
			}
			if value.Body["kind"] != test.kind {
				t.Fatalf("%s response Body = %#v", test.kind, value.Body)
			}
			assertFileBackedBody("response_"+test.kind+"_file", value.Body, test.body)
		})
	}

	responseFileKindSource := filepath.Join(t.TempDir(), "response-file-kind.bin")
	if err := os.WriteFile(responseFileKindSource, []byte("response file kind"), 0o600); err != nil {
		t.Fatal(err)
	}
	rejectedResponseFileResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onResponse", OutputDirectory: outputDirectory,
		Context: integrationContext(map[string]any{
			"action": "reject_response_file_kind", "source": responseFileKindSource,
		}, map[string]any{}),
		Value: map[string]any{
			"code": 200, "headers": []map[string]string{}, "trailers": []map[string]string{},
			"body": map[string]any{"kind": "text", "value": "original"}, "request": minimalRequestWire(),
		},
	})
	if err != nil {
		t.Fatalf("invoke rejected response file kind: %v", err)
	}
	if rejectedResponseFileResult.BodyChanged || !rejectedResponseFileResult.Transformed ||
		!strings.Contains(string(rejectedResponseFileResult.Value), "X-Body-Check") {
		t.Fatalf("rejected response file kind result = %+v value=%s", rejectedResponseFileResult, rejectedResponseFileResult.Value)
	}

	assignedResponseResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onResponse", OutputDirectory: outputDirectory,
		Context: integrationContext(map[string]any{
			"action": "assign_response_json",
		}, map[string]any{}),
		Value: map[string]any{
			"code": 200, "headers": []map[string]string{}, "trailers": []map[string]string{},
			"body":    map[string]any{"kind": "text", "value": "original"},
			"request": minimalRequestWire(),
		},
	})
	if err != nil {
		t.Fatalf("invoke response assignment: %v", err)
	}
	if !assignedResponseResult.BodyChanged || !assignedResponseResult.Transformed {
		t.Fatalf("response assignment result = %+v", assignedResponseResult)
	}
	var assignedResponseValue struct {
		Body map[string]any `json:"body"`
	}
	if err := json.Unmarshal(assignedResponseResult.Value, &assignedResponseValue); err != nil {
		t.Fatal(err)
	}
	assignedResponseJSON, ok := assignedResponseValue.Body["value"].(map[string]any)
	if assignedResponseValue.Body["kind"] != "json" || !ok || assignedResponseJSON["response"] != true {
		t.Fatalf("assigned response Body = %#v", assignedResponseValue.Body)
	}

}

func fileBodyWireValue(kind, path string, size int64) map[string]any {
	return map[string]any{
		"kind": kind, "storage": "file", "size": size,
		"file": map[string]any{
			"path": path, "name": filepath.Base(path), "size": size, "readOnly": true,
		},
	}
}

func TestInlineRevisionValidationLogsCarryExecutionID(t *testing.T) {
	var logMu sync.Mutex
	logs := make([]WorkerLog, 0)
	_, manager, _ := newPythonWorkerHarness(t, 1, func(entry WorkerLog) {
		logMu.Lock()
		logs = append(logs, entry)
		logMu.Unlock()
	})
	revision, lease, err := manager.createInlineRevision(context.Background(), "inline-validation", `from flowlens import *

print("inline import output")

def onRequest(context, request):
    return request

def onResponse(context, response):
    return response
`)
	if err != nil {
		t.Fatalf("createInlineRevision: %v", err)
	}
	defer lease.Release()
	if !isRevisionName(revision) {
		t.Fatalf("revision = %q", revision)
	}
	logMu.Lock()
	defer logMu.Unlock()
	for _, entry := range logs {
		if entry.Message == "inline import output" {
			if entry.ExecutionID != "inline-validation" || entry.PluginID != inlineHTTPRequestPluginID || entry.Stream != "stdout" {
				t.Fatalf("inline validation log = %+v", entry)
			}
			return
		}
	}
	t.Fatalf("inline validation output missing from logs: %+v", logs)
}

func TestWorkerFileDescriptorFromFileStagesManagedMultipartUpload(t *testing.T) {
	pool, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, bodyAPIPluginSource)
	revisionPath := manager.revisionPath(plugin.ID, plugin.ActiveRevision)
	outputDirectory := t.TempDir()
	sourceDirectory := t.TempDir()
	sourcePath := filepath.Join(sourceDirectory, "upload.bin")
	sourceBytes := []byte{0x00, 0x7f, 0xff, 0x42}
	if err := os.WriteFile(sourcePath, sourceBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	invoke := func(action string, params map[string]any) InvokeResult {
		t.Helper()
		params["action"] = action
		result, err := pool.Invoke(context.Background(), InvokeRequest{
			PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
			Path: revisionPath, Hook: "onRequest", OutputDirectory: outputDirectory,
			Context: integrationContext(params, map[string]any{}), Value: minimalRequestWire(),
		})
		if err != nil {
			t.Fatalf("invoke action %q: %v", action, err)
		}
		return result
	}

	result := invoke("file_descriptor_from_file", map[string]any{
		"source": sourcePath, "expectedSize": len(sourceBytes),
	})
	if !result.BodyChanged || !result.Transformed {
		t.Fatalf("multipart file result = %+v", result)
	}
	var transformed struct {
		Body struct {
			Kind  string `json:"kind"`
			Items []struct {
				Enabled bool   `json:"enabled"`
				Name    string `json:"name"`
				Kind    string `json:"kind"`
				File    struct {
					Path     string `json:"path"`
					Name     string `json:"name"`
					Size     int64  `json:"size"`
					ReadOnly bool   `json:"readOnly"`
				} `json:"file"`
			} `json:"items"`
		} `json:"body"`
	}
	if err := json.Unmarshal(result.Value, &transformed); err != nil {
		t.Fatal(err)
	}
	if transformed.Body.Kind != "multipart" || len(transformed.Body.Items) != 1 {
		t.Fatalf("multipart Body = %+v", transformed.Body)
	}
	item := transformed.Body.Items[0]
	if !item.Enabled || item.Name != "upload" || item.Kind != "file" {
		t.Fatalf("multipart item = %+v", item)
	}
	if item.File.Path == sourcePath || item.File.Name != filepath.Base(sourcePath) || item.File.Size != int64(len(sourceBytes)) || !item.File.ReadOnly {
		t.Fatalf("multipart file descriptor = %+v", item.File)
	}
	managedBytes, err := os.ReadFile(item.File.Path)
	if err != nil || !bytes.Equal(managedBytes, sourceBytes) {
		t.Fatalf("managed multipart bytes=%v err=%v", managedBytes, err)
	}
	managedEntries, err := os.ReadDir(outputDirectory)
	if err != nil || len(managedEntries) != 1 || managedEntries[0].Name() != filepath.Base(item.File.Path) {
		t.Fatalf("managed multipart files=%v err=%v", managedEntries, err)
	}
	if sourceBytesAfter, err := os.ReadFile(sourcePath); err != nil || !bytes.Equal(sourceBytesAfter, sourceBytes) {
		t.Fatalf("multipart source was modified: bytes=%v err=%v", sourceBytesAfter, err)
	}

	errorParams := map[string]any{"directory": sourceDirectory}
	symlinkPath := filepath.Join(t.TempDir(), "upload-link.bin")
	if err := os.Symlink(sourcePath, symlinkPath); err == nil {
		errorParams["symlink"] = symlinkPath
	}
	errorResult := invoke("file_descriptor_from_file_errors", errorParams)
	if errorResult.BodyChanged || !strings.Contains(string(errorResult.Value), "X-Body-Check") {
		t.Fatalf("multipart file error result = %+v value=%s", errorResult, errorResult.Value)
	}
}

func TestWorkerFileBodyFromDescriptorStagesManagedSource(t *testing.T) {
	pool, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, bodyAPIPluginSource)
	revisionPath := manager.revisionPath(plugin.ID, plugin.ActiveRevision)
	outputDirectory := t.TempDir()
	sourcePath := filepath.Join(t.TempDir(), "request.dat")
	sourceBytes := []byte("top-level file body")
	if err := os.WriteFile(sourcePath, sourceBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	invoke := func(action string) InvokeResult {
		t.Helper()
		result, err := pool.Invoke(context.Background(), InvokeRequest{
			PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
			Path: revisionPath, Hook: "onRequest", OutputDirectory: outputDirectory,
			Context: integrationContext(map[string]any{
				"action": action, "source": sourcePath,
			}, map[string]any{}),
			Value: minimalRequestWire(),
		})
		if err != nil {
			t.Fatalf("invoke action %q: %v", action, err)
		}
		return result
	}

	result := invoke("assign_file_body")
	if !result.BodyChanged || !result.Transformed {
		t.Fatalf("file Body result = %+v", result)
	}
	var transformed struct {
		Body struct {
			Kind    string `json:"kind"`
			Storage string `json:"storage"`
			File    struct {
				Path     string `json:"path"`
				Name     string `json:"name"`
				Size     int64  `json:"size"`
				ReadOnly bool   `json:"readOnly"`
			} `json:"file"`
		} `json:"body"`
	}
	if err := json.Unmarshal(result.Value, &transformed); err != nil {
		t.Fatal(err)
	}
	if transformed.Body.Kind != "file" || transformed.Body.Storage != "file" {
		t.Fatalf("file Body wire = %+v", transformed.Body)
	}
	descriptor := transformed.Body.File
	if descriptor.Path == sourcePath || descriptor.Name != filepath.Base(sourcePath) || descriptor.Size != int64(len(sourceBytes)) || !descriptor.ReadOnly {
		t.Fatalf("file Body descriptor = %+v", descriptor)
	}
	managedBytes, err := os.ReadFile(descriptor.Path)
	if err != nil || !bytes.Equal(managedBytes, sourceBytes) {
		t.Fatalf("managed file Body bytes=%q err=%v", managedBytes, err)
	}
	managedEntries, err := os.ReadDir(outputDirectory)
	if err != nil || len(managedEntries) != 1 || managedEntries[0].Name() != filepath.Base(descriptor.Path) {
		t.Fatalf("managed file Body files=%v err=%v", managedEntries, err)
	}
	if _, err := os.Stat(sourcePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file Body source still exists after assignment: %v", err)
	}

	errorResult := invoke("file_body_errors")
	if errorResult.BodyChanged || !strings.Contains(string(errorResult.Value), "X-Body-Check") {
		t.Fatalf("file Body error result = %+v value=%s", errorResult, errorResult.Value)
	}
}

func TestWorkerRunsDocumentedHeaderJSONSharedAndBlockingExamples(t *testing.T) {
	var logMu sync.Mutex
	logs := make([]WorkerLog, 0)
	pool, manager, plugin := newPythonWorkerHarness(t, 1, func(entry WorkerLog) {
		logMu.Lock()
		logs = append(logs, entry)
		logMu.Unlock()
	})
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, documentedExampleSource(t, "header-json-shared.py"))

	requestResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: manager.revisionPath(plugin.ID, plugin.ActiveRevision), Hook: "onRequest",
		Context: integrationContext(map[string]any{"header_value": "documented"}, map[string]any{}),
		Value: map[string]any{
			"method": "POST", "url": "https://example.com/api",
			"headers": []map[string]string{},
			"body":    map[string]any{"kind": "json", "value": map[string]any{"before": true}},
		},
	})
	if err != nil {
		t.Fatalf("invoke documented request example: %v", err)
	}
	if requestResult.Blocked || !requestResult.Transformed || !requestResult.BodyChanged {
		t.Fatalf("documented request result = %+v", requestResult)
	}
	if !strings.Contains(string(requestResult.Value), `"X-FlowLens-Plugin","value":"documented"`) ||
		!strings.Contains(string(requestResult.Value), `"request_plugin":true`) {
		t.Fatalf("documented request value = %s", requestResult.Value)
	}

	var shared map[string]any
	if err := json.Unmarshal(requestResult.Shared, &shared); err != nil {
		t.Fatalf("decode documented shared state: %v", err)
	}
	responseResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: manager.revisionPath(plugin.ID, plugin.ActiveRevision), Hook: "onResponse",
		Context: integrationContext(map[string]any{}, shared),
		Value: map[string]any{
			"code": 200, "headers": []map[string]string{}, "trailers": []map[string]string{},
			"body":    map[string]any{"kind": "json", "value": map[string]any{"before": true}},
			"request": minimalRequestWire(),
		},
	})
	if err != nil {
		t.Fatalf("invoke documented response example: %v", err)
	}
	if !strings.Contains(string(responseResult.Value), `"X-FlowLens-Shared","value":"yes"`) ||
		!strings.Contains(string(responseResult.Value), `"response_plugin":true`) {
		t.Fatalf("documented response value = %s", responseResult.Value)
	}

	logMu.Lock()
	if len(logs) == 0 || logs[0].Message != "request hook completed" {
		logMu.Unlock()
		t.Fatalf("documented example logs = %#v", logs)
	}
	logMu.Unlock()

	plugin = writeAndActivatePlugin(t, manager, plugin.ID, documentedExampleSource(t, "block-request.py"))
	blockedResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: manager.revisionPath(plugin.ID, plugin.ActiveRevision), Hook: "onRequest",
		Context: integrationContext(map[string]any{"blocked_url_prefix": "https://example.com/private"}, map[string]any{}),
		Value: map[string]any{
			"method": "GET", "url": "https://example.com/private/token",
			"headers": []map[string]string{}, "body": map[string]any{"kind": "none", "value": nil},
		},
	})
	if err != nil {
		t.Fatalf("invoke documented blocking example: %v", err)
	}
	if !blockedResult.Blocked || blockedResult.Transformed {
		t.Fatalf("documented blocking result = %+v", blockedResult)
	}
}

func TestWorkerRunsDocumentedBodyKindsExample(t *testing.T) {
	pool, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, documentedExampleSource(t, "body-kinds.py"))
	revisionPath := manager.revisionPath(plugin.ID, plugin.ActiveRevision)

	fileBytes := []byte("user-selected file body")
	filePath := filepath.Join(t.TempDir(), "request.bin")
	if err := os.WriteFile(filePath, fileBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	fileDigest := sha256.Sum256(fileBytes)
	binaryBytes := []byte{0x00, 0x01, 0x7f, 0xff}
	binaryDigest := sha256.Sum256(binaryBytes)
	multipartUploadPath := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(multipartUploadPath, []byte("multipart upload"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name            string
		body            map[string]any
		params          map[string]any
		wantBodyChanged bool
		wantFragments   []string
	}{
		{
			name: "none", body: map[string]any{"kind": "none", "value": nil},
			params: map[string]any{"fill_empty_body": true}, wantBodyChanged: true,
			wantFragments: []string{`"kind":"json"`, `"flowlens":true`},
		},
		{
			name: "text", body: map[string]any{"kind": "text", "value": "hello"},
			params: map[string]any{}, wantBodyChanged: true,
			wantFragments: []string{`"kind":"text"`, `"value":"hello\nFlowLens"`},
		},
		{
			name: "xml", body: map[string]any{"kind": "xml", "value": "<root/>"},
			params: map[string]any{}, wantBodyChanged: true,
			wantFragments: []string{`"kind":"xml"`, `"value":"<!-- FlowLens -->\n<root/>"`},
		},
		{
			name: "json", body: map[string]any{"kind": "json", "value": map[string]any{"before": true}},
			params: map[string]any{}, wantBodyChanged: true,
			wantFragments: []string{`"before":true`, `"flowlens":true`},
		},
		{
			name: "binary", body: map[string]any{
				"kind": "binary", "base64": base64.StdEncoding.EncodeToString(binaryBytes),
			},
			params:        map[string]any{},
			wantFragments: []string{fmt.Sprintf(`"X-FlowLens-Body-SHA256","value":"%x"`, binaryDigest)},
		},
		{
			name: "file", body: fileBodyWireValue("file", filePath, int64(len(fileBytes))),
			params:        map[string]any{},
			wantFragments: []string{fmt.Sprintf(`"X-FlowLens-Body-SHA256","value":"%x"`, fileDigest)},
		},
		{
			name: "urlencoded", body: map[string]any{
				"kind": "urlencoded", "items": []map[string]any{{"enabled": true, "name": "before", "value": "1"}},
			},
			params: map[string]any{}, wantBodyChanged: true,
			wantFragments: []string{`"name":"flowlens","value":"enabled"`},
		},
		{
			name: "multipart", body: map[string]any{
				"kind": "multipart", "items": []map[string]any{{"enabled": true, "name": "before", "kind": "text", "value": "1"}},
			},
			params: map[string]any{"multipart_upload_path": multipartUploadPath}, wantBodyChanged: true,
			wantFragments: []string{
				`"name":"flowlens","kind":"text","value":"enabled"`,
				`"name":"upload","kind":"file"`, `"name":"upload.txt"`,
				`"filename":"flowlens-upload.bin"`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := pool.Invoke(context.Background(), InvokeRequest{
				PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
				Path: revisionPath, Hook: "onRequest", OutputDirectory: t.TempDir(),
				Context: integrationContext(test.params, map[string]any{}),
				Value: map[string]any{
					"method": "POST", "url": "https://example.com/body-kinds",
					"headers": []map[string]string{}, "body": test.body,
				},
			})
			if err != nil {
				t.Fatalf("invoke documented %s Body example: %v", test.name, err)
			}
			if result.Blocked || !result.Transformed || result.BodyChanged != test.wantBodyChanged {
				t.Fatalf("documented %s Body result = %+v", test.name, result)
			}
			value := string(result.Value)
			if !strings.Contains(value, fmt.Sprintf(`"X-FlowLens-Body-Kind","value":"%s"`, test.name)) {
				t.Fatalf("documented %s Body kind header missing: %s", test.name, value)
			}
			for _, fragment := range test.wantFragments {
				if !strings.Contains(value, fragment) {
					t.Fatalf("documented %s Body value missing %q: %s", test.name, fragment, value)
				}
			}
		})
	}

	responseResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onResponse",
		Context: integrationContext(map[string]any{}, map[string]any{}),
		Value: map[string]any{
			"code": 200, "headers": []map[string]string{}, "trailers": []map[string]string{},
			"body":    map[string]any{"kind": "unavailable", "value": nil, "streaming": true},
			"request": minimalRequestWire(),
		},
	})
	if err != nil {
		t.Fatalf("invoke documented unavailable Body example: %v", err)
	}
	if responseResult.BodyChanged || !responseResult.Transformed ||
		!strings.Contains(string(responseResult.Value), `"X-FlowLens-Body-Readable","value":"no"`) {
		t.Fatalf("documented unavailable Body result = %+v value=%s", responseResult, responseResult.Value)
	}

	xmlResponseResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onResponse",
		Context: integrationContext(map[string]any{}, map[string]any{}),
		Value: map[string]any{
			"code": 200, "headers": []map[string]string{{"name": "Content-Type", "value": "application/xml"}},
			"trailers": []map[string]string{}, "body": map[string]any{"kind": "xml", "value": "<root/>"},
			"request": minimalRequestWire(),
		},
	})
	if err != nil {
		t.Fatalf("invoke documented XML response Body example: %v", err)
	}
	if !xmlResponseResult.BodyChanged || !xmlResponseResult.Transformed ||
		!strings.Contains(string(xmlResponseResult.Value), `"kind":"xml"`) ||
		!strings.Contains(string(xmlResponseResult.Value), `"value":"<!-- FlowLens response -->\n<root/>"`) {
		t.Fatalf("documented XML response Body result = %+v value=%s", xmlResponseResult, xmlResponseResult.Value)
	}
}

func TestWorkerExampleLargeBodyFile(t *testing.T) {
	pool, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, documentedExampleSource(t, "large-body-file.py"))
	revisionPath := manager.revisionPath(plugin.ID, plugin.ActiveRevision)
	inputDirectory := t.TempDir()
	outputDirectory := t.TempDir()
	scriptTempDirectory := t.TempDir()
	requestBody := bytes.Repeat([]byte("FlowLens-large-request-0123456789\n"), 128*1024)
	requestPath := filepath.Join(inputDirectory, "request.bin")
	if err := os.WriteFile(requestPath, requestBody, 0o600); err != nil {
		t.Fatal(err)
	}

	requestResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onRequest", OutputDirectory: outputDirectory,
		Context: integrationContext(map[string]any{"temp_dir": scriptTempDirectory}, map[string]any{}),
		Value: map[string]any{
			"method": "POST", "url": "https://example.com/upload",
			"headers": []map[string]string{},
			"body": map[string]any{
				"kind": "binary", "storage": "file", "size": len(requestBody),
				"file": map[string]any{
					"path": requestPath, "name": "request.bin", "size": len(requestBody), "readOnly": true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("invoke large Body request example: %v", err)
	}
	digest := sha256.Sum256(requestBody)
	if !strings.Contains(string(requestResult.Value), fmt.Sprintf(`"X-Body-SHA256","value":"%x"`, digest)) {
		t.Fatalf("request result = %s", requestResult.Value)
	}

	responseBody := bytes.Repeat([]byte("response-chunk-abcdef\n"), 96*1024)
	responsePath := filepath.Join(inputDirectory, "response.bin")
	if err := os.WriteFile(responsePath, responseBody, 0o600); err != nil {
		t.Fatal(err)
	}
	responseResult, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: revisionPath, Hook: "onResponse", OutputDirectory: outputDirectory,
		Context: integrationContext(map[string]any{"temp_dir": scriptTempDirectory}, map[string]any{}),
		Value: map[string]any{
			"code": 200, "headers": []map[string]string{}, "trailers": []map[string]string{},
			"body": map[string]any{
				"kind": "binary", "storage": "file", "size": len(responseBody),
				"file": map[string]any{
					"path": responsePath, "name": "response.bin", "size": len(responseBody), "readOnly": true,
				},
			},
			"request": minimalRequestWire(),
		},
	})
	if err != nil {
		t.Fatalf("invoke large Body response example: %v", err)
	}
	if !responseResult.BodyChanged {
		t.Fatalf("response result did not change Body: %+v", responseResult)
	}
	var responseWireValue responseWire
	if err := json.Unmarshal(responseResult.Value, &responseWireValue); err != nil {
		t.Fatalf("decode response result: %v", err)
	}
	if responseWireValue.Body.Storage != "file" || responseWireValue.Body.File == nil {
		t.Fatalf("response Body wire = %+v", responseWireValue.Body)
	}
	wantResponse := append([]byte("processed by FlowLens\n"), responseBody...)
	gotResponse, err := os.ReadFile(responseWireValue.Body.File.Path)
	if err != nil {
		t.Fatalf("read staged response: %v", err)
	}
	if !bytes.Equal(gotResponse, wantResponse) {
		t.Fatalf("staged response bytes=%d, want %d", len(gotResponse), len(wantResponse))
	}
	entries, err := os.ReadDir(scriptTempDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("script-owned temporary files remain: %#v", entries)
	}
	if err := os.Remove(responseWireValue.Body.File.Path); err != nil {
		t.Fatalf("remove staged response fixture: %v", err)
	}
}

func TestWorkerReportsTracebackAndInvalidResult(t *testing.T) {
	pool, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, integrationPluginSource)
	for action, want := range map[string]string{"exception": "request exploded", "invalid": "must return Request or None"} {
		_, err := pool.Invoke(context.Background(), InvokeRequest{
			PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
			Path: manager.revisionPath(plugin.ID, plugin.ActiveRevision), Hook: "onRequest",
			Context: integrationContext(map[string]any{"action": action}, map[string]any{}),
			Value:   minimalRequestWire(),
		})
		var executionError *PythonExecutionError
		if !errors.As(err, &executionError) {
			t.Fatalf("action %s error = %T %v", action, err, err)
		}
		if !strings.Contains(executionError.Message, want) || !strings.Contains(executionError.Traceback, "onRequest") {
			t.Fatalf("action %s execution error = %+v", action, executionError)
		}
	}
}

func TestWorkerTimeoutCancellationCrashAndProtocolCorruptionRestart(t *testing.T) {
	pool, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, integrationPluginSource)
	invoke := func(ctx context.Context, action string) error {
		_, err := pool.Invoke(ctx, InvokeRequest{
			PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
			Path: manager.revisionPath(plugin.ID, plugin.ActiveRevision), Hook: "onRequest",
			Context: integrationContext(map[string]any{"action": action}, map[string]any{}),
			Value:   minimalRequestWire(),
		})
		return err
	}

	deadlineContext, cancelDeadline := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelDeadline()
	if err := invoke(deadlineContext, "sleep"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	if err := invoke(context.Background(), ""); err != nil {
		t.Fatalf("replacement after timeout: %v", err)
	}

	cancelContext, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	if err := invoke(cancelContext, "sleep"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	if err := invoke(context.Background(), ""); err != nil {
		t.Fatalf("replacement after cancellation: %v", err)
	}

	if err := invoke(context.Background(), "crash"); err == nil {
		t.Fatal("worker crash unexpectedly succeeded")
	}
	if err := invoke(context.Background(), ""); err != nil {
		t.Fatalf("replacement after crash: %v", err)
	}

	if err := invoke(context.Background(), "corrupt"); !errors.Is(err, errProtocolFrameTooLarge) && !strings.Contains(fmt.Sprint(err), "64 MiB") {
		t.Fatalf("protocol corruption error = %v", err)
	}
	if err := invoke(context.Background(), ""); err != nil {
		t.Fatalf("replacement after protocol corruption: %v", err)
	}
}

func TestWorkerPoolRunsTwoHooksConcurrentlyAndBoundsAcquisition(t *testing.T) {
	pool, manager, plugin := newPythonWorkerHarness(t, 2, nil)
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, integrationPluginSource)
	started := time.Now()
	errorsChannel := make(chan error, 4)
	for range 4 {
		go func() {
			_, err := pool.Invoke(context.Background(), InvokeRequest{
				PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
				Path: manager.revisionPath(plugin.ID, plugin.ActiveRevision), Hook: "onRequest",
				Context: integrationContext(map[string]any{"action": "short_sleep"}, map[string]any{}),
				Value:   minimalRequestWire(),
			})
			errorsChannel <- err
		}()
	}
	for range 4 {
		if err := <-errorsChannel; err != nil {
			t.Fatalf("concurrent invoke: %v", err)
		}
	}
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("bounded two-worker execution took %s", elapsed)
	}

	first, releaseFirst, err := pool.acquire(context.Background())
	if err != nil || first == nil {
		t.Fatalf("acquire first worker: %v", err)
	}
	second, releaseSecond, err := pool.acquire(context.Background())
	if err != nil || second == nil {
		releaseFirst(true)
		t.Fatalf("acquire second worker: %v", err)
	}
	waitContext, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := pool.acquire(waitContext); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded acquisition error = %v", err)
	}
	releaseSecond(true)
	releaseFirst(true)
}

func TestWorkerRejectsAsyncHookDuringValidation(t *testing.T) {
	_, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	asyncSource := `from flowlens import *

async def onRequest(context, request):
    return request

def onResponse(context, response):
    return response
`
	if _, err := manager.writeFile(context.Background(), plugin.ID, mainFileName, []byte(asyncSource)); err == nil || !strings.Contains(err.Error(), "synchronous") {
		t.Fatalf("async validation error = %v", err)
	}
}

func TestWorkerValidationRejectsSyntaxImportPresenceAndCallabilityErrors(t *testing.T) {
	_, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	tests := map[string]struct {
		source string
		want   string
	}{
		"syntax": {
			source: "def onRequest(:\n    pass\n",
			want:   "invalid syntax",
		},
		"import": {
			source: "from flowlens import *\nimport dependency_that_does_not_exist\n",
			want:   "No module named",
		},
		"presence": {
			source: "from flowlens import *\n\ndef onRequest(context, request):\n    return request\n",
			want:   "onResponse",
		},
		"callability": {
			source: "from flowlens import *\n\nonRequest = 1\n\ndef onResponse(context, response):\n    return response\n",
			want:   "callable",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := manager.writeFile(context.Background(), plugin.ID, mainFileName, []byte(test.source)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validation error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestWorkerRejectsOversizedSharedStateBeforeAndAfterHook(t *testing.T) {
	pool, manager, plugin := newPythonWorkerHarness(t, 1, nil)
	source := `from flowlens import *

def onRequest(context, request):
    if context.params.get("expand"):
        context.shared["large"] = "x" * (1024 * 1024 + 1)
    return request

def onResponse(context, response):
    return response
`
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, source)
	large := strings.Repeat("x", maxSharedStateBytes+1)
	for name, hookContext := range map[string]map[string]any{
		"before": integrationContext(map[string]any{}, map[string]any{"large": large}),
		"after":  integrationContext(map[string]any{"expand": true}, map[string]any{}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := pool.Invoke(context.Background(), InvokeRequest{
				PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
				Path: manager.revisionPath(plugin.ID, plugin.ActiveRevision), Hook: "onRequest",
				Context: hookContext, Value: minimalRequestWire(),
			})
			if err == nil || !strings.Contains(err.Error(), "1 MiB") && !strings.Contains(err.Error(), fmt.Sprint(maxSharedStateBytes)) {
				t.Fatalf("oversized shared error = %v", err)
			}
		})
	}
}

func TestWorkerImportsDependencyFromSelectedVirtualEnvironment(t *testing.T) {
	basePython := requirePython311(t)
	venvRoot := filepath.Join(t.TempDir(), "venv")
	command := exec.Command(basePython, "-m", "venv", "--without-pip", venvRoot)
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("create virtual environment: %v: %s", err, output)
	}
	venvPython := filepath.Join(venvRoot, "bin", "python")
	if runtime.GOOS == "windows" {
		venvPython = filepath.Join(venvRoot, "Scripts", "python.exe")
	}
	siteOutput, err := exec.Command(venvPython, "-c", "import site; print(site.getsitepackages()[0])").Output()
	if err != nil {
		t.Fatalf("query venv site-packages: %v", err)
	}
	sitePackages := strings.TrimSpace(string(siteOutput))
	if err := os.WriteFile(filepath.Join(sitePackages, "requests.py"), []byte("__version__ = 'documented-venv'\n"), 0o600); err != nil {
		t.Fatalf("write venv dependency: %v", err)
	}

	pool, manager, plugin := newPythonWorkerHarnessWithInterpreter(t, venvPython, 1, nil)
	source := documentedExampleSource(t, "third-party-package.py")
	plugin = writeAndActivatePlugin(t, manager, plugin.ID, source)
	result, err := pool.Invoke(context.Background(), InvokeRequest{
		PluginID: plugin.ID, PluginName: plugin.Name, Revision: plugin.ActiveRevision,
		Path: manager.revisionPath(plugin.ID, plugin.ActiveRevision), Hook: "onRequest",
		Context: integrationContext(map[string]any{}, map[string]any{}), Value: minimalRequestWire(),
	})
	if err != nil {
		t.Fatalf("invoke venv dependency plugin: %v", err)
	}
	if !strings.Contains(string(result.Value), "documented-venv") {
		t.Fatalf("venv dependency result = %s", result.Value)
	}
}

func TestWorkerPoolShutdownTerminatesChildren(t *testing.T) {
	pythonPath := requirePython311(t)
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	pool, err := newWorkerPool(workerPoolConfig{InterpreterPath: pythonPath, RuntimeRoot: runtimeRoot, Size: 1})
	if err != nil {
		t.Fatalf("newWorkerPool: %v", err)
	}
	if _, err := pool.Probe(context.Background()); err != nil {
		t.Fatalf("probe: %v", err)
	}
	pool.mu.Lock()
	var worker *pythonWorker
	for candidate := range pool.workers {
		worker = candidate
	}
	pool.mu.Unlock()
	if worker == nil {
		t.Fatal("probe did not create a worker")
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pool.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case <-worker.waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit after shutdown")
	}
	// waitDone closes only after Cmd.Wait returns. On Unix, Exited reports
	// false when the process was terminated by a signal, which is the expected
	// shutdown path for the worker process group.
	if worker.command.ProcessState == nil {
		t.Fatal("worker process state was not recorded")
	}
}

func TestInterpreterPathMustBeAbsoluteRegularExecutable(t *testing.T) {
	for _, value := range []string{"", "python", filepath.Join(t.TempDir(), "missing-python")} {
		if _, err := validateInterpreterPath(value); err == nil {
			t.Fatalf("invalid interpreter path %q was accepted", value)
		}
	}
	directory := t.TempDir()
	if _, err := validateInterpreterPath(directory); err == nil {
		t.Fatal("interpreter directory was accepted")
	}
}

func newPythonWorkerHarness(t *testing.T, size int, sink WorkerLogSink) (*workerPool, *packageManager, *Plugin) {
	t.Helper()
	return newPythonWorkerHarnessWithInterpreter(t, requirePython311(t), size, sink)
}

func newPythonWorkerHarnessWithInterpreter(t *testing.T, interpreter string, size int, sink WorkerLogSink) (*workerPool, *packageManager, *Plugin) {
	t.Helper()
	repository := newTestRepository(t)
	root := t.TempDir()
	runtimeRoot := filepath.Join(root, "runtime")
	pool, err := newWorkerPool(workerPoolConfig{
		InterpreterPath: interpreter, RuntimeRoot: runtimeRoot, Size: size, LogSink: sink,
	})
	if err != nil {
		t.Fatalf("newWorkerPool: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pool.Shutdown(ctx)
	})
	manager, err := newPackageManager(repository, filepath.Join(root, "packages"), runtimeRoot, pool)
	if err != nil {
		t.Fatalf("newPackageManager: %v", err)
	}
	plugin, err := manager.createPlugin(context.Background(), CreatePluginInput{
		ID: testPluginIDOne, Name: "Integration Plugin", ParamsJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	return pool, manager, plugin
}

func writeAndActivatePlugin(t *testing.T, manager *packageManager, pluginID, source string) *Plugin {
	t.Helper()
	plugin, err := manager.writeFile(context.Background(), pluginID, mainFileName, []byte(source))
	if err != nil {
		t.Fatalf("write and activate plugin: %v", err)
	}
	return plugin
}

func requirePython311(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("python")
	if err != nil {
		t.Skip("Python is unavailable")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve Python path: %v", err)
	}
	output, err := exec.Command(path, "-c", "import sys; print(f'{sys.version_info[0]}.{sys.version_info[1]}')").Output()
	if err != nil {
		t.Skipf("query Python version: %v", err)
	}
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(output)), "%d.%d", &major, &minor); err != nil || major != 3 || minor < 11 {
		t.Skipf("Python 3.11+ is required; got %s", output)
	}
	return path
}

func documentedExampleSource(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "docs", "examples", "python-plugins", name)
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read documented example %q: %v", name, err)
	}
	return string(value)
}

func integrationContext(params, shared map[string]any) map[string]any {
	return map[string]any{
		"id": "request-1", "timestamp": time.Now().UnixMicro(),
		"original_url": "https://example.com/api", "original_method": "POST",
		"params": params, "shared": shared,
		"transport": map[string]any{"protocol": "auto", "proxy_mode": "none"},
	}
}

func minimalRequestWire() map[string]any {
	return map[string]any{
		"method": "GET", "url": "https://example.com/",
		"headers": []map[string]string{}, "body": map[string]any{"kind": "none", "value": nil},
	}
}
