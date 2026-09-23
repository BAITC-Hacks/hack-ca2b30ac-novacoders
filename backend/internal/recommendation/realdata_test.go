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
