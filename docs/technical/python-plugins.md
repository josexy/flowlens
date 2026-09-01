# HTTP Request Editor Python Plugins

**English** | [简体中文](python-plugins.zh-CN.md)

This guide is for FlowLens users who want to customize HTTP requests and responses with Python. It covers setup, the two scripting scopes, the API, examples, limits, security, and troubleshooting.

FlowLens provides two Python scripting scopes for HTTP Request Editor: global Python plugins, which are managed in the **Python Plugins** workbench and selected by matching rules, and a current-request script bound to one HTTP request. Either scope can inspect or mutate a request before transport and inspect or mutate the returned response. This feature applies only to HTTP Request Editor and never enters ordinary MITM capture, resend, or WebSocket Client. A current-request script source can be saved with an HTTP request in API Collection, but it is not written to Settings, HAR, history, or HBIN data.

Python plugin support is optional and disabled by default.

## Choose a scripting scope

| Scope | Best for | Configuration | Persistence |
| --- | --- | --- | --- |
| Global Python plugin | Reusable behavior for multiple requests or endpoints | Managed files, matching rules, per-plugin params, plugin order, and an enable switch | Saved by FlowLens and available in later sessions |
| Current-request script | Testing or customizing one HTTP request | One script and a per-tab enable switch; no matching rules or configurable params | Source can be saved with an API Collection HTTP request; the enable switch is not saved and defaults off when reopened |

Both scopes use the same synchronous `onRequest` and `onResponse` API. If you only want to try scripting, start with a current-request script. Use a global plugin when the behavior should be reusable or selected automatically by method and URL.

## Security model

Run only code you trust. Plugins are normal local Python code and run with the same operating-system user permissions as FlowLens. They can read and write local files, access the network, start processes, import installed packages, and expose data through logs. The Worker's isolated Python mode (`-I`) makes imports more predictable; it is not a security sandbox.

Review both the plugin source and every third-party dependency before enabling them. FlowLens does not install Python packages automatically.

## Set up Python

Install CPython 3.11 or newer, or create a dedicated virtual environment. A dedicated environment keeps plugin dependencies separate from other Python applications.

Windows:

```powershell
py -3.11 -m venv C:\venvs\flowlens-plugins
C:\venvs\flowlens-plugins\Scripts\python.exe -m pip install requests
```

macOS or Linux:

```shell
python3.11 -m venv "$HOME/.venvs/flowlens-plugins"
"$HOME/.venvs/flowlens-plugins/bin/python" -m pip install requests
```

Then configure FlowLens:

1. Open **Settings > Python**.
2. Select **Detect** to search platform registrations, `PATH`, the active environment, and common installation locations, then choose an interpreter. You can also choose an absolute executable path manually; for a virtual environment, choose its `python.exe` on Windows or `bin/python` on macOS/Linux.
3. Select **Test** and confirm that FlowLens reports the interpreter as available.
4. Set the per-hook timeout. The default is 5,000 ms; accepted values are 100–60,000 ms.
5. Enable Python plugins and accept the trusted-code warning.

Detection is bounded and does not scan the entire disk. Select a virtual environment manually when it is outside the active environment or common installation locations.

If runtime validation fails, FlowLens does not save the enabled state. FlowLens starts normally when Python is missing or the feature is disabled.

## Five-minute quick start

After configuring and enabling Python in **Settings > Python**:

1. Open an HTTP Request Editor tab and select **Script**.
2. Paste the script below and turn on the script switch.
3. Send the request.
4. Open the response-side **Console** tab. It should contain the request URL and response status.

```python
from flowlens import *


def onRequest(context, request):
    print(f"request {request.method} {request.url}")
    request.headers.set("X-FlowLens-Script", "enabled")
    return request


def onResponse(context, response):
    print(f"response {response.code}")
    return response
```

This script changes only the request sent from that Request Editor tab. It does not affect ordinary captured traffic. Continue with the global-plugin workflow below when you want to reuse the behavior.

## Create and run a global plugin

1. Open the **Python Plugins** workbench and create a plugin.
2. Edit `main.py`. Both `onRequest` and `onResponse` must be present, callable, synchronous functions.
3. Add at least one enabled matching rule in the **Rules** tab.
4. Optionally add a JSON object in **Params**. Params are available to both hooks as `context.params`.
5. Save and validate the plugin, then enable it in the plugin list.
6. Open an HTTP Request Editor tab and leave its global Python plugin switch on when sending.

The global-plugin switch in HTTP Request Editor is transient and defaults off for a new tab. It is a per-tab bypass only; it is not saved in API Collection requests. A global plugin runs only when the Python plugin master switch in Settings, the current request's global-plugin switch, the plugin's own switch, its validation state, and at least one rule all allow it.

Saving or validating creates an immutable revision. A request send snapshots the matched plugin order, revision, and params before the first hook, so edits made during an in-flight request affect only later sends. If a changed package fails validation, the last good revision remains active.

## Configure plugin params

**Params** are configuration values for one global plugin. They are not HTTP query parameters and are not automatically added to the URL, headers, or body. They affect a request only when the plugin reads `context.params` and uses a value to change or block that request.

The Params editor accepts one JSON object. For example:

```json
{
  "header_value": "staging",
  "blocked_url_prefix": "https://api.example.com/private/",
  "feature_enabled": true
}
```

Read the values from either hook:

```python
def onRequest(context, request):
    if context.params.get("feature_enabled", False):
        value = str(context.params.get("header_value", "default"))
        request.headers.set("X-Environment", value)
    return request
```

- Params are saved with the global plugin and are isolated from every other plugin.
- Each request send receives a deeply read-only snapshot. Nested objects are read-only and JSON arrays appear as tuples.
- Saving new params affects later sends, not a request that is already running.
- The value must be a JSON object such as `{}`; arrays, strings, numbers, booleans, and `null` are not accepted as the top-level value.
- The encoded object is limited to 1 MiB.
- Current-request scripts have no Params editor and receive an empty `context.params` object.

Use `context.shared`, not `context.params`, for mutable state that must pass from `onRequest` to `onResponse`. Params are persisted configuration rather than a secret store. FlowLens does not automatically send or log them, but plugin code can read, transmit, or print them, so avoid storing credentials unless you trust the complete script and its dependencies.

## Use a script for the current request

For a single request, open the **Script** tab inside HTTP Request Editor, edit the template, and enable it. Saving or updating the HTTP request in API Collection writes the script source to SQLite. Reopening that request restores the source but always leaves the enable switch off, preventing persisted code from running automatically. Closing a new tab that has not been saved to API Collection still discards its script. The source is not written to Settings, HAR, history, or HBIN, and source edits count as unsaved tab changes. Both synchronous `onRequest` and `onResponse` hooks are required, just as they are for global plugins.

Current-request scripts share the configured interpreter, Worker pool, hook timeout, SDK, permissions, and trusted-code security model. The source is limited to 1 MiB. Each send receives an isolated temporary revision, so module globals are not reused by a later send; use `context.shared` to pass JSON state from the request hook to the response hook.

When global plugins and a current-request script are both active, request hooks run in this order:

```text
global plugins -> current-request script -> network
```

Response hooks unwind in the opposite order:

```text
network -> current-request script -> global plugins
```

This places the current-request script closest to the network boundary. Turning off the global-plugin switch for the current request does not turn off its script; use the switch in the **Script** tab for that script.

## Read script output in the Console

The response-side **Console** tab streams `stdout`, `stderr`, and `context.log` entries while the current send is running. Entries are correlated by the send's execution ID, so output from other Request Editor tabs is excluded. The console renders the latest 1,000 entries for the tab's latest send in a read-only Monaco editor. Automatic wrapping starts enabled and can be toggled; the toolbar can also copy all output, save it to a local file, or clear the tab's current output. The console includes every global plugin and the current-request script in the same execution chain, identified by the global plugin ID or the **Current Request Script** label.

## Package layout and manifest

FlowLens creates and owns each plugin directory:

```text
<plugin-id>/
|-- manifest.json
|-- main.py
`-- helpers.py
```

`main.py` is the fixed entry point. Import other files in the package relatively, for example:

```python
from .helpers import build_token
```

Workers load revisions under private module names. FlowLens currently uses two Worker processes, so module globals are not guaranteed to carry between calls. Use `context.shared` for request-to-response state and external storage for intentionally persistent state.

Manifest schema v1 is strict and rejects unknown fields:

```json
{
  "schemaVersion": 1,
  "apiVersion": 1,
  "id": "d9712a7a-5b17-4e6a-a6d2-cb2e8149e734",
  "name": "Example Plugin",
  "description": "Mutates matching request traffic"
}
```

The `id` must be the canonical lowercase UUID assigned to the package and must match its managed directory. Prefer editing name and description through the FlowLens UI so the database and manifest stay consistent.

There is no separate `flowlens` package to install with pip. FlowLens supplies the API v1 module to its Worker process.

## Matching rules and ordering

A rule contains an enabled state, an HTTP method, and a URL wildcard:

- Method is an uppercase exact HTTP token such as `GET` or `POST`, or `*` for every method.
- URL matching uses the normalized full URL. `*` matches zero or more characters and `?` matches exactly one character; regular expressions are not supported.
- Scheme and host are case-insensitive for normal absolute URL patterns. Path and query matching remain case-sensitive.
- A plugin with multiple matching rules still runs only once.
- All plugins are matched once against the original HTTP method and URL. A preceding hook changing the URL does not change the already-snapshotted plugin set.
- Request hooks run in plugin-list order. Response hooks run in the reverse order, which gives wrapper-style behavior.

For example, `GET` plus `https://api.example.com/v?/items/*` matches `https://api.example.com/v1/items/42`.

## Hook contract

The minimal plugin is:

```python
from flowlens import *


def onRequest(context, request):
    return request


def onResponse(context, response):
    return response
```

Hooks must not be `async def` and must return the same SDK object they received or `None`:

- `onRequest` returning `Request` continues to the next plugin and then transport.
- `onRequest` returning `None` blocks the request before a network connection is opened.
- `onResponse` returning `Response` continues the reverse response chain.
- `onResponse` returning `None` marks the network response as blocked and suppresses its transformed presentation.
- Returning another type, raising an exception, timing out, crashing, or producing invalid HTTP data fails the hook.

A Request-hook failure is fail-closed: no request is sent. A Response-hook failure is fail-open: FlowLens returns the untouched wire response with plugin diagnostics. Response mutations change the HTTP Request Editor result presentation; they do not rewrite captured wire truth or transport metrics.

## Context API

`context` exposes these properties:

| Property | Type and semantics |
| --- | --- |
| `id` | Request execution ID. |
| `timestamp` | Send start time as Unix microseconds. |
| `original_url` | URL before any plugin mutation. |
| `original_method` | Method before any plugin mutation. |
| `plugin_id`, `plugin_name` | Current plugin identity. |
| `params` | Deeply read-only values from the plugin's JSON object. JSON arrays appear as tuples. |
| `transport` | Deeply read-only transport metadata. |
| `shared` | Mutable, JSON-serializable object isolated to this plugin and request send. |
| `log` | `debug`, `info`, `warning`, and `error` methods. |

`context.transport` contains `protocol`, `proxy_mode`, `tls_client_hello_profile`, and `http2_fingerprint`. Proxy credentials are not exposed.

`context.shared` is returned to Go after each hook and passed back to that same plugin in the response phase, even if a different Worker executes it. It must remain a JSON object and its encoded size must not exceed 1 MiB. State is not shared between different plugins or different sends.

## Request and response API

`request` has mutable `method`, `url`, `path`, `queries`, `headers`, and `body` properties. `scheme`, `host`, `port`, and `content_type` are read-only views derived from the current URL and headers. Assigning `url` reparses `path` and `queries`; changing `path` or `queries` rebuilds `url`. The final result is revalidated and then passed through the existing request URL, generated-header, framing, content-encoding, proxy, protocol, TLS, and fingerprint pipeline. Methods must remain uppercase and generated/pseudo/framing headers remain authoritative.

`response` has mutable `code`, `headers`, `trailers`, and `body` properties. `protocol` is the actual upstream response protocol, such as `HTTP/1.1` or `HTTP/2.0`, and is read-only. `status_text` initially preserves the upstream status line text and is also read-only; assigning a different `code` updates it to the standard text for the new code. `content_type` is also read-only.

`response.request` is a read-only semantic snapshot of the request handed to transport. It exposes `method`, `url`, `scheme`, `host`, `port`, `path`, `queries`, `content_type`, `headers`, and `body`. The snapshot does not expose FlowLens body-storage fields or managed temporary-file paths.

`Headers` preserves field order, duplicate names, original casing, and empty values. Name lookup is case-insensitive:

| Operation | Behavior |
| --- | --- |
| `headers.get(name, default=None)` | Return the first matching value. |
| `headers.get_all(name)` | Return all matching values in field order. |
| `headers.set(name, value)` | Replace all matches at the first match position, or append. |
| `headers.add(name, value)` | Append another field, including a duplicate. |
| `headers.remove(name)` | Remove every matching field. |
| `headers.clear()` | Remove every field. |

Iteration yields `HeaderField` objects with mutable `name` and `value`. The frozen request snapshot instead yields `(name, value)` tuples.

`Queries` preserves field order, duplicate names, empty values, and an unchanged URL's original query encoding. Query names are case-sensitive:

| Operation | Behavior |
| --- | --- |
| `queries.get(name, default=None)` | Return the first matching decoded value. |
| `queries.get_all(name)` | Return all matching decoded values in order. |
| `queries.set(name, value)` | Replace all matches at the first match position, or append. |
| `queries.add(name, value)` | Append another field, including a duplicate. |
| `queries.remove(name)` | Remove every matching field. |
| `queries.clear()` | Remove every field. |
| `queries.to_string()` | Return the query string without the leading `?`. |

Iteration over mutable queries yields `QueryField` objects. The frozen request snapshot yields `(name, value)` tuples and only supports `get`, `get_all`, and `to_string`. Once queries are modified, FlowLens encodes the rebuilt query with Python's standard URL encoding rules; for example, a space becomes `+`.

`content_type` is a read-only view of the first `Content-Type` header on `request`, `response`, and `response.request`. For a non-empty request Body, FlowLens replaces the outgoing value according to the final Body kind. To change a response Content-Type, use `response.headers.set("Content-Type", value)` or `response.headers.remove("Content-Type")`.

## Body API

`Body` exposes two read-only semantic properties:

| Member | Semantics |
| --- | --- |
| `kind` | `none`, `text`, `json`, `xml`, `binary`, `file`, `urlencoded`, `multipart`, or `unavailable`. |
| `value` | The Python value associated with `kind`. Reading ordinary content may materialize the complete Body in Worker memory. |

The value types are:

| `kind` | `value` |
| --- | --- |
| `none` | `None` |
| `text`, `xml` | `str` |
| `json` | A decoded JSON value, normally `dict` or `list` |
| `binary` | `bytes` |
| `file` | Read-only `FileDescriptor` with `path`, `name`, and `size` |
| `urlencoded` | Mutable `list[URLEncodedField]` |
| `multipart` | Mutable `list[MultipartPart]` |
| `unavailable` | Reading raises `ValueError` |

FlowLens may transparently store large text, JSON, XML, or binary content in a managed file. This does not change `kind`; reading `value` still returns the semantic `str`, JSON value, or `bytes` and may load all content into memory. The `file` kind is different: it represents a user-selected request file, so its value remains a `FileDescriptor`.

Each `MultipartPart` has mutable `enabled`, `name`, `value`, `file`, and `filename` properties. `filename` is independent from the descriptor's source name. FlowLens generates each part's `Content-Disposition` from `name` and `filename`, falling back to the descriptor name for a file part. File parts use `application/octet-stream` as their `Content-Type`.

`response.request.body` is a read-only `BodySnapshot`. Its `kind` keeps the same semantic meaning. Ordinary values are immutable snapshots; a `file` value is a `FileSnapshot` exposing only `name` and `size`, and multipart file parts use the same path-free snapshot. Call `response.request.body.write_file(absolute_path)` to copy a readable ordinary or file Body without accessing its managed storage path. As with `Body`, reading a large ordinary snapshot value may materialize it completely in Worker memory.

Response kinds are inferred from `Content-Type` and the received bytes. Valid JSON responses use `json`; valid UTF-8 responses with `application/xml`, `text/xml`, or a media type ending in `+xml` use `xml`; binary media types or invalid UTF-8 use `binary`; other UTF-8 responses use `text`.

### Direct assignment

Assign common Python values directly to either a request or response Body:

```python
request.body = None
request.body = "Hello World"
request.body = b"\x00\x01\x02"
request.body = {"name": "FlowLens", "enabled": True}
request.body = ["one", "two"]
```

| Assigned value | Result |
| --- | --- |
| `None` | Empty Body |
| `str` | UTF-8 text |
| `bytes`, `bytearray`, or `memoryview` | Binary Body |
| `dict` or `list` | Compact JSON Body |
| `Body` | Explicit semantic kind, or reuse an existing Body |

Other values raise `TypeError`. During request normalization, FlowLens derives the outgoing `Content-Type` from the final non-empty Body kind and replaces any existing value. Clearing a Body does not remove an existing `Content-Type`; remove that header explicitly when necessary.

Use an explicit `Body` when an ordinary Python value cannot identify the intended kind:

```python
request.body = Body("xml", "<root>FlowLens</root>")
request.body = Body(
    "file",
    FileDescriptor.from_file("C:/files/request.bin"),
)
request.body = Body(
    "urlencoded",
    [URLEncodedField("name", "FlowLens")],
)
request.body = Body(
    "multipart",
    [
        MultipartPart("name", "FlowLens"),
        MultipartPart(
            "upload",
            file=FileDescriptor.from_file("C:/files/report.pdf"),
            filename="monthly-report.pdf",
        ),
    ],
)
```

`kind` and `value` cannot be reassigned on an existing Body. To replace content, assign a new value or `Body` to `request.body` or `response.body`. Structured item objects and their containing lists remain mutable.

For JSON, read the value, modify it, and assign it back. Reassignment is important because a large JSON Body may have been materialized from a managed file:

```python
value = request.body.value
if isinstance(value, str):
    value = json.loads(value)
if isinstance(value, dict):
    value["processed"] = True
    request.body = value
```

### File workflow and memory responsibility

The public API has no Body-size threshold or storage mode. Reading `value` for ordinary content may load the complete Body into Python memory. The script author owns that memory use and the time it consumes within the hook deadline.

Use `write_file(path)` to copy raw Body bytes to an absolute destination without first materializing them. To replace a Body from a local file, construct a `FileDescriptor` and explicitly select the semantic kind:

```python
response.body.write_file("C:/response-original.bin")

with open("C:/response-original.bin", "rb") as source, \
        open("C:/response-updated.bin", "wb") as output:
    while chunk := source.read(1024 * 1024):
        output.write(process(chunk))

response.body = Body(
    "binary",
    FileDescriptor.from_file("C:/response-updated.bin"),
)
```

Descriptor-backed request Bodies support `text`, `json`, `xml`, `binary`, and `file`. Descriptor-backed response Bodies support `text`, `json`, `xml`, and `binary`; `file` remains request-only. `write_file()` does not support URL-encoded or multipart Bodies.

Use `FileDescriptor.from_file(path)` when the request should retain the semantic `file` kind. It validates the path and reads the basename and size automatically:

```python
request.body = Body(
    "file",
    FileDescriptor.from_file("C:/files/request.bin"),
)
```

The same descriptor constructor creates new multipart file parts:

```python
request.body = Body(
    "multipart",
    [
        MultipartPart("description", "monthly report"),
        MultipartPart(
            "upload",
            file=FileDescriptor.from_file("C:/files/report.pdf"),
            filename="monthly-report.pdf",
        ),
    ],
)
```

The source must be an absolute, non-symlink regular file. Assigning a descriptor-backed `Body` to `request.body` or `response.body` immediately copies the source into managed temporary storage, so the script may delete its source after assignment, including on Windows. Both `file` and multipart are request-only Body kinds.

The kind is always explicit; there is no extension-based inference. Assigning the path string itself creates a text Body rather than reading the file:

```python
request.body = Body("binary", FileDescriptor.from_file("C:/files/request.bin"))
request.body = "C:/files/request.bin"  # text containing the path
```

Large inline replacements are automatically staged before crossing IPC. The 4 MiB threshold and the managed file location are internal implementation details, not Python API.

### Temporary-file ownership and failures

- Paths passed to `FileDescriptor.from_file()` must be absolute, non-symlink regular files. Close writers before constructing the descriptor.
- Assigning `Body(kind, descriptor)` to a request or response copies a pending source into the current FlowLens session before the assignment returns. The script may then delete its source.
- New multipart file parts are mutable list items and therefore stage during request serialization rather than owner assignment. Do not delete or replace their source before the hook returns.
- FlowLens removes managed files after success, blocking, hook failure, timeout, cancellation, or Worker exit.
- Invalid request files fail closed. Invalid response files fail open and return the untouched response with diagnostics.
- An `unavailable` or streaming SSE Body rejects reads and modification. Python never receives SSE event chunks.

## Tested examples

The following copy-ready files are executed by the real-CPython Worker integration tests.

### Handling different Body kinds

[`docs/examples/python-plugins/body-kinds.py`](../examples/python-plugins/body-kinds.py) demonstrates every request Body kind plus the `unavailable` SSE response state. The important patterns are:

- `none`: assign `None` to clear it, or assign another ordinary Python value to replace it.
- `text`: `value` is `str`; assign the replacement string to `request.body` or `response.body`.
- `xml`: `value` is `str`; use `Body("xml", value)` for inline XML or `Body("xml", FileDescriptor.from_file(absolute_path))` for file-sourced XML.
- `json`: `value` is decoded JSON; after editing, assign it back to the owner Body.
- `binary`: `value` is `bytes`; assign bytes directly or use `Body("binary", FileDescriptor.from_file(absolute_path))`.
- `file`: this request semantic kind represents a user-selected file. `value` is a read-only `FileDescriptor`; create a replacement with `Body("file", FileDescriptor.from_file(absolute_path))`, or use `write_file()` to copy existing bytes without materializing them.
- `urlencoded`: `body.value` is a mutable `list[URLEncodedField]`; edit fields or append `URLEncodedField("name", "value")`.
- `multipart`: `body.value` is a mutable `list[MultipartPart]`; append text with `MultipartPart("name", "value")` or files with `MultipartPart("upload", file=FileDescriptor.from_file(absolute_path), filename="upload.bin")`. Existing upload parts expose a read-only `part.file` descriptor, while `part.filename` remains independently editable.
- `unavailable`: this appears only for an SSE response. Do not access or replace the Body; change only the status or headers before streaming begins.

For example, structured request Bodies are edited as objects rather than raw bytes:

```python
if request.body.kind == "urlencoded":
    request.body.value.append(URLEncodedField("flowlens", "enabled"))
elif request.body.kind == "multipart":
    request.body.value.append(MultipartPart("flowlens", "enabled"))
    request.body.value.append(
        MultipartPart("upload", file=FileDescriptor.from_file(absolute_path))
    )
```

FlowLens's internal transport representation does not change `kind` and is intentionally invisible to scripts. The complete example covers every existing kind and restricts `unavailable` handling to response headers.

### Header injection, JSON mutation, and `context.shared`

[`docs/examples/python-plugins/header-json-shared.py`](../examples/python-plugins/header-json-shared.py) injects a request header, mutates JSON in both phases, and carries a flag to the response hook:

```python
import json

from flowlens import *


def _json_object(body):
    value = body.value
    if isinstance(value, str):
        value = json.loads(value)
    return value if isinstance(value, dict) else None


def onRequest(context, request):
    header_value = str(context.params.get("header_value", "enabled"))
    request.headers.add("X-FlowLens-Plugin", header_value)
    if request.body.kind in {"text", "json"}:
        try:
            value = _json_object(request.body)
            if value is not None:
                value["request_plugin"] = True
                request.body = value
        except (TypeError, ValueError, json.JSONDecodeError):
            pass
    context.shared["request_seen"] = True
    context.log.info("request hook completed")
    return request


def onResponse(context, response):
    shared_value = "yes" if context.shared.get("request_seen") else "no"
    response.headers.set("X-FlowLens-Shared", shared_value)
    if response.body.kind in {"text", "json"}:
        try:
            value = _json_object(response.body)
            if value is not None:
                value["response_plugin"] = True
                response.body = value
        except (TypeError, ValueError, json.JSONDecodeError):
            pass
    return response
```

Use this params object:

```json
{
  "header_value": "documentation-example"
}
```

### Request blocking

[`docs/examples/python-plugins/block-request.py`](../examples/python-plugins/block-request.py) returns `None` for a configured URL prefix:

```python
from flowlens import *


def onRequest(context, request):
    blocked_prefix = str(context.params.get("blocked_url_prefix", ""))
    if blocked_prefix and request.url.startswith(blocked_prefix):
        context.log.warning("request blocked by configured URL prefix")
        return None
    return request


def onResponse(context, response):
    return response
```

Example params:

```json
{
  "blocked_url_prefix": "https://api.example.com/private/"
}
```

### File-based large-Body processing and replacement

[`docs/examples/python-plugins/large-body-file.py`](../examples/python-plugins/large-body-file.py) uses `write_file()` before processing request and response Bodies with ordinary Python file APIs:

```python
import hashlib
import os
import tempfile


def onRequest(context, request):
    source_path = _new_temp_path(context.params.get("temp_dir"), ".request")
    try:
        request.body.write_file(source_path)
        digest = hashlib.sha256()
        with open(source_path, "rb") as source:
            while chunk := source.read(1024 * 1024):
                digest.update(chunk)
        request.headers.set("X-Body-SHA256", digest.hexdigest())
    finally:
        os.remove(source_path)
    return request


def onResponse(context, response):
    source_path = _new_temp_path(context.params.get("temp_dir"), ".response")
    replacement_path = _new_temp_path(context.params.get("temp_dir"), ".replacement")
    try:
        response.body.write_file(source_path)
        with open(source_path, "rb") as source, open(replacement_path, "wb") as output:
            output.write(b"processed by FlowLens\n")
            while chunk := source.read(1024 * 1024):
                output.write(chunk)
        response.body = Body("binary", FileDescriptor.from_file(replacement_path))
    finally:
        for path in (source_path, replacement_path):
            try:
                os.remove(path)
            except FileNotFoundError:
                pass
    return response
```

The complete example skips empty, structured, and unavailable Bodies and is executed by the real-CPython integration test. The script owns and removes its working files; assigning the descriptor-backed Body copies the finished replacement before the assignment returns.

### Third-party package import

Install a dependency with the exact interpreter selected in Settings:

```shell
/absolute/path/to/python -m pip install requests
```

Then use [`docs/examples/python-plugins/third-party-package.py`](../examples/python-plugins/third-party-package.py):

```python
from flowlens import *

import requests


def onRequest(context, request):
    request.headers.set("X-Requests-Version", requests.__version__)
    return request


def onResponse(context, response):
    return response
```

FlowLens has no automatic dependency installer and v1 uses one configured interpreter for every plugin. Restart or reload the Python runtime after changing packages if an existing Worker has already cached imports.

## Streaming, size, and failure limits

- SSE response hooks run after status and headers arrive but before FlowLens starts forwarding events. `response.body.kind` is `unavailable`.
- SSE hooks may change only status and headers. Chunks never pass through Python, and response trailer or body mutation is rejected. Returning `None` blocks the response before streaming starts.
- Reading `body.value` for ordinary content may materialize the complete Body in Python memory. The script author is responsible for the resulting memory use and hook time.
- FlowLens may transparently use managed files while transporting Body data to and from a Worker. This is not a public storage mode. Use `write_file()` and ordinary Python file APIs when file-based processing is preferable.
- A Worker protocol frame is limited to 64 MiB.
- `context.shared` and the params JSON object are each limited to 1 MiB.
- One managed source file is limited to 32 MiB and a complete plugin package to 64 MiB.
- Each hook has the configured 100–60,000 ms deadline. A timed-out, canceled, crashed, or protocol-corrupt Worker is terminated and replaced.
- Hooks are synchronous. Blocking file or network I/O consumes the hook timeout.

## Logging and diagnostics

Use `context.log.debug()`, `info()`, `warning()`, or `error()`. `print()` is captured as `info` from `stdout`; writes to `stderr` are captured as `error`. Execution output is shown in the HTTP Request Editor **Console** tab for the current send. The plugin workbench intentionally has no separate log history: rules, params, files, and validation stay in the workbench, while runtime output stays with the request execution that produced it.

Hook outcomes, matched revisions, per-phase durations, transformations, and sanitized diagnostics appear with the HTTP Request Editor result. Log messages are your plugin's responsibility and may contain sensitive data, so avoid printing credentials or request bodies.

## Troubleshooting

- **Enabling Python plugins fails:** select an absolute path to a regular Python 3.11+ executable and run **Test**. For a venv, select that venv's interpreter, not its directory.
- **Validation reports an import error:** install the dependency with `selected-python -m pip install ...`. For files inside the plugin package, use a relative import such as `from .helpers import value`.
- **The plugin does not run:** check the Python plugin master switch in Settings, the global-plugin switch in the HTTP Request Editor tab, the plugin switch, the validation marker, and at least one enabled rule. Matching uses the original full URL.
- **Params have no effect:** save a top-level JSON object, then read it through `context.params` in the plugin. Params are configuration and are never added to the HTTP request automatically.
- **The Console is empty:** send the request again after enabling the relevant global plugin or current-request script, and make sure the hook calls `print()` or `context.log`. The Console shows only the latest send from its own Request Editor tab.
- **A current-request script disappeared:** only HTTP requests saved or updated in API Collection persist their script source; closing an unsaved new tab discards it. A reopened saved request leaves the script disabled until you turn it on. Use a global plugin to reuse behavior across requests.
- **A response Body is unavailable:** the response is SSE. Restrict that hook to `code` and headers; ordinary responses remain available through the Body API.
- **Body processing uses too much memory:** avoid reading `body.value` for that Body. Export it with `write_file()` and process the resulting file with ordinary Python APIs instead.
- **`file()`, `binary(path)`, or `textFromFile()` fails:** close the source writer, pass an absolute non-symlink regular-file path, and keep the copy within the hook timeout.
- **Edits fail validation:** the last good immutable revision stays active. Correct the saved source and validate again; unsaved editor content remains local to the current FlowLens window.

Plugin package import/export, automatic dependency installation, regex rules, directory watching, asynchronous hooks, and per-plugin interpreters are not part of API v1.
