// Package forecast contains deterministic procurement arithmetic without I/O.
// Dates, source data and manager decisions are explicit inputs.
package forecast

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/anomaly"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
)

func Months(asOf time.Time, n int) []string {
	if n <= 0 {
		return []string{}
	}
	end := time.Date(asOf.Year(), asOf.Month(), 1, 0, 0, 0, 0, time.UTC)
	out := make([]string, n)
	for i := range n {
		out[i] = end.AddDate(0, i-n, 0).Format("2006-01")
	}
	return out
}

func ValidateConfig(cfg domain.RunConfig) error {
	if cfg.LeadTimeDays < 1 || cfg.LeadTimeDays > 365 || cfg.SafetyDays < 0 || cfg.SafetyDays > 365 {
		return errors.New("leadTimeDays 1–365; safetyDays 0–365")
	}
	return nil
}

func Calculate(ctx context.Context, p *domain.Product, asOf time.Time, seasonality map[int]float64, cfg domain.RunConfig, candidates []domain.Anomaly, managerDecision string) (domain.Item, error) {
	if err := ctx.Err(); err != nil {
		return domain.Item{}, err
	}
	if err := ValidateConfig(cfg); err != nil {
		return domain.Item{}, err
	}
	if p == nil || asOf.IsZero() {
		return domain.Item{}, errors.New("product and asOf are required")
	}
	item := domain.Item{Code1C: p.Code1C, Article: p.Article, Name: p.Name, Supplier: p.Supplier, UnitCost: p.UnitCost, Anomalies: append([]domain.Anomaly{}, candidates...), Warnings: append([]domain.Diagnostic{}, p.Warnings...), AnomalyDecision: managerDecision}
	warn := func(code, message string, blocking bool) {
		item.Warnings = append(item.Warnings, domain.Diagnostic{Code: code, Message: message, Code1C: p.Code1C, Blocking: blocking})
	}
	conflicts := anomaly.Reconcile(p, asOf)
	item.Warnings = append(item.Warnings, conflicts...)
	conflictMonths := map[string]bool{}
	for _, w := range conflicts {
		conflictMonths[w.Month] = true
	}
	clean := map[string]float64{}
	for m, v := range p.MonthlySales {
		if m < asOf.Format("2006-01") {
			clean[m] = math.Max(0, v)
		}
	}
	for i := range item.Anomalies {
		a := &item.Anomalies[i]
		// Recheck current sources; a stale candidate flag cannot authorize removal.
		a.SourceConsistent = a.SourceConsistent && !conflictMonths[a.Month]
		a.Excluded = a.SourceConsistent && managerDecision == "EXCLUDE"
		if a.Excluded {
			clean[a.Month] = math.Max(0, clean[a.Month]-a.Quantity)
		} else if managerDecision != "KEEP" {
			warn("UNCONFIRMED_ANOMALY", "Крупная операция требует подтверждения менеджера.", true)
		}
	}
	period := Months(asOf, 12)
	baseMonths := Months(asOf, 6)
	restored, stockouts := restoreStockouts(p, clean, period)
	profile := seasonalProfile(restored, seasonality)
	base, used, baseSeasonOK := baseDemand(clean, baseMonths, profile)
	restoredBase, _, _ := baseDemand(restored, baseMonths, profile)
	if len(used) == 0 {
		warn("MISSING_SALES_HISTORY", "Нет продаж за последние полные месяцы.", true)
	} else if len(used) < 6 {
		warn("INSUFFICIENT_SALES_HISTORY", "Для базового спроса доступны не все шесть полных месяцев; пропуски не заменяются нулями.", true)
	}
	if p.OrderMultiple <= 0 {
		warn("MISSING_ORDER_MULTIPLE", "Кратность поставки отсутствует или некорректна.", true)
	}
	if !p.PresentFields["freeStock"] {
		warn("MISSING_CURRENT_STOCK", "Свободный текущий остаток неизвестен.", true)
	}
	if !p.PresentFields["inTransit"] {
		warn("MISSING_IN_TRANSIT", "Количество товара в пути неизвестно.", true)
	}
	if p.FreeStock < 0 || p.InTransit < 0 {
		warn("NEGATIVE_CURRENT_STOCK", "Текущий остаток или товар в пути отрицателен.", true)
	}
	if p.UnitCost == nil || *p.UnitCost <= 0 {
		warn("MISSING_UNIT_COST", "Ориентировочная стоимость недоступна.", false)
	}
	growth, growthOK := growthFactor(restored, asOf)
	if !growthOK {
		warn("GROWTH_UNAVAILABLE", "Недостаточно сопоставимых данных для роста; коэффициент равен 1.", false)
	}
	season, source, seasonOK := seasonFactor(profile, asOf, cfg.LeadTimeDays)
	safetySeason, _, safetySeasonOK := seasonFactor(profile, asOf.AddDate(0, 0, cfg.LeadTimeDays), cfg.SafetyDays)
	if !baseSeasonOK || !seasonOK || !safetySeasonOK {
		warn("MISSING_SEASONALITY", "Для части базового периода или горизонта сезонность неизвестна; использован коэффициент 1.", false)
	}
	target := float64(cfg.LeadTimeDays) / 30
	// Only compensate the baseline's actual shortfall. The median may already
	// recover isolated zero months; adding their lost units again inflates demand.
	compensation := math.Max(0, restoredBase-base) * season * growth * target
	monthlyDemand := base * season * growth
	demand := monthlyDemand*target + compensation
	safety := restoredBase * safetySeason * growth * float64(cfg.SafetyDays) / 30
	additional := 0.0
	for _, field := range []struct {
		enabled bool
		key     string
		value   float64
	}{{cfg.IncludeShowcase, "showcaseStock", p.ShowcaseStock}, {cfg.IncludeTZStock, "tzStock", p.TZStock}, {cfg.IncludeRetailStock, "retailStock", p.RetailStock}} {
		if field.enabled {
			if !p.PresentFields[field.key] {
				warn("MISSING_ADDITIONAL_STOCK", "Выбранный дополнительный остаток неизвестен: "+field.key, true)
			}
			additional += field.value
			if field.value < 0 {
				warn("NEGATIVE_CURRENT_STOCK", "Выбранный дополнительный остаток отрицателен.", true)
			}
		}
	}
	available := p.FreeStock + p.InTransit + additional
	raw := math.Max(0, demand+safety-available)
	qty := 0.0
	if p.OrderMultiple > 0 && raw > 0 {
		qty = math.Ceil(raw/float64(p.OrderMultiple)) * float64(p.OrderMultiple)
	}
	item.RecommendedQuantity = qty
	item.FinalQuantity = qty
	item.Breakdown = &domain.Breakdown{PeriodStart: period[0], PeriodEnd: period[11], BaseMonths: used, BaseMonthlyDemand: base, SeasonFactor: season, SeasonalitySource: source, GrowthFactor: growth, TargetMonths: target, StockoutCompensation: compensation, StockoutPeriods: stockouts, ForecastDemand: demand, SafetyStock: safety, SafetySeasonFactor: safetySeason, FreeStock: p.FreeStock, InTransit: p.InTransit, AdditionalStock: additional, Available: available, RawNeed: raw, OrderMultiple: p.OrderMultiple}
	item.Decision = "NO_BUY"
	if qty > 0 {
		item.Decision = "BUY"
	}
	for _, w := range item.Warnings {
		if w.Blocking {
			item.Decision = "REVIEW"
			break
		}
	}
	item.Urgency = "LOW"
	if qty > 0 || item.Decision == "REVIEW" {
		item.Urgency = "MEDIUM"
		if p.FreeStock+additional < demand || p.FreeStock+additional <= 0 {
			item.Urgency = "HIGH"
		}
	}
	SetCost(&item)
	return item, nil
}

func growthFactor(sales map[string]float64, asOf time.Time) (float64, bool) {
	recent, previous := 0.0, 0.0
	for _, m := range Months(asOf, 3) {
		t, _ := time.Parse("2006-01", m)
		a, okA := sales[m]
		b, okB := sales[t.AddDate(-1, 0, 0).Format("2006-01")]
		if !okA || !okB {
			return 1, false
		}
		recent += a
		previous += b
	}
	if previous <= 0 {
		return 1, false
	}
	return math.Max(.5, math.Min(1.5, recent/previous)), true
}

type seasonalityProfile struct {
	factors [13]float64
	sources [13]string
}

func seasonalProfile(sales map[string]float64, fallback map[int]float64) seasonalityProfile {
	sums := map[int]float64{}
	counts := map[int]int{}
	keys := make([]string, 0, len(sales))
	for m := range sales {
		keys = append(keys, m)
	}
	sort.Strings(keys)
	for _, m := range keys {
		t, err := time.Parse("2006-01", m)
		if err != nil {
			continue
		}
		v := sales[m]
		sums[int(t.Month())] += v
		counts[int(t.Month())]++
	}
	// Give each calendar month equal weight; incomplete years otherwise bias
	// the annual mean. Require two observations of every month for SKU seasonality.
	annualMean := 0.0
	completeHistory := true
	for m := 1; m <= 12; m++ {
		if counts[m] < 2 {
			completeHistory = false
		}
		if counts[m] > 0 {
			annualMean += sums[m] / float64(counts[m]) / 12
		}
	}
	profile := seasonalityProfile{}
	for m := 1; m <= 12; m++ {
		profile.factors[m], profile.sources[m] = 1, "neutral"
		if completeHistory && annualMean > 0 {
			profile.factors[m] = math.Max(.25, math.Min(3, sums[m]/float64(counts[m])/annualMean))
			profile.sources[m] = "sku"
		} else if v, ok := fallback[m]; ok && v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v) {
			profile.factors[m], profile.sources[m] = v, "global"
		}
	}
	return profile
}

func baseDemand(sales map[string]float64, months []string, profile seasonalityProfile) (float64, []string, bool) {
	values, used := []float64{}, []string{}
	complete := true
	for _, m := range months {
		if value, ok := sales[m]; ok {
			date, _ := time.Parse("2006-01", m)
			values = append(values, value/profile.factors[date.Month()])
			used = append(used, m)
			if profile.sources[date.Month()] == "neutral" {
				complete = false
			}
		}
	}
	return anomaly.Quantile(values, .5), used, complete
}

func seasonFactor(profile seasonalityProfile, asOf time.Time, days int) (float64, string, bool) {
	if days <= 0 {
		return 1, "neutral", true
	}
	weighted := 0.0
	sources := map[string]bool{}
	complete := true
	for i := range days {
		month := int(asOf.AddDate(0, 0, i).Month())
		source := profile.sources[month]
		sources[source] = true
		if source == "neutral" {
			complete = false
		}
		weighted += profile.factors[month]
	}
	source := "mixed"
	if len(sources) == 1 {
		for s := range sources {
			source = s
		}
	}
	return weighted / float64(days), source, complete
}

func restoreStockouts(p *domain.Product, sales map[string]float64, period []string) (map[string]float64, []string) {
	recovered := make(map[string]float64, len(sales))
	for m, value := range sales {
		recovered[m] = value
	}
	periods := []string{}
	for _, m := range period {
		stock, exists := p.MonthlyStock[m]
		actual, hasSale := sales[m]
		if !exists || !hasSale || p.MonthlySales[m] < 0 || (stock != nil && *stock != 0) {
			continue
		}
		t, _ := time.Parse("2006-01", m)
		prior, after := false, false
		neighbours := []float64{}
		for offset := -2; offset <= 2; offset++ {
			if offset == 0 {
				continue
			}
			key := t.AddDate(0, offset, 0).Format("2006-01")
			v, ok := sales[key]
			if ok && v > 0 {
				neighbours = append(neighbours, v)
				if offset < 0 {
					prior = true
				} else {
					after = true
				}
			}
			if s := p.MonthlyStock[key]; offset < 0 && s != nil && *s > 0 {
				prior = true
			}
		}
		// Require activity on both sides: leading/trailing empty cells alone do
		// not justify classifying a new or discontinued item as stocked out.
		if !prior || !after {
			continue
		}
		restored := anomaly.Quantile(neighbours, .5)
		if v, ok := sales[t.AddDate(-1, 0, 0).Format("2006-01")]; ok && v > 0 {
			restored = v
		}
		if restored > actual {
			recovered[m] = restored
			periods = append(periods, m)
		}
	}
	return recovered, periods
}

func SetCost(item *domain.Item) {
	item.EstimatedCost = nil
	if item.UnitCost != nil && *item.UnitCost > 0 {
		v := math.Round(item.FinalQuantity**item.UnitCost*100) / 100
		item.EstimatedCost = &v
	}
}

func Summarize(items []domain.Item) domain.Summary {
	s := domain.Summary{}
	for _, i := range items {
		switch i.Decision {
		case "BUY":
			s.Buy++
		case "NO_BUY":
			s.NoBuy++
		case "REVIEW":
			s.Review++
		}
		s.TotalUnits += i.FinalQuantity
		if i.EstimatedCost != nil {
			s.EstimatedCost += *i.EstimatedCost
		} else if i.FinalQuantity > 0 {
			s.UnpricedItems++
		}
		if i.Approved && i.ApprovedQuantity != nil {
			s.ApprovedItems++
		}
	}
	s.EstimatedCost = math.Round(s.EstimatedCost*100) / 100
	return s
}
