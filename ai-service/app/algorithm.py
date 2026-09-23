"""Small calendar-aware baseline; changes only a separate analytical history."""

from calendar import monthrange
from collections import defaultdict
from datetime import date, timedelta
from decimal import Decimal, ROUND_CEILING
from math import lcm
from statistics import mean

from .schemas import Adjustment, AnalyticalPeriod, Calculation, HistoryPeriod, Warning

ALGORITHM_VERSION = "calendar-baseline-0.1.0"
PIECE_UNITS = {"шт", "шт.", "pcs", "piece", "pieces", "unit", "units"}


def rounded_order(need, minimum, multiple, step):
    if need <= 0:
        return 0.0
    # Common integer scale also supports fractional pack sizes / unit steps.
    a, b = Decimal(str(multiple)), Decimal(str(step))
    scale = 10 ** max(0, -a.as_tuple().exponent, -b.as_tuple().exponent)
    increment = Decimal(lcm(int(a * scale), int(b * scale))) / scale
    target = max(Decimal(str(round(need, 10))), Decimal(str(minimum)))
    return float((target / increment).to_integral_value(rounding=ROUND_CEILING) * increment)


def series_rows(payload, product, rows):
    result = []
    for row in rows or []:
        if row.sku != product.sku:
            continue
        matches = [p for p in payload.products if p.sku == row.sku
                   and (row.warehouse_id is None or p.warehouse_id == row.warehouse_id)
                   and (row.supplier_id is None or p.supplier_id == row.supplier_id)]
        if len(matches) == 1 and matches[0].key == product.key:
            result.append(row)
    return result


def calculate_product(payload, product):
    warnings, missing, adjustments = [], [], []
    assumptions = [
        "Покрытие = срок поставки + период пересмотра; forecastHorizonMonths не добавляется повторно.",
        "Используются только полностью завершённые календарные месяцы до asOfDate.",
        "Отсутствующие месяцы не считаются нулевыми продажами.",
        "Дополнительный прирост применяется один раз поверх простого статистического тренда.",
    ]

    def warn(code, message, severity="WARNING"):
        if not any(w.code == code for w in warnings):
            warnings.append(Warning(code=code, message=message, severity=severity))

    def require(field):
        if field not in missing:
            missing.append(field)

    settings = payload.settings
    lead = product.lead_time_days if product.lead_time_days is not None else settings.lead_time_days
    review = product.review_period_days if product.review_period_days is not None else settings.review_period_days
    coverage = lead + review
    cutoff = payload.as_of_date + timedelta(days=lead)
    end = payload.as_of_date + timedelta(days=coverage)
    if coverage <= 0:
        require("settings.coverageDays")
    step = product.quantity_step
    if step is None and (product.unit or "").lower() in PIECE_UNITS:
        step = 1
        assumptions.append("Для штучной единицы допустимый шаг количества — 1.")
    multiple = product.order_multiple if product.order_multiple is not None else product.moq
    calculation = Calculation(
        forecast_horizon_months=settings.forecast_horizon_months,
        coverage_days=coverage, coverage_end=end, transit_cutoff=cutoff,
        minimum_order_quantity=product.minimum_order_quantity,
        order_multiple=multiple, moq=multiple, quantity_step=step,
    )
    month_number = payload.as_of_date.year * 12 + payload.as_of_date.month

    def closed(month):
        y, m = map(int, month.split("-"))
        index = y * 12 + m
        return month_number - settings.history_months <= index < month_number

    monthly = series_rows(payload, product, payload.monthly_sales)
    sales = series_rows(payload, product, payload.sales_history)
    orders = series_rows(payload, product, payload.transactions)
    source_details = sales if sales else orders
    grouped = defaultdict(list)
    for row in source_details:
        grouped[row.date.strftime("%Y-%m")].append(row)
    quantities, labels = {}, {}
    if monthly:
        for row in monthly:
            if closed(row.month):
                quantities[row.month] = row.quantity
                labels[row.month] = row
            elif row.month == payload.as_of_date.strftime("%Y-%m"):
                warn("PARTIAL_MONTH_EXCLUDED", "Текущий месяц исключён из истории.", "INFO")
    else:
        for month, rows in grouped.items():
            if not closed(month):
                continue
            y, m = map(int, month.split("-"))
            complete = len({r.date.day for r in rows}) == monthrange(y, m)[1]
            if not settings.sales_history_complete and not complete:
                require("settings.salesHistoryComplete")
                warn("HISTORY_COVERAGE_UNKNOWN", "Для разреженной истории не подтверждена полнота месяца.")
            quantities[month] = None if any(r.quantity is None for r in rows) else sum(r.quantity for r in rows)
    periods = []
    for month, quantity in sorted(quantities.items()):
        regular, excluded = quantity, 0
        label = labels.get(month)
        details = grouped.get(month, [])
        # Order-level business confirmation can accompany unlabelled sales rows.
        order_details = [r for r in orders if r.date.strftime("%Y-%m") == month]
        if not any(r.confirmed_one_off for r in details) and any(r.confirmed_one_off for r in order_details):
            details = order_details
        marked = [r for r in details if r.confirmed_one_off]
        # If both detail sources label the same month, do not guess their overlap.
        both_sources = (any(r.confirmed_one_off and r.date.strftime("%Y-%m") == month for r in sales)
                        and any(r.confirmed_one_off and r.date.strftime("%Y-%m") == month for r in orders))
        if quantity is None:
            warn("UNKNOWN_HISTORY_QUANTITY", "Месяцы с неизвестными продажами не подменены нулём и не используются.")
        elif quantity < 0:
            require("history.nonNegativeNetDemand")
            warn("NEGATIVE_NET_SALES", "Отрицательный месячный итог требует проверки возвратов.")
            regular = None
        elif label is not None and label.confirmed_one_off:
            if not (label.business_reason or "").strip():
                require("monthlySales.businessReason")
            else:
                excluded, regular = quantity, 0
                adjustments.append(Adjustment(row_id=f"month:{month}", original_quantity=quantity,
                    adjusted_quantity=0, applied=True, reason=label.business_reason))
        elif marked:
            reconcilable = (not both_sources and all(r.quantity is not None for r in details)
                            and abs(sum(r.quantity for r in details) - quantity) < 1e-8)
            if not reconcilable:
                require("history.oneOffReconciliation")
                warn("ONE_OFF_UNRECONCILED", "Разовый заказ не исключён: детализация не согласуется с месячным итогом.")
            elif any(not (r.business_reason or "").strip() or r.quantity < 0 for r in marked):
                require("history.oneOffBusinessReason")
            else:
                excluded = sum(r.quantity for r in marked)
                if excluded > quantity:
                    require("history.oneOffReconciliation")
                    excluded = 0
                else:
                    regular = quantity - excluded
                    for row in marked:
                        adjustments.append(Adjustment(row_id=row.row_id, original_quantity=row.quantity,
                            adjusted_quantity=0, applied=True, reason=row.business_reason))
        periods.append(AnalyticalPeriod(month=month, original_quantity=quantity,
                       regular_quantity=regular, estimated_regular_demand=regular,
                       excluded_one_off_quantity=excluded))
    if not periods or not any(p.regular_quantity is not None for p in periods):
        require("history.completedKnownPeriods")
    if periods:
        calculation.history_period = HistoryPeriod(**{"from": periods[0].month, "to": periods[-1].month})
        indices = [int(p.month[:4]) * 12 + int(p.month[5:]) for p in periods]
        if max(indices) - min(indices) + 1 != len(indices):
            warn("HISTORY_GAPS", "В истории есть пропущенные месяцы; они не приняты за нулевой спрос.")

    factors = {s.month: s.coefficient for s in payload.seasonality or []}

    def factor(month):
        value = factors.get(month)
        if value is None:
            warn("SEASONALITY_MISSING", "Для части периодов нет сезонных коэффициентов; используется 1 без оценки сезонности.")
            return 1.0
        return value

    availability = series_rows(payload, product, payload.availability)
    for period in periods:
        y, m = map(int, period.month.split("-"))
        first, last = date(y, m, 1), date(y, m, monthrange(y, m)[1])
        occupied, blocked = set(), 0
        for row in availability:
            if row.start_date > last or row.end_date < first:
                continue
            if row.available is not False and not (row.stockout_days is not None and row.stockout_days > 0):
                continue
            a, b = max(first, row.start_date), min(last, row.end_date)
            dates = {a + timedelta(days=i) for i in range((b - a).days + 1)}
            if dates & occupied:
                require("availability.nonOverlappingIntervals")
                continue
            occupied |= dates
            if row.available is False and (row.stockout_days is None or row.stockout_days == (row.end_date - row.start_date).days + 1):
                blocked += len(dates)
            elif row.start_date >= first and row.end_date <= last and row.available is not True:
                blocked += row.stockout_days or 0
            else:
                require("availability.monthlyStockoutDays")
                warn("STOCKOUT_AMBIGUOUS", "Нельзя распределить или подтвердить дни stockout; требуется проверка.")
        period.stockout_days = blocked

    comparable = []
    for period in periods:
        if period.regular_quantity is None or period.stockout_days:
            continue
        y, m = map(int, period.month.split("-"))
        seasonal = factor(m)
        if seasonal <= 0:
            require("seasonality.positiveHistoricalCoefficient")
            continue
        comparable.append(period.regular_quantity / monthrange(y, m)[1] / seasonal)
    for period in periods:
        if period.stockout_days and period.regular_quantity is not None:
            if not comparable:
                require("history.comparableInStockPeriods")
                warn("STOCKOUT_BASELINE_MISSING", "Нет сопоставимых периодов без stockout для оценки потерянного спроса.")
                continue
            seasonal = factor(int(period.month[5:]))
            compensation = mean(comparable) * period.stockout_days * seasonal
            period.estimated_regular_demand = round(period.regular_quantity + compensation, 10)
            calculation.stockout_compensation += compensation
            adjustments.append(Adjustment(row_id=f"stockout:{period.month}",
                original_quantity=period.regular_quantity, adjusted_quantity=period.estimated_regular_demand,
                applied=True, reason="Оценка потерянного спроса по подтверждённым дням stockout и сопоставимым месяцам."))
    calculation.stockout_compensation = round(calculation.stockout_compensation, 10)
    calculation.anomaly_excluded_quantity = sum(p.excluded_one_off_quantity for p in periods)
    calculation.analytical_history = periods
    rates, weighted_total, total_days = [], 0.0, 0
    for period in periods:
        if period.estimated_regular_demand is None:
            continue
        y, m = map(int, period.month.split("-"))
        seasonal = factor(m)
        if seasonal <= 0:
            require("seasonality.positiveHistoricalCoefficient")
            continue
        days = monthrange(y, m)[1]
        normalized = period.estimated_regular_demand / seasonal
        rates.append(normalized / days)
        weighted_total += normalized
        total_days += days
    if rates:
        daily = weighted_total / total_days
        trend = 1.0
        if len(rates) >= 3:
            half = len(rates) // 2
            early, recent = mean(rates[:half]), mean(rates[-half:])
            if early > 0:
                trend = min(1.25, max(0.75, recent / early))
            else:
                warn("TREND_BASE_ZERO", "Ранний спрос равен нулю; относительный тренд не оценён.")
        else:
            warn("SHORT_HISTORY", "Меньше трёх месяцев: тренд не оценивается, множитель 1.")
        growth = 1 + (product.additional_growth_rate if product.additional_growth_rate is not None else 0)
        adjusted_daily = daily * trend * growth
        calculation.base_monthly_demand = mean(p.original_quantity for p in periods if p.regular_quantity is not None)
        calculation.corrected_monthly_demand = mean(p.estimated_regular_demand for p in periods if p.estimated_regular_demand is not None)
        calculation.daily_demand = round(adjusted_daily, 10)
        calculation.trend_factor = round(trend, 10)
        calculation.additional_growth_factor = growth
        calculation.growth_factor = round(trend * growth, 10)
        future_factors = [factor((payload.as_of_date + timedelta(days=i)).month) for i in range(1, coverage + 1)]
        safety_factors = [factor((end + timedelta(days=i)).month) for i in range(1, settings.safety_stock_days + 1)]
        calculation.seasonality_factor = mean(future_factors) if future_factors else 1
        calculation.forecast_demand = round(adjusted_daily * sum(future_factors), 10)
        calculation.safety_stock = round(adjusted_daily * sum(safety_factors), 10)
        assumptions.append("Тренд: отношение средней интенсивности последней половины истории к первой, ограничено 0.75–1.25.")
        assumptions.append("История делится на её сезонные коэффициенты, прогноз умножается на будущие коэффициенты один раз.")

    stocks = series_rows(payload, product, payload.inventory)
    stock = stocks[0] if stocks else None
    if stock is None:
        require("inventory")
    else:
        if stock.stock_as_of_date != payload.as_of_date:
            require("inventory.stockAsOfDate")
            warn("STOCK_DATE_UNCONFIRMED", "Для MVP нужен остаток, актуальный на asOfDate.")
        free = stock.free_stock
        if free is None and stock.total_stock is not None and stock.reserved_stock is not None:
            free = stock.total_stock - stock.reserved_stock
            assumptions.append("Свободный остаток = totalStock − reservedStock.")
        if free is None:
            require("inventory.freeStock")
        calculation.free_stock = calculation.available_stock = free

    eligible = 0.0
    eligible_known = True
    shipments = series_rows(payload, product, payload.incoming_shipments)
    if settings.available_stock_policy == "FREE_ONLY":
        assumptions.append("FREE_ONLY: товар в пути не вычитается.")
    elif payload.incoming_shipments is None:
        if stock is None or stock.in_transit is None:
            require("incomingShipments")
            eligible_known = False
        elif stock.in_transit > 0:
            warn("TRANSIT_DATE_UNKNOWN", "У агрегированного товара в пути нет даты: он не вычтен.")
    else:
        for row in shipments:
            if row.expected_date is None:
                warn("TRANSIT_DATE_UNKNOWN", "Поставка с неизвестной датой не вычтена.")
            elif row.expected_date <= payload.as_of_date:
                warn("TRANSIT_OVERDUE", "Просроченная поставка не вычтена без подтверждения получения.")
            elif row.expected_date > cutoff:
                warn("TRANSIT_TOO_LATE", "Поставка после срока поставки не вычтена.")
            elif row.quantity is None:
                require("incomingShipments.quantity")
                eligible_known = False
            else:
                eligible += row.quantity
    calculation.in_transit = calculation.eligible_in_transit = round(eligible, 10) if eligible_known else None
    assumptions.append("Вычитаются только поставки после asOfDate и не позже asOfDate + leadTimeDays; агрегат inTransit повторно не добавляется.")
    if calculation.forecast_demand is not None and calculation.free_stock is not None and eligible_known:
        calculation.raw_requirement = round(max(0, calculation.forecast_demand + calculation.safety_stock
                                               - calculation.free_stock - eligible), 10)
    if calculation.raw_requirement is not None and calculation.raw_requirement > 0:
        if not product.unit:
            require("products.unit")
        if step is None:
            require("products.quantityStep")
        if product.minimum_order_quantity is None:
            require("products.minimumOrderQuantity")
        if multiple is None:
            require("products.orderMultiple")
    if not missing and calculation.raw_requirement is not None:
        calculation.rounded_requirement = rounded_order(calculation.raw_requirement,
            product.minimum_order_quantity, multiple, step)
    if missing:
        warn("MISSING_CRITICAL_DATA", "Расчёт заказа не завершён: отсутствуют или не согласованы критические данные.")
    return calculation, adjustments, warnings, assumptions, missing
