import asyncio
from copy import deepcopy
import json

import httpx
import pytest
from fastapi.testclient import TestClient

from app.analysis.context import ContextError, build_context
from app.analysis.providers import MockAnalysisProvider
from app.analysis.service import AnalysisService
from app.main import create_app
from app.providers.clients import ProviderError
from app.schemas import RecommendationRequest
from app.settings import Settings


@pytest.fixture
def context(payload, local_settings):
    request = RecommendationRequest.model_validate(payload)
    return build_context(request, request.products[0], local_settings)


def proposal(context, **changes):
    row = next(r for r in context.rows if r.source == "sales_history")
    value = dict(evidence_row_ids=[row.row_id], anomaly_type="demand_spike",
                 proposed_action="flag_for_review", explanation="Требуется проверить наблюдение.",
                 missing_evidence=[], referenced_client_ids=[], referenced_dates=[])
    value.update(changes)
    return {"proposals": [value], "no_anomalies_reason": None}


class StubProvider:
    source = "mock"

    def __init__(self, output):
        self.output = output
        self.calls = 0

    async def analyze(self, context):
        self.calls += 1
        if isinstance(self.output, Exception):
            raise self.output
        return self.output


def analyze(output, context, settings):
    return asyncio.run(AnalysisService(StubProvider(output), settings).analyze_series(context))


def test_success_and_app_assigned_status(context, local_settings):
    before = context.model_dump()
    result = analyze(proposal(context), context, local_settings)
    assert result.status == "completed" and result.error_code is None
    assert result.is_demo and result.source == "mock"
    assert result.proposals[0].status == "needs_review"
    assert context.model_dump() == before


@pytest.mark.parametrize("changes,code", [
    ({"evidence_row_ids": ["unknown-row"]}, "unknown_evidence"),
    ({"anomaly_type": "made_up_type"}, "invalid_response"),
    ({"proposed_action": "delete_dataset"}, "invalid_response"),
    ({"proposed_action": "review_baseline"}, "invalid_action"),
    ({"status": "accepted"}, "invalid_response"),
    ({"client_id": "invented"}, "invalid_response"),
    ({"date": "2030-01-01"}, "invalid_response"),
    ({"referenced_dates": ["2030-01-01"]}, "unknown_date"),
    ({"referenced_client_ids": ["client_invented"]}, "unknown_client"),
    ({"explanation": "Продажа произошла 2030-01-01."}, "unknown_date"),
    ({"explanation": "Покупатель client_invented."}, "unknown_client"),
    ({"explanation": "См. row[not-in-context]."}, "unknown_evidence"),
    ({"explanation": "   "}, "invalid_response"),
])
def test_invalid_proposals_are_errors_not_no_anomalies(context, local_settings, changes, code):
    result = analyze(proposal(context, **changes), context, local_settings)
    assert result.status == "failed" and result.error_code == code
    assert result.proposals == () and result.no_anomalies_reason is None


@pytest.mark.parametrize("output", [None, {}, "", [], {"proposals": [], "no_anomalies_reason": None}])
def test_empty_or_invalid_output_is_failure(context, local_settings, output):
    result = analyze(output, context, local_settings)
    assert result.status == "failed" and result.error_code == "invalid_response"


def test_explicit_no_anomalies_is_distinct_from_error(context, local_settings):
    result = analyze({"proposals": [], "no_anomalies_reason": "В переданных строках оснований для замечаний нет."},
                     context, local_settings)
    assert result.status == "completed" and result.proposals == ()
    assert result.no_anomalies_reason


@pytest.mark.parametrize("code", ["refused", "timeout", "access_denied", "incomplete", "empty_response"])
def test_provider_failure_preserved(context, local_settings, code):
    result = analyze(ProviderError(code), context, local_settings)
    assert result.status == "failed" and result.error_code == code
    assert not result.no_anomalies_reason


def test_one_off_needs_customer_evidence_or_explicit_gap(context, local_settings):
    value = proposal(context, anomaly_type="possible_one_off_order",
                     proposed_action="consider_excluding_from_regular_demand")
    assert analyze(value, context, local_settings).error_code == "missing_customer_evidence"
    value["proposals"][0]["missing_evidence"] = ["Нет детализации покупок по клиентам."]
    assert analyze(value, context, local_settings).proposals[0].status == "needs_review"


def test_mock_provider_is_explicit_and_does_not_mutate(context, local_settings):
    before = context.model_dump()
    result = asyncio.run(AnalysisService(MockAnalysisProvider(), local_settings).analyze_series(context))
    assert result.status == "completed" and result.is_demo
    assert result.source == "mock"
    assert result.proposals[0].status == "needs_review"
    assert context.model_dump() == before


def test_context_privacy_nulls_and_statistics(payload, local_settings):
    payload["products"][0]["name"] = "PRIVATE PRODUCT NAME"
    payload["transactions"][0]["clientId"] = "PRIVATE CLIENT ID"
    request = RecommendationRequest.model_validate(payload)
    before = request.model_dump()
    context = build_context(request, request.products[0], local_settings)
    serialized = context.model_dump_json()
    assert "PRIVATE" not in serialized
    assert "unit_cost" not in serialized and "file_name" not in serialized
    assert context.statistics.known_count == 1 and context.statistics.unknown_count == 1
    assert context.statistics.mean_quantity == 30
    assert context.statistics.zero_count == 0
    assert any(r.source == "order" and r.client_id.startswith("client_") for r in context.rows)
    assert request.model_dump() == before


def test_stock_snapshot_does_not_invent_stockout(payload, local_settings):
    payload["availability"] = None
    payload["monthlyStock"][0]["quantity"] = 0
    request = RecommendationRequest.model_validate(payload)
    context = build_context(request, request.products[0], local_settings)
    assert not any(row.source == "confirmed_stockout" for row in context.rows)


def test_monthly_only_rows_have_explicit_granularity_and_stable_ids(payload, local_settings):
    payload["salesHistory"] = []
    payload["transactions"] = None
    request = RecommendationRequest.model_validate(payload)
    context = build_context(request, request.products[0], local_settings)
    assert context.granularity == "monthly"
    monthly = next(r for r in context.rows if r.source == "monthly_sales")
    assert monthly.row_id == "month:2026-08" and monthly.source_row_id is None
    assert monthly.client_id is None


def test_series_isolation(payload, local_settings):
    other = deepcopy(payload["products"][0])
    other["warehouseId"] = "other-warehouse"
    payload["products"].append(other)
    for name in ("monthlySales", "monthlyStock"):
        payload[name][0].update(warehouseId="warehouse-demo", supplierId="supplier-demo")
    other_row = deepcopy(payload["salesHistory"][0])
    other_row.update(warehouseId="other-warehouse", rowId="other-row", quantity=9999)
    payload["salesHistory"].append(other_row)
    request = RecommendationRequest.model_validate(payload)
    context = build_context(request, request.products[0], local_settings)
    assert "other-row" not in context.model_dump_json()
    assert context.statistics.maximum_quantity == 30


def test_context_limits_are_explicit(payload, local_settings):
    request = RecommendationRequest.model_validate(payload)
    bounded = Settings(ai_local_mode=True, ai_analysis_max_rows=2)
    context = build_context(request, request.products[0], bounded)
    assert len(context.rows) == 2 and context.truncated
    assert context.total_available_rows > len(context.rows)
    assert context.statistics.known_count + context.statistics.unknown_count > 0
    with pytest.raises(ContextError, match="context_too_large"):
        build_context(request, request.products[0], Settings(ai_local_mode=True, ai_analysis_max_context_bytes=100))


def test_oversized_context_never_reaches_provider(context):
    provider = StubProvider(None)
    service = AnalysisService(provider, Settings(ai_local_mode=True, ai_analysis_max_rows=1))
    result = asyncio.run(service.analyze_series(context))
    assert result.error_code == "context_too_large" and provider.calls == 0


def test_lifecycle_owns_mock_analysis_service_without_sdk(context, local_settings, monkeypatch):
    def forbidden(*args, **kwargs):
        raise AssertionError("No SDK creation")
    monkeypatch.setattr("app.providers.clients.AsyncOpenAI", forbidden)
    with TestClient(create_app(local_settings), client=("127.0.0.1", 1)) as client:
        service = client.app.state.analysis_service
        result = client.portal.call(service.analyze_series, context)
        assert result.status == "completed" and result.is_demo
        assert client.app.state.ai_clients._client is None


def test_actual_sdk_analysis_schema_and_lifecycle(context, monkeypatch):
    payloads = []
    async def send(self, request, **kwargs):
        payloads.append(json.loads(request.content))
        wire = {
            "id": "resp_test", "object": "response", "created_at": 1,
            "model": "gpt-6-luna", "status": "completed", "error": None, "incomplete_details": None,
            "output": [{"id": "msg_test", "type": "message", "role": "assistant", "status": "completed",
                        "content": [{"type": "output_text", "text": json.dumps(proposal(context)), "annotations": []}]}],
        }
        return httpx.Response(200, json=wire, request=request)
    monkeypatch.setattr(httpx.AsyncClient, "send", send)
    settings = Settings(ai_mode="live", allow_external_ai=True, ai_service_token="test-only-token",
                        openai_api_key="test-only-not-a-key")
    with TestClient(create_app(settings), client=("127.0.0.1", 1)) as client:
        shared = client.app.state.ai_clients._client
        service = client.app.state.analysis_service
        for _ in range(2):
            result = client.portal.call(service.analyze_series, context)
            assert result.status == "completed" and not result.is_demo
            assert result.proposals[0].status == "needs_review"
        assert client.app.state.ai_clients._client is shared
    assert shared.is_closed()
    assert len(payloads) == 2
    schema = payloads[0]["text"]["format"]["schema"]
    assert schema["additionalProperties"] is False
    assert "status" not in schema["$defs"]["ModelProposal"]["properties"]
    assert json.loads(payloads[0]["input"][0]["content"])["sku"] == context.sku
    assert payloads[0]["store"] is False
