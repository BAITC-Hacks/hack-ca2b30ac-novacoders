package service

import (
	"testing"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/recommendation"
)

func TestHistoryDiagnosticsRespectSettingsAndUnknowns(t *testing.T) {
	settings, err := recommendation.Settings(domain.RunConfig{})
	if err != nil {
		t.Fatal(err)
	}
	p := &domain.Product{Code1C: "001_", MonthlySales: map[string]float64{}, MonthlyStock: map[string]*float64{}, Warnings: []domain.Diagnostic{{Code: "INVALID_CELL", Month: "2023-09", Blocking: true}, {Code: "UNKNOWN_MOQ", Blocking: true}}}
	zero := 0.0
	for month := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC); month.Before(domain.DatasetDate()); month = month.AddDate(0, 1, 0) {
		p.MonthlySales[month.Format("2006-01")] = 0
		p.MonthlyStock[month.Format("2006-01")] = &zero
	}
	conflicts := []domain.Diagnostic{{Code: "SOURCE_CONFLICT", Month: "2025-08", Blocking: true}, {Code: "SOURCE_CONFLICT", Month: "2026-09", Blocking: true}}
	assertCodes := func(want ...string) {
		t.Helper()
		got := historyWarnings(p, domain.DatasetDate(), settings, conflicts)
		if len(got) != len(want) {
			t.Fatalf("unexpected warnings: %+v", got)
		}
		for i, code := range want {
			if got[i].Code != code {
				t.Fatalf("%+v", got)
			}
		}
	}
	assertCodes("UNKNOWN_MOQ") // Known zero months are complete data.
	settings.ExcludePartialMonth = false
	assertCodes("UNKNOWN_MOQ", "SOURCE_CONFLICT")
	settings.ExcludePartialMonth = true
	delete(p.MonthlySales, "2026-08")
	p.MonthlyStock["2026-08"] = nil
	assertCodes("UNKNOWN_MOQ", "INCOMPLETE_SALES_HISTORY", "INCOMPLETE_STOCK_HISTORY")
	p.MonthlySales["2026-08"] = 0
	p.BlankSalesMonths = []string{"2026-08"}
	assertCodes("UNKNOWN_MOQ", "INCOMPLETE_SALES_HISTORY", "INCOMPLETE_STOCK_HISTORY")
	settings.HistoryMonths = 1
	first, last := recommendation.HistoryWindow(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), settings)
	if first.Format("2006-01") != "2026-09" || !first.Equal(last) {
		t.Fatal("complete month excluded")
	}
}
