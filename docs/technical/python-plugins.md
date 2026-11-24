# HTTP Request Editor Python Plugins

**English** | [简体中文](python-plugins.zh-CN.md)

This guide covers configuring Python 3, choosing between scripting scopes, writing `onRequest` and `onResponse` hooks, working with the Body API, respecting limits and security boundaries, and troubleshooting plugins in FlowLens.

FlowLens supports two Python scripting scopes exclusively inside the **HTTP Request Editor**:

1. **Global Python plugins:** Managed in the **Python Plugins** workbench and selected automatically by matching rules.
2. **Current-request scripts:** Bound to a single HTTP Request Editor tab.

Both scopes can inspect or modify a request before transport and inspect or modify the returned response. Python hooks never run during MITM capture, resend, or in the WebSocket Client. A current-request script's source can be saved alongside an HTTP request in **API Collections**, but is never written to Settings, HAR, history, or HBIN data.

Python plugin support is optional and disabled by default.

## Contents

- [Choose a Scripting Scope](#choose-a-scripting-scope)
- [In-App API Help and System Prompt](#in-app-api-help-and-system-prompt)
- [Security Model](#security-model)
- [Set Up Python](#set-up-python)
- [Five-Minute Quick Start](#five-minute-quick-start)
- [Create and Run a Global Plugin](#create-and-run-a-global-plugin)
- [Configure Plugin Params](#configure-plugin-params)
- [Use a Script for the Current Request](#use-a-script-for-the-current-request)
- [Read Script Output in the Console](#read-script-output-in-the-console)
- [Package Layout and Manifest](#package-layout-and-manifest)
- [Matching Rules and Ordering](#matching-rules-and-ordering)
- [Hook Contract](#hook-contract)
- [Context API](#context-api)
- [Request and Response API](#request-and-response-api)
- [Body API](#body-api)
- [Tested Examples](#tested-examples)
- [Streaming, Size, and Failure Limits](#streaming-size-and-failure-limits)
- [Logging and Diagnostics](#logging-and-diagnostics)
- [Troubleshooting](#troubleshooting)

## Choose a Scripting Scope

| Scope | Best For | Configuration | Persistence |
| --- | --- | --- | --- |
| **Global Python plugin** | Reusable behavior across multiple requests or endpoints | Managed files, matching rules, per-plugin JSON params, execution order, and enable switch | Saved by FlowLens across sessions |
| **Current-request script** | Testing or customizing a single HTTP request | Single script and per-tab enable switch; no matching rules or configurable params | Source saves with an API Collection HTTP request; enable switch is never saved and defaults off when reopened |

Both scopes share the same synchronous `onRequest` and `onResponse` API. Start with a current-request script to experiment quickly, and promote reusable or rule-matched logic to a global plugin.

## In-App API Help and System Prompt

Open **Python API Help** using the book icon in the **Python Plugins** sidebar header or the HTTP Request Editor **Script** toolbar—even before configuring Python or creating a plugin. The 420 px panel slides in from the right edge and closes on `Esc`, an outside click, leaving the workbench, or switching request tabs without modifying your script.

- **Quick start:** Lists three setup steps and core hook behavior, with expandable sections for the minimal script and runtime rules.
- **API reference:** Searchable interfaces grouped by type (matching groups expand automatically). Signatures include parameter and return types, with expandable usage notes and import guidance.
- **Copy system prompt:** Copies the complete API v1 contract, runtime constraints, a minimal two-hook script template, and a requirement placeholder in the active UI language. The button you open help from determines whether the prompt targets a global plugin (`main.py`, Params, and matching rules) or a current-request script. Search text and the active help tab never truncate the copied prompt. Paste it into an AI assistant, replace the placeholder with your task, and review the generated code before enabling it. The prompt contains only product documentation and a template—never your plugin source, request data, or parameter values.

## Security Model

Run only code you trust. Plugins execute as standard local Python code with the same operating-system user permissions as FlowLens: they can read and write local files, make network connections, spawn processes, import installed packages, and log sensitive data. FlowLens runs Workers in Python isolated mode (`-I`) for predictable imports, **not** as a security sandbox.

Review both plugin source code and all third-party dependencies before enabling them. FlowLens never installs Python packages automatically.

## Set Up Python

Install CPython 3 or create a dedicated virtual environment to isolate plugin dependencies from other Python installations.

**Windows (PowerShell):**

```powershell
py -3 -m venv C:\venvs\flowlens-plugins
C:\venvs\flowlens-plugins\Scripts\python.exe -m pip install requests
```

**macOS / Linux:**

```shell
python3 -m venv "$HOME/.venvs/flowlens-plugins"
"$HOME/.venvs/flowlens-plugins/bin/python" -m pip install requests
```

Then configure FlowLens:

1. Open **Settings > Python**.
2. Click **Detect** to scan platform registrations, `PATH`, the active environment, and common install locations, then select an interpreter—or pick an absolute executable path manually (`python.exe` on Windows or `bin/python` on macOS/Linux for virtual environments).
3. Click **Test** and confirm FlowLens reports the interpreter as available.
4. Configure the per-hook timeout (default `5,000 ms`, allowed range `100–60,000 ms`).
5. Enable Python plugins and accept the trusted-code warning.

### Interpreter Detection Notes

- Detection is bounded and does not scan the entire disk; select custom virtual environments manually if they live outside common locations.
- On Windows, detection also discovers current-user Microsoft Store Python packages and installed runtimes listed by Python Install Manager. Store Python exposes a package-specific execution alias such as `%LOCALAPPDATA%\Microsoft\WindowsApps\PythonSoftwareFoundation.Python.3.13_qbz5n2kfra8p0\python.exe`; FlowLens supports this alias for detection, testing, and execution. Prefer the detected alias over versioned paths under `C:\Program Files\WindowsApps`, which can change after Store updates (no `WindowsApps` permission changes are needed).
- Only successfully probed CPython 3 runtimes appear in detection results. Global Store install shortcuts and Python Install Manager itself are excluded, and FlowLens disables automatic runtime installation during detection and execution. If only the manager is present, install a Python runtime yourself and run **Detect** again.
- FlowLens does not enforce a specific Python 3 minor version—use **Test** to verify that the bundled Worker and SDK start cleanly under your chosen interpreter, and ensure your script syntax and third-party packages match it.
- If runtime validation fails, FlowLens refuses to save the enabled state. FlowLens launches normally when Python is absent or the feature is disabled.

## Five-Minute Quick Start

After configuring and enabling Python in **Settings > Python**:

1. Open an **HTTP Request Editor** tab and switch to the **Script** tab.
2. Paste the script below and turn on the script switch.
3. Send the request.
4. Open the response **Console** tab to see the logged request URL and response status.

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

This script affects only requests sent from that tab and never touches captured proxy traffic. To reuse logic across tabs, create a global plugin next.

## Create and Run a Global Plugin

1. Open the **Python Plugins** workbench and create a new plugin.
2. Edit `main.py` so both `onRequest` and `onResponse` are defined as callable, synchronous functions.
3. Add at least one enabled matching rule under the **Rules** tab.
4. Optionally define a JSON object under **Params** (accessible in both hooks via `context.params`).
5. Save and validate the plugin, then enable it in the plugin list.
6. In an **HTTP Request Editor** tab, turn on the tab's global Python plugin switch before sending.

**Activation rules:**

- The Request Editor's global-plugin switch is transient, defaults off on every new tab, and is not saved in API Collections (it acts solely as a per-tab bypass).
- A global plugin runs only when all five conditions hold: the **Settings > Python** master switch is on, the tab's global-plugin switch is on, the plugin's own switch is on, the plugin has a valid revision, and at least one enabled rule matches.
- Saving or validating creates an immutable revision. Every send snapshots matched plugin order, revisions, and params before the first hook executes, so concurrent edits affect only later sends. If an updated plugin fails validation, the last valid revision stays active.

## Configure Plugin Params

**Params** hold per-plugin configuration for a global plugin. They are **not** HTTP query parameters and are never injected into the URL, headers, or body automatically; they take effect only when your code reads `context.params`.

Enter a single top-level JSON object in the **Params** editor:

```json
{
  "header_value": "staging",
  "blocked_url_prefix": "https://api.example.com/private/",
  "feature_enabled": true
}
```

Read values in either hook:

```python
def onRequest(context, request):
    if context.params.get("feature_enabled", False):
        value = str(context.params.get("header_value", "default"))
        request.headers.set("X-Environment", value)
    return request
```

- Params persist with the global plugin and are isolated from all other plugins.
- Each send receives a deeply read-only snapshot (nested objects are read-only; JSON arrays become Python `tuple`s).
- Saving updated params affects only future sends, not in-flight requests.
- The top-level value must be a JSON object (e.g., `{}`); arrays, strings, numbers, booleans, and `null` are rejected.
- Maximum encoded size: `1 MiB`.
- Current-request scripts have no Params editor and always receive an empty `context.params`.

Use `context.shared`—not `context.params`—for mutable state passed from `onRequest` to `onResponse`. Params are plain configuration rather than a secret vault: FlowLens does not transmit or log them automatically, but plugin code can read, send, or print them, so avoid storing secrets unless you trust the entire plugin and its dependencies.

## Use a Script for the Current Request

Open the **Script** tab inside an HTTP Request Editor tab, edit the template, and enable the script switch:

- Saving or updating the HTTP request in **API Collections** persists the script source in SQLite. Reopening that request restores the source with the enable switch **off** to prevent saved code from executing automatically.
- Closing an unsaved tab discards its script. Script source is never written to Settings, HAR, history, or HBIN, and script edits mark the tab as unsaved.
- Both synchronous `onRequest` and `onResponse` functions are required, matching global plugins.
- Current-request scripts share the configured interpreter, Worker pool, hook timeout, SDK, OS permissions, and trusted-code model. Maximum source size is `1 MiB`.
- Each send loads an isolated temporary revision, so module globals never persist between sends; use `context.shared` for request-to-response JSON state.

When both global plugins and a current-request script are active, hooks execute as a nested wrapper around the network call:

```text
Request phase:   global plugins -> current-request script -> network
Response phase:  network -> current-request script -> global plugins
```

The current-request script sits closest to the network boundary. Disabling the tab's global-plugin switch does not disable its current-request script—toggle the switch inside the **Script** tab instead.

## Read Script Output in the Console

The response-side **Console** tab streams `stdout`, `stderr`, and `context.log` entries live while a send executes:

- Entries are tagged with the send's execution ID, isolating output per Request Editor tab.
- The console displays up to the latest `1,000` entries for the tab's most recent send in a virtualized table showing time, level/type, source, and message.
- Click any row to open the full entry in a detail window with wrap, copy, and save actions; the toolbar also supports copying all entries, saving to a file, or clearing the console.
- All hooks in the execution chain appear together: global plugins use their plugin name, and the current-request script appears as **Current script**.
- Each `print()` call produces one log entry, preserving embedded newlines inside that entry.

## Package Layout and Manifest

FlowLens creates and manages each global plugin directory:

```text
<plugin-id>/
|-- manifest.json
|-- main.py
`-- helpers.py
```

- `main.py` is the fixed entry point. Import sibling modules inside the plugin using relative imports:

```python
from .helpers import build_token
```

- Workers load revisions under private module names. Because FlowLens runs two Worker processes, module globals are not guaranteed to persist across calls—use `context.shared` for per-request state and external files/storage for long-lived state.
- There is no PyPI `flowlens` package to install; FlowLens injects the API v1 `flowlens` module directly into the Worker runtime.

`manifest.json` uses strict schema v1 validation and rejects unknown fields:

```json
{
  "schemaVersion": 1,
  "apiVersion": 1,
  "id": "d9712a7a-5b17-4e6a-a6d2-cb2e8149e734",
  "name": "Example Plugin",
  "description": "Mutates matching request traffic"
}
```

The `id` must match the canonical lowercase UUID assigned to the managed directory. Prefer updating `name` and `description` through the FlowLens UI so SQLite and `manifest.json` stay in sync.

## Matching Rules and Ordering

Each rule specifies an enabled switch, an HTTP method, and a URL wildcard pattern:

- **Method:** An uppercase HTTP token (such as `GET` or `POST`), or `*` to match any method.
- **URL pattern:** Matched against the normalized full URL. `*` matches zero or more characters and `?` matches exactly one character (regular expressions are not supported). For standard absolute URL patterns, scheme and host are case-insensitive while path and query remain case-sensitive.
- **Deduplication:** A plugin matched by multiple rules still executes only once per send.
- **Single snapshot match:** All global plugins match once against the original method and URL before the first hook runs; mutating the URL in an earlier hook never adds or removes plugins from the current send.
- **Execution order:** Request hooks run in plugin-list order; response hooks unwind in reverse order for middleware-style wrapping.

Example: method `GET` with `https://api.example.com/v?/items/*` matches `https://api.example.com/v1/items/42`.

## Hook Contract

Minimal valid script:

```python
from flowlens import *


def onRequest(context, request):
    return request


def onResponse(context, response):
    return response
```

Hooks must be synchronous (`def`, never `async def`) and must return either the SDK object passed in or `None`:

- `onRequest` returning `request` passes the request to the next hook and then to transport.
- `onRequest` returning `None` blocks the request before opening a network connection.
- `onResponse` returning `response` continues the reverse response chain.
- `onResponse` returning `None` marks the network response as blocked and suppresses its transformed presentation.
- Returning any other type, raising an uncaught exception, timing out, crashing the Worker, or producing invalid HTTP data fails the hook.

**Failure policies:**

- **Request hooks fail closed:** No network request is sent.
- **Response hooks fail open:** FlowLens returns the untouched wire response along with plugin diagnostics. Response mutations alter only the HTTP Request Editor's displayed result—they never rewrite captured wire truth or transport metrics.

## Context API

`context` exposes the following properties:

| Property | Type and Semantics |
| --- | --- |
| `id` | Unique execution ID for this send. |
| `timestamp` | Send start timestamp in Unix microseconds. |
| `original_url` | Request URL prior to any plugin mutations. |
| `original_method` | HTTP method prior to any plugin mutations. |
| `plugin_id`, `plugin_name` | Identity of the active plugin. |
| `params` | Deeply read-only snapshot of the plugin's JSON params (JSON arrays become tuples). |
| `transport` | Deeply read-only transport metadata (`protocol`, `proxy_mode`, `tls_client_hello_profile`, `http2_fingerprint`; proxy credentials are excluded). |
| `shared` | Mutable JSON-serializable object scoped to this plugin and send. |
| `log` | Structured logger with `debug()`, `info()`, `warning()`, and `error()` methods. |

`context.shared` round-trips through Go after each hook and is restored for the same plugin during `onResponse`, even when executed by a different Worker process. It must remain a JSON object not exceeding `1 MiB` when encoded, and is never shared across different plugins or different sends.

## Request and Response API

### `request`

- **Mutable properties:** `method`, `url`, `path`, `queries`, `headers`, `body`.
- **Read-only derived views:** `scheme`, `host`, `port`, `content_type`.
- **URL synchronization:** Assigning `url` reparses `path` and `queries`; mutating `path` or `queries` rebuilds `url`.
- **Normalization:** The final request is revalidated and fed into FlowLens's request pipeline (URL routing, generated headers, framing, content encoding, proxy, protocol, TLS, and HTTP/2 fingerprint). Methods must stay uppercase, and FlowLens retains ownership of pseudo-headers, generated headers, and framing headers.

### `response` and `response.request`

- **Mutable properties on `response`:** `code`, `headers`, `trailers`, `body`.
- **Read-only properties on `response`:** `protocol` (actual upstream protocol such as `HTTP/1.1` or `HTTP/2.0`), `status_text` (initial upstream reason phrase; automatically updated to standard text if `code` changes), and `content_type`.
- **`response.request`:** Read-only semantic snapshot of the request handed to transport (`method`, `url`, `scheme`, `host`, `port`, `path`, `queries`, `content_type`, `headers`, `body`). It never exposes FlowLens storage internals or managed temporary-file paths.

### `Headers`

Preserves field order, duplicate names, original casing, and empty values. Lookups are case-insensitive:

| Method | Behavior |
| --- | --- |
| `headers.get(name, default=None)` | Returns the first matching value, or `default`. |
| `headers.get_all(name)` | Returns all matching values in field order. |
| `headers.set(name, value)` | Replaces all matches at the first match's position, or appends if absent. |
| `headers.add(name, value)` | Appends a new field (allowing duplicates). |
| `headers.remove(name)` | Removes every matching field. |
| `headers.clear()` | Removes all fields. |

Iterating mutable `headers` yields `HeaderField` objects with mutable `name` and `value`. Iterating `response.request.headers` yields `(name, value)` tuples.

### `Queries`

Preserves parameter order, duplicate names, empty values, and—while untouched—the original URL query encoding. Parameter names are case-sensitive:

| Method | Behavior |
| --- | --- |
| `queries.get(name, default=None)` | Returns the first matching decoded value, or `default`. |
| `queries.get_all(name)` | Returns all matching decoded values in order. |
| `queries.set(name, value)` | Replaces all matches at the first match's position, or appends if absent. |
| `queries.add(name, value)` | Appends a new parameter (allowing duplicates). |
| `queries.remove(name)` | Removes every matching parameter. |
| `queries.clear()` | Removes all parameters. |
| `queries.to_string()` | Returns the encoded query string without a leading `?`. |

Iterating mutable `queries` yields `QueryField` objects. `response.request.queries` yields `(name, value)` tuples and exposes only `get`, `get_all`, and `to_string`. Once modified, FlowLens re-encodes the query using Python's standard URL encoding rules (for example, spaces encode as `+`).

### `content_type`

`content_type` is a read-only view of the first `Content-Type` header on `request`, `response`, and `response.request`. For any non-empty request body, FlowLens sets the outgoing `Content-Type` from the final body kind. To alter a response's `Content-Type`, call `response.headers.set("Content-Type", value)` or `response.headers.remove("Content-Type")`.

## Body API

`Body` exposes two read-only semantic properties:

| Property | Semantics |
| --- | --- |
| `kind` | One of `none`, `text`, `json`, `xml`, `binary`, `file`, `urlencoded`, `multipart`, or `unavailable`. |
| `value` | Python representation for `kind`. Reading ordinary bodies may load the entire body into Worker memory. |

Value types by `kind`:

| `kind` | `value` Type |
| --- | --- |
| `none` | `None` |
| `text`, `xml` | `str` |
| `json` | Decoded JSON value (typically `dict` or `list`) |
| `binary` | `bytes` |
| `file` | Read-only `FileDescriptor` (`path`, `name`, `size`) |
| `urlencoded` | Mutable `list[URLEncodedField]` |
| `multipart` | Mutable `list[MultipartPart]` |
| `unavailable` | Raises `ValueError` on read |

- **Transparent large-body backing:** FlowLens may back large `text`, `json`, `xml`, or `binary` bodies with managed files transparently without changing `kind`. Reading `value` still returns `str`, decoded JSON, or `bytes` (materializing it in memory). By contrast, `file` represents a user-selected upload file and keeps a `FileDescriptor` value.
- **Multipart parts:** Each `MultipartPart` has mutable `enabled`, `name`, `value`, `file`, and `filename` properties (`filename` is independent of the descriptor's source filename). FlowLens builds each part's `Content-Disposition` from `name` and `filename` (falling back to the descriptor name for file parts) and uses `application/octet-stream` for file parts.
- **Frozen request body (`response.request.body`):** Read-only `BodySnapshot` preserving `kind`. Ordinary values are immutable snapshots; `file` values and multipart file parts expose path-free `FileSnapshot` objects (`name`, `size`). Use `response.request.body.write_file(absolute_path)` to copy readable ordinary or file bodies without touching managed paths.
- **Response kind inference:** Determined from `Content-Type` and received bytes—valid JSON becomes `json`; valid UTF-8 with `application/xml`, `text/xml`, or `*+xml` becomes `xml`; binary media types or invalid UTF-8 become `binary`; remaining valid UTF-8 becomes `text`.

### Direct Assignment

Assign ordinary Python values directly to `request.body` or `response.body`:

```python
request.body = None
request.body = "Hello World"
request.body = b"\x00\x01\x02"
request.body = {"name": "FlowLens", "enabled": True}
request.body = ["one", "two"]
```

| Assigned Value | Resulting Body |
| --- | --- |
| `None` | Empty body (`none`) |
| `str` | UTF-8 text (`text`) |
| `bytes`, `bytearray`, or `memoryview` | Binary body (`binary`) |
| `dict` or `list` | Compact JSON body (`json`) |
| `Body` | Explicit semantic kind or existing `Body` instance |

Other types raise `TypeError`. During request normalization, FlowLens derives outgoing `Content-Type` from a non-empty final body kind and replaces any existing value. Clearing a body (`None`) does not delete an existing `Content-Type`; remove it explicitly if needed.

Construct an explicit `Body` whenever a plain Python value cannot convey the intended kind:

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

`kind` and `value` cannot be mutated directly on an existing `Body`—assign a new value or `Body` to `request.body` or `response.body` instead (though structured `urlencoded`/`multipart` lists and items are mutable in place).

For JSON bodies, always assign the modified object back (essential when a large JSON body was materialized from a managed file):

```python
value = request.body.value
if isinstance(value, str):
    value = json.loads(value)
if isinstance(value, dict):
    value["processed"] = True
    request.body = value
```

### File Workflow and Memory Responsibility

The public API exposes no body-size threshold or storage mode. Accessing `body.value` on ordinary content may load the entire payload into Python memory, and the script author is responsible for that memory footprint and processing time within the hook timeout.

To process large bodies without loading them into memory, export raw bytes with `write_file(absolute_path)` and replace the body via `FileDescriptor.from_file(absolute_path)` with an explicit kind:

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

- `FileDescriptor.from_file(path)` validates the path and populates `name` and `size`. Use it with `Body("file", ...)` to preserve the semantic `file` kind, or inside a `MultipartPart`.
- Descriptor-backed **request** bodies support `text`, `json`, `xml`, `binary`, and `file`. Descriptor-backed **response** bodies support `text`, `json`, `xml`, and `binary` (`file` and `multipart` are request-only). `write_file()` does not support `urlencoded` or `multipart` bodies.
- Body kind is always explicit—there is no filename-extension inference, and assigning a path string creates a `text` body containing the string itself:

```python
request.body = Body("binary", FileDescriptor.from_file("C:/files/request.bin"))
request.body = "C:/files/request.bin"  # creates a text body containing the literal path
```

### Temporary-File Ownership and Failures

- Paths passed to `FileDescriptor.from_file()` must be absolute paths to non-symlink regular files, with any writers closed beforehand.
- Assigning `Body(kind, descriptor)` copies the file into FlowLens-managed temporary storage before the assignment returns, so your script can immediately delete its source file (including on Windows).
- New `MultipartPart` file items are staged during request serialization rather than at list insertion—keep their source files intact until `onRequest` returns.
- FlowLens cleans up all managed temporary files after success, blocking, hook error, timeout, cancellation, or Worker exit.
- Invalid request files fail closed; invalid response files fail open (returning the untouched wire response with diagnostics).
- `unavailable` (SSE) bodies reject all reads and writes; SSE chunks never pass through Python.

## Tested Examples

All example files under `docs/examples/python-plugins/` are executed directly by FlowLens's real-CPython Worker integration tests.

### Handling Different Body Kinds

[`docs/examples/python-plugins/body-kinds.py`](../examples/python-plugins/body-kinds.py) handles every request body kind plus the `unavailable` SSE response state:

- `none`: Assign `None` to clear or any supported Python value to populate.
- `text`: `value` is `str`; assign a replacement string directly.
- `xml`: `value` is `str`; assign `Body("xml", value)` or `Body("xml", FileDescriptor.from_file(absolute_path))`.
- `json`: `value` is decoded JSON; mutate and assign it back.
- `binary`: `value` is `bytes`; assign bytes directly or use `Body("binary", FileDescriptor.from_file(absolute_path))`.
- `file`: User-selected upload file (`value` is a read-only `FileDescriptor`); replace via `Body("file", FileDescriptor.from_file(absolute_path))`, or copy raw bytes with `write_file()`.
- `urlencoded`: `value` is a mutable `list[URLEncodedField]`; edit fields or append `URLEncodedField("name", "value")`.
- `multipart`: `value` is a mutable `list[MultipartPart]`; append `MultipartPart("name", "value")` or `MultipartPart("upload", file=FileDescriptor.from_file(absolute_path), filename="upload.bin")`. Existing file parts expose a read-only `part.file` descriptor with an independently editable `part.filename`.
- `unavailable`: Appears only on SSE responses. Modify only status or headers before streaming begins.

```python
if request.body.kind == "urlencoded":
    request.body.value.append(URLEncodedField("flowlens", "enabled"))
elif request.body.kind == "multipart":
    request.body.value.append(MultipartPart("flowlens", "enabled"))
    request.body.value.append(
        MultipartPart("upload", file=FileDescriptor.from_file(absolute_path))
    )
```

### Header Injection, JSON Mutation, and `context.shared`

[`docs/examples/python-plugins/header-json-shared.py`](../examples/python-plugins/header-json-shared.py) injects a request header, updates JSON in both phases, and passes state via `context.shared`:

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

Example params:

```json
{
  "header_value": "documentation-example"
}
```

### Request Blocking

[`docs/examples/python-plugins/block-request.py`](../examples/python-plugins/block-request.py) returns `None` when the URL matches a configured prefix:

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

### File-Based Large-Body Processing and Replacement

Excerpt from [`docs/examples/python-plugins/large-body-file.py`](../examples/python-plugins/large-body-file.py), using `write_file()` and chunked Python file I/O (the complete file also skips empty, structured, and unavailable bodies and defines `_new_temp_path`):

```python
from flowlens import *

import hashlib
import os


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

Your script owns and cleans up its temporary working files; assigning the descriptor-backed `Body` copies the replacement file into FlowLens-managed storage before returning.

### Third-Party Package Import

Install packages into the exact Python interpreter configured in **Settings > Python**:

```shell
/absolute/path/to/python -m pip install requests
```

Then import them normally:

```python
from flowlens import *

import requests


def onRequest(context, request):
    request.headers.set("X-Requests-Version", requests.__version__)
    return request


def onResponse(context, response):
    return response
```

API v1 uses one shared interpreter for all plugins and never installs dependencies automatically. Reload the Python runtime or restart FlowLens after installing or upgrading packages if running Workers have cached earlier imports.

## Streaming, Size, and Failure Limits

- **SSE responses:** `onResponse` runs after status and headers arrive, before event streaming begins, with `response.body.kind == "unavailable"`. Hooks may modify only `code` and headers (or return `None` to block before streaming); event chunks never enter Python, and trailer/body mutations are rejected.
- **Memory and spooling:** Reading `body.value` on ordinary content may materialize the full body in Python memory. FlowLens may transparently spool large bodies to managed files during Worker transport, which is not a public storage mode—use `write_file()` when file-based streaming is preferred.
- **Worker protocol frame limit:** `64 MiB`.
- **`context.shared` and params JSON limits:** `1 MiB` each.
- **Source size limits:** `32 MiB` per managed plugin file, `64 MiB` per complete plugin package, and `1 MiB` for a current-request script.
- **Hook deadline:** Configurable from `100` to `60,000 ms` (default `5,000 ms`). Timed-out, canceled, crashed, or protocol-corrupt Workers are terminated (including child process trees on Windows) and replaced automatically.
- **Synchronous execution:** Blocking file or network I/O counts toward the hook deadline.

## Logging and Diagnostics

- Use `context.log.debug()`, `info()`, `warning()`, or `error()`, or standard `print()` / `sys.stdout.write()` (`info`) and `sys.stderr.write()` (`error`). Multi-line writes stay within a single log entry rather than splitting per line.
- Runtime output appears exclusively in the active HTTP Request Editor tab's **Console** tab. The **Python Plugins** workbench intentionally maintains no separate log history.
- **Do not bypass the log wrapper:** Never call `os.write(1, ...)`, write to `sys.__stdout__`, or allow child processes to inherit Worker `stdout`—non-protocol bytes written to stdout corrupt the Worker channel and fail execution. Direct `os.write(2, ...)`, `sys.__stderr__`, and inherited child `stderr` bypass the Console and remain in Worker diagnostic stderr only.
- Hook outcomes, matched revisions, phase durations, transformations, and sanitized diagnostics appear alongside the Request Editor response. Avoid printing credentials or sensitive payloads in plugin logs.

## Troubleshooting

- **Cannot enable Python plugins:** Provide an absolute path to a Python 3 executable (or a Windows Store Python execution alias) and click **Test**. For a virtual environment, select the interpreter executable rather than the venv folder. If a Store Python alias is broken, reinstall Store Python or detect another runtime.
- **Validation reports an import error:** Install third-party packages via `<selected-python> -m pip install ...`. For sibling files inside a plugin package, use relative imports (`from .helpers import value`).
- **Plugin never runs:** Verify all five gates—the Settings master switch, the Request Editor tab's global-plugin switch, the plugin's own switch, a valid revision marker, and at least one enabled rule matching the original full URL.
- **Params have no effect:** Save a valid top-level JSON object in **Params** and explicitly read it via `context.params` in your hook.
- **Console is empty:** Re-send the request after enabling the global plugin or current-request script, and verify your hook calls `print()` or `context.log`. The Console shows only the latest send for its own tab.
- **Current-request script disappeared:** Only requests saved to **API Collections** persist their script source; closing an unsaved tab discards it. Reopened saved requests always start with the script switch off.
- **Response body is `unavailable`:** The response is SSE—restrict `onResponse` to `code` and headers.
- **High memory usage when handling bodies:** Stop reading `body.value` directly; export with `write_file()` and process the file in chunks.
- **`FileDescriptor.from_file()` or descriptor-backed assignment fails:** Ensure the source file writer is closed, the path is an absolute non-symlink regular file, and copying completes within the hook timeout.
- **Edits fail validation:** The previous valid immutable revision stays active. Fix the saved source and revalidate; unsaved editor changes remain local to the current window.

Plugin package import/export, automatic dependency installation, regex matching rules, filesystem watching, asynchronous hooks, and per-plugin interpreters are out of scope for API v1.
