"""Project one series, bound its context, calculate statistics without imputing zeros."""

from calendar import monthrange
from datetime import date
from hashlib import sha256
from statistics import mean, median

from ..schemas import Product, RecommendationRequest
from ..settings import Settings
from .schemas import AnalysisContext, EvidenceRow, SeasonalFactor, Statistics


class ContextError(ValueError):
    pass


def statistics_for(rows: tuple[EvidenceRow, ...]) -> Statistics:
    # Never add order details to sales aggregates: use one primary representation.
    primary = [r for r in rows if r.source == "sales_history"]
    if not primary:
        primary = [r for r in rows if r.source == "monthly_sales"]
    if not primary:
        primary = [r for r in rows if r.source == "order"]
    values = [r.quantity for r in primary if r.quantity is not None]
    mid = median(values) if values else None
    maximum = max(values) if values else None
    return Statistics(
        known_count=len(values), unknown_count=sum(r.quantity is None for r in primary),
        zero_count=values.count(0), median_quantity=mid,
        mean_quantity=mean(values) if values else None, maximum_quantity=maximum,
        max_to_positive_median_ratio=(maximum / mid if mid is not None and mid > 0 else None),
    )


def build_context(payload: RecommendationRequest, product: Product, settings: Settings) -> AnalysisContext:
    if product.key not in {p.key for p in payload.products}:
        raise ContextError("unknown_series")
    product = next(p for p in payload.products if p.key == product.key)

    def belongs(record):
        matches = [p.key for p in payload.products if p.sku == record.sku
                   and (record.warehouse_id is None or p.warehouse_id == record.warehouse_id)
                   and (record.supplier_id is None or p.supplier_id == record.supplier_id)]
        if len(matches) != 1:
            raise ContextError("ambiguous_series")
        return matches[0] == product.key

    rows = []
    history = [r for r in payload.sales_history if belongs(r)]
    monthly = [r for r in payload.monthly_sales if belongs(r)]
    granularity = "dated_sales_rows" if history else "monthly" if monthly else "order_rows"
    for row in history:
        rows.append(EvidenceRow(
            row_id=f"sales:{row.row_id}", source_row_id=row.row_id, source="sales_history",
            start_date=row.date, end_date=row.date, quantity=row.quantity,
            client_id=None, stockout_days=None,
        ))
    if not history:
        for row in monthly:
            year, month = map(int, row.month.split("-"))
            rows.append(EvidenceRow(
                row_id=f"month:{row.month}", source_row_id=None, source="monthly_sales",
                start_date=date(year, month, 1),
                end_date=min(date(year, month, monthrange(year, month)[1]), payload.as_of_date),
                quantity=row.quantity, client_id=None, stockout_days=None,
            ))
    for row in payload.transactions or []:
        if belongs(row):
            # Raw client identifiers/names never leave this process.
            client = ("client_" + sha256(row.client_id.encode()).hexdigest()[:16]
                      if row.client_id is not None else None)
            rows.append(EvidenceRow(
                row_id=f"order:{row.row_id}", source_row_id=row.row_id, source="order",
                start_date=row.date, end_date=row.date, quantity=row.quantity,
                client_id=client, stockout_days=None,
            ))
    for row in payload.availability or []:
        if not belongs(row) or row.end_date > payload.as_of_date:
            continue
        if row.available is False or (row.stockout_days is not None and row.stockout_days > 0):
            rows.append(EvidenceRow(
                row_id=f"stockout:{row.start_date}:{row.end_date}", source_row_id=None,
                source="confirmed_stockout", start_date=row.start_date, end_date=row.end_date,
                quantity=None, client_id=None, stockout_days=row.stockout_days,
            ))
    if not rows:
        raise ContextError("insufficient_context")
    total = len(rows)
    ordered = sorted(rows, key=lambda r: (r.end_date, r.row_id), reverse=True)
    window = ordered[:settings.ai_analysis_max_rows]
    primary_source = "sales_history" if history else "monthly_sales" if monthly else "order"
    primary = [r for r in ordered if r.source == primary_source]
    # A recent auxiliary row must not silently change the statistical series.
    if primary and not any(r.source == primary_source for r in window):
        window[-1] = primary[0]
    retained = tuple(window)
    context = AnalysisContext(
        sku=product.sku, warehouse_id=product.warehouse_id, supplier_id=product.supplier_id,
        as_of_date=payload.as_of_date, unit=product.unit, granularity=granularity,
        rows=retained,
        seasonality=tuple(SeasonalFactor(month=s.month, coefficient=s.coefficient)
                          for s in payload.seasonality or []),
        statistics=statistics_for(retained), total_available_rows=total, truncated=total > len(retained),
        limitations=(
            "Only supplied rows are observed; missing dates are not zero sales.",
            "Order details are evidence, not extra quantities to add to sales history.",
            "Monthly sales do not identify customers; month-end stock does not establish stockout duration.",
        ) + (("Context limited to recent rows with at least one primary sales row when available; statistics describe this window only.",)
             if total > len(retained) else ()),
    )
    if len(context.model_dump_json().encode()) > settings.ai_analysis_max_context_bytes:
        raise ContextError("context_too_large")
    return context
