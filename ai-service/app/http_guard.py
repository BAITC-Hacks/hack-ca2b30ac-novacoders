"""ASGI guard: authenticate before parsing, bound streamed bytes and request time."""

import asyncio
import ipaddress
import json
import secrets

from starlette.responses import JSONResponse

from .settings import Settings


def error_response(status: int, code: str, message: str, request_id=None, fields=None):
    return JSONResponse(status_code=status, content={
        "requestId": request_id,
        "error": {"code": code, "message": message, "fields": fields or []},
    })


class HTTPGuard:
    def __init__(self, app, settings: Settings):
        self.app = app
        self.settings = settings

    async def __call__(self, scope, receive, send):
        if scope["type"] != "http":
            return await self.app(scope, receive, send)
        settings = self.settings
        headers = dict(scope.get("headers", []))
        expected = settings.ai_service_token.get_secret_value()
        if expected:
            authorization = headers.get(b"authorization", b"").decode("latin-1")
            parts = authorization.split(" ", 1)
            valid = (len(parts) == 2 and parts[0].lower() == "bearer"
                     and secrets.compare_digest(parts[1].encode(), expected.encode()))
        else:
            peer = (scope.get("client") or ("", 0))[0]
            try:
                local = ipaddress.ip_address(peer).is_loopback
            except ValueError:
                local = False
            valid = settings.ai_mode == "mock" and settings.ai_local_mode and local
        if not valid:
            return await error_response(401, "UNAUTHORIZED", "Требуется внутренний токен")(scope, receive, send)

        length = headers.get(b"content-length")
        if length is not None:
            try:
                declared_size = int(length)
                if declared_size < 0:
                    raise ValueError
            except ValueError:
                return await error_response(400, "INVALID_REQUEST", "Некорректная длина запроса")(scope, receive, send)
            if declared_size > settings.ai_max_request_bytes:
                return await error_response(413, "PAYLOAD_TOO_LARGE", "Превышен размер запроса")(scope, receive, send)

        started = False

        async def guarded_send(message):
            nonlocal started
            if message["type"] == "http.response.start":
                started = True
            await send(message)

        try:
            async with asyncio.timeout(settings.ai_request_timeout_seconds):
                body = bytearray()
                while True:
                    message = await receive()
                    if message["type"] == "http.disconnect":
                        return
                    chunk = message.get("body", b"")
                    if len(body) + len(chunk) > settings.ai_max_request_bytes:
                        return await error_response(413, "PAYLOAD_TOO_LARGE", "Превышен размер запроса")(scope, receive, send)
                    body.extend(chunk)
                    if not message.get("more_body", False):
                        break

                if scope["method"] == "POST" and scope["path"] == "/v1/recommendations":
                    media_type = headers.get(b"content-type", b"").split(b";", 1)[0].strip().lower()
                    if media_type != b"application/json":
                        return await error_response(415, "UNSUPPORTED_MEDIA_TYPE", "Ожидается application/json")(scope, receive, send)
                    try:
                        data = json.loads(body)
                    except (ValueError, UnicodeDecodeError, RecursionError):
                        return await error_response(400, "INVALID_REQUEST", "Некорректный JSON")(scope, receive, send)
                    products = data.get("products") if isinstance(data, dict) else None
                    if isinstance(products, list) and len(products) > settings.ai_max_products:
                        return await error_response(422, "PRODUCT_LIMIT_EXCEEDED", "Превышено число товаров")(scope, receive, send)

                delivered = False

                async def replay():
                    nonlocal delivered
                    if not delivered:
                        delivered = True
                        return {"type": "http.request", "body": bytes(body), "more_body": False}
                    return await receive()

                await self.app(scope, replay, guarded_send)
        except TimeoutError:
            if not started:
                await error_response(504, "REQUEST_TIMEOUT", "Истекло время обработки запроса")(scope, receive, send)
