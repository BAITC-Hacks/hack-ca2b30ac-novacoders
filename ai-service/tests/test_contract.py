import asyncio
from copy import deepcopy
from pathlib import Path

import pytest
from pydantic import ValidationError

from app.pipeline import run_pipeline
from app.schemas import RecommendationRequest, RecommendationResponse

ROOT = Path(__file__).resolve().parents[1]


def test_examples_validate_with_api_models(mvp_payload):
    request = RecommendationRequest.model_validate(mvp_payload)
    response = RecommendationResponse.model_validate_json((ROOT / "contracts/response.example.json").read_text())
    assert request.request_id == response.request_id
    assert response.recommendations[0].key == request.products[0].key


def test_stored_response_matches_current_pipeline(mvp_payload, local_settings):
    result = asyncio.run(run_pipeline(RecommendationRequest.model_validate(mvp_payload), local_settings))
    stored = RecommendationResponse.model_validate_json((ROOT / "contracts/response.example.json").read_text())
    volatile = {"run_id", "generated_at", "processing_time_ms"}
    assert result.model_dump(exclude=volatile) == stored.model_dump(exclude=volatile)


def test_snake_case_and_unknown_values(payload):
    model = RecommendationRequest.model_validate(payload)
    snake = model.model_dump(mode="json")
    restored = RecommendationRequest.model_validate(snake)
    assert restored == model
    assert restored.sales_history[1].quantity is None
    assert restored.monthly_stock[0].quantity is None
    assert restored.products[0].sku == "000317_"
    assert restored.products[0].minimum_order_quantity == 10
    assert restored.products[0].order_multiple == 5


def test_legacy_v1_shape_and_optional_fields():
    model = RecommendationRequest.model_validate({
        "schemaVersion": "1.0", "requestId": "93a40813-12dd-4aef-829d-4803251f6814",
        "asOfDate": "2026-09-22", "products": [{"code1C": "0001_", "moq": 5}],
        "transactions": [{"code1C": "0001_", "transactionId": "s1", "date": "2026-09-10", "quantity": -2}],
    })
    assert model.products[0].minimum_order_quantity is None
    assert model.products[0].order_multiple is None
    assert model.transactions[0].quantity == -2
    assert model.transactions[0].row_id == "s1"
    assert model.incoming_shipments is None


@pytest.mark.parametrize("field", ["products", "salesHistory", "monthlySales", "inventory", "transactions"])
def test_duplicates_rejected(payload, field):
    payload[field].append(deepcopy(payload[field][0]))
    with pytest.raises(ValidationError):
        RecommendationRequest.model_validate(payload)


def test_ambiguous_legacy_reference_rejected(payload):
    product = deepcopy(payload["products"][0])
    product["warehouseId"] = "other"
    payload["products"].append(product)
    with pytest.raises(ValidationError, match="exactly one product"):
        RecommendationRequest.model_validate(payload)


def test_missing_reference_rejected(payload):
    payload["inventory"][0]["code1C"] = "missing"
    with pytest.raises(ValidationError):
        RecommendationRequest.model_validate(payload)


@pytest.mark.parametrize("value", [317, 317.0, True])
def test_numeric_sku_rejected(payload, value):
    payload["products"][0]["code1C"] = value
    with pytest.raises(ValidationError):
        RecommendationRequest.model_validate(payload)


def test_invalid_dates_and_intervals(payload):
    payload["availability"][0]["stockoutDays"] = 50
    with pytest.raises(ValidationError):
        RecommendationRequest.model_validate(payload)
    payload["availability"] = None
    payload["salesHistory"][0]["date"] = "2026-10-01"
    with pytest.raises(ValidationError):
        RecommendationRequest.model_validate(payload)


@pytest.mark.parametrize("month", ["0000-01", "2026-13", "2026-10"])
def test_invalid_or_future_month_rejected(payload, month):
    payload["monthlySales"][0]["month"] = month
    with pytest.raises(ValidationError):
        RecommendationRequest.model_validate(payload)


def test_conflicting_moq_is_not_silently_reinterpreted(payload):
    payload["products"][0]["moq"] = 7
    with pytest.raises(ValidationError):
        RecommendationRequest.model_validate(payload)


def test_pipeline_preserves_original_input(payload, local_settings):
    request = RecommendationRequest.model_validate(payload)
    before = request.model_dump()
    asyncio.run(run_pipeline(request, local_settings))
    assert request.model_dump() == before


def test_response_rejects_inconsistent_numbers(payload, local_settings):
    result = asyncio.run(run_pipeline(RecommendationRequest.model_validate(payload), local_settings)).model_dump()
    result["recommendations"][0]["recommended_quantity"] = 25
    with pytest.raises(ValidationError):
        RecommendationResponse.model_validate(result)
