import json
import re
from pathlib import Path
from typing import Annotated

from pydantic import Field, ValidationError, model_validator

from ..analysis.schemas import InternalModel
from ..providers.clients import ProviderError

PROMPT_VERSION = "explanation-1.0.0"
PROMPT = (Path(__file__).resolve().parents[1] / "prompts/explanation_v1.txt").read_text()


class Sentence(InternalModel):
    text: Annotated[str, Field(min_length=1, max_length=350)]
    reason_ids: Annotated[list[str], Field(min_length=1, max_length=8)]


class ModelExplanation(InternalModel):
    sentences: Annotated[list[Sentence], Field(min_length=1, max_length=3)]

    @model_validator(mode="after")
    def qualitative_only(self):
        for sentence in self.sentences:
            text = sentence.text.strip()
            if (not text or re.search(r"[\d=<>%\n]", text) or not re.search(r"[А-Яа-яЁё]", text)
                    or len(re.findall(r"[.!?]+(?:\s|$)", text)) > 1):
                raise ValueError("Explanation must be a short qualitative Russian sentence without numbers")
        return self


def explanation_context(calculation, adjustments, warnings, assumptions, missing):
    # Never pass raw model proposals, client identifiers, sale rows or business free text.
    c = calculation
    reasons = {}
    if c.forecast_demand is not None:
        reasons["coverage_demand"] = "Прогноз Python покрывает срок поставки и период пересмотра."
    if c.safety_stock:
        reasons["safety_buffer"] = "Расчёт включает страховой запас по заданному периоду."
    if c.available_stock is not None and c.available_stock > 0:
        reasons["stock_deducted"] = "Свободный остаток уменьшает потребность в закупке."
    if c.eligible_in_transit:
        reasons["transit_deducted"] = "Подходящие по срокам поставки уменьшают потребность."
    if c.rounded_requirement is not None and c.raw_requirement != c.rounded_requirement:
        reasons["rounding"] = "Количество увеличено согласно минимуму и допустимой кратности/шагу."
    if any(a.applied for a in adjustments):
        reasons["business_adjustment"] = "Python применил разрешённые бизнес-правила к аналитической копии."
    if missing:
        reasons["incomplete_data"] = "Критических данных недостаточно; количество заказа неизвестно."
    return {
        "calculation": c.model_dump(mode="json", include={
            "forecast_demand", "safety_stock", "available_stock", "eligible_in_transit",
            "raw_requirement", "rounded_requirement", "coverage_days", "minimum_order_quantity",
            "order_multiple", "quantity_step", "anomaly_excluded_quantity", "stockout_compensation",
        }),
        "confirmed_reasons": reasons,
        "warnings": [{"code": w.code, "message": w.message} for w in warnings],
        "assumptions_not_facts": assumptions,
        "missing_fields": missing,
    }


class ExplanationService:
    def __init__(self, clients):
        self.clients = clients

    async def explain(self, context):
        try:
            raw = await self.clients.parse("explanation", PROMPT,
                json.dumps(context, ensure_ascii=False), ModelExplanation)
            output = ModelExplanation.model_validate(raw.model_dump() if isinstance(raw, ModelExplanation) else raw)
            reasons = context["confirmed_reasons"]
            if any(not set(s.reason_ids) <= reasons.keys() for s in output.sentences):
                raise ProviderError("invalid_response")
            return [s.text.strip() for s in output.sentences]
        except (ValidationError, ValueError):
            raise ProviderError("invalid_response") from None
