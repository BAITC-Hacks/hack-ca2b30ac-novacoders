package importer

import (
	"bytes"
	"context"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestIEKHeadersShipmentsAndMonthlyAggregation(t *testing.T) {
	files := demoReaders(t)
	files["moq"] = workbook(t, [][]any{{"№", "Код 1с", "Артикул поставщика", "Наименование", "Мин. разр. к отгр."}, {1, "001_", "A", "Товар", 5}})
	files["monthly_sales"] = workbook(t, [][]any{{"Номенклатура", "Номенклатура.Код", "янв. 2026", "фев. 2026", "мар. 2026"}, {nil, nil, "Количество", "Количество", "Количество"}, {"Товар", "001_", 10, 0, nil}, {"Товар", "001_", -3, 0, 4}})
	files["monthly_stock"] = workbook(t, [][]any{{"Номенклатура", "Ед.", "Номенклатура.Код", "янв. 2026", "фев. 2026", "мар. 2026"}, {nil, nil, nil, "Количество", "Количество", "Количество"}, {nil, nil, nil, "нач. остаток", "нач. остаток", "нач. остаток"}, {"Товар", "шт", "001_", 0, 5, 2}, {"Товар", "шт", "001_", 0, nil, 3}})
	files["in_transit"] = workbook(t, [][]any{{"Код 1с", "Артикул ИЭК", "Наименование", "РФ УТ-1 (поступление до 01.10.2026)", "РФ УТ-2 (поступление до 15.10.2026)"}, {"001_", "A", "Товар", 10, 5}, {"002_", "B", "Другой", 10, nil}, {"003_", "C", "Ещё", 0, 0}})
	files["sales_transactions"] = workbook(t, [][]any{{"Дата", "Номер", "Документ", "Код", "Номенклатура", "Склад", "Количество"}, {"2026-01-02", "T", "Док", "001_", "Товар", "Алматы", 10}, {"2026-01-02", "T", "Док", "001_", "Товар", "Алматы", -3}})
	rows := [][]any{{"Месяц", "СЕЗОННОСТЬ"}}
	for m := 1; m <= 12; m++ {
		rows = append(rows, []any{m, 1})
	}
	rows = append(rows, []any{"Итого", 12}, []any{"Вторая таблица", "Не коэффициент"})
	files["seasonality"] = workbook(t, rows)
	d, err := ImportSupplier(context.Background(), files, "IEK")
	if err != nil {
		t.Fatal(err)
	}
	p := d.Products["001_"]
	if p == nil || p.Supplier != "IEK" || p.OrderMultiple != 5 || p.UnitCost != nil || p.PresentFields["freeStock"] || p.InTransit != 15 || !p.PresentFields["inTransit"] {
		t.Fatalf("%+v", p)
	}
	if d.Products["002_"].PresentFields["inTransit"] || !d.Products["003_"].PresentFields["inTransit"] || d.Products["003_"].InTransit != 0 {
		t.Fatal("unknown or zero transit lost")
	}
	if p.MonthlySales["2026-01"] != 7 || p.MonthlySales["2026-02"] != 0 || !containsMonth(p.BlankSalesMonths, "2026-03") || len(p.MonthlySales) != 2 {
		t.Fatalf("%+v", p.MonthlySales)
	}
	if *p.MonthlyStock["2026-01"] != 0 || p.MonthlyStock["2026-02"] != nil || *p.MonthlyStock["2026-03"] != 5 || len(p.Transactions) != 2 {
		t.Fatal("stock or transaction aggregation wrong")
	}
	if len(d.Seasonality) != 12 || d.Diagnostics.ProcessedRows["seasonality"] != 12 || len(d.Diagnostics.Errors) != 0 {
		t.Fatalf("%+v", d.Diagnostics)
	}
}

func TestNumericDisplayFormattingDoesNotChangeValues(t *testing.T) {
	files := demoReaders(t)
	f := excelize.NewFile()
	defer f.Close()
	rows := [][]any{{"Номенклатура.Код", "Артикул", "янв. 2026", "фев. 2026"}, {123, "A", 1220, -1525.5}}
	for n, row := range rows {
		a, _ := excelize.CoordinatesToCellName(1, n+1)
		if err := f.SetSheetRow("Sheet1", a, &row); err != nil {
			t.Fatal(err)
		}
	}
	codeFormat := "000000"
	codeStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &codeFormat})
	if err != nil {
		t.Fatal(err)
	}
	numericStyle, err := f.NewStyle(&excelize.Style{NumFmt: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellStyle("Sheet1", "A2", "A2", codeStyle); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellStyle("Sheet1", "C2", "D2", numericStyle); err != nil {
		t.Fatal(err)
	}
	b, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	files["monthly_sales"] = bytes.NewReader(b.Bytes())
	d, err := Import(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	p := d.Products["000123"]
	if p == nil || p.MonthlySales["2026-01"] != 1220 || p.MonthlySales["2026-02"] != -1525.5 {
		t.Fatalf("formatted numbers corrupted: %+v", p)
	}
}

func TestInvalidTransactionQuantityIsRetainedAsUnknownWithMonth(t *testing.T) {
	files := demoReaders(t)
	files["sales_transactions"] = workbook(t, [][]any{
		{"Дата", "Номер", "Документ", "Код", "Номенклатура", "Склад", "Количество"},
		{"2026-08-01", "SALE", "Док", "030200128_", "Товар", "Алматы", "ошибка"},
		{"2026-08-01", "SALE", "Док", "030200128_", "Товар", "Алматы", 0},
		{"2026-08-02", "RETURN", "Док", "030200128_", "Товар", "Алматы", -5},
	})
	d, err := Import(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	p := d.Products["030200128_"]
	if len(p.Transactions) != 3 || !p.Transactions[0].QuantityMissing || p.Transactions[1].QuantityMissing || p.Transactions[1].Quantity != 0 || p.Transactions[2].Quantity != -5 {
		t.Fatalf("%+v", p.Transactions)
	}
	found := false
	for _, w := range p.Warnings {
		if w.Code == "INVALID_CELL" && w.Month == "2026-08" && w.Blocking {
			found = true
		}
	}
	if !found {
		t.Fatal("invalid quantity has no scoped blocking diagnostic")
	}
}

func TestExplicitAIOrderFieldsDoNotInventUnknownValues(t *testing.T) {
	files := demoReaders(t)
	files["moq"] = workbook(t, [][]any{
		{"Номенклатура.Код", "Артикул", "Кратность", "Единица измерения", "Минимальная партия", "Шаг количества"},
		{"030200128_", "DEMO-001", 10, "шт", 0, 1},
		{"030200129_", "DEMO-002", 10, nil, nil, nil},
	})
	f, err := excelize.OpenReader(files["in_transit"])
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Sheet1", "N1", "Дата остатка"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Sheet1", "N2", "2026-09-22"); err != nil {
		t.Fatal(err)
	}
	b, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	files["in_transit"] = bytes.NewReader(b.Bytes())
	d, err := Import(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	known, unknown := d.Products["030200128_"], d.Products["030200129_"]
	if known.Unit != "шт" || known.MinimumOrderQuantity == nil || *known.MinimumOrderQuantity != 0 || known.QuantityStep == nil || *known.QuantityStep != 1 || known.StockAsOfDate != "2026-09-22" {
		t.Fatal("explicit source values lost")
	}
	if unknown.Unit != "" || unknown.MinimumOrderQuantity != nil || unknown.QuantityStep != nil || unknown.StockAsOfDate != "" {
		t.Fatal("unknown fields were invented from MOQ/asOfDate")
	}
}
