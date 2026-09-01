"""Block requests whose URL starts with a configured prefix."""

from flowlens import *


def onRequest(context, request):
    blocked_prefix = str(context.params.get("blocked_url_prefix", ""))
    if blocked_prefix and request.url.startswith(blocked_prefix):
        context.log.warning("request blocked by configured URL prefix")
        return None
    return request


def onResponse(context, response):
    return response
