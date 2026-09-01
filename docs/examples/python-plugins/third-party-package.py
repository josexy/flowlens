"""Use a package installed in the selected Python interpreter environment."""

from flowlens import *

import requests


def onRequest(context, request):
    request.headers.set("X-Requests-Version", requests.__version__)
    return request


def onResponse(context, response):
    return response
