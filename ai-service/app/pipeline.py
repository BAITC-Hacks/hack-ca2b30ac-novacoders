import asyncio

from datetime import datetime, timezone
from time import perf_counter
from uuid import uuid4

from .algorithm import ALGORITHM_VERSION, calculate_product
from .analysis.context import ContextError, build_context
from .analysis.providers import MockAnalysisProvider, PROMPT_VERSION as ANALYSIS_PROMPT_VERSION
from .explanation.service import explanation_context, PROMPT_VERSION as EXPLANATION_PROMPT_VERSION
from .providers.clients import ProviderError
from .analysis.service import AnalysisService
from .compat import mock_provider_metadata, mock_versions
from .schemas import (
    AnalysisProposal, AnomalyAnalysis, Explanation, Recommendation,
    RecommendationRequest, RecommendationResponse, Summary, Warning,
)
from .settings import Settings


class LiveConfigurationError(RuntimeError):
    pass


async def run_pipeline(payload: RecommendationRequest, settings: Settings,
                       analysis_service=None, explanation_service=None) -> RecommendationResponse:
    started = perf_counter()
    live = settings.ai_mode == "live"
    if live and (not settings.allow_external_ai or not settings.openai_api_key.get_secret_value()
                 or analysis_service is None or explanation_service is None):
        raise LiveConfigurationError()
    if not live:
        analysis_service = analysis_service or AnalysisService(MockAnalysisProvider(), settings)
    items = []
    used_ai = failed_ai = explanation_calls = generated_explanations = 0
    used_analysis_prompt = used_explanation_prompt = False

    async def bounded(call):
        # Reserve time for assembling the deterministic answer; no additional retries.
        remaining = settings.ai_request_timeout_seconds - (perf_counter() - started) - 0.5
        if remaining <= 0:
            raise ProviderError("timeout")
        async with asyncio.timeout(min(settings.ai_provider_timeout_seconds, remaining)):
            return await call()

    for product in payload.products:
        analysis = AnomalyAnalysis(status="NOT_EVALUATED")
        stage_warnings = []
        analysis_source = "deterministic"
        if payload.settings.anomaly_review_enabled:
            try:
                context = build_context(payload, product, settings)
                if live:
                    used_analysis_prompt = True
                result = await bounded(lambda: analysis_service.analyze_series(context))
                analysis = AnomalyAnalysis(
                    status="FAILED" if result.status == "failed" else
                           "PENDING_REVIEW" if result.proposals else "COMPLETED",
                    source=result.source, is_demo=result.is_demo, error_code=result.error_code,
                    proposals=[AnalysisProposal(**p.model_dump(include={
                        "evidence_row_ids", "anomaly_type", "proposed_action",
                        "explanation", "missing_evidence", "status",
                    })) for p in result.proposals],
                )
                if result.status == "completed":
                    analysis_source = "openai" if live else "mock"
                    used_ai += int(live)
            except (ContextError, ProviderError, TimeoutError) as exc:
                code = exc.code if isinstance(exc, ProviderError) else "timeout" if isinstance(exc, TimeoutError) else "insufficient_context"
                analysis = AnomalyAnalysis(status="FAILED", source="live" if live else "mock",
                                           is_demo=not live, error_code=code)
            if analysis.status == "FAILED":
                failed_ai += int(live)
                stage_warnings.append(Warning(code="ANALYSIS_FALLBACK",
                    message="Анализ не выполнен: " + (analysis.error_code or "failed") +
                            ". Использован deterministic расчёт; отсутствие аномалий не подтверждено."))
            elif live and analysis.proposals:
                stage_warnings.append(Warning(code="ANALYSIS_NEEDS_REVIEW",
                    message="Предложения OpenAI требуют проверки и не разрешают корректировки автоматически."))
            if not live:
                stage_warnings.append(Warning(code="MOCK_ANALYSIS", severity="INFO",
                    message="Анализ аномалий демонстрационный; OpenAI не вызывался. Предложения не меняют расчёт."))
        # Only existing business labels/rules may change the analytical copy.
        # Validated AI proposals are deliberately NOT mapped to business confirmation.
        calculation, adjustments, warnings, assumptions, missing = calculate_product(payload, product)
        warnings.extend(stage_warnings)
        warnings.append(Warning(code="PYTHON_BASELINE", severity="INFO",
            message="Источник расчёта: deterministic (Python); количество не определяется текстом модели."))
        if payload.is_demo:
            warnings.append(Warning(code="SYNTHETIC_INPUT", severity="INFO",
                                    message="Вход помечен как синтетический пример."))
        quantity = calculation.rounded_requirement
        review = bool(missing or any(w.severity in {"WARNING", "ERROR"} for w in warnings))
        action = "REVIEW" if review else "BUY" if quantity and quantity > 0 else "NO_BUY"
        if quantity is None:
            explanation = Explanation(short="Количество заказа неизвестно: требуется проверка данных.",
                details=["Недостающие или противоречивые данные: " + ", ".join(missing)], generated_by="SYSTEM")
        else:
            c = calculation
            explanation = Explanation(
                short=f"Рекомендуемое количество: {quantity:g} {product.unit or ''}.".strip(),
                details=[
                    f"Покрытие {c.coverage_days} дней: прогноз {c.forecast_demand:g}; страховой запас {c.safety_stock:g}.",
                    f"max(0, {c.forecast_demand:g} + {c.safety_stock:g} − {c.available_stock:g} − {c.eligible_in_transit:g}) = {c.raw_requirement:g}.",
                    f"После применения минимума {c.minimum_order_quantity}, кратности {c.order_multiple} и шага {c.quantity_step}: {quantity:g}.",
                    "Корректировки относятся только к аналитической оценке; исходные продажи сохранены.",
                ], generated_by="SYSTEM")
        explanation_source = "template"
        important = review or action == "BUY" or bool(adjustments)
        requested = (payload.settings.explanation_mode == "ALL" or
                     payload.settings.explanation_mode == "IMPORTANT_ONLY" and important)
        if live and requested and explanation_calls < payload.settings.max_ai_explanations:
            explanation_calls += 1
            used_explanation_prompt = True
            context = explanation_context(calculation, adjustments, warnings, assumptions, missing)
            try:
                sentences = await bounded(lambda: explanation_service.explain(context))
                explanation.details.extend(sentences)
                explanation.generated_by = "OPENAI"
                explanation_source = "openai"
                generated_explanations += 1
                used_ai += 1
            except (ProviderError, TimeoutError) as exc:
                failed_ai += 1
                code = exc.code if isinstance(exc, ProviderError) else "timeout"
                warnings.append(Warning(code="EXPLANATION_FALLBACK", severity="INFO",
                    message="Объяснение OpenAI недоступно: " + code + ". Сохранён Python-шаблон (template)."))
        elif live and requested:
            warnings.append(Warning(code="EXPLANATION_LIMIT", severity="INFO",
                message="Лимит maxAIExplanations: использован template."))
        warnings.append(Warning(code="RESULT_SOURCES", severity="INFO",
            message=f"analysis={analysis_source}; calculation=deterministic; explanation={explanation_source}"))
        # Mandatory warnings come from code and cannot be removed by model prose.
        explanation.details.extend("Предупреждение: " + w.message for w in warnings
                                   if w.severity != "INFO" or w.code in {"MOCK_ANALYSIS", "SYNTHETIC_INPUT", "EXPLANATION_FALLBACK"})
        cost = None if quantity is None or product.unit_cost is None else round(quantity * product.unit_cost, 2)
        items.append(Recommendation(
            sku=product.sku, warehouse_id=product.warehouse_id, supplier_id=product.supplier_id,
            article=product.article, name=product.name, supplier=product.supplier,
            category=product.category, unit=product.unit, action=action, urgency="NONE",
            confidence=None, requires_manual_review=review,
            forecast=calculation.forecast_demand, recommended_quantity=quantity,
            missing_fields=missing, processing_status="needs_review" if review else "computed",
            estimated_unit_cost=product.unit_cost, estimated_cost=cost, calculation=calculation,
            anomaly_analysis=analysis, adjustments=adjustments, explanation=explanation,
            warnings=warnings, assumptions=assumptions, source="fallback", is_demo=payload.is_demo,
        ))
    # Preserve the existing uniform response/item source contract for mixed batches.
    source = "live" if live and used_ai and not failed_ai else "fallback"
    for item in items:
        item.source = source
    providers = mock_provider_metadata()
    providers.openai.status = "FALLBACK" if failed_ai and used_ai else "FAILED" if failed_ai else "USED" if used_ai else "SKIPPED"
    providers.openai.generated_explanations = generated_explanations
    versions = mock_versions(ALGORITHM_VERSION, payload.schema_version)
    prompt_versions = []
    if used_analysis_prompt:
        prompt_versions.append(ANALYSIS_PROMPT_VERSION)
    if used_explanation_prompt:
        prompt_versions.append(EXPLANATION_PROMPT_VERSION)
    versions.openai_prompt = "+".join(prompt_versions) or "not-used"
    all_warnings = {w.code: w for item in items for w in item.warnings}
    return RecommendationResponse(
        schema_version=payload.schema_version, request_id=payload.request_id,
        run_id=f"run-{uuid4()}", status="COMPLETED_WITH_WARNINGS" if all_warnings else "COMPLETED",
        generated_at=datetime.now(timezone.utc), processing_time_ms=int((perf_counter() - started) * 1000),
        summary=Summary(total_products=len(items),
            buy_count=sum(i.action == "BUY" for i in items),
            no_buy_count=sum(i.action == "NO_BUY" for i in items),
            review_count=sum(i.action == "REVIEW" for i in items),
            total_recommended_quantity=None if any(i.recommended_quantity is None for i in items)
                else round(sum(i.recommended_quantity for i in items), 10),
            estimated_order_cost=None if any(i.estimated_cost is None for i in items)
                else round(sum(i.estimated_cost for i in items), 2)),
        providers=providers, recommendations=items,
        warnings=list(all_warnings.values()), assumptions=list(dict.fromkeys(a for i in items for a in i.assumptions)),
        source=source, is_demo=payload.is_demo,
        versions=versions,
    )
