"""Handle the request Body kinds exposed by HTTP Request Editor."""

from flowlens import *

import hashlib


def _set_digest_header(headers, body):
    digest = hashlib.sha256()
    value = body.value
    if isinstance(value, FileDescriptor):
        with open(value.path, "rb") as source:
            while chunk := source.read(1024 * 1024):
                digest.update(chunk)
    else:
        if isinstance(value, str):
            value = value.encode("utf-8")
        digest.update(value)
    headers.set("X-FlowLens-Body-SHA256", digest.hexdigest())


def _upsert_urlencoded_field(body):
    for field in body.value:
        if field.name == "flowlens":
            field.enabled = True
            field.value = "enabled"
            return
    body.value.append(URLEncodedField("flowlens", "enabled"))


def _upsert_multipart_part(body, upload_path=""):
    for part in body.value:
        if part.name == "flowlens" and part.file is None:
            part.enabled = True
            part.value = "enabled"
            break
    else:
        body.value.append(MultipartPart("flowlens", "enabled"))
    if upload_path:
        body.value.append(
            MultipartPart(
                "upload",
                file=FileDescriptor.from_file(upload_path),
                filename="flowlens-upload.bin",
            )
        )


def onRequest(context, request):
    body = request.body
    request.headers.set("X-FlowLens-Body-Kind", body.kind)

    if body.kind == "none":
        if context.params.get("fill_empty_body") is True:
            request.body = {"flowlens": True}
    elif body.kind == "text":
        request.body = body.value + "\nFlowLens"
    elif body.kind == "xml":
        request.body = Body("xml", "<!-- FlowLens -->\n" + body.value)
    elif body.kind == "json":
        value = body.value
        if isinstance(value, dict):
            value["flowlens"] = True
            request.body = value
    elif body.kind in ("binary", "file"):
        _set_digest_header(request.headers, body)
    elif body.kind == "urlencoded":
        _upsert_urlencoded_field(body)
    elif body.kind == "multipart":
        _upsert_multipart_part(body, context.params.get("multipart_upload_path", ""))

    return request


def onResponse(context, response):
    body = response.body
    response.headers.set("X-FlowLens-Body-Kind", body.kind)
    if body.kind == "unavailable":
        # SSE exposes status and headers, but its streaming Body cannot be read or replaced.
        response.headers.set("X-FlowLens-Body-Readable", "no")
    elif body.kind == "xml":
        response.body = Body("xml", "<!-- FlowLens response -->\n" + body.value)
    return response
