from __future__ import annotations

import importlib.util
import inspect
import json
import os
import struct
import sys
import time
import traceback
import types
import uuid

PROTOCOL_VERSION = 1
SDK_API_VERSION = 1
MAX_FRAME_BYTES = 64 * 1024 * 1024
MAX_SHARED_BYTES = 1024 * 1024
MAX_MODULE_CACHE_ENTRIES = 128

_protocol_stdout = sys.stdout.buffer
_protocol_stdin = sys.stdin.buffer
_active_request_id = ""
_active_execution_id = ""
_active_plugin_id = ""
_module_cache = {}


def _write_frame(message):
    payload = json.dumps(message, ensure_ascii=False, allow_nan=False, separators=(",", ":")).encode("utf-8")
    if len(payload) > MAX_FRAME_BYTES:
        raise ValueError("protocol frame exceeds the 64 MiB limit")
    _protocol_stdout.write(struct.pack(">I", len(payload)))
    _protocol_stdout.write(payload)
    _protocol_stdout.flush()


def _read_frame():
    header = _protocol_stdin.read(4)
    if header == b"":
        return None
    if len(header) != 4:
        raise RuntimeError("truncated protocol frame header")
    length = struct.unpack(">I", header)[0]
    if length > MAX_FRAME_BYTES:
        raise RuntimeError("protocol frame exceeds the 64 MiB limit")
    payload = _protocol_stdin.read(length)
    if len(payload) != length:
        raise RuntimeError("truncated protocol frame payload")
    return json.loads(payload.decode("utf-8"))


def _emit_log(level, message, stream="plugin"):
    text = str(message)
    if len(text) > 64 * 1024:
        text = text[: 64 * 1024] + "\n[log entry truncated]"
    _write_frame(
        {
            "type": "log",
            "requestId": _active_request_id,
            "executionId": _active_execution_id,
            "pluginId": _active_plugin_id,
            "level": level,
            "stream": stream,
            "message": text,
            "timestamp": int(time.time() * 1_000_000),
        }
    )


class _LogWriter:
    def __init__(self, level, stream):
        self.level = level
        self.stream = stream
        self.buffer = ""

    def write(self, value):
        if not isinstance(value, str):
            value = str(value)
        self.buffer += value
        while "\n" in self.buffer:
            line, self.buffer = self.buffer.split("\n", 1)
            if line:
                _emit_log(self.level, line, self.stream)
        return len(value)

    def flush(self):
        if self.buffer:
            _emit_log(self.level, self.buffer, self.stream)
            self.buffer = ""

    def isatty(self):
        return False


_stdout_log = _LogWriter("info", "stdout")
_stderr_log = _LogWriter("error", "stderr")
sys.stdout = _stdout_log
sys.stderr = _stderr_log
sys.dont_write_bytecode = True


def _flush_logs():
    _stdout_log.flush()
    _stderr_log.flush()


def _module_namespace(plugin_id, revision):
    clean_id = "".join(char if char.isalnum() else "_" for char in plugin_id)
    return "_flowlens_plugin_{}_{}_{}".format(clean_id, revision[:16], uuid.uuid4().hex)


def _load_module(plugin_id, revision, plugin_path):
    cache_key = (plugin_id, revision)
    cached = _module_cache.pop(cache_key, None)
    if cached is not None:
        _module_cache[cache_key] = cached
        return cached
    main_path = os.path.join(plugin_path, "main.py")
    namespace = _module_namespace(plugin_id, revision)
    spec = importlib.util.spec_from_file_location(namespace, main_path, submodule_search_locations=[plugin_path])
    if spec is None or spec.loader is None:
        raise ImportError("unable to create a module spec for main.py")
    module = importlib.util.module_from_spec(spec)
    sys.modules[namespace] = module
    try:
        spec.loader.exec_module(module)
        _validate_hooks(module)
    except BaseException:
        _unload_module_namespace(namespace)
        raise
    _module_cache[cache_key] = module
    while len(_module_cache) > MAX_MODULE_CACHE_ENTRIES:
        oldest_key = next(iter(_module_cache))
        oldest_module = _module_cache.pop(oldest_key)
        _unload_module_namespace(oldest_module.__name__)
    return module


def _unload_module_namespace(namespace):
    prefix = namespace + "."
    for name in [name for name in sys.modules if name == namespace or name.startswith(prefix)]:
        sys.modules.pop(name, None)


def _validate_hooks(module):
    for name in ("onRequest", "onResponse"):
        hook = getattr(module, name, None)
        if hook is None:
            raise TypeError("plugin must define {}".format(name))
        if not callable(hook):
            raise TypeError("{} must be callable".format(name))
        if inspect.iscoroutinefunction(hook) or inspect.isasyncgenfunction(hook):
            raise TypeError("{} must be synchronous".format(name))


def _send_error(request_id, code, error):
    detail = "".join(traceback.format_exception(type(error), error, error.__traceback__))
    if len(detail) > 256 * 1024:
        detail = detail[: 256 * 1024] + "\n[traceback truncated]"
    _write_frame(
        {
            "type": "error",
            "requestId": request_id,
            "code": code,
            "message": str(error),
            "traceback": detail,
        }
    )


def _handle_validate(message):
    request_id = message.get("requestId", "")
    try:
        _load_module(message["pluginId"], message["revision"], message["path"])
        _flush_logs()
        _write_frame({"type": "result", "requestId": request_id, "validated": True})
    except BaseException as error:
        _flush_logs()
        _send_error(request_id, "validation_failed", error)


def _handle_invoke(message, flowlens):
    request_id = message.get("requestId", "")
    try:
        module = _load_module(message["pluginId"], message["revision"], message["path"])
        hook_name = message["hook"]
        if hook_name not in ("onRequest", "onResponse"):
            raise ValueError("unsupported hook {}".format(hook_name))
        context_value = dict(message.get("context") or {})
        context_value["plugin_id"] = message["pluginId"]
        context_value["plugin_name"] = message.get("pluginName", "")
        context = flowlens.Context(context_value, lambda level, value: _emit_log(level, value, "context"))
        output_directory = message.get("outputDirectory", "")
        if hook_name == "onRequest":
            hook_value = flowlens._request_from_wire(message.get("value") or {}, output_directory)
            expected_type = flowlens.Request
        else:
            hook_value = flowlens._response_from_wire(message.get("value") or {}, output_directory)
            expected_type = flowlens.Response
        result = getattr(module, hook_name)(context, hook_value)
        if inspect.isawaitable(result) or inspect.isasyncgen(result):
            raise TypeError("plugin hooks must be synchronous")
        shared = context._shared_to_wire()
        encoded_shared = json.dumps(shared, ensure_ascii=False, allow_nan=False, separators=(",", ":")).encode("utf-8")
        if len(encoded_shared) > MAX_SHARED_BYTES:
            raise ValueError("context.shared exceeds the 1 MiB limit")
        _flush_logs()
        if result is None:
            _write_frame(
                {
                    "type": "result",
                    "requestId": request_id,
                    "blocked": True,
                    "shared": shared,
                    "transformed": False,
                }
            )
            return
        if not isinstance(result, expected_type):
            raise TypeError("{} must return {} or None".format(hook_name, expected_type.__name__))
        value = result._to_wire()
        _write_frame(
            {
                "type": "result",
                "requestId": request_id,
                "blocked": False,
                "value": value,
                "shared": shared,
                "transformed": result._changed,
                "bodyChanged": result.body._changed,
            }
        )
    except BaseException as error:
        _flush_logs()
        _send_error(request_id, "hook_failed", error)


def main():
    global _active_request_id, _active_execution_id, _active_plugin_id
    if len(sys.argv) != 2:
        raise RuntimeError("expected SDK root argument")
    sdk_root = os.path.abspath(sys.argv[1])
    sys.path.insert(0, sdk_root)
    _write_frame(
        {
            "type": "hello",
            "protocolVersion": PROTOCOL_VERSION,
            "sdkApiVersion": SDK_API_VERSION,
            "pythonVersion": list(sys.version_info[:3]),
            "implementation": sys.implementation.name,
        }
    )
    import flowlens

    while True:
        message = _read_frame()
        if message is None:
            return
        if not isinstance(message, dict):
            raise RuntimeError("protocol message must be an object")
        request_id = message.get("requestId", "")
        _active_request_id = request_id
        _active_execution_id = message.get("executionId", "")
        _active_plugin_id = message.get("pluginId", "")
        try:
            message_type = message.get("type")
            if message_type == "validate":
                _handle_validate(message)
            elif message_type == "invoke":
                _handle_invoke(message, flowlens)
            elif message_type == "shutdown":
                _flush_logs()
                _write_frame({"type": "result", "requestId": request_id, "shutdown": True})
                return
            else:
                _send_error(request_id, "protocol_error", ValueError("unsupported message type {}".format(message_type)))
        finally:
            _active_request_id = ""
            _active_execution_id = ""
            _active_plugin_id = ""


if __name__ == "__main__":
    try:
        main()
    except BaseException:
        try:
            _flush_logs()
        finally:
            os._exit(70)
