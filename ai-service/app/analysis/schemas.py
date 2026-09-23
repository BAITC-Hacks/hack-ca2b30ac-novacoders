from datetime import date
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from ..schemas import Identifier, Number


class InternalModel(BaseModel):
    model_config = ConfigDict(extra="forbid", frozen=True, allow_inf_nan=False,
                              hide_input_in_errors=True)


EvidenceId = Annotated[str, Field(min_length=1, max_length=300)]


class EvidenceRow(InternalModel):
    row_id: EvidenceId
    source_row_id: Identifier | None
    source: Literal["sales_history", "monthly_sales", "order", "confirmed_stockout"]
    start_date: date
    end_date: date
    quantity: Number | None
    client_id: Identifier | None
    stockout_days: int | None


class Statistics(InternalModel):
    known_count: int
    unknown_count: int
    zero_count: int
    median_quantity: Number | None
    mean_quantity: Number | None
    maximum_quantity: Number | None
    max_to_positive_median_ratio: Number | None


class SeasonalFactor(InternalModel):
    month: int
    coefficient: Number | None


class AnalysisContext(InternalModel):
    sku: Identifier
    warehouse_id: Identifier | None
    supplier_id: Identifier | None
    as_of_date: date
    unit: str | None
    granularity: Literal["dated_sales_rows", "monthly", "order_rows"]
    rows: tuple[EvidenceRow, ...]
    seasonality: tuple[SeasonalFactor, ...]
    statistics: Statistics
    total_available_rows: int
    truncated: bool
    limitations: tuple[str, ...]

    @model_validator(mode="after")
    def unique_evidence(self):
        ids = [r.row_id for r in self.rows]
        if not ids or len(ids) != len(set(ids)):
            raise ValueError("Context must contain unique evidence rows")
        for row in self.rows:
            if row.start_date > row.end_date or row.end_date > self.as_of_date:
                raise ValueError("Invalid evidence date range")
        return self


AnomalyType = Literal[
    "demand_spike", "possible_one_off_order", "possible_stockout",
    "possible_data_error", "possible_regime_change",
]
ProposedAction = Literal[
    "flag_for_review", "investigate_data", "consider_excluding_from_regular_demand",
    "investigate_stockout", "review_baseline",
]
Text = Annotated[str, Field(min_length=1, max_length=1500)]


class ModelProposal(InternalModel):
    evidence_row_ids: Annotated[list[EvidenceId], Field(min_length=1, max_length=50)]
    anomaly_type: AnomalyType
    proposed_action: ProposedAction
    explanation: Text
    missing_evidence: Annotated[list[Text], Field(max_length=20)]
    # Explicit citations are validated against the cited rows, not the whole dataset.
    referenced_client_ids: Annotated[list[Identifier], Field(max_length=50)]
    referenced_dates: Annotated[list[str], Field(max_length=50)]


class ModelAnalysis(InternalModel):
    proposals: Annotated[list[ModelProposal], Field(max_length=30)]
    no_anomalies_reason: Text | None

    @model_validator(mode="after")
    def meaningful_result(self):
        if not self.proposals and not (self.no_anomalies_reason or "").strip():
            raise ValueError("Empty analysis is not a no-anomalies result")
        if self.proposals and self.no_anomalies_reason is not None:
            raise ValueError("Conflicting analysis outcome")
        for proposal in self.proposals:
            if not proposal.explanation.strip():
                raise ValueError("Blank explanation")
        return self


class Proposal(ModelProposal):
    status: Literal["proposed", "needs_review"] = "needs_review"


class AnalysisResult(InternalModel):
    status: Literal["completed", "failed"]
    proposals: tuple[Proposal, ...] = ()
    no_anomalies_reason: str | None = None
    error_code: str | None = None
    source: Literal["mock", "live"]
    is_demo: bool
    prompt_version: str
    limitations: tuple[str, ...] = ()
