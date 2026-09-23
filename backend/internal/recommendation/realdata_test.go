package recommendation

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/importer"
)

// Optional integration check: real workbooks are deliberately not committed.
func TestRealIEKContract(t *testing.T) {
	dir := os.Getenv("SUPPLYLENS_IEK_DIR")
	if dir == "" {
		t.Skip("set SUPPLYLENS_IEK_DIR to check real workbooks")
	}
	names := map[string]string{"moq": "MOQ  ИЭК.xlsx", "sales_transactions": "Динамика продаж_2025-2026.xlsx", "monthly_stock": "Ежемесячные остатки продукции за последние 2 года  ИЭК.xlsx", "monthly_sales": "Ежемесячные продажи в количественном выражении за последние 2 года.xlsx", "seasonality": "Сезонность ИЭК.xlsx", "in_transit": "Путь ИЭК 22.09.2026.xlsx"}
	files := map[string]io.Reader{}
	for key, name := range names {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		files[key] = f
	}
	d, err := importer.ImportSupplier(context.Background(), files, "IEK")
	if err != nil {
		t.Fatal(err)
	}
	d.SourceFiles = names
	req, err := Build(d, domain.RunConfig{Supplier: "IEK"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxPayloadBytes {
		t.Fatalf("payload too large: %d", len(data))
	}
	if len(req.Products) == 0 || len(req.Transactions) == 0 || len(req.Seasonality) != 12 {
		t.Fatal("incomplete import")
	}
	for _, inv := range req.Inventory {
		if inv.FreeStock != nil || inv.TotalStock != nil || inv.ReservedStock != nil {
			t.Fatal("current stock invented from historical stock")
		}
	}
	negatives, unknown := 0, 0
	for _, tx := range req.Transactions {
		if tx.Quantity == nil {
			unknown++
		} else if *tx.Quantity < 0 {
			negatives++
		}
	}
	if negatives == 0 {
		t.Fatal("returns were lost")
	}
	issues := map[string]int{}
	for _, e := range d.Diagnostics.Errors {
		issues[e.Code]++
	}
	t.Logf("products=%d transactions=%d returns=%d unknownTransactionQuantity=%d monthlySales=%d monthlyStock=%d payloadBytes=%d errors=%v", len(req.Products), len(req.Transactions), negatives, unknown, len(req.MonthlySales), len(req.MonthlyStock), len(data), issues)
}

// Run against both local suppliers without replacing the production AI service
// or changing a workbook: SUPPLYLENS_DATA_DIR="$PWD/data/demo" go test -run TestLocalDatasetsContract -v ./internal/recommendation
func TestLocalDatasetsContract(t *testing.T) {
	root := os.Getenv("SUPPLYLENS_DATA_DIR")
	if root == "" {
		t.Skip("set SUPPLYLENS_DATA_DIR to check both local datasets")
	}
	for _, supplier := range []string{"IEK", domain.Supplier} {
		t.Run(supplier, func(t *testing.T) {
			dir, err := importer.LocalDirectory(root, supplier)
			if err != nil {
				t.Fatal(err)
			}
			d, err := importer.ImportDirectory(context.Background(), dir, supplier)
			if err != nil {
				t.Fatal(err)
			}
			req, err := Build(d, domain.RunConfig{Supplier: supplier})
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) > MaxPayloadBytes || len(req.Products) == 0 || len(req.Seasonality) != 12 {
				t.Fatalf("invalid dataset: products=%d seasonality=%d payloadBytes=%d", len(req.Products), len(req.Seasonality), len(data))
			}
			negatives, unknownQty, unknownStock, unknownTransit, txCount := 0, 0, 0, 0, 0
			for _, p := range d.Products {
				txCount += len(p.Transactions)
			}
			if len(req.Transactions) != txCount || len(req.Products) != len(d.Products) {
				t.Fatal("products or transactions lost")
			}
			for _, p := range req.Products {
				if p.Supplier != supplier || d.Products[p.Code1C] == nil {
					t.Fatal("supplier/code corrupted")
				}
			}
			for _, tx := range req.Transactions {
				if tx.Quantity == nil {
					unknownQty++
				} else if *tx.Quantity < 0 {
					negatives++
				}
			}
			for _, inv := range req.Inventory {
				if inv.FreeStock == nil {
					unknownStock++
				}
				if inv.InTransit == nil {
					unknownTransit++
				}
			}
			for _, monthly := range [][]Monthly{req.MonthlySales, req.MonthlyStock} {
				seen := map[string]bool{}
				for _, m := range monthly {
					key := m.Code1C + "/" + m.Month
					if seen[key] {
						t.Fatalf("duplicate monthly record %s", key)
					}
					seen[key] = true
				}
			}
			conflicts, errors, kinds := map[string]int{}, map[string]int{}, map[string]int{}
			first, last := HistoryWindow(d.AsOf, req.Settings.RecommendationSettings)
			inHistory := 0
			for _, w := range d.Diagnostics.SourceConflicts {
				conflicts[w.Month]++
				kinds[w.Code]++
				if w.Month >= first.Format("2006-01") && w.Month <= last.Format("2006-01") {
					inHistory++
				}
			}
			for _, w := range d.Diagnostics.Errors {
				errors[w.Code]++
			}
			t.Logf("directory=%s products=%d transactions=%d returns=%d unknownQuantity=%d unknownStock=%d unknownTransit=%d monthlySales=%d monthlyStock=%d payloadBytes=%d errors=%v", filepath.Base(dir), len(req.Products), len(req.Transactions), negatives, unknownQty, unknownStock, unknownTransit, len(req.MonthlySales), len(req.MonthlyStock), len(data), errors)
			t.Logf("sourceConflictsByMonth=%v", conflicts)
			t.Logf("reconciliation=%v issuesWithinHistory=%d sourceRows=%v", kinds, inHistory, d.Diagnostics.ProcessedRows)
		})
	}
}
