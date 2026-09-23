package anomaly

import (
	"fmt"
	"testing"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
)

func transactions(large int) *domain.Product {
	p := &domain.Product{Code1C: "030200128_", MonthlySales: map[string]float64{}}
	for i := range 24 {
		month := i%6 + 3
		quantity := 10.0
		date := time.Date(2026, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		p.Transactions = append(p.Transactions, domain.Transaction{Date: date, DocumentID: fmt.Sprint(i), Quantity: quantity})
		p.MonthlySales[date.Format("2006-01")] += quantity
	}
	for i := range large {
		date := time.Date(2026, time.Month(i+3), 2, 0, 0, 0, 0, time.UTC)
		// Two lines of the same document must be treated as one event.
		for range 2 {
			p.Transactions = append(p.Transactions, domain.Transaction{Date: date, DocumentID: fmt.Sprintf("large-%d", i), Quantity: 210})
		}
		p.MonthlySales[date.Format("2006-01")] += 420
	}
	return p
}

func TestOneOffCandidateGroupsDocumentLines(t *testing.T) {
	a, w := Detect(transactions(1), domain.DatasetDate())
	if len(a) != 1 || len(w) != 0 || a[0].Quantity != 420 || !a[0].SourceConsistent {
		t.Fatalf("%+v %+v", a, w)
	}
}

func TestRecurringLargeEventsNotCandidates(t *testing.T) {
	a, _ := Detect(transactions(3), domain.DatasetDate())
	if len(a) != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestConflictRetainsCandidate(t *testing.T) {
	p := transactions(1)
	p.MonthlySales["2026-03"] = 421
	a, w := Detect(p, domain.DatasetDate())
	if len(a) != 1 || a[0].SourceConsistent || len(w) != 1 || w[0].Code != "SOURCE_CONFLICT" {
		t.Fatalf("%+v %+v", a, w)
	}
}

func TestReturnsIncludedInReconciliation(t *testing.T) {
	p := transactions(0)
	p.Transactions = append(p.Transactions, domain.Transaction{Date: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), DocumentID: "return", Quantity: -5})
	p.MonthlySales["2026-03"] -= 5
	if w := Reconcile(p, domain.DatasetDate()); len(w) != 0 {
		t.Fatalf("%+v", w)
	}
}

func TestQuantilesAndInputIsolation(t *testing.T) {
	values := []float64{40, 10, 30, 20}
	for _, tc := range []struct{ q, want float64 }{{0, 10}, {.25, 17.5}, {.5, 25}, {.75, 32.5}, {1, 40}} {
		if got := Quantile(values, tc.q); got != tc.want {
			t.Fatalf("q=%v: got=%v want=%v", tc.q, got, tc.want)
		}
	}
	if values[0] != 40 || values[1] != 10 {
		t.Fatal("statistics mutated input")
	}
	if Quantile(nil, .5) != 0 || Quantile([]float64{7}, .5) != 7 {
		t.Fatal("empty/singleton quantile failed")
	}
}

func TestReconciliationToleranceAndMissingSource(t *testing.T) {
	p := &domain.Product{Code1C: "sku", MonthlySales: map[string]float64{"2026-08": 100}, Transactions: []domain.Transaction{{Date: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), DocumentID: "1", Quantity: 101}}}
	if warnings := Reconcile(p, domain.DatasetDate()); len(warnings) != 0 {
		t.Fatal("1% difference should be tolerated")
	}
	p.Transactions[0].Quantity = 101.01
	if warnings := Reconcile(p, domain.DatasetDate()); len(warnings) != 1 {
		t.Fatal("difference above 1% should require review")
	}
	delete(p.MonthlySales, "2026-08")
	if warnings := Reconcile(p, domain.DatasetDate()); len(warnings) != 1 || warnings[0].Code != "SOURCE_COMPARISON_UNAVAILABLE" {
		t.Fatal("missing monthly source must not be treated as zero")
	}
	p.MonthlySales["2026-08"] = 101.01
	p.Transactions[0].QuantityMissing = true
	if warnings := Reconcile(p, domain.DatasetDate()); len(warnings) != 1 || warnings[0].Code != "SOURCE_COMPARISON_UNAVAILABLE" {
		t.Fatal("unknown operation must not be reconciled as zero")
	}
}
