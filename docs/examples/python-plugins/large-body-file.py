from flowlens import *

import hashlib
import os
import tempfile


CHUNK_SIZE = 1024 * 1024


def _new_temp_path(temp_dir, suffix):
    descriptor, path = tempfile.mkstemp(
        dir=temp_dir, prefix="flowlens-example-", suffix=suffix
    )
    os.close(descriptor)
    return path


def onRequest(context, request):
    if request.body.kind in {"text", "json", "xml", "binary", "file"}:
        source_path = None
        temp_dir = str(context.params.get("temp_dir", "")).strip() or None
        try:
            source_path = _new_temp_path(temp_dir, ".request")
            request.body.write_file(source_path)
            digest = hashlib.sha256()
            with open(source_path, "rb") as source:
                while chunk := source.read(CHUNK_SIZE):
                    digest.update(chunk)
            request.headers.set("X-Body-SHA256", digest.hexdigest())
        finally:
            if source_path is not None:
                try:
                    os.remove(source_path)
                except FileNotFoundError:
                    pass
    return request


def onResponse(context, response):
    if response.body.kind in {"none", "unavailable"}:
        return response

    source_path = None
    replacement_path = None
    temp_dir = str(context.params.get("temp_dir", "")).strip() or None
    try:
        source_path = _new_temp_path(temp_dir, ".response")
        replacement_path = _new_temp_path(temp_dir, ".replacement")
        response.body.write_file(source_path)
        # Close both files before assigning the replacement so this also works on Windows.
        with open(source_path, "rb") as source, open(replacement_path, "wb") as output:
            output.write(b"processed by FlowLens\n")
            while chunk := source.read(CHUNK_SIZE):
                output.write(chunk)

        # Owner assignment copies the source into FlowLens's session directory,
        # so the script-owned replacement can be removed immediately afterward.
        response.body = Body("binary", FileDescriptor.from_file(replacement_path))
    finally:
        for path in (source_path, replacement_path):
            if path is None:
                continue
            try:
                os.remove(path)
            except FileNotFoundError:
                pass
    return response
