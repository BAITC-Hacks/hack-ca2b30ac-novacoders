// Package demo generates synthetic XLSX inputs; it contains no customer data.
package demo

import (
	"fmt"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/xuri/excelize/v2"
)

var Filenames = map[string]string{
	"moq":                "MOQ SystemElectric.xlsx",
	"sales_transactions": "Динамика продаж_Syseme Electric_2025-2026.xlsx",
	"monthly_stock":      "Ежемесячные остатки SystemElectric 2024-2026.xlsx",
	"monthly_sales":      "Ежемесячные продажи в кол-м выражении SystemElectric 2024-2026.xlsx",
	"seasonality":        "Сезонность SystemElectric 2024-2026.xlsx",
	"in_transit":         "Товар в пути_SystemElectric на 22.09.2026.xlsx",
}

func Workbooks() (map[string][]byte, error) {
	codes := []string{"030200128_", "030200129_", "030200130_", "030200131_"}
	tables := map[string][][]any{
		"moq":                {{"Номенклатура.Код", "Артикул", "Кратность"}},
		"sales_transactions": {{"Дата", "Номер", "Документ", "Код", "Номенклатура", "Склад", "Количество"}},
		"monthly_stock":      {{"Номенклатура.Код"}},
		"monthly_sales":      {{"Номенклатура.Код", "Артикул"}},
		"seasonality":        {{"Месяц", "Общий коэффициент сезонности"}},
		"in_transit":         {{"Артикул поставщика", "Код 1с", "Наименование", "Категория 2026", "СС реал", "Витрина", "Остаток ТЗ", "Розничный склад", "Остаток", "Зарезервировано", "Свободный остаток", "Заказ", "СЭ в пути 24.09"}},
	}
	months := []string{}
	for date := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC); !date.After(domain.DatasetDate()); date = date.AddDate(0, 1, 0) {
		m := date.Format("2006-01")
		months = append(months, m)
		tables["monthly_stock"][0] = append(tables["monthly_stock"][0], m)
		tables["monthly_sales"][0] = append(tables["monthly_sales"][0], m)
	}
	for i, code := range codes {
		article := fmt.Sprintf("DEMO-%03d", i+1)
		name := fmt.Sprintf("Демонстрационный товар %d", i+1)
		var multiple any = 10
		if i == 2 {
			multiple = nil
		}
		tables["moq"] = append(tables["moq"], []any{code, article, multiple})
		stock := []any{code}
		sales := []any{code, article}
		for _, m := range months {
			qty := 100.0
			if m == "2026-09" {
				qty = 60
			}
			if i == 3 && m == "2026-05" {
				qty += 420
			}
			sales = append(sales, qty)
			stock = append(stock, 50)
			if m >= "2025-01" {
				// All detailed sums agree with their corresponding monthly cells.
				count := 10
				if m == "2026-09" {
					count = 6
				}
				for j := range count {
					tables["sales_transactions"] = append(tables["sales_transactions"], []any{m + "-01", fmt.Sprintf("%s-%s-%d", code, m, j), "Реализация", code, name, "Основной", 10})
				}
				if i == 3 && m == "2026-05" {
					tables["sales_transactions"] = append(tables["sales_transactions"], []any{m + "-02", "PROJECT-001", "Реализация", code, name, "Основной", 420})
				}
			}
		}
		tables["monthly_stock"] = append(tables["monthly_stock"], stock)
		tables["monthly_sales"] = append(tables["monthly_sales"], sales)
		free := 10
		if i == 1 {
			free = 1000
		}
		tables["in_transit"] = append(tables["in_transit"], []any{article, code, name, "A", 1000, 0, 0, 0, free, 0, free, 0, 20})
	}
	for m := 1; m <= 12; m++ {
		tables["seasonality"] = append(tables["seasonality"], []any{m, 1})
	}
	tables["moq"] = append(tables["moq"], []any{"Итого"})
	out := map[string][]byte{}
	for field, rows := range tables {
		data, err := Workbook(rows)
		if err != nil {
			return nil, err
		}
		out[field] = data
	}
	return out, nil
}

func Workbook(rows [][]any) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	for i, row := range rows {
		addr, err := excelize.CoordinatesToCellName(1, i+1)
		if err != nil {
			return nil, err
		}
		if err := f.SetSheetRow("Sheet1", addr, &row); err != nil {
			return nil, err
		}
	}
	buffer, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
