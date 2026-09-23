package importer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/demo"
	"github.com/xuri/excelize/v2"
)

func demoReaders(t *testing.T) map[string]io.Reader {
	t.Helper()
	files, err := demo.Workbooks()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]io.Reader{}
	for k, v := range files {
		out[k] = bytes.NewReader(v)
	}
	return out
}

func workbook(t *testing.T, rows [][]any) io.Reader {
	t.Helper()
	b, err := demo.Workbook(rows)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(b)
}

func TestImportSixXLSXAndPreserveCodes(t *testing.T) {
	d, err := Import(context.Background(), demoReaders(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Products) != 4 || d.Products["030200128_"] == nil || d.Diagnostics.FullyMatchedProducts != 4 || d.Diagnostics.SkippedTotalRows["moq"] != 1 || len(d.Diagnostics.SourceConflicts) != 0 || len(d.Diagnostics.ProductsWithoutMOQ) != 1 {
		t.Fatalf("%+v", d.Diagnostics)
	}
}

func TestBlankZeroMissingAndMalformedRows(t *testing.T) {
	files := demoReaders(t)
	files["monthly_stock"] = workbook(t, [][]any{{"Номенклатура.\nКод", "Январь 2026", "Февраль 2026"}, {"030200128_", 0, nil}})
	files["monthly_sales"] = workbook(t, [][]any{{"Номенклатура.Код", "Артикул", "Январь 2026", "Февраль 2026"}, {"030200128_", "DEMO-001", 0, nil}, {"broken", "X", "not a number", 10}, {"", "", 5, 5}})
	d, err := Import(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	p := d.Products["030200128_"]
	if p.MonthlyStock["2026-01"] == nil || *p.MonthlyStock["2026-01"] != 0 {
		t.Fatal("zero lost")
	}
	if v, ok := p.MonthlyStock["2026-02"]; !ok || v != nil {
		t.Fatal("blank lost")
	}
	if _, ok := p.MonthlyStock["2026-03"]; ok {
		t.Fatal("missing invented")
	}
	if v, ok := p.MonthlySales["2026-01"]; !ok || v != 0 {
		t.Fatal("sales zero lost")
	}
	if _, ok := p.MonthlySales["2026-02"]; ok || len(p.BlankSalesMonths) != 1 {
		t.Fatal("blank sales became zero")
	}
	if len(d.Diagnostics.Errors) < 2 || d.Products["broken"].MonthlySales["2026-02"] != 10 {
		t.Fatalf("%+v", d.Diagnostics)
	}
}

func TestReturnsArePreserved(t *testing.T) {
	files := demoReaders(t)
	files["sales_transactions"] = workbook(t, [][]any{{"Дата", "Номер", "Документ", "Код", "Номенклатура", "Склад", "Количество"}, {"2026-08-01", "R1", "Возврат", "030200128_", "Товар", "Склад", "-2,5"}})
	d, err := Import(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	if d.Products["030200128_"].Transactions[0].Quantity != -2.5 {
		t.Fatal("return lost")
	}
}

func TestMissingHeadersAndCorruptFilesReturnDiagnostics(t *testing.T) {
	for _, data := range [][]byte{[]byte("not a zip"), nil} {
		files := demoReaders(t)
		if data == nil {
			files["moq"] = workbook(t, [][]any{{"wrong"}})
		} else {
			files["moq"] = bytes.NewReader(data)
		}
		_, err := Import(context.Background(), files)
		var invalid *ValidationError
		if !errors.As(err, &invalid) || len(invalid.Diagnostics.Errors) == 0 {
			t.Fatalf("%v", err)
		}
	}
}

func TestMergedYearHeadersAndFormattedCode(t *testing.T) {
	files := demoReaders(t)
	f := excelize.NewFile()
	defer f.Close()
	rows := [][]any{{"Номенклатура.Код", 2026}, {nil, "Январь", "Февраль"}, {30200128, 5, 6}}
	for i, row := range rows {
		addr, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("Sheet1", addr, &row); err != nil {
			t.Fatal(err)
		}
	}
	format := "000000000"
	style, err := f.NewStyle(&excelize.Style{CustomNumFmt: &format})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellStyle("Sheet1", "A3", "A3", style); err != nil {
		t.Fatal(err)
	}
	if err := f.MergeCell("Sheet1", "B1", "C1"); err != nil {
		t.Fatal(err)
	}
	b, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	files["monthly_stock"] = bytes.NewReader(b.Bytes())
	d, err := Import(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	p := d.Products["030200128"]
	if p == nil || p.MonthlyStock["2026-01"] == nil || *p.MonthlyStock["2026-02"] != 6 {
		t.Fatalf("formatted code / year headers not imported: %+v", p)
	}
}

func TestCancelImport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Import(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestExcelDatesWithCustomDisplayAnd1904Epoch(t *testing.T) {
	for _, date1904 := range []bool{false, true} {
		files := demoReaders(t)
		f := excelize.NewFile()
		if err := f.SetWorkbookProps(&excelize.WorkbookPropsOptions{Date1904: &date1904}); err != nil {
			t.Fatal(err)
		}
		header := []any{"Дата", "Номер", "Документ", "Код", "Номенклатура", "Склад", "Количество"}
		row := []any{time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), "R1", "Реализация", "030200128_", "Товар", "Склад", 10}
		if err := f.SetSheetRow("Sheet1", "A1", &header); err != nil {
			t.Fatal(err)
		}
		if err := f.SetSheetRow("Sheet1", "A2", &row); err != nil {
			t.Fatal(err)
		}
		format := "dddd, mmmm dd, yyyy"
		style, err := f.NewStyle(&excelize.Style{CustomNumFmt: &format})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.SetCellStyle("Sheet1", "A2", "A2", style); err != nil {
			t.Fatal(err)
		}
		b, err := f.WriteToBuffer()
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		files["sales_transactions"] = bytes.NewReader(b.Bytes())
		d, err := Import(context.Background(), files)
		if err != nil {
			t.Fatal(err)
		}
		transactions := d.Products["030200128_"].Transactions
		if len(transactions) != 1 || transactions[0].Date.Format("2006-01-02") != "2026-08-01" {
			t.Fatalf("date1904=%v transactions=%+v errors=%+v", date1904, transactions, d.Diagnostics.Errors)
		}
	}
}
