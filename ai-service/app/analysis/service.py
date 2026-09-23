import logging
import re

from pydantic import ValidationError

from ..providers.clients import ProviderError
from ..settings import Settings
from .context import statistics_for
from .providers import AnalysisProvider, PROMPT_VERSION
from .schemas import AnalysisContext, AnalysisResult, ModelAnalysis, Proposal

logger = logging.getLogger("ai_service.analysis")
ALLOWED_ACTIONS = {
    "demand_spike": {"flag_for_review"},
    "possible_one_off_order": {"flag_for_review", "consider_excluding_from_regular_demand"},
    "possible_stockout": {"investigate_stockout"},
    "possible_data_error": {"investigate_data"},
    "possible_regime_change": {"review_baseline"},
}


class AnalysisError(ValueError):
    pass


def validate_text_references(text, rows):
    dates = {str(d) for row in rows for d in (row.start_date, row.end_date)}
    clients = {row.client_id for row in rows if row.client_id is not None}
    ids = {row.row_id for row in rows}
    if not set(re.findall(r"\b\d{4}-\d{2}-\d{2}\b", text)) <= dates:
        raise AnalysisError("unknown_date")
    if not set(re.findall(r"\bclient_[\w-]+", text)) <= clients:
        raise AnalysisError("unknown_client")
    if not set(re.findall(r"row\[([^\]]+)\]", text)) <= ids:
        raise AnalysisError("unknown_evidence")


def validate_proposals(output: ModelAnalysis, context: AnalysisContext) -> tuple[Proposal, ...]:
    evidence = {row.row_id: row for row in context.rows}
    proposals = []
    if output.no_anomalies_reason:
        validate_text_references(output.no_anomalies_reason, context.rows)
    for item in output.proposals:
        if len(item.evidence_row_ids) != len(set(item.evidence_row_ids)):
            raise AnalysisError("duplicate_evidence")
        if any(row_id not in evidence for row_id in item.evidence_row_ids):
            raise AnalysisError("unknown_evidence")
        if item.proposed_action not in ALLOWED_ACTIONS[item.anomaly_type]:
            raise AnalysisError("invalid_action")
        cited = [evidence[row_id] for row_id in item.evidence_row_ids]
        dates = {str(d) for row in cited for d in (row.start_date, row.end_date)}
        clients = {row.client_id for row in cited if row.client_id is not None}
        text = " ".join([item.explanation, *item.missing_evidence])
        validate_text_references(text, cited)
        if not set(item.referenced_dates) <= dates:
            raise AnalysisError("unknown_date")
        if not set(item.referenced_client_ids) <= clients:
            raise AnalysisError("unknown_client")
        if item.anomaly_type == "possible_one_off_order":
            has_customer_order = any(r.source == "order" and r.client_id for r in cited)
            if not has_customer_order and not item.missing_evidence:
                raise AnalysisError("missing_customer_evidence")
        if item.anomaly_type == "possible_stockout":
            if not any(r.source == "confirmed_stockout" for r in cited) and not item.missing_evidence:
                raise AnalysisError("missing_stockout_evidence")
        proposals.append(Proposal(**item.model_dump(), status="needs_review"))
    return tuple(proposals)


class AnalysisService:
    def __init__(self, provider: AnalysisProvider, settings: Settings):
        self.provider = provider
        self.settings = settings

    async def analyze_series(self, context: AnalysisContext) -> AnalysisResult:
        source = self.provider.source
        common = dict(source=source, is_demo=source == "mock", prompt_version=PROMPT_VERSION,
                      limitations=context.limitations)
        try:
            # Validate callers too, not only the HTTP context builder.
            context = AnalysisContext.model_validate(context.model_dump())
            if len(context.rows) > self.settings.ai_analysis_max_rows:
                raise AnalysisError("context_too_large")
            if len(context.model_dump_json().encode()) > self.settings.ai_analysis_max_context_bytes:
                raise AnalysisError("context_too_large")
            if context.statistics != statistics_for(context.rows):
                raise AnalysisError("invalid_statistics")
            raw = await self.provider.analyze(context)
            output = ModelAnalysis.model_validate(raw.model_dump() if isinstance(raw, ModelAnalysis) else raw)
            proposals = validate_proposals(output, context)
            logger.info("analysis_completed source=%s proposals=%d", source, len(proposals))
            return AnalysisResult(status="completed", proposals=proposals,
                                  no_anomalies_reason=output.no_anomalies_reason, **common)
        except ProviderError as exc:
            code = exc.code
        except AnalysisError as exc:
            code = str(exc)  # Only fixed local error codes, never provider exception text.
        except ValidationError:
            code = "invalid_response"
        except TimeoutError:
            code = "timeout"
        logger.warning("analysis_failed source=%s code=%s", source, code)
        return AnalysisResult(status="failed", error_code=code, **common)
