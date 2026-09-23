"""One lifecycle-owned OpenAI client for analysis and explanation."""

import asyncio
import logging
from typing import Literal, TypeVar

import httpx
from openai import (
    APIConnectionError, APIError, APIStatusError, APITimeoutError, AsyncOpenAI,
    AuthenticationError, PermissionDeniedError, RateLimitError,
)
from pydantic import BaseModel, ValidationError

from ..settings import Settings

OFFICIAL_BASE_URL = "https://api.openai.com/v1"
Task = Literal["analysis", "explanation"]
ErrorCode = Literal[
    "disabled", "not_configured", "not_started", "access_denied", "timeout",
    "rate_limited", "provider_error", "refused", "incomplete", "empty_response",
    "invalid_response", "unexpected_output",
]
T = TypeVar("T", bound=BaseModel)
logger = logging.getLogger("ai_service.provider")


class ProviderError(RuntimeError):
    def __init__(self, code: ErrorCode):
        self.code = code
        super().__init__(code)  # Never attach SDK response bodies or exception text.


class ProviderClients:
    def __init__(self, settings: Settings):
        self.settings = settings
        self._slots = asyncio.Semaphore(settings.ai_max_concurrent_api_calls)
        self._client: AsyncOpenAI | None = None

    async def start(self):
        settings = self.settings
        if self._client is not None or settings.ai_mode == "mock" or not settings.allow_external_ai:
            return
        key = settings.openai_api_key.get_secret_value()
        if not key:
            return
        # Explicit URL overrides OPENAI_BASE_URL; proxies and redirects are disabled.
        self._client = AsyncOpenAI(
            api_key=key, base_url=OFFICIAL_BASE_URL,
            timeout=settings.ai_provider_timeout_seconds,
            max_retries=settings.ai_provider_max_retries,
            http_client=httpx.AsyncClient(
                trust_env=False, follow_redirects=False,
                timeout=settings.ai_provider_timeout_seconds,
            ),
        )

    async def close(self):
        if self._client is not None:
            client, self._client = self._client, None
            await client.close()

    async def parse(self, task: Task, instructions: str, input_json: str, output_type: type[T]) -> T:
        if task not in ("analysis", "explanation"):
            raise ValueError("Unknown AI task")
        try:
            return await self._parse(task, instructions, input_json, output_type)
        except ProviderError as exc:
            # Fixed event/code/task only: no prompts, rows, IDs, keys, or SDK messages.
            logger.warning("ai_call_failed task=%s code=%s", task, exc.code)
            raise

    async def _parse(self, task: Task, instructions: str, input_json: str, output_type: type[T]) -> T:
        settings = self.settings
        if settings.ai_mode != "live" or not settings.allow_external_ai:
            raise ProviderError("disabled")
        if not settings.openai_api_key.get_secret_value():
            raise ProviderError("not_configured")
        if self._client is None:
            raise ProviderError("not_started")
        try:
            # Retries live ONLY in the SDK. Deadline includes queue + retries.
            async with asyncio.timeout(settings.ai_provider_timeout_seconds):
                async with self._slots:
                    response = await self._client.responses.parse(
                        model=settings.model_for(task), instructions=instructions,
                        input=[{"role": "user", "content": input_json}],
                        text_format=output_type, store=False,
                        max_output_tokens=settings.ai_max_output_tokens,
                    )
        except (TimeoutError, APITimeoutError):
            raise ProviderError("timeout") from None
        except (AuthenticationError, PermissionDeniedError):
            raise ProviderError("access_denied") from None
        except RateLimitError:
            raise ProviderError("rate_limited") from None
        except APIStatusError as exc:
            raise ProviderError("access_denied" if exc.status_code == 404 else "provider_error") from None
        except APIConnectionError:
            raise ProviderError("provider_error") from None
        except APIError:
            raise ProviderError("provider_error") from None
        except (ValidationError, ValueError):
            # SDK may reject truncated JSON before exposing response.status.
            raise ProviderError("invalid_response") from None

        for item in response.output:
            if item.type not in ("message", "reasoning"):
                raise ProviderError("unexpected_output")
            if item.type == "message":
                for part in item.content:
                    if part.type == "refusal":
                        raise ProviderError("refused")
                if item.status != "completed":
                    raise ProviderError("incomplete")
        if response.status != "completed":
            raise ProviderError("incomplete")
        if response.error is not None or response.incomplete_details is not None:
            raise ProviderError("incomplete")
        messages = [i for i in response.output if i.type == "message"]
        parts = [p for i in messages for p in i.content if p.type == "output_text"]
        if not parts or not any(p.text.strip() for p in parts):
            raise ProviderError("empty_response")
        if len(parts) != 1 or not isinstance(response.output_parsed, output_type):
            raise ProviderError("invalid_response")
        try:
            result = output_type.model_validate(response.output_parsed.model_dump())
        except ValidationError:
            raise ProviderError("invalid_response") from None
        logger.info("ai_call_completed task=%s", task)
        return result
