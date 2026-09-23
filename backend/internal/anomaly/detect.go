// Package anomaly contains deterministic detection and source reconciliation only.
package anomaly

import (
	"math"
	"sort"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
)

func Quantile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	x := append([]float64(nil), values...)
	sort.Float64s(x)
	p := float64(len(x)-1) * q
	i := int(p)
	if i == len(x)-1 {
		return x[i]
	}
	return x[i] + (x[i+1]-x[i])*(p-float64(i))
}

// Reconcile never treats absent detail months as zero sales. Comparisons use net
// quantities, including returns, with a 1% (minimum 0.01 unit) tolerance.
func Reconcile(p *domain.Product, asOf time.Time) []domain.Diagnostic {
	totals := map[string]float64{}
	unknown := map[string]bool{}
	for _, t := range p.Transactions {
		if t.Date.Before(asOf.AddDate(0, 0, 1)) {
			if t.QuantityMissing {
				unknown[t.Date.Format("2006-01")] = true
			}
			totals[t.Date.Format("2006-01")] += t.Quantity
		}
	}
	months := make([]string, 0, len(totals))
	for m := range totals {
		months = append(months, m)
	}
	sort.Strings(months)
	issues := []domain.Diagnostic{}
	for _, m := range months {
		monthly, ok := p.MonthlySales[m]
		if unknown[m] || !ok || math.Abs(monthly-totals[m]) > math.Max(.01, math.Abs(monthly)*.01) {
			issues = append(issues, domain.Diagnostic{Code: "SOURCE_CONFLICT", Message: "Месячные и детальные продажи не согласованы или месячная запись отсутствует.", Code1C: p.Code1C, Month: m, Blocking: true})
		}
	}
	return issues
}

func Detect(p *domain.Product, asOf time.Time) ([]domain.Anomaly, []domain.Diagnostic) {
	type key struct{ month, document string }
	groups := map[key]float64{}
	currentMonth := asOf.Format("2006-01")
	for _, t := range p.Transactions {
		month := t.Date.Format("2006-01")
		if month >= currentMonth || t.DocumentID == "" || t.QuantityMissing {
			continue
		}
		groups[key{month, t.DocumentID}] += t.Quantity
	}
	keys := make([]key, 0, len(groups))
	values := []float64{}
	for k, v := range groups {
		if v > 0 {
			keys = append(keys, k)
			values = append(values, v)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].month == keys[j].month {
			return keys[i].document < keys[j].document
		}
		return keys[i].month < keys[j].month
	})
	issues := Reconcile(p, asOf)
	conflicts := map[string]bool{}
	for _, w := range issues {
		conflicts[w.Month] = true
	}
	median := Quantile(values, .5)
	q1, q3 := Quantile(values, .25), Quantile(values, .75)
	threshold := math.Max(q3+3*(q3-q1), median*5)
	candidates := []domain.Anomaly{}
	for _, k := range keys {
		qty := groups[k]
		monthly := p.MonthlySales[k.month]
		if qty <= threshold || monthly <= 0 || qty/monthly < .5 {
			continue
		}
		similar := 0
		for _, v := range values {
			if v >= qty*.5 {
				similar++
			}
		}
		if similar > 2 {
			continue
		} // Regular large batches are ordinary demand.
		candidates = append(candidates, domain.Anomaly{DocumentID: k.document, Month: k.month, Quantity: qty, MedianTransaction: median, MonthShare: qty / monthly, SimilarLargeEvents: similar, Deterministic: "ONE_OFF", SourceConsistent: !conflicts[k.month]})
	}
	return candidates, issues
}
