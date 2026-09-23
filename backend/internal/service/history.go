package service

import (
	"fmt"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/recommendation"
)

// Import diagnostics retain every source issue. A run only blocks on monthly
// issues inside its chosen history window; stock/MOQ issues have no month and
// always apply. Missing months are unknown demand, never zero demand.
func historyWarnings(p *domain.Product, asOf time.Time, settings domain.RecommendationSettings, conflicts []domain.Diagnostic) []domain.Diagnostic {
	first, last := recommendation.HistoryWindow(asOf, settings)
	from, to := first.Format("2006-01"), last.Format("2006-01")
	warnings := []domain.Diagnostic{}
	for _, list := range [][]domain.Diagnostic{p.Warnings, conflicts} {
		for _, w := range list {
			if w.Month == "" || w.Month >= from && w.Month <= to {
				warnings = append(warnings, w)
			}
		}
	}
	missingSales, missingStock := 0, 0
	blank := map[string]bool{}
	for _, m := range p.BlankSalesMonths {
		blank[m] = true
	}
	for month := first; !month.After(last); month = month.AddDate(0, 1, 0) {
		key := month.Format("2006-01")
		if _, exists := p.MonthlySales[key]; !exists || blank[key] {
			missingSales++
		}
		if p.MonthlyStock[key] == nil {
			missingStock++
		}
	}
	if missingSales > 0 {
		warnings = append(warnings, domain.Diagnostic{Code: "INCOMPLETE_SALES_HISTORY", Message: fmt.Sprintf("За период %s — %s неизвестны продажи за %d из %d месяцев. Пропуски нельзя считать нулевыми продажами.", from, to, missingSales, settings.HistoryMonths), Code1C: p.Code1C, Blocking: true})
	}
	if missingStock > 0 {
		warnings = append(warnings, domain.Diagnostic{Code: "INCOMPLETE_STOCK_HISTORY", Message: fmt.Sprintf("За период %s — %s неизвестны остатки за %d из %d месяцев. Определение дефицита по истории ограничено.", from, to, missingStock, settings.HistoryMonths), Code1C: p.Code1C})
	}
	return warnings
}
