import asyncio
import json
import httpx
import pytest
from pydantic import BaseModel, ValidationError

from app.providers.clients import ProviderClients, ProviderError, OFFICIAL_BASE_URL
from app.settings import Settings


class Output(BaseModel):
    text: str


def live_settings(**kwargs):
    return Settings(ai_mode="live", allow_external_ai=True,
                    ai_service_token="test-only-token", openai_api_key="test-only-not-a-key", **kwargs)


def sdk_response(text='{"text":"ok"}', **changes):
    result = {
        "id": "resp_test", "object": "response", "created_at": 1,
        "model": "gpt-6-luna", "status": "completed", "error": None,
        "incomplete_details": None, "parallel_tool_calls": False, "tools": [],
        "tool_choice": "auto",
        "output": [{"id": "msg_test", "type": "message", "role": "assistant", "status": "completed",
                    "content": [{"type": "output_text", "text": text, "annotations": []}]}],
    }
    result.update(changes)
    return result


def test_local_bypass_requires_explicit_setting():
    with pytest.raises(ValidationError, match="AI_SERVICE_TOKEN required"):
        Settings()
    Settings(ai_local_mode=True)
    with pytest.raises(ValidationError):
        Settings(ai_local_mode=True, ai_mode="live", allow_external_ai=True)


def test_live_requires_external_permission():
    with pytest.raises(ValidationError, match="ALLOW_EXTERNAL_AI"):
        Settings(ai_mode="live", ai_service_token="test-only-token")


def test_environment_is_used_without_dotenv(monkeypatch, tmp_path):
    (tmp_path / ".env").write_text("AI_MODE=live\n")
    monkeypatch.chdir(tmp_path)
    monkeypatch.setenv("AI_LOCAL_MODE", "true")
    monkeypatch.setenv("AI_MAX_PRODUCTS", "7")
    settings = Settings()
    assert settings.ai_mode == "mock" and settings.ai_max_products == 7
    assert settings.model_for("analysis") == "gpt-6-luna"


@pytest.mark.parametrize("analysis,explanation", [("", ""), ("  ", "  "), ("analysis-test", "explanation-test")])
def test_model_selection(analysis, explanation):
    settings = Settings(ai_local_mode=True, openai_model="base-test",
                        openai_analysis_model=analysis, openai_explanation_model=explanation)
    assert settings.model_for("analysis") == (analysis.strip() or "base-test")
    assert settings.model_for("explanation") == (explanation.strip() or "base-test")


def test_legacy_environment_is_ignored(monkeypatch):
    for name in ("NVIDIA_API_KEY", "NVIDIA_MODEL", "NVIDIA_BASE_URL"):
        monkeypatch.setenv(name, "test-only-obsolete")
    settings = Settings(ai_local_mode=True)
    assert not any(name.startswith("nvidia") for name in Settings.model_fields)
    assert settings.model_for("analysis") == "gpt-6-luna"


def test_mock_never_constructs_client(monkeypatch):
    def forbidden(*args, **kwargs):
        raise AssertionError("No SDK client in mock")
    monkeypatch.setattr("app.providers.clients.AsyncOpenAI", forbidden)
    async def run():
        clients = ProviderClients(Settings(ai_local_mode=True, allow_external_ai=True))
        await clients.start()
        with pytest.raises(ProviderError, match="disabled"):
            await clients.parse("analysis", "test", "{}", Output)
        await clients.close()
    asyncio.run(run())


def test_unconfigured_provider_blocked():
    async def run():
        clients = ProviderClients(Settings(ai_mode="live", allow_external_ai=True,
                                           ai_service_token="test-only-token"))
        await clients.start()
        with pytest.raises(ProviderError, match="not_configured"):
            await clients.parse("analysis", "test", "{}", Output)
        await clients.close()
    asyncio.run(run())


def test_actual_sdk_parse_payload_reuse_models_and_close(monkeypatch):
    calls = []
    async def send(self, request, **kwargs):
        assert str(request.url) == OFFICIAL_BASE_URL + "/responses"
        calls.append(json.loads(request.content))
        return httpx.Response(200, json=sdk_response(), request=request)
    monkeypatch.setattr(httpx.AsyncClient, "send", send)
    monkeypatch.setenv("OPENAI_BASE_URL", "https://invalid.example/v1")
    async def run():
        clients = ProviderClients(live_settings(openai_analysis_model="analysis-test",
                                               openai_explanation_model="explanation-test"))
        await clients.start()
        sdk = clients._client
        await clients.start()
        assert clients._client is sdk
        assert (await clients.parse("analysis", "instructions", "{}", Output)).text == "ok"
        assert (await clients.parse("explanation", "instructions", "{}", Output)).text == "ok"
        await clients.close()
        assert sdk.is_closed() and clients._client is None
    asyncio.run(run())
    assert [c["model"] for c in calls] == ["analysis-test", "explanation-test"]
    for call in calls:
        assert call["store"] is False
        assert call["max_output_tokens"] == 2048
        assert call["text"]["format"]["type"] == "json_schema"
        assert call["text"]["format"]["strict"] is True
        assert not {"tools", "temperature", "top_p", "reasoning"} & call.keys()


@pytest.mark.parametrize("status,code", [(401, "access_denied"), (403, "access_denied"),
                                         (404, "access_denied"), (429, "rate_limited"),
                                         (500, "provider_error")])
def test_http_error_codes_and_no_model_switch(monkeypatch, caplog, status, code):
    calls = []
    async def send(self, request, **kwargs):
        calls.append(json.loads(request.content)["model"])
        return httpx.Response(status, json={"error": {"message": "PRIVATE_PROVIDER_MESSAGE"}}, request=request)
    monkeypatch.setattr(httpx.AsyncClient, "send", send)
    async def run():
        clients = ProviderClients(live_settings(ai_provider_max_retries=0))
        await clients.start()
        try:
            with pytest.raises(ProviderError, match=code) as err:
                await clients.parse("analysis", "PRIVATE_PROMPT", "{}", Output)
            assert "PRIVATE" not in str(err.value)
        finally:
            await clients.close()
    asyncio.run(run())
    assert calls == ["gpt-6-luna"]
    assert "PRIVATE" not in caplog.text


def test_retries_exist_only_in_sdk(monkeypatch):
    calls = []
    async def send(self, request, **kwargs):
        calls.append(request)
        return httpx.Response(500 if len(calls) == 1 else 200,
                              json={"error": {"message": "test"}} if len(calls) == 1 else sdk_response(),
                              request=request, headers={"retry-after-ms": "1"})
    monkeypatch.setattr(httpx.AsyncClient, "send", send)
    async def run():
        clients = ProviderClients(live_settings(ai_provider_max_retries=1))
        await clients.start()
        try:
            assert (await clients.parse("analysis", "test", "{}", Output)).text == "ok"
        finally:
            await clients.close()
    asyncio.run(run())
    assert len(calls) == 2


@pytest.mark.parametrize("wire,code", [
    (sdk_response(text=""), "invalid_response"),
    (sdk_response(output=[]), "empty_response"),
    (sdk_response(text="{}"), "invalid_response"),
    (sdk_response(text="{broken"), "invalid_response"),
    (sdk_response(status="incomplete", incomplete_details={"reason": "max_output_tokens"}), "incomplete"),
    (sdk_response(output=[{"id": "m", "type": "message", "status": "completed", "role": "assistant",
                          "content": [{"type": "refusal", "refusal": "test refusal"}]}]), "refused"),
])
def test_non_success_outputs_are_errors(monkeypatch, wire, code):
    async def send(self, request, **kwargs):
        return httpx.Response(200, json=wire, request=request)
    monkeypatch.setattr(httpx.AsyncClient, "send", send)
    async def run():
        clients = ProviderClients(live_settings())
        await clients.start()
        try:
            with pytest.raises(ProviderError, match=code):
                await clients.parse("analysis", "test", "{}", Output)
        finally:
            await clients.close()
    asyncio.run(run())


def test_concurrency_and_timeout(monkeypatch):
    active = peak = 0
    async def send(self, request, **kwargs):
        nonlocal active, peak
        active += 1
        peak = max(peak, active)
        try:
            await asyncio.sleep(0.02)
            return httpx.Response(200, json=sdk_response(), request=request)
        finally:
            active -= 1
    monkeypatch.setattr(httpx.AsyncClient, "send", send)
    async def run():
        clients = ProviderClients(live_settings(ai_max_concurrent_api_calls=2))
        await clients.start()
        try:
            await asyncio.gather(*(clients.parse(t, "test", "{}", Output)
                                   for t in ["analysis", "explanation"] * 3))
        finally:
            await clients.close()
        assert peak == 2
        clients = ProviderClients(live_settings(ai_provider_timeout_seconds=0.001))
        await clients.start()
        try:
            with pytest.raises(ProviderError, match="timeout"):
                await clients.parse("analysis", "test", "{}", Output)
            assert active == 0
        finally:
            await clients.close()
    asyncio.run(run())


@pytest.mark.parametrize("name", ["ai_max_products", "ai_max_request_bytes", "ai_max_concurrent_api_calls",
                                  "ai_request_timeout_seconds", "ai_provider_timeout_seconds",
                                  "ai_max_output_tokens", "ai_analysis_max_rows", "ai_analysis_max_context_bytes"])
def test_limits_must_be_positive(name):
    with pytest.raises(ValidationError):
        Settings(ai_local_mode=True, **{name: 0})
