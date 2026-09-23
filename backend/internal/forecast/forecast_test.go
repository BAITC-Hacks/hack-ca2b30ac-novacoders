package forecast

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
)

func fixture() (*domain.Product, domain.RunConfig) {
	p := &domain.Product{Code1C: "030200128_", Supplier: domain.Supplier, OrderMultiple: 10, MonthlySales: map[string]float64{}, MonthlyStock: map[string]*float64{}, PresentFields: map[string]bool{"freeStock": true, "inTransit": true}}
	for _, m := range Months(domain.DatasetDate(), 15) {
		p.MonthlySales[m] = 100
	}
	return p, domain.RunConfig{Supplier: domain.Supplier, LeadTimeDays: 30}
}

func calc(t *testing.T, p *domain.Product, cfg domain.RunConfig) domain.Item {
	t.Helper()
	item, err := Calculate(context.Background(), p, domain.DatasetDate(), map[int]float64{9: 1, 10: 1}, cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestInTransitReducesOrder(t *testing.T) {
	p, cfg := fixture()
	before := calc(t, p, cfg)
	p.InTransit = 40
	after := calc(t, p, cfg)
	if before.RecommendedQuantity != 100 || after.RecommendedQuantity != 60 {
		t.Fatalf("before=%v after=%v", before.RecommendedQuantity, after.RecommendedQuantity)
	}
}

func TestSufficientStockNoBuy(t *testing.T) {
	p, cfg := fixture()
	p.FreeStock = 200
	if got := calc(t, p, cfg); got.Decision != "NO_BUY" || got.RecommendedQuantity != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestStockoutIncreasesForecast(t *testing.T) {
	p, cfg := fixture()
	months := []string{"2026-03", "2026-05", "2026-07"}
	for _, m := range months {
		p.MonthlySales[m] = 0
	}
	before := calc(t, p, cfg)
	zero := 0.0
	for _, m := range months {
		p.MonthlyStock[m] = &zero
	}
	after := calc(t, p, cfg)
	if after.Breakdown.StockoutCompensation != 50 || after.Breakdown.ForecastDemand != 100 || after.Breakdown.ForecastDemand <= before.Breakdown.ForecastDemand || len(after.Breakdown.StockoutPeriods) != 3 {
		t.Fatalf("before=%+v after=%+v", before.Breakdown, after.Breakdown)
	}
}

func TestUnknownOrTerminalStockNotStockout(t *testing.T) {
	p, cfg := fixture()
	p.MonthlySales["2026-07"] = 0
	p.MonthlySales["2026-08"] = 0
	p.MonthlyStock["2026-08"] = nil
	if got := calc(t, p, cfg); got.Breakdown.StockoutCompensation != 0 {
		t.Fatalf("%+v", got.Breakdown)
	}
}

func TestMultipleTenRounds83To90(t *testing.T) {
	p, cfg := fixture()
	p.FreeStock = 17
	if got := calc(t, p, cfg); got.Breakdown.RawNeed != 83 || got.RecommendedQuantity != 90 {
		t.Fatalf("%+v", got)
	}
}

func TestPartialSeptemberExcluded(t *testing.T) {
	p, cfg := fixture()
	p.MonthlySales["2026-09"] = 1e9
	i := calc(t, p, cfg)
	if i.Breakdown.BaseMonthlyDemand != 100 || i.Breakdown.PeriodStart != "2025-09" || i.Breakdown.PeriodEnd != "2026-08" || len(i.Breakdown.BaseMonths) != 6 {
		t.Fatalf("%+v", i.Breakdown)
	}
}

func TestMissingMultipleReview(t *testing.T) {
	p, cfg := fixture()
	p.OrderMultiple = 0
	i := calc(t, p, cfg)
	if i.Decision != "REVIEW" || i.Breakdown.OrderMultiple != 0 || i.RecommendedQuantity != 0 {
		t.Fatalf("%+v", i)
	}
	found := false
	for _, w := range i.Warnings {
		if w.Code == "MISSING_ORDER_MULTIPLE" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing warning")
	}
}

func TestSeasonalityWeightsActualHorizon(t *testing.T) {
	p, cfg := fixture()
	cfg.LeadTimeDays = 30
	i, err := Calculate(context.Background(), p, domain.DatasetDate(), map[int]float64{9: 2, 10: 1}, cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// September 22–30 = 9 days; October 1–21 = 21 days.
	if math.Abs(i.Breakdown.SeasonFactor-1.3) > 1e-9 {
		t.Fatalf("%+v", i.Breakdown)
	}
}

func TestSafetyOnlyCountedOnce(t *testing.T) {
	p, cfg := fixture()
	cfg.SafetyDays = 15
	i := calc(t, p, cfg)
	if i.Breakdown.TargetMonths != 1 || i.Breakdown.SafetyStock != 50 || i.RecommendedQuantity != 150 {
		t.Fatalf("%+v", i.Breakdown)
	}
}

func TestMissingMonthIsNotZero(t *testing.T) {
	p, cfg := fixture()
	delete(p.MonthlySales, "2026-04")
	i := calc(t, p, cfg)
	if i.Decision != "REVIEW" || i.Breakdown.BaseMonthlyDemand != 100 {
		t.Fatalf("%+v", i)
	}
}

func TestGrowthClampedAndReturnsNonNegative(t *testing.T) {
	p, cfg := fixture()
	for _, m := range []string{"2025-06", "2025-07", "2025-08"} {
		p.MonthlySales[m] = 1
	}
	if got := calc(t, p, cfg); got.Breakdown.GrowthFactor != 1.5 {
		t.Fatalf("%+v", got.Breakdown)
	}
	for m := range p.MonthlySales {
		p.MonthlySales[m] = -100
	}
	if got := calc(t, p, cfg); got.Breakdown.ForecastDemand < 0 || got.RecommendedQuantity < 0 {
		t.Fatalf("%+v", got)
	}
}

func TestAdditionalStockOptIn(t *testing.T) {
	p, cfg := fixture()
	p.ShowcaseStock = 100
	p.PresentFields["showcaseStock"] = true
	if got := calc(t, p, cfg); got.RecommendedQuantity != 100 {
		t.Fatal(got)
	}
	cfg.IncludeShowcase = true
	if got := calc(t, p, cfg); got.RecommendedQuantity != 0 {
		t.Fatal(got)
	}
}

func TestStockoutDoesNotAddDemandAlreadyCoveredByMedian(t *testing.T) {
	p, cfg := fixture()
	p.MonthlySales["2026-05"] = 0
	zero := 0.0
	p.MonthlyStock["2026-05"] = &zero
	i := calc(t, p, cfg)
	if i.Breakdown.BaseMonthlyDemand != 100 || i.Breakdown.StockoutCompensation != 0 || i.Breakdown.ForecastDemand != 100 {
		t.Fatalf("one missing month must not inflate steady demand of 100: %+v", i.Breakdown)
	}
}

func TestSeasonalBaselineIsNotSeasonalizedTwice(t *testing.T) {
	p, cfg := fixture()
	for _, m := range Months(domain.DatasetDate(), 32) {
		month, _ := time.Parse("2006-01", m)
		p.MonthlySales[m] = 100
		if month.Month() >= time.March && month.Month() <= time.August {
			p.MonthlySales[m] = 200
		}
	}
	i := calc(t, p, cfg)
	if math.Abs(i.Breakdown.ForecastDemand-100) > 1e-9 || i.RecommendedQuantity != 100 {
		t.Fatalf("stable annual pattern must order 100 in September/October: %+v", i)
	}
}

func TestSafetyUsesMonthsAfterLeadTime(t *testing.T) {
	p, cfg := fixture()
	cfg.LeadTimeDays = 9 // September 22–30.
	cfg.SafetyDays = 30  // October 1–30.
	seasons := map[int]float64{}
	for month := 1; month <= 12; month++ {
		seasons[month] = 1
	}
	seasons[10] = 2
	i, err := Calculate(context.Background(), p, domain.DatasetDate(), seasons, cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if i.Breakdown.ForecastDemand != 30 || i.Breakdown.SafetyStock != 200 {
		t.Fatalf("lead demand=30, safety demand=200: %+v", i.Breakdown)
	}
}

func TestMultipleRoundingNeverUnderOrders(t *testing.T) {
	p, cfg := fixture()
	p.FreeStock = 90 - 1e-11
	i := calc(t, p, cfg)
	if i.RecommendedQuantity < i.Breakdown.RawNeed || i.RecommendedQuantity != 20 {
		t.Fatalf("rounding must cover even a fractional need above a multiple: %+v", i)
	}
}

func TestGlobalSeasonalityRemovesBaselineSeason(t *testing.T) {
	p, cfg := fixture()
	seasons := map[int]float64{}
	for m := 1; m <= 12; m++ {
		seasons[m] = 1
	}
	for m := 3; m <= 8; m++ {
		seasons[m] = 2
	}
	for key := range p.MonthlySales {
		date, _ := time.Parse("2006-01", key)
		p.MonthlySales[key] = 100 * seasons[int(date.Month())]
	}
	i, err := Calculate(context.Background(), p, domain.DatasetDate(), seasons, cfg, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if i.Breakdown.BaseMonthlyDemand != 100 || i.Breakdown.ForecastDemand != 100 || i.RecommendedQuantity != 100 {
		t.Fatalf("summer sales of 200 with seasonality 2 must imply autumn demand of 100: %+v", i)
	}
}

func TestStockoutOutsideBaselineDoesNotAddDemand(t *testing.T) {
	p, cfg := fixture()
	p.MonthlySales["2025-12"] = 0
	p.MonthlyStock["2025-12"] = nil
	i := calc(t, p, cfg)
	if len(i.Breakdown.StockoutPeriods) != 1 || i.Breakdown.StockoutCompensation != 0 || i.Breakdown.ForecastDemand != 100 {
		t.Fatalf("historical gap must not add losses to an already complete recent baseline: %+v", i.Breakdown)
	}
}

func TestReturnsNotRestoredAsStockout(t *testing.T) {
	p, cfg := fixture()
	p.MonthlySales["2026-05"] = -50
	p.MonthlyStock["2026-05"] = nil
	i := calc(t, p, cfg)
	if len(i.Breakdown.StockoutPeriods) != 0 || i.Breakdown.StockoutCompensation != 0 {
		t.Fatalf("a return is not evidence of lost sales: %+v", i.Breakdown)
	}
}

func TestStockCannotIncreaseOrder(t *testing.T) {
	for _, stock := range []float64{0, .25, 17, 100, 1000} {
		p, cfg := fixture()
		cfg.LeadTimeDays, cfg.SafetyDays = 47, 13 // Total demand = 200.
		p.FreeStock = stock
		withoutTransit := calc(t, p, cfg)
		p.InTransit = 25
		withTransit := calc(t, p, cfg)
		p.FreeStock += 25
		p.InTransit = 0
		withFree := calc(t, p, cfg)
		if withTransit.RecommendedQuantity > withoutTransit.RecommendedQuantity || withFree.RecommendedQuantity != withTransit.RecommendedQuantity {
			t.Fatalf("free and in-transit stock must reduce order equally: free=%v", stock)
		}
		for _, i := range []domain.Item{withoutTransit, withTransit, withFree} {
			if i.RecommendedQuantity < i.Breakdown.RawNeed || math.Mod(i.RecommendedQuantity, 10) != 0 {
				t.Fatalf("quantity must cover demand and honor MOQ: %+v", i)
			}
		}
	}
}

func TestSeasonalHorizonIncludesLeapDay(t *testing.T) {
	profile := seasonalProfile(nil, map[int]float64{2: 2, 3: 1})
	start := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	factor, _, ok := seasonFactor(profile, start, 30)
	if !ok || math.Abs(factor-59.0/30) > 1e-12 {
		t.Fatalf("29 days of February and 1 of March: %v", factor)
	}
}

func TestGrowthComparisonEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		recent, previous, want float64
		complete               bool
	}{
		{"growth_cap", 300, 100, 1.5, true},
		{"decline_floor", 10, 100, .5, true},
		{"zero_previous", 100, 0, 1, false},
		{"zero_recent", 0, 100, .5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sales := map[string]float64{}
			for _, month := range []string{"06", "07", "08"} {
				sales["2026-"+month], sales["2025-"+month] = tc.recent, tc.previous
			}
			factor, ok := growthFactor(sales, domain.DatasetDate())
			if factor != tc.want || ok != tc.complete {
				t.Fatalf("factor=%v, complete=%v", factor, ok)
			}
			delete(sales, "2025-07")
			if factor, ok := growthFactor(sales, domain.DatasetDate()); factor != 1 || ok {
				t.Fatal("missing comparison must use neutral growth")
			}
		})
	}
}

func TestCalculationIsRepeatableAndDoesNotMutateHistory(t *testing.T) {
	p, cfg := fixture()
	p.MonthlySales["2026-05"], p.MonthlyStock["2026-05"] = 0, nil
	before, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	first, err := json.Marshal(calc(t, p, cfg))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(calc(t, p, cfg))
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || string(before) != string(after) {
		t.Fatal("calculation must be deterministic and leave history intact")
	}
}

func TestForecastValidatesHorizonWithoutHTTP(t *testing.T) {
	p, cfg := fixture()
	for _, bad := range []domain.RunConfig{{LeadTimeDays: 0}, {LeadTimeDays: -1}, {LeadTimeDays: 366}, {LeadTimeDays: 30, SafetyDays: -1}, {LeadTimeDays: 30, SafetyDays: 366}} {
		if _, err := Calculate(context.Background(), p, domain.DatasetDate(), nil, bad, nil, ""); err == nil {
			t.Fatalf("accepted invalid horizon %+v", bad)
		}
	}
	if _, err := Calculate(context.Background(), nil, domain.DatasetDate(), nil, cfg, nil, ""); err == nil {
		t.Fatal("accepted nil product")
	}
}

func TestCostAndSummaryUseApprovedQuantity(t *testing.T) {
	price, approved := 2.55, 3.0
	item := domain.Item{Decision: "BUY", RecommendedQuantity: 10, FinalQuantity: approved, ApprovedQuantity: &approved, UnitCost: &price}
	SetCost(&item)
	if item.EstimatedCost == nil || *item.EstimatedCost != 7.65 {
		t.Fatalf("cost must use 3 approved units: %+v", item)
	}
	s := Summarize([]domain.Item{item, {Decision: "REVIEW", FinalQuantity: 2}, {Decision: "NO_BUY"}})
	if s.Buy != 1 || s.NoBuy != 1 || s.Review != 1 || s.TotalUnits != 5 || s.EstimatedCost != 7.65 || s.UnpricedItems != 1 || s.ApprovedItems != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestStaleCandidateCannotBypassSourceConflict(t *testing.T) {
	p, cfg := fixture()
	p.Transactions = []domain.Transaction{{Date: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), DocumentID: "project", Quantity: 420}}
	candidates := []domain.Anomaly{{Month: "2026-05", DocumentID: "project", Quantity: 420, SourceConsistent: true}}
	i, err := Calculate(context.Background(), p, domain.DatasetDate(), nil, cfg, candidates, "EXCLUDE")
	if err != nil {
		t.Fatal(err)
	}
	if i.Anomalies[0].Excluded || i.Anomalies[0].SourceConsistent || i.Decision != "REVIEW" {
		t.Fatalf("stale source consistency trusted: %+v", i)
	}
	if !candidates[0].SourceConsistent {
		t.Fatal("input candidates mutated")
	}
}
