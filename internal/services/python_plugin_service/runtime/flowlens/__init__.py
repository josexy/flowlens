"""FlowLens Python plugin SDK API v1."""

from __future__ import annotations

import base64
import copy
import io
import json
import os
import shutil
import stat
import tempfile
from http import HTTPStatus
from types import MappingProxyType
from typing import Any, Iterable, Iterator
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit


def _freeze(value: Any) -> Any:
    if isinstance(value, dict):
        return MappingProxyType({str(key): _freeze(item) for key, item in value.items()})
    if isinstance(value, list):
        return tuple(_freeze(item) for item in value)
    return value


def _json_clone(value: Any) -> Any:
    return json.loads(json.dumps(value, ensure_ascii=False, allow_nan=False))


def _validate_source_file(path: str) -> os.stat_result:
    if not isinstance(path, str) or not os.path.isabs(path):
        raise ValueError("file paths must be absolute")
    source_info = os.lstat(path)
    if stat.S_ISLNK(source_info.st_mode) or not stat.S_ISREG(source_info.st_mode):
        raise ValueError("file paths must reference non-symlink regular files")
    return source_info


def _content_type(headers: Any) -> str | None:
    value = headers.get("Content-Type")
    return value if isinstance(value, str) else None


def _status_text_for_code(code: int) -> str:
    try:
        phrase = HTTPStatus(code).phrase
    except ValueError:
        phrase = ""
    return f"{code} {phrase}" if phrase else str(code)


def _split_url(value: str):
    if not isinstance(value, str) or not value:
        raise ValueError("request URL must be a non-empty string")
    return urlsplit(value)


class QueryField:
    def __init__(self, name: str, value: str):
        if not isinstance(name, str) or not isinstance(value, str):
            raise TypeError("query name and value must be strings")
        self.name = name
        self.value = value


class Queries:
    def __init__(
        self,
        fields: Iterable[QueryField | tuple[str, str] | dict[str, str]] | None = None,
        *,
        original_query: str | None = None,
    ):
        self._fields: list[QueryField] = []
        for field in fields or []:
            if isinstance(field, QueryField):
                self._fields.append(QueryField(field.name, field.value))
            elif isinstance(field, tuple) and len(field) == 2:
                self._fields.append(QueryField(field[0], field[1]))
            elif isinstance(field, dict):
                self._fields.append(QueryField(field.get("name"), field.get("value")))
            else:
                raise TypeError("queries must contain QueryField, tuple, or dict values")
        self._original_query = original_query
        self._original_fields = self._pairs()

    @classmethod
    def _from_query(cls, value: str) -> "Queries":
        return cls(parse_qsl(value, keep_blank_values=True), original_query=value)

    @classmethod
    def _coerce(cls, value: Any) -> "Queries":
        if isinstance(value, Queries):
            return cls(value._fields)
        if isinstance(value, str):
            return cls._from_query(value)
        if isinstance(value, dict):
            return cls(value.items())
        if isinstance(value, Iterable):
            return cls(value)
        raise TypeError("queries must be a query string, mapping, iterable, or Queries")

    def __iter__(self) -> Iterator[QueryField]:
        return iter(self._fields)

    def __len__(self) -> int:
        return len(self._fields)

    def __getitem__(self, key: int | str) -> QueryField | str | None:
        if isinstance(key, int):
            return self._fields[key]
        return self.get(key)

    def __setitem__(self, name: str, value: str) -> None:
        self.set(name, value)

    def get(self, name: str, default: Any = None) -> Any:
        for field in self._fields:
            if field.name == name:
                return field.value
        return default

    def get_all(self, name: str) -> list[str]:
        return [field.value for field in self._fields if field.name == name]

    def set(self, name: str, value: str) -> None:
        replacement = QueryField(name, value)
        first_index: int | None = None
        kept: list[QueryField] = []
        for field in self._fields:
            if field.name == name:
                if first_index is None:
                    first_index = len(kept)
                continue
            kept.append(field)
        if first_index is None:
            kept.append(replacement)
        else:
            kept.insert(first_index, replacement)
        self._fields = kept

    def add(self, name: str, value: str) -> None:
        self._fields.append(QueryField(name, value))

    def remove(self, name: str) -> None:
        self._fields = [field for field in self._fields if field.name != name]

    def clear(self) -> None:
        self._fields.clear()

    def to_string(self) -> str:
        pairs = self._pairs()
        if self._original_query is not None and pairs == self._original_fields:
            return self._original_query
        return urlencode(pairs)

    def _pairs(self) -> tuple[tuple[str, str], ...]:
        return tuple((field.name, field.value) for field in self._fields)


class FrozenQueries:
    def __init__(self, query: str):
        self._fields = tuple(parse_qsl(query, keep_blank_values=True))
        self._original_query = query

    def __iter__(self) -> Iterator[tuple[str, str]]:
        return iter(self._fields)

    def __len__(self) -> int:
        return len(self._fields)

    def __getitem__(self, key: int | str) -> tuple[str, str] | str | None:
        if isinstance(key, int):
            return self._fields[key]
        return self.get(key)

    def get(self, name: str, default: Any = None) -> Any:
        for field_name, value in self._fields:
            if field_name == name:
                return value
        return default

    def get_all(self, name: str) -> list[str]:
        return [value for field_name, value in self._fields if field_name == name]

    def to_string(self) -> str:
        return self._original_query


class HeaderField:
    def __init__(self, name: str, value: str):
        if not isinstance(name, str) or not isinstance(value, str):
            raise TypeError("header name and value must be strings")
        self.name = name
        self.value = value

    def _to_wire(self) -> dict[str, str]:
        return {"name": self.name, "value": self.value}


class Headers:
    def __init__(self, fields: Iterable[HeaderField | dict[str, str]] | None = None):
        self._fields: list[HeaderField] = []
        for field in fields or []:
            if isinstance(field, HeaderField):
                self._fields.append(HeaderField(field.name, field.value))
            elif isinstance(field, dict):
                self._fields.append(HeaderField(field.get("name"), field.get("value")))
            else:
                raise TypeError("headers must contain HeaderField or dict values")

    def __iter__(self) -> Iterator[HeaderField]:
        return iter(self._fields)

    def __len__(self) -> int:
        return len(self._fields)

    def __getitem__(self, index: int) -> HeaderField:
        return self._fields[index]

    def get(self, name: str, default: Any = None) -> Any:
        lowered = name.lower()
        for field in self._fields:
            if field.name.lower() == lowered:
                return field.value
        return default

    def get_all(self, name: str) -> list[str]:
        lowered = name.lower()
        return [field.value for field in self._fields if field.name.lower() == lowered]

    def set(self, name: str, value: str) -> None:
        replacement = HeaderField(name, value)
        lowered = name.lower()
        first_index: int | None = None
        kept: list[HeaderField] = []
        for field in self._fields:
            if field.name.lower() == lowered:
                if first_index is None:
                    first_index = len(kept)
                continue
            kept.append(field)
        if first_index is None:
            kept.append(replacement)
        else:
            kept.insert(first_index, replacement)
        self._fields = kept

    def add(self, name: str, value: str) -> None:
        self._fields.append(HeaderField(name, value))

    def remove(self, name: str) -> None:
        lowered = name.lower()
        self._fields = [field for field in self._fields if field.name.lower() != lowered]

    def clear(self) -> None:
        self._fields.clear()

    def _to_wire(self) -> list[dict[str, str]]:
        return [field._to_wire() for field in self._fields]


class FrozenHeaders:
    def __init__(self, fields: Iterable[dict[str, str]] | None = None):
        self._fields = tuple((field["name"], field["value"]) for field in fields or [])

    def __iter__(self) -> Iterator[tuple[str, str]]:
        return iter(self._fields)

    def __len__(self) -> int:
        return len(self._fields)

    def get(self, name: str, default: Any = None) -> Any:
        lowered = name.lower()
        for field_name, value in self._fields:
            if field_name.lower() == lowered:
                return value
        return default

    def get_all(self, name: str) -> list[str]:
        lowered = name.lower()
        return [value for field_name, value in self._fields if field_name.lower() == lowered]


class FileDescriptor:
    def __init__(self, path: str, name: str = "", size: int = -1, read_only: bool = True):
        self.path = path
        self.name = name
        self.size = size
        self.read_only = bool(read_only)
        self._source_path: str | None = None

    @classmethod
    def from_file(cls, path: str) -> "FileDescriptor":
        source_info = _validate_source_file(path)
        descriptor = cls(
            path,
            os.path.basename(path),
            source_info.st_size,
            read_only=True,
        )
        descriptor._source_path = path
        return descriptor

    @classmethod
    def _from_wire(cls, value: dict[str, Any]) -> "FileDescriptor":
        if not isinstance(value, dict):
            raise TypeError("file descriptor must be an object")
        return cls(
            value.get("path", ""),
            value.get("name", ""),
            value.get("size", -1),
            bool(value.get("readOnly", True)),
        )

    def _to_wire(self) -> dict[str, Any]:
        return {
            "path": self.path,
            "name": self.name,
            "size": self.size,
            "readOnly": self.read_only,
        }


class URLEncodedField:
    def __init__(self, name: str, value: str, enabled: bool = True):
        self.enabled = enabled
        self.name = name
        self.value = value

    @classmethod
    def _from_wire(cls, value: dict[str, Any]) -> "URLEncodedField":
        return cls(value.get("name", ""), value.get("value", ""), bool(value.get("enabled", True)))

    def _to_wire(self) -> dict[str, Any]:
        return {"enabled": bool(self.enabled), "name": self.name, "value": self.value}


class MultipartPart:
    def __init__(
        self,
        name: str,
        value: str = "",
        file: FileDescriptor | None = None,
        enabled: bool = True,
        filename: str = "",
    ):
        self.enabled = enabled
        self.name = name
        self.value = value
        self.file = file
        self.filename = filename

    @classmethod
    def _from_wire(cls, value: dict[str, Any]) -> "MultipartPart":
        file_value = value.get("file")
        return cls(
            name=value.get("name", ""),
            value=value.get("value", ""),
            file=FileDescriptor._from_wire(file_value) if file_value is not None else None,
            enabled=bool(value.get("enabled", True)),
            filename=value.get("filename", ""),
        )

    def _to_wire(self) -> dict[str, Any]:
        return {
            "enabled": bool(self.enabled),
            "name": self.name,
            "kind": "file" if self.file is not None else "text",
            "value": self.value,
            "file": self.file._to_wire() if self.file is not None else None,
            "filename": self.filename,
        }


class Body:
    _KNOWN_KINDS = {
        "none",
        "text",
        "json",
        "xml",
        "binary",
        "file",
        "urlencoded",
        "multipart",
        "unavailable",
    }
    _STRUCTURED_KINDS = {"urlencoded", "multipart"}
    _FILE_DESCRIPTOR_KINDS = {"text", "json", "xml", "binary", "file"}
    _INLINE_TRANSPORT_LIMIT = 4 * 1024 * 1024

    def __init__(self, kind: str = "none", value: Any = None):
        if kind not in self._KNOWN_KINDS:
            raise ValueError(f"unsupported body kind: {kind}")
        if isinstance(value, FileDescriptor):
            if kind not in self._FILE_DESCRIPTOR_KINDS:
                raise TypeError(f"{kind} body does not support FileDescriptor values")
            storage = "file"
        elif kind == "file":
            raise TypeError("file body value must be FileDescriptor")
        else:
            storage = "inline"
        self._kind = kind
        self._value = value
        self._storage = storage
        self._phase = "request"
        self._output_directory = ""
        self._streaming = False
        self._original_wire: dict[str, Any] | None = None

    @property
    def kind(self) -> str:
        return self._kind

    @property
    def value(self) -> Any:
        if self.kind in self._STRUCTURED_KINDS:
            return self._value
        self._ensure_readable()
        if self._storage == "inline" or self.kind == "file":
            return self._value
        materialized = self._materialize_bytes()
        if self.kind in {"text", "xml"}:
            return materialized.decode("utf-8")
        if self.kind == "json":
            return json.loads(materialized.decode("utf-8"))
        return materialized

    @classmethod
    def _coerce(
        cls,
        value: Any = None,
        *,
        phase: str = "request",
        output_directory: str = "",
    ) -> "Body":
        if isinstance(value, Body):
            value._bind(phase, output_directory)
            return value
        body = cls()
        body._bind(phase, output_directory)
        if value is None:
            body._assign_value("none", None)
        elif isinstance(value, str):
            body._assign_value("text", value)
        elif isinstance(value, (bytes, bytearray, memoryview)):
            body._assign_value("binary", bytes(value))
        elif isinstance(value, (dict, list)):
            body._assign_json(value)
        else:
            raise TypeError("body must be None, str, bytes-like, dict, list, or Body")
        return body

    def _bind(self, phase: str, output_directory: str) -> None:
        if phase == "response" and self.kind == "file":
            raise ValueError("response bodies do not support file kind")
        self._phase = phase
        self._output_directory = output_directory
        if (
            self._storage == "file"
            and isinstance(self._value, FileDescriptor)
            and self._value._source_path is not None
        ):
            self._value = self._stage_source_file(self._value._source_path)

    @classmethod
    def _from_wire(
        cls,
        value: dict[str, Any] | None,
        *,
        phase: str,
        output_directory: str,
    ) -> "Body":
        value = value or {"kind": "none"}
        if not isinstance(value, dict):
            raise TypeError("body must be an object")
        kind = value.get("kind", "none")
        storage = value.get("storage")
        if storage is None:
            if kind == "file":
                storage = "file"
            elif kind == "unavailable":
                storage = "unavailable"
            else:
                storage = "inline"
        if storage not in {"inline", "file", "unavailable"}:
            raise ValueError(f"unsupported body storage: {storage}")
        if storage == "file":
            raw_value: Any = FileDescriptor._from_wire(value.get("file") or {})
        elif kind == "binary":
            raw_value = base64.b64decode(value.get("base64", ""), validate=True)
        elif kind == "urlencoded":
            raw_value = [URLEncodedField._from_wire(item) for item in value.get("items", [])]
        elif kind == "multipart":
            raw_value = [MultipartPart._from_wire(item) for item in value.get("items", [])]
        else:
            raw_value = copy.deepcopy(value.get("value"))
        body = cls(kind, raw_value)
        body._storage = storage
        body._streaming = bool(value.get("streaming", False))
        body._bind(phase, output_directory)
        body._original_wire = copy.deepcopy(value)
        return body

    def write_file(self, path: str) -> None:
        if not isinstance(path, str) or not os.path.isabs(path):
            raise ValueError("write_file() requires an absolute path")
        if self.kind in self._STRUCTURED_KINDS:
            raise TypeError(f"write_file() does not support {self.kind} bodies")
        self._ensure_readable()
        with self._open_raw() as source, open(path, "wb") as destination:
            while True:
                chunk = source.read(1024 * 1024)
                if not chunk:
                    break
                destination.write(chunk)

    def _stage_source_file(self, path: str) -> FileDescriptor:
        _validate_source_file(path)
        output_directory = self._output_directory
        if not output_directory or not os.path.isabs(output_directory) or not os.path.isdir(output_directory):
            raise ValueError("Body output directory is unavailable")

        suffix = os.path.splitext(path)[1][:32]
        descriptor, staged_path = tempfile.mkstemp(
            prefix="body-",
            suffix=suffix,
            dir=output_directory,
        )
        completed = False
        try:
            os.close(descriptor)
            descriptor = -1
            os.chmod(staged_path, 0o600)
            shutil.copyfile(path, staged_path)
            staged_size = os.stat(staged_path).st_size
            result = FileDescriptor(
                staged_path,
                os.path.basename(path),
                staged_size,
                read_only=True,
            )
            completed = True
            return result
        finally:
            if descriptor >= 0:
                os.close(descriptor)
            if not completed:
                try:
                    os.remove(staged_path)
                except OSError:
                    pass

    def _ensure_readable(self) -> None:
        if self._streaming or self.kind == "unavailable" or self._storage == "unavailable":
            raise ValueError("an unavailable or streaming body cannot be read")
        if self.kind in self._STRUCTURED_KINDS:
            raise TypeError(f"{self.kind} bodies use structured items and cannot be read as raw bytes")

    def _ensure_writable(self) -> None:
        if self._streaming or self.kind == "unavailable" or self._storage == "unavailable":
            raise ValueError("an unavailable or streaming body cannot be modified")

    def _assign_value(self, kind: str, value: Any) -> None:
        self._ensure_writable()
        self._kind = kind
        self._value = value
        self._storage = "inline"

    def _assign_json(self, value: Any) -> None:
        self._assign_value("json", _json_clone(value))

    def _file_descriptor(self) -> FileDescriptor | None:
        if self._storage == "file" and isinstance(self._value, FileDescriptor):
            return self._value
        return None

    def _open_raw(self):
        self._ensure_readable()
        descriptor = self._file_descriptor()
        if descriptor is not None:
            if not isinstance(descriptor.path, str) or not descriptor.path:
                raise ValueError("file-backed body has no readable path")
            return open(descriptor.path, "rb")
        return io.BytesIO(self._inline_bytes())

    def _materialize_bytes(self) -> bytes:
        with self._open_raw() as stream:
            return stream.read()

    def _stage_bytes(self, encoded: bytes, name: str) -> FileDescriptor:
        output_directory = self._output_directory
        if not output_directory or not os.path.isabs(output_directory) or not os.path.isdir(output_directory):
            raise ValueError("Body output directory is unavailable")
        suffix = os.path.splitext(name)[1][:32]
        descriptor, staged_path = tempfile.mkstemp(
            prefix="body-",
            suffix=suffix,
            dir=output_directory,
        )
        completed = False
        try:
            os.chmod(staged_path, 0o600)
            with os.fdopen(descriptor, "wb") as destination:
                descriptor = -1
                destination.write(encoded)
            completed = True
            return FileDescriptor(
                staged_path,
                name,
                len(encoded),
                read_only=True,
            )
        finally:
            if descriptor >= 0:
                os.close(descriptor)
            if not completed:
                try:
                    os.remove(staged_path)
                except OSError:
                    pass

    def _inline_bytes(self) -> bytes:
        if self._storage != "inline":
            raise ValueError("body is not inline")
        if self.kind == "none":
            return b""
        if self.kind in {"text", "xml"}:
            if not isinstance(self._value, str):
                raise TypeError(f"{self.kind} body value must be a string")
            return self._value.encode("utf-8")
        if self.kind == "json":
            return json.dumps(
                self._value,
                ensure_ascii=False,
                allow_nan=False,
                separators=(",", ":"),
            ).encode("utf-8")
        if self.kind == "binary":
            if not isinstance(self._value, (bytes, bytearray, memoryview)):
                raise TypeError("binary body value must be bytes-like")
            return bytes(self._value)
        if self.kind == "file":
            raise TypeError("file body value must be a FileDescriptor")
        if self.kind in self._STRUCTURED_KINDS:
            raise TypeError(f"{self.kind} body cannot be represented as raw bytes")
        raise ValueError(f"unsupported body kind: {self.kind}")

    def _stage_inline_for_wire(self) -> None:
        if self._storage != "inline" or self.kind not in {"text", "xml", "json", "binary"}:
            return
        encoded = self._inline_bytes()
        if len(encoded) <= self._INLINE_TRANSPORT_LIMIT:
            return
        names = {
            "text": "body.txt",
            "xml": "body.xml",
            "json": "body.json",
            "binary": "body.bin",
        }
        self._value = self._stage_bytes(encoded, names[self.kind])
        self._storage = "file"

    def _to_wire(self) -> dict[str, Any]:
        if self.kind not in self._KNOWN_KINDS:
            raise ValueError(f"unsupported body kind: {self.kind}")
        self._stage_inline_for_wire()
        if self._storage == "file":
            if not isinstance(self._value, FileDescriptor):
                raise TypeError("file-backed body value must be FileDescriptor")
            if self._value._source_path is not None:
                self._value = self._stage_source_file(self._value._source_path)
            result: dict[str, Any] = {
                "kind": self.kind,
                "storage": "file",
                "file": self._value._to_wire(),
                "size": self._value.size,
            }
        elif self._storage == "unavailable" or self.kind == "unavailable":
            result = {"kind": "unavailable", "value": None}
        elif self.kind == "binary":
            if not isinstance(self._value, (bytes, bytearray, memoryview)):
                raise TypeError("binary body value must be bytes-like")
            result = {
                "kind": "binary",
                "base64": base64.b64encode(bytes(self._value)).decode("ascii"),
            }
        elif self.kind == "file":
            raise TypeError("file body value must be FileDescriptor")
        elif self.kind == "urlencoded":
            if not isinstance(self._value, list) or not all(
                isinstance(item, URLEncodedField) for item in self._value
            ):
                raise TypeError("urlencoded body value must be a list of URLEncodedField")
            result = {"kind": "urlencoded", "items": [item._to_wire() for item in self._value]}
        elif self.kind == "multipart":
            if not isinstance(self._value, list) or not all(
                isinstance(item, MultipartPart) for item in self._value
            ):
                raise TypeError("multipart body value must be a list of MultipartPart")
            for item in self._value:
                if item.file is None or item.file._source_path is None:
                    continue
                item.file = self._stage_source_file(item.file._source_path)
            result = {"kind": "multipart", "items": [item._to_wire() for item in self._value]}
        else:
            result = {"kind": self.kind, "value": copy.deepcopy(self._value)}
        if self._streaming:
            result["streaming"] = True
        if (
            self._original_wire is not None
            and (
                self._original_wire.get("kind") == "unavailable"
                or bool(self._original_wire.get("streaming", False))
            )
            and result != self._original_wire
        ):
            raise ValueError("an unavailable or streaming body cannot be modified")
        _json_clone(result)
        return result

    @property
    def _changed(self) -> bool:
        return self._original_wire is None or self._to_wire() != self._original_wire


class Request:
    def __init__(
        self,
        method: str,
        url: str,
        headers: Headers | None = None,
        body: Any = None,
        *,
        output_directory: str = "",
    ):
        self.method = method
        self.url = url
        self.headers = headers or Headers()
        self._output_directory = output_directory
        self._body = Body._coerce(
            body,
            phase="request",
            output_directory=output_directory,
        )
        self._original_wire: dict[str, Any] | None = None

    @property
    def url(self) -> str:
        parsed = _split_url(self._url)
        query = self._queries.to_string()
        if query == parsed.query:
            return self._url
        return urlunsplit(
            (
                parsed.scheme,
                parsed.netloc,
                parsed.path,
                query,
                parsed.fragment,
            )
        )

    @url.setter
    def url(self, value: str) -> None:
        parsed = _split_url(value)
        self._url = value
        self._queries = Queries._from_query(parsed.query)

    @property
    def scheme(self) -> str:
        return _split_url(self.url).scheme

    @property
    def host(self) -> str:
        return _split_url(self.url).hostname or ""

    @property
    def port(self) -> int | None:
        parsed = _split_url(self.url)
        if parsed.port is not None:
            return parsed.port
        if parsed.scheme.lower() == "https":
            return 443
        if parsed.scheme.lower() == "http":
            return 80
        return None

    @property
    def path(self) -> str:
        return _split_url(self.url).path

    @path.setter
    def path(self, value: str) -> None:
        if not isinstance(value, str) or (value and not value.startswith("/")):
            raise ValueError("request path must be empty or start with '/'")
        parsed = _split_url(self.url)
        self.url = urlunsplit(
            (parsed.scheme, parsed.netloc, value, parsed.query, parsed.fragment)
        )

    @property
    def queries(self) -> Queries:
        return self._queries

    @queries.setter
    def queries(self, value: Any) -> None:
        self._queries = Queries._coerce(value)

    @property
    def content_type(self) -> str | None:
        return _content_type(self.headers)

    @property
    def body(self) -> Body:
        return self._body

    @body.setter
    def body(self, value: Any) -> None:
        if hasattr(self, "_body"):
            self._body._ensure_writable()
        self._body = Body._coerce(
            value,
            phase="request",
            output_directory=self._output_directory,
        )

    @classmethod
    def _from_wire(cls, value: dict[str, Any], output_directory: str = "") -> "Request":
        request = cls(
            value.get("method", ""),
            value.get("url", ""),
            Headers(value.get("headers", [])),
            Body._from_wire(
                value.get("body"),
                phase="request",
                output_directory=output_directory,
            ),
            output_directory=output_directory,
        )
        request._original_wire = copy.deepcopy(value)
        return request

    def _to_wire(self) -> dict[str, Any]:
        value = {
            "method": self.method,
            "url": self.url,
            "headers": self.headers._to_wire(),
            "body": self.body._to_wire(),
        }
        _json_clone(value)
        return value

    @property
    def _changed(self) -> bool:
        return self._original_wire is None or self._to_wire() != self._original_wire


class FileSnapshot:
    def __init__(self, value: FileDescriptor):
        self._name = value.name
        self._size = value.size

    @property
    def name(self) -> str:
        return self._name

    @property
    def size(self) -> int:
        return self._size


class URLEncodedFieldSnapshot:
    def __init__(self, value: URLEncodedField):
        self._enabled = bool(value.enabled)
        self._name = value.name
        self._value = value.value

    @property
    def enabled(self) -> bool:
        return self._enabled

    @property
    def name(self) -> str:
        return self._name

    @property
    def value(self) -> str:
        return self._value


class MultipartPartSnapshot:
    def __init__(self, value: MultipartPart):
        self._enabled = bool(value.enabled)
        self._name = value.name
        self._value = value.value
        self._file = FileSnapshot(value.file) if value.file is not None else None
        self._filename = value.filename

    @property
    def enabled(self) -> bool:
        return self._enabled

    @property
    def name(self) -> str:
        return self._name

    @property
    def value(self) -> str:
        return self._value

    @property
    def file(self) -> FileSnapshot | None:
        return self._file

    @property
    def filename(self) -> str:
        return self._filename


class BodySnapshot:
    def __init__(self, value: dict[str, Any] | None, output_directory: str = ""):
        self._body = Body._from_wire(
            value,
            phase="request",
            output_directory=output_directory,
        )

    @property
    def kind(self) -> str:
        return self._body.kind

    @property
    def value(self) -> Any:
        value = self._body.value
        if isinstance(value, FileDescriptor):
            return FileSnapshot(value)
        if self.kind == "urlencoded":
            return tuple(URLEncodedFieldSnapshot(item) for item in value)
        if self.kind == "multipart":
            return tuple(MultipartPartSnapshot(item) for item in value)
        return _freeze(copy.deepcopy(value))

    def write_file(self, path: str) -> None:
        self._body.write_file(path)


class RequestSnapshot:
    def __init__(self, value: dict[str, Any], output_directory: str = ""):
        self._method = value.get("method", "")
        self._url = value.get("url", "")
        self._headers = FrozenHeaders(value.get("headers", []))
        self._queries = FrozenQueries(_split_url(self._url).query)
        self._body = BodySnapshot(value.get("body"), output_directory)

    @property
    def method(self) -> str:
        return self._method

    @property
    def url(self) -> str:
        return self._url

    @property
    def scheme(self) -> str:
        return _split_url(self.url).scheme

    @property
    def host(self) -> str:
        return _split_url(self.url).hostname or ""

    @property
    def port(self) -> int | None:
        parsed = _split_url(self.url)
        if parsed.port is not None:
            return parsed.port
        if parsed.scheme.lower() == "https":
            return 443
        if parsed.scheme.lower() == "http":
            return 80
        return None

    @property
    def path(self) -> str:
        return _split_url(self.url).path

    @property
    def queries(self) -> FrozenQueries:
        return self._queries

    @property
    def headers(self) -> FrozenHeaders:
        return self._headers

    @property
    def content_type(self) -> str | None:
        return _content_type(self.headers)

    @property
    def body(self) -> BodySnapshot:
        return self._body


class Response:
    def __init__(
        self,
        code: int,
        headers: Headers | None = None,
        trailers: Headers | None = None,
        body: Any = None,
        request: RequestSnapshot | None = None,
        protocol: str = "",
        status_text: str = "",
        *,
        output_directory: str = "",
    ):
        self._code = code
        self._protocol = protocol
        self._status_text = status_text or _status_text_for_code(code)
        self.headers = headers or Headers()
        self.trailers = trailers or Headers()
        self._output_directory = output_directory
        self._body = Body._coerce(
            body,
            phase="response",
            output_directory=output_directory,
        )
        self._request = request
        self._original_wire: dict[str, Any] | None = None

    @property
    def code(self) -> int:
        return self._code

    @code.setter
    def code(self, value: int) -> None:
        if not isinstance(value, int) or isinstance(value, bool):
            raise TypeError("response code must be an integer")
        changed = hasattr(self, "_code") and self._code != value
        self._code = value
        if changed:
            self._status_text = _status_text_for_code(value)

    @property
    def protocol(self) -> str:
        return self._protocol

    @property
    def status_text(self) -> str:
        return self._status_text

    @property
    def content_type(self) -> str | None:
        return _content_type(self.headers)

    @property
    def body(self) -> Body:
        return self._body

    @body.setter
    def body(self, value: Any) -> None:
        if hasattr(self, "_body"):
            self._body._ensure_writable()
        self._body = Body._coerce(
            value,
            phase="response",
            output_directory=self._output_directory,
        )

    @property
    def request(self) -> RequestSnapshot | None:
        return self._request

    @classmethod
    def _from_wire(cls, value: dict[str, Any], output_directory: str = "") -> "Response":
        request_value = value.get("request")
        response = cls(
            value.get("code", 0),
            Headers(value.get("headers", [])),
            Headers(value.get("trailers", [])),
            Body._from_wire(
                value.get("body"),
                phase="response",
                output_directory=output_directory,
            ),
            RequestSnapshot(request_value, output_directory)
            if isinstance(request_value, dict)
            else None,
            value.get("protocol", ""),
            value.get("statusText", ""),
            output_directory=output_directory,
        )
        response._original_wire = copy.deepcopy(value)
        return response

    def _to_wire(self) -> dict[str, Any]:
        value = {
            "code": self.code,
            "headers": self.headers._to_wire(),
            "trailers": self.trailers._to_wire(),
            "body": self.body._to_wire(),
        }
        if self._original_wire is None or "protocol" in self._original_wire:
            value["protocol"] = self.protocol
        if (
            self._original_wire is None
            or "statusText" in self._original_wire
            or self.code != self._original_wire.get("code")
        ):
            if (
                self._original_wire is not None
                and not self._original_wire.get("statusText")
                and self.code == self._original_wire.get("code")
            ):
                value["statusText"] = self._original_wire.get("statusText", "")
            else:
                value["statusText"] = self.status_text
        if self._original_wire is not None and "request" in self._original_wire:
            value["request"] = copy.deepcopy(self._original_wire["request"])
        _json_clone(value)
        return value

    @property
    def _changed(self) -> bool:
        return self._original_wire is None or self._to_wire() != self._original_wire


class _ContextLogger:
    def __init__(self, emit):
        self._emit = emit

    def debug(self, message: Any) -> None:
        self._emit("debug", str(message))

    def info(self, message: Any) -> None:
        self._emit("info", str(message))

    def warning(self, message: Any) -> None:
        self._emit("warning", str(message))

    def error(self, message: Any) -> None:
        self._emit("error", str(message))


class Context:
    def __init__(self, value: dict[str, Any], emit_log):
        self._id = value.get("id", "")
        self._timestamp = value.get("timestamp", 0)
        self._original_url = value.get("original_url", "")
        self._original_method = value.get("original_method", "")
        self._plugin_id = value.get("plugin_id", "")
        self._plugin_name = value.get("plugin_name", "")
        self._params = _freeze(copy.deepcopy(value.get("params", {})))
        self._transport = _freeze(copy.deepcopy(value.get("transport", {})))
        shared = value.get("shared", {})
        if not isinstance(shared, dict):
            raise TypeError("context.shared must be a JSON object")
        self.shared = _json_clone(shared)
        self.log = _ContextLogger(emit_log)

    @property
    def id(self):
        return self._id

    @property
    def timestamp(self):
        return self._timestamp

    @property
    def original_url(self):
        return self._original_url

    @property
    def original_method(self):
        return self._original_method

    @property
    def plugin_id(self):
        return self._plugin_id

    @property
    def plugin_name(self):
        return self._plugin_name

    @property
    def params(self):
        return self._params

    @property
    def transport(self):
        return self._transport

    def _shared_to_wire(self) -> dict[str, Any]:
        if not isinstance(self.shared, dict):
            raise TypeError("context.shared must remain a JSON object")
        return _json_clone(self.shared)


def _request_from_wire(value: dict[str, Any], output_directory: str = "") -> Request:
    return Request._from_wire(value, output_directory)


def _response_from_wire(value: dict[str, Any], output_directory: str = "") -> Response:
    return Response._from_wire(value, output_directory)


__all__ = [
    "Body",
    "Context",
    "FileDescriptor",
    "HeaderField",
    "Headers",
    "MultipartPart",
    "Queries",
    "QueryField",
    "Request",
    "Response",
    "URLEncodedField",
]
