from pathlib import Path
from typing import Protocol

from ..providers.clients import ProviderClients
from .schemas import AnalysisContext, ModelAnalysis, ModelProposal

PROMPT_VERSION = "analysis-1.0.0"
PROMPT = (Path(__file__).resolve().parents[1] / "prompts/analysis_v1.txt").read_text()


class AnalysisProvider(Protocol):
    source: str

    async def analyze(self, context: AnalysisContext) -> ModelAnalysis: ...


class OpenAIAnalysisProvider:
    source = "live"

    def __init__(self, client: ProviderClients):
        self.client = client

    async def analyze(self, context: AnalysisContext) -> ModelAnalysis:
        return await self.client.parse("analysis", PROMPT, context.model_dump_json(), ModelAnalysis)


class MockAnalysisProvider:
    source = "mock"

    async def analyze(self, context: AnalysisContext) -> ModelAnalysis:
        # Explicit demo proposal, not a claim that a real anomaly has been detected.
        row = context.rows[0]
        return ModelAnalysis(proposals=[ModelProposal(
            evidence_row_ids=[row.row_id], anomaly_type="possible_data_error",
            proposed_action="investigate_data",
            explanation="Демонстрационное предложение проверить строку; реальный анализ не выполнялся.",
            missing_evidence=["Необходим реальный анализ и проверка исходных данных."],
            referenced_client_ids=[], referenced_dates=[],
        )], no_anomalies_reason=None)
