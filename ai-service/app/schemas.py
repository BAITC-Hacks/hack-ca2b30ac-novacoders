"""Wire models: preserve v1 camelCase; accept Python snake_case on input."""

from datetime import date as Date, datetime
from typing import Annotated, Literal
from uuid import UUID

from pydantic import (
    AliasChoices, BaseModel, ConfigDict, Field, StrictStr, field_validator, model_validator,
)
from pydantic.alias_generators import to_camel

Identifier = Annotated[StrictStr, Field(min_length=1, max_length=256)]
Number = Annotated[float, Field(allow_inf_nan=False)]
NonNegative = Annotated[Number, Field(ge=0)]
Positive = Annotated[Number, Field(gt=0)]
Month = Annotated[str, Field(pattern=r"^\d{4}-(0[1-9]|1[0-2])$")]


class WireModel(BaseModel):
    model_config = ConfigDict(
        alias_generator=to_camel, populate_by_name=True, extra="forbid",
        allow_inf_nan=False, hide_input_in_errors=True,
    )


class Identity(WireModel):
    sku: Identifier = Field(
        validation_alias=AliasChoices("code1C", "sku"), serialization_alias="code1C"
    )
    warehouse_id: Identifier | None = None
    supplier_id: Identifier | None = None

    @property
    def key(self):
        return self.sku, self.warehouse_id, self.supplier_id


class Product(Identity):
    article: StrictStr | None = None
    name: str | None = None
    supplier: str | None = None
    category: StrictStr | None = None
    unit: str | None = None
    # Existing v1 moq meant order multiple. Do not silently change its semantics.
    moq: Positive | None = None
    minimum_order_quantity: NonNegative | None = None
    order_multiple: Positive | None = None
    quantity_step: Positive | None = None
    unit_cost: NonNegative | None = None
    additional_growth_rate: Annotated[Number, Field(ge=-1)] | None = None
    lead_time_days: Annotated[int, Field(ge=0, le=3650)] | None = None
    review_period_days: Annotated[int, Field(ge=0, le=3650)] | None = None

    @model_validator(mode="after")
    def consistent_multiple(self):
        if self.moq is not None and self.order_multiple is not None:
            if self.moq != self.order_multiple:
                raise ValueError("moq and orderMultiple conflict")
        return self


class BusinessLabels(WireModel):
    confirmed_one_off: bool = False
    business_reason: str | None = None


class MonthlyRecord(Identity, BusinessLabels):
    month: Month
    quantity: Number | None

    @field_validator("month")
    @classmethod
    def calendar_month(cls, value):
        Date.fromisoformat(f"{value}-01")
        return value


class SaleRow(Identity, BusinessLabels):
    row_id: Identifier
    date: Date
    quantity: Number | None


class Transaction(Identity, BusinessLabels):
    row_id: Identifier = Field(
        validation_alias=AliasChoices("transactionId", "transaction_id", "rowId", "row_id"),
        serialization_alias="transactionId",
    )
    date: Date
    quantity: Number | None
    warehouse: str | None = None  # Legacy display name; never used as an ID.
    client_id: Identifier | None = None  # Must be anonymized by Go.


class Inventory(Identity):
    stock_as_of_date: Date | None = None
    total_stock: Number | None = None
    reserved_stock: NonNegative | None = None
    free_stock: Number | None = None
    in_transit: NonNegative | None = None


class Shipment(Identity):
    row_id: Identifier
    quantity: NonNegative | None
    expected_date: Date | None = None


class Availability(Identity):
    start_date: Date
    end_date: Date
    available: bool | None = None
    stockout_days: Annotated[int, Field(ge=0)] | None = None

    @model_validator(mode="after")
    def valid_interval(self):
        days = (self.end_date - self.start_date).days + 1
        if days <= 0 or (self.stockout_days is not None and self.stockout_days > days):
            raise ValueError("Invalid availability interval or stockoutDays")
        return self


class Seasonality(WireModel):
    month: Annotated[int, Field(ge=1, le=12)]
    coefficient: NonNegative | None


class CalculationSettings(WireModel):
    sales_history_complete: bool = False
    history_months: Annotated[int, Field(gt=0, le=120)] = 12
    forecast_horizon_months: Annotated[int, Field(gt=0, le=60)] = 2
    lead_time_days: Annotated[int, Field(ge=0, le=3650)] = 30
    review_period_days: Annotated[int, Field(ge=0, le=3650)] = 30
    safety_stock_days: Annotated[int, Field(ge=0, le=3650)] = 14
    exclude_partial_month: bool = True
    available_stock_policy: Literal["FREE_PLUS_IN_TRANSIT", "FREE_ONLY"] = "FREE_PLUS_IN_TRANSIT"
    anomaly_review_enabled: bool = True
    explanation_mode: Literal["IMPORTANT_ONLY", "ALL", "NONE"] = "IMPORTANT_ONLY"
    max_ai_explanations: Annotated[int, Field(ge=0, le=10000)] = Field(
        default=50, alias="maxAIExplanations"
    )


class SourceFile(WireModel):
    file_name: str
    row_count: Annotated[int, Field(ge=0)]


class RecommendationRequest(WireModel):
    is_demo: bool = Field(default=False, alias="is_demo")
    schema_version: Literal["1.0", "1.1"]
    request_id: UUID
    as_of_date: Date
    currency: Annotated[str, Field(pattern=r"^[A-Z]{3}$")] | None = None
    settings: CalculationSettings = Field(default_factory=CalculationSettings)
    source_meta: dict[str, SourceFile] | None = None
    products: Annotated[list[Product], Field(min_length=1)]
    monthly_sales: list[MonthlyRecord] = Field(default_factory=list)
    monthly_stock: list[MonthlyRecord] = Field(default_factory=list)
    sales_history: list[SaleRow] = Field(default_factory=list)
    transactions: list[Transaction] | None = None
    inventory: list[Inventory] = Field(default_factory=list)
    incoming_shipments: list[Shipment] | None = None
    seasonality: list[Seasonality] | None = None
    availability: list[Availability] | None = None

    @model_validator(mode="after")
    def validate_relations(self):
        keys = [p.key for p in self.products]
        if len(keys) != len(set(keys)):
            raise ValueError("Duplicate product identity")
        by_sku = {}
        for product in self.products:
            by_sku.setdefault(product.sku, []).append(product)
        for collection in (
            self.monthly_sales, self.monthly_stock, self.sales_history,
            self.transactions, self.inventory, self.incoming_shipments, self.availability,
        ):
            seen = set()
            for record in collection or []:
                matches = [p.key for p in by_sku.get(record.sku, [])
                           if (record.warehouse_id is None or record.warehouse_id == p.warehouse_id)
                           and (record.supplier_id is None or record.supplier_id == p.supplier_id)]
                if len(matches) != 1:
                    raise ValueError("Row must reference exactly one product by sku/warehouse/supplier")
                key = matches[0]
                if isinstance(record, MonthlyRecord):
                    if record.month > self.as_of_date.strftime("%Y-%m"):
                        raise ValueError("Historical month cannot be after asOfDate")
                    identity = (key, record.month)
                elif isinstance(record, Availability):
                    identity = (key, record.start_date, record.end_date)
                else:
                    identity = (key, getattr(record, "row_id", None))
                if identity in seen:
                    raise ValueError("Duplicate row identity in collection")
                seen.add(identity)
                if isinstance(record, (SaleRow, Transaction)) and record.date > self.as_of_date:
                    raise ValueError("Sales history cannot be after asOfDate")
                if isinstance(record, Inventory) and record.stock_as_of_date:
                    if record.stock_as_of_date > self.as_of_date:
                        raise ValueError("Stock snapshot cannot be after asOfDate")
        months = [s.month for s in self.seasonality or []]
        if len(months) != len(set(months)):
            raise ValueError("Duplicate seasonality month")
        return self


class Warning(WireModel):
    code: str
    severity: Literal["INFO", "WARNING", "ERROR"] = "WARNING"
    message: str


class HistoryPeriod(WireModel):
    from_month: Month = Field(alias="from")
    to_month: Month = Field(alias="to")


class AnalyticalPeriod(WireModel):
    month: Month
    original_quantity: Number | None
    regular_quantity: NonNegative | None
    estimated_regular_demand: NonNegative | None
    excluded_one_off_quantity: NonNegative = 0
    stockout_days: int = 0


class Calculation(WireModel):
    history_period: HistoryPeriod | None = None
    base_monthly_demand: NonNegative | None = None
    corrected_monthly_demand: NonNegative | None = None
    anomaly_excluded_quantity: NonNegative = 0
    stockout_compensation: NonNegative = 0
    growth_factor: NonNegative = 1
    seasonality_factor: NonNegative = 1
    forecast_horizon_months: int
    forecast_demand: NonNegative | None = None
    safety_stock: NonNegative | None = None
    free_stock: Number | None = None
    in_transit: NonNegative | None = None
    available_stock: Number | None = None
    raw_requirement: NonNegative | None = None
    moq: Positive | None = None
    minimum_order_quantity: NonNegative | None = None
    order_multiple: Positive | None = None
    rounded_requirement: NonNegative | None = None
    daily_demand: NonNegative | None = None
    trend_factor: NonNegative = 1
    additional_growth_factor: NonNegative = 1
    coverage_days: int = 0
    coverage_end: Date | None = None
    transit_cutoff: Date | None = None
    eligible_in_transit: NonNegative | None = None
    quantity_step: Positive | None = None
    analytical_history: list[AnalyticalPeriod] = Field(default_factory=list)


class Anomaly(WireModel):
    row_id: Identifier = Field(
        validation_alias=AliasChoices("transactionId", "transaction_id", "rowId", "row_id"),
        serialization_alias="transactionId",
    )
    date: Date | None = None
    quantity: Number | None = None
    median_transaction_quantity: Number | None = None
    deviation_ratio: NonNegative | None = None
    confidence: Annotated[Number, Field(ge=0, le=1)] | None = None
    legacy_verdict: Literal["ONE_OFF", "RECURRING", "UNCERTAIN", "NOT_EVALUATED"] = Field(
        alias="nvidiaVerdict", validation_alias=AliasChoices("nvidiaVerdict", "nvidia_verdict", "legacy_verdict")
    )
    system_decision: Literal["EXCLUDED", "INCLUDED", "PENDING_REVIEW"]
    reason: str


class AnalysisProposal(WireModel):
    evidence_row_ids: list[str]
    anomaly_type: str
    proposed_action: str
    explanation: str
    missing_evidence: list[str]
    status: Literal["proposed", "needs_review"]


class AnomalyAnalysis(WireModel):
    status: Literal["NOT_EVALUATED", "PENDING_REVIEW", "COMPLETED", "FAILED"]
    source: Literal["mock", "live"] | None = None
    is_demo: bool = Field(default=False, alias="is_demo")
    proposals: list[AnalysisProposal] = Field(default_factory=list)
    error_code: str | None = None
    candidates: list[Anomaly] = Field(default_factory=list)


class Adjustment(WireModel):
    row_id: Identifier
    original_quantity: Number | None
    adjusted_quantity: Number | None
    applied: bool
    reason: str


class Explanation(WireModel):
    short: str
    details: list[str]
    generated_by: Literal["MOCK", "OPENAI", "SYSTEM"]


class Recommendation(Identity):
    article: str | None
    name: str | None
    supplier: str | None
    category: str | None
    unit: str | None
    action: Literal["BUY", "NO_BUY", "REVIEW"]
    urgency: Literal["HIGH", "MEDIUM", "LOW", "NONE"]
    confidence: Annotated[Number, Field(ge=0, le=1)] | None
    requires_manual_review: bool
    forecast: NonNegative | None
    recommended_quantity: NonNegative | None
    missing_fields: list[str] = Field(default_factory=list)
    processing_status: Literal["computed", "needs_review"] = "computed"
    calculation_source: Literal["python"] = "python"
    estimated_unit_cost: NonNegative | None
    estimated_cost: NonNegative | None
    calculation: Calculation
    anomaly_analysis: AnomalyAnalysis
    adjustments: list[Adjustment]
    explanation: Explanation
    warnings: list[Warning]
    assumptions: list[str]
    source: Literal["mock", "live", "fallback"]
    is_demo: bool = Field(alias="is_demo")

    @model_validator(mode="after")
    def consistent_result(self):
        if self.recommended_quantity != self.calculation.rounded_requirement:
            raise ValueError("Quantity must equal roundedRequirement")
        if self.forecast != self.calculation.forecast_demand:
            raise ValueError("Forecast must equal forecastDemand")
        expected_cost = (None if self.estimated_unit_cost is None or self.recommended_quantity is None else
                         round(self.estimated_unit_cost * self.recommended_quantity, 2))
        if self.estimated_cost != expected_cost:
            raise ValueError("Inconsistent estimated cost")
        if self.source == "mock" and not self.is_demo:
            raise ValueError("Mock results must be marked as demo")
        return self


class Summary(WireModel):
    total_products: int
    buy_count: int
    no_buy_count: int
    review_count: int
    total_recommended_quantity: NonNegative | None
    estimated_order_cost: NonNegative | None


class LegacyProviderStatus(WireModel):
    status: Literal["USED", "SKIPPED", "FALLBACK", "FAILED"]
    reviewed_anomalies: Annotated[int, Field(ge=0)]


class OpenAIStatus(WireModel):
    status: Literal["USED", "SKIPPED", "FALLBACK", "FAILED"]
    generated_explanations: Annotated[int, Field(ge=0)]


class Providers(WireModel):
    retired_provider: LegacyProviderStatus = Field(alias="nvidia")
    openai: OpenAIStatus


class Versions(WireModel):
    algorithm: str
    schema_version: str
    retired_prompt: str = Field(alias="nvidiaPrompt")
    openai_prompt: str


class RecommendationResponse(WireModel):
    schema_version: Literal["1.0", "1.1"]
    request_id: UUID
    run_id: str
    status: Literal["COMPLETED", "COMPLETED_WITH_WARNINGS", "FAILED"]
    generated_at: datetime
    processing_time_ms: Annotated[int, Field(ge=0)]
    summary: Summary
    providers: Providers
    recommendations: list[Recommendation]
    warnings: list[Warning]
    assumptions: list[str]
    source: Literal["mock", "live", "fallback"]
    is_demo: bool = Field(alias="is_demo")
    versions: Versions

    @model_validator(mode="after")
    def consistent_summary(self):
        items = self.recommendations
        summary = self.summary
        if summary.total_products != len(items):
            raise ValueError("Summary count does not match recommendations")
        for action, count in (("BUY", summary.buy_count), ("NO_BUY", summary.no_buy_count),
                              ("REVIEW", summary.review_count)):
            if count != sum(item.action == action for item in items):
                raise ValueError("Summary action count mismatch")
        expected_quantity = (None if any(i.recommended_quantity is None for i in items)
                             else round(sum(i.recommended_quantity for i in items), 10))
        if summary.total_recommended_quantity != expected_quantity:
            raise ValueError("Summary quantity mismatch")
        expected_cost = (None if any(i.estimated_cost is None for i in items)
                         else round(sum(i.estimated_cost for i in items), 2))
        if summary.estimated_order_cost != expected_cost:
            raise ValueError("Summary cost mismatch")
        if self.source == "mock" and not self.is_demo:
            raise ValueError("Inconsistent demo source")
        if any(i.source != self.source or i.is_demo != self.is_demo for i in items):
            raise ValueError("Item source mismatch")
        return self
