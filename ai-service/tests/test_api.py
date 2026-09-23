import asyncio
from fastapi.testclient import TestClient
import pytest

from app.main import create_app
from app.schemas import RecommendationResponse
from app.settings import Settings


def local_client(settings):
    return TestClient(create_app(settings), client=("127.0.0.1", 50000))


def test_health_and_mock_are_offline_and_consistent(payload, local_settings, monkeypatch):
    from app.providers import clients

    def forbidden_sdk(*args, **kwargs):
        raise AssertionError("Mock must never create an SDK client")

    monkeypatch.setattr(clients, "AsyncOpenAI", forbidden_sdk)
    with local_client(local_settings) as client:
        assert client.get("/health").json()["dependencies"]["openai"] == "disabled"
        response = client.post("/v1/recommendations", json=payload)
        assert response.status_code == 200, response.text
        result = RecommendationResponse.model_validate(response.json())
        assert not result.is_demo and result.source == "fallback"
        assert result.request_id.hex == payload["requestId"].replace("-", "")
        item = result.recommendations[0]
        assert item.sku == "000317_" and item.article == "000017"
        assert item.recommended_quantity is None
        assert "history.comparableInStockPeriods" in item.missing_fields
        assert item.processing_status == "needs_review"
        assert item.estimated_cost is None
        assert result.summary.total_recommended_quantity is None
        assert item.explanation.generated_by == "SYSTEM"
        assert item.anomaly_analysis.source == "mock"
        assert item.anomaly_analysis.is_demo
        assert item.adjustments == []
        assert "access-control-allow-origin" not in response.headers


def test_auth_required_even_for_health_and_for_local_when_configured(payload):
    # A literal test-only token, never a real credential.
    with local_client(Settings(ai_service_token="test-only-token")) as client:
        assert client.get("/health").status_code == 401
        assert client.post("/v1/recommendations", json=payload).status_code == 401
        assert client.get("/health", headers={"Authorization": "Bearer wrong"}).status_code == 401
        assert client.get("/health", headers={"Authorization": "Bearer test-only-token"}).status_code == 200


def test_remote_client_cannot_use_local_bypass(local_settings):
    with TestClient(create_app(local_settings), client=("203.0.113.10", 80)) as client:
        assert client.get("/health", headers={"X-Forwarded-For": "127.0.0.1"}).status_code == 401


def test_request_byte_limit_with_and_without_content_length(payload):
    with local_client(Settings(ai_local_mode=True, ai_max_request_bytes=128)) as client:
        assert client.post("/v1/recommendations", json=payload).status_code == 413
        def chunks():
            yield b'{"products":'
            yield b" " * 200
        response = client.post("/v1/recommendations", content=chunks(), headers={"Content-Type": "application/json"})
        assert response.status_code == 413


def test_product_limit_before_schema_validation(payload):
    with local_client(Settings(ai_local_mode=True, ai_max_products=1)) as client:
        payload["products"].append({"code1C": "second"})
        result = client.post("/v1/recommendations", json=payload)
        assert result.status_code == 422
        assert result.json()["error"]["code"] == "PRODUCT_LIMIT_EXCEEDED"


@pytest.mark.parametrize("body,content_type,status", [
    ("{", "application/json", 400),
    ("{}", "application/json", 422),
    ("null", "application/json", 422),
    ("not excel", "application/vnd.ms-excel", 415),
])
def test_bad_requests(body, content_type, status, local_settings):
    with local_client(local_settings) as client:
        response = client.post("/v1/recommendations", content=body, headers={"Content-Type": content_type})
        assert response.status_code == status
        assert "error" in response.json()


def test_validation_does_not_echo_input(payload, local_settings):
    payload["requestId"] = "private-input-marker"
    with local_client(local_settings) as client:
        response = client.post("/v1/recommendations", json=payload)
        assert response.status_code == 422
        assert "private-input-marker" not in response.text


def test_snake_case_works_over_http_and_openapi_uses_existing_aliases(payload, local_settings):
    from app.schemas import RecommendationRequest
    snake = RecommendationRequest.model_validate(payload).model_dump(mode="json")
    with local_client(local_settings) as client:
        response = client.post("/v1/recommendations", json=snake)
        assert response.status_code == 200
        assert response.json()["recommendations"][0]["code1C"] == "000317_"
        schema = client.get("/openapi.json")
        assert schema.status_code == 200
        properties = schema.json()["components"]["schemas"]["RecommendationRequest"]["properties"]
        assert "requestId" in properties and "schemaVersion" in properties


def test_same_sku_multiple_dimensions_are_not_merged(payload, local_settings):
    from copy import deepcopy
    second = deepcopy(payload["products"][0])
    second["warehouseId"] = "second-warehouse"
    third = deepcopy(second)
    third["supplierId"] = "second-supplier"
    payload["products"].extend([second, third])
    # Explicit identities are necessary when multiple matches exist.
    for group in ("monthlySales", "monthlyStock"):
        payload[group][0].update(warehouseId="warehouse-demo", supplierId="supplier-demo")
    with local_client(local_settings) as client:
        response = client.post("/v1/recommendations", json=payload)
        assert response.status_code == 200, response.text
        result = RecommendationResponse.model_validate(response.json())
        assert len({i.key for i in result.recommendations}) == 3
        assert result.summary.total_recommended_quantity is None


def test_request_timeout(payload, monkeypatch):
    async def slow(*args):
        await asyncio.sleep(1)
    monkeypatch.setattr("app.routes.run_pipeline", slow)
    with local_client(Settings(ai_local_mode=True, ai_request_timeout_seconds=0.01)) as client:
        response = client.post("/v1/recommendations", json=payload)
        assert response.status_code == 504


def test_live_never_returns_mock(payload):
    settings = Settings(ai_mode="live", allow_external_ai=True, ai_service_token="test-only-token")
    with local_client(settings) as client:
        response = client.post("/v1/recommendations", json=payload,
                               headers={"Authorization": "Bearer test-only-token"})
        assert response.status_code == 503
        assert response.json()["error"]["code"] == "AI_NOT_CONFIGURED"


def test_real_stream_chunks_are_bounded(local_settings):
    from app.http_guard import HTTPGuard
    called, sent = [], []
    async def downstream(*args):
        called.append(True)
    chunks = iter([
        {"type": "http.request", "body": b"a" * 70, "more_body": True},
        {"type": "http.request", "body": b"b" * 70, "more_body": False},
    ])
    async def receive():
        return next(chunks)
    async def send(message):
        sent.append(message)
    settings = Settings(ai_local_mode=True, ai_max_request_bytes=100)
    asyncio.run(HTTPGuard(downstream, settings)(
        {"type": "http", "method": "POST", "path": "/v1/recommendations",
         "headers": [], "client": ("127.0.0.1", 80)}, receive, send,
    ))
    assert not called
    assert sent[0]["status"] == 413
