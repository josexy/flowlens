"""Inject headers, mutate JSON, and carry state between hook phases."""

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
