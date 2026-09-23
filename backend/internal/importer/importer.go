package importer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/anomaly"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/xuri/excelize/v2"
)

const MaxFileBytes int64 = 20 << 20
const maxRows = 250000

var Fields = []string{"moq", "sales_transactions", "monthly_stock", "monthly_sales", "seasonality", "in_transit"}

type ValidationError struct{ Diagnostics domain.ImportDiagnostics }

func (e *ValidationError) Error() string {
	return "Некорректные XLSX-файлы: см. diagnostics."
}

// File names and worksheet names may vary. A required worksheet is located by
// its complete header signature, scanning the first 30 rows of every sheet.
var schemas = map[string]map[string][]string{
	"moq":                {"code": {"номенклатура.код", "номенклатура. код"}, "article": {"артикул"}, "multiple": {"кратность"}},
	"sales_transactions": {"date": {"дата"}, "number": {"номер"}, "document": {"документ"}, "code": {"код"}, "name": {"номенклатура"}, "warehouse": {"склад"}, "quantity": {"количество"}},
	"monthly_stock":      {"code": {"номенклатура.код", "номенклатура. код"}},
	"monthly_sales":      {"code": {"номенклатура.код", "номенклатура. код"}, "article": {"артикул"}},
	"seasonality":        {"month": {"месяц", "месяцы"}, "factor": {"общий коэффициент сезонности", "коэффициент сезонности", "сезонность", "общий коэффициент"}},
	"in_transit":         {"article": {"артикул поставщика"}, "code": {"код 1с", "код 1c"}, "name": {"наименование"}, "category": {"категория 2026"}, "unitCost": {"сс реал"}, "showcaseStock": {"витрина"}, "tzStock": {"остаток тз"}, "retailStock": {"розничный склад"}, "totalStock": {"остаток"}, "reservedStock": {"зарезервировано"}, "freeStock": {"свободный остаток"}, "order": {"заказ"}, "inTransit": {"сэ в пути 24.09"}},
}

type header struct {
	sheet          string
	row            int
	columns        map[string]int
	months         map[int]string
	date1904       bool
	transitColumns []int
}

func Import(ctx context.Context, files map[string]io.Reader) (*domain.Dataset, error) {
	return ImportSupplier(ctx, files, domain.Supplier)
}

func ImportSupplier(ctx context.Context, files map[string]io.Reader, supplier string) (*domain.Dataset, error) {
	if supplier != domain.Supplier && supplier != "IEK" {
		return nil, &ValidationError{Diagnostics: domain.ImportDiagnostics{Errors: []domain.Diagnostic{{Code: "INVALID_SUPPLIER", Message: "supplier: SystemElectric или IEK"}}}}
	}
	d := &domain.Dataset{Supplier: supplier, SourceFiles: map[string]string{}, AsOf: domain.DatasetDate(), Products: map[string]*domain.Product{}, Seasonality: map[int]float64{}, Diagnostics: domain.ImportDiagnostics{ProcessedRows: map[string]int{}, SkippedTotalRows: map[string]int{}, ProductsWithoutMOQ: []string{}, ProductsWithoutSales: []string{}, ProductsWithoutCurrentStock: []string{}, SourceConflicts: []domain.Diagnostic{}, Warnings: []domain.Diagnostic{}, Errors: []domain.Diagnostic{}}}
	d.Diagnostics.Warnings = append(d.Diagnostics.Warnings, domain.Diagnostic{Severity: "INFO", Code: "PARTIAL_CURRENT_MONTH", Message: "Данные на " + d.AsOf.Format("02.01.2006") + ": текущий месяц неполный. Его включение в расчёт определяется параметром excludePartialMonth."})
	fatal := false
	for _, field := range Fields {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d.Diagnostics.ProcessedRows[field] = 0
		d.Diagnostics.SkippedTotalRows[field] = 0
		r := files[field]
		if r == nil {
			fatal = true
			d.Diagnostics.Errors = append(d.Diagnostics.Errors, domain.Diagnostic{Code: "MISSING_FILE", Message: "Отсутствует обязательный файл.", File: field})
			continue
		}
		if err := readFile(ctx, d, field, r); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			fatal = true
			d.Diagnostics.Errors = append(d.Diagnostics.Errors, domain.Diagnostic{Code: "INVALID_WORKBOOK", Message: err.Error(), File: field})
		}
	}
	if len(d.Products) == 0 {
		fatal = true
		d.Diagnostics.Errors = append(d.Diagnostics.Errors, domain.Diagnostic{Code: "NO_PRODUCTS", Message: "Не найдено ни одного товара."})
	}
	finishDiagnostics(d)
	if fatal {
		return nil, &ValidationError{Diagnostics: d.Diagnostics}
	}
	return d, nil
}

func readFile(ctx context.Context, d *domain.Dataset, field string, r io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
	if err != nil {
		return errors.New("Не удалось прочитать файл.")
	}
	if int64(len(data)) > MaxFileBytes {
		return errors.New("Файл превышает лимит 20 MiB.")
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 128 << 20, UnzipXMLSizeLimit: 8 << 20})
	if err != nil {
		return errors.New("Не удалось открыть XLSX или превышен лимит распаковки 128 MiB.")
	}
	defer f.Close()
	h, err := findHeader(ctx, f, field, d.Supplier)
	if err != nil {
		return err
	}
	rows, err := f.Rows(h.sheet)
	if err != nil {
		return errors.New("Не удалось прочитать лист.")
	}
	defer rows.Close()
	// Read numeric cells without display formatting (e.g. 1,220.00) while
	// retaining displayed codes, including their leading-zero number formats.
	rawRows, err := f.Rows(h.sheet)
	if err != nil {
		return errors.New("Не удалось прочитать значения листа.")
	}
	defer rawRows.Close()
	seen := map[string]bool{}
	rowNumber := 0
	for rows.Next() {
		if !rawRows.Next() {
			return errors.New("Не удалось прочитать значения строки.")
		}
		rowNumber++
		if rowNumber > maxRows {
			return fmt.Errorf("Лист превышает лимит %d строк.", maxRows)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if rowNumber <= h.row {
			continue
		}
		row, err := rows.Columns()
		if err != nil {
			return errors.New("Не удалось прочитать строку листа.")
		}
		raw, err := rawRows.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return errors.New("Не удалось прочитать значения строки.")
		}
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		if totalRow(row) {
			d.Diagnostics.SkippedTotalRows[field]++
			if field == "seasonality" && len(d.Seasonality) > 0 {
				break
			}
			continue
		}
		// Monthly reports have one or two descriptive rows below the header.
		if (field == "monthly_sales" || field == "monthly_stock") && cell(row, h.columns["code"]) == "" && monthlySubheader(row, h.months) {
			continue
		}
		d.Diagnostics.ProcessedRows[field]++
		issueMonth := ""
		issue := func(code, message, sku string, blocking bool) domain.Diagnostic {
			return domain.Diagnostic{Code: code, Message: message, File: field, Sheet: h.sheet, Row: rowNumber, Code1C: sku, Month: issueMonth, Blocking: blocking}
		}
		if field == "seasonality" {
			m := cell(row, h.columns["month"])
			month := monthNumber(m)
			if month == 0 {
				if key := monthKey(m, 0, h.date1904); key != "" {
					month, _ = strconv.Atoi(key[5:])
				}
			}
			value, ok, err := number(cell(raw, h.columns["factor"]))
			if month == 0 || err != nil || !ok || value <= 0 || value > 10 {
				d.Diagnostics.Errors = append(d.Diagnostics.Errors, issue("INVALID_SEASONALITY", "Некорректный месяц или коэффициент (допустимо 0 < k ≤ 10).", "", false))
				continue
			}
			if _, exists := d.Seasonality[month]; exists {
				d.Diagnostics.Warnings = append(d.Diagnostics.Warnings, issue("DUPLICATE_SEASONALITY", "Повторный месяц сезонности пропущен; сохранена первая запись.", "", false))
				continue
			}
			d.Seasonality[month] = value
			continue
		}
		// Codes are never parsed numerically: formatted leading zeros and '_' survive.
		code := cell(row, h.columns["code"])
		if code == "" {
			d.Diagnostics.Errors = append(d.Diagnostics.Errors, issue("MISSING_CODE", "Строка без кода 1С пропущена.", "", false))
			continue
		}
		p := d.Products[code]
		if p == nil {
			p = &domain.Product{Code1C: code, Supplier: d.Supplier, MonthlySales: map[string]float64{}, MonthlyStock: map[string]*float64{}, BlankSalesMonths: []string{}, Transactions: []domain.Transaction{}, Sources: map[string]bool{}, PresentFields: map[string]bool{}, Warnings: []domain.Diagnostic{}}
			d.Products[code] = p
		}
		p.Sources[field] = true
		if seen[code] && (field == "monthly_sales" || field == "monthly_stock") {
			d.Diagnostics.Warnings = append(d.Diagnostics.Warnings, issue("MONTHLY_ROWS_MERGED", "Повторные месячные строки объединены; неизвестная часть суммы сохраняется как null.", code, false))
		}
		if seen[code] && field != "sales_transactions" && field != "monthly_sales" && field != "monthly_stock" {
			w := issue("DUPLICATE_PRODUCT_ROW", "Повторная строка товара пропущена; требуется проверка, значения не суммируются.", code, true)
			d.Diagnostics.Errors = append(d.Diagnostics.Errors, w)
			p.Warnings = append(p.Warnings, w)
			continue
		}
		seen[code] = true
		warn := func(codeName, message string, blocking bool) {
			w := issue(codeName, message, code, blocking)
			p.Warnings = append(p.Warnings, w)
			d.Diagnostics.Warnings = append(d.Diagnostics.Warnings, w)
		}
		bad := func(message string) {
			w := issue("INVALID_CELL", message, code, true)
			p.Warnings = append(p.Warnings, w)
			d.Diagnostics.Errors = append(d.Diagnostics.Errors, w)
		}
		for _, attr := range []struct {
			key    string
			target *string
		}{{"article", &p.Article}, {"name", &p.Name}, {"category", &p.Category}} {
			col, exists := h.columns[attr.key]
			if !exists {
				continue
			}
			value := cell(row, col)
			if value != "" {
				if *attr.target != "" && *attr.target != value && attr.key == "article" {
					warn("ARTICLE_CONFLICT", "Артикулы для одного кода 1С различаются.", true)
				}
				*attr.target = value
			}
		}
		switch field {
		case "moq":
			v, ok, err := number(cell(raw, h.columns["multiple"]))
			if err != nil || (ok && (v <= 0 || v != math.Trunc(v) || v > 1e9)) {
				bad("Некорректная кратность поставки.")
			} else if ok {
				p.OrderMultiple = int(v)
			}
		case "monthly_sales", "monthly_stock":
			cols := make([]int, 0, len(h.months))
			for c := range h.months {
				cols = append(cols, c)
			}
			sort.Ints(cols)
			for _, col := range cols {
				month := h.months[col]
				issueMonth = month
				v, ok, err := number(cell(raw, col))
				if err != nil {
					bad("Некорректное число за " + month + ".")
					ok = false
				}
				if field == "monthly_sales" {
					if ok {
						if !containsMonth(p.BlankSalesMonths, month) {
							p.MonthlySales[month] += v
						}
					} else {
						delete(p.MonthlySales, month)
						if !containsMonth(p.BlankSalesMonths, month) {
							p.BlankSalesMonths = append(p.BlankSalesMonths, month)
						}
					}
				} else {
					old, exists := p.MonthlyStock[month]
					if !ok || (exists && old == nil) {
						p.MonthlyStock[month] = nil
					} else {
						if old != nil {
							v += *old
						}
						p.MonthlyStock[month] = &v
					}
				}
			}
		case "sales_transactions":
			date, err := parseDate(cell(row, h.columns["date"]), h.date1904)
			if err != nil {
				// Date display formats are arbitrary in Excel; fall back to the
				// raw serial value while keeping codes in their displayed format.
				date, err = parseDate(cell(raw, h.columns["date"]), h.date1904)
			}
			if err != nil || !date.Before(d.AsOf.AddDate(0, 0, 1)) {
				bad("Некорректная дата операции или дата позже среза 22.09.2026.")
				continue
			}
			issueMonth = date.Format("2006-01")
			quantity, ok, err := number(cell(raw, h.columns["quantity"]))
			if err != nil {
				bad("Некорректное количество операции; передаётся как null.")
				quantity, ok = 0, false
			} else if !ok {
				warn("UNKNOWN_TRANSACTION_QUANTITY", "Количество операции отсутствует; передаётся как null.", true)
			}
			doc := cell(row, h.columns["number"])
			if doc == "" {
				doc = cell(row, h.columns["document"])
			}
			if doc == "" {
				warn("MISSING_DOCUMENT_ID", "Операция без документа сохранена, но не участвует в поиске аномалий.", true)
			}
			p.Transactions = append(p.Transactions, domain.Transaction{Date: date, DocumentID: doc, Warehouse: cell(row, h.columns["warehouse"]), Quantity: quantity, QuantityMissing: !ok})
		case "in_transit":
			if len(h.transitColumns) > 0 {
				// A blank shipment cell is unknown. Preserve that uncertainty for
				// the aggregate instead of silently converting it to zero.
				total, complete := 0.0, true
				for _, col := range h.transitColumns {
					v, ok, err := number(cell(raw, col))
					if err != nil {
						bad("Некорректное количество партии в пути.")
					}
					if !ok || err != nil {
						complete = false
					} else {
						total += v
					}
				}
				if complete {
					p.InTransit = total
					p.PresentFields["inTransit"] = true
				} else {
					warn("INCOMPLETE_TRANSIT", fmt.Sprintf("Общий товар в пути неизвестен: есть пустые партии. Сумма известных партий: %g.", total), true)
				}
			}
			for _, attr := range []struct {
				key    string
				target *float64
			}{{"showcaseStock", &p.ShowcaseStock}, {"tzStock", &p.TZStock}, {"retailStock", &p.RetailStock}, {"totalStock", &p.TotalStock}, {"reservedStock", &p.ReservedStock}, {"freeStock", &p.FreeStock}, {"inTransit", &p.InTransit}} {
				col, exists := h.columns[attr.key]
				if !exists {
					continue
				}
				v, ok, err := number(cell(raw, col))
				if err != nil {
					bad("Некорректный остаток: " + attr.key)
					continue
				}
				if ok {
					*attr.target = v
					p.PresentFields[attr.key] = true
				}
			}
			col, exists := h.columns["unitCost"]
			if !exists {
				continue
			}
			v, ok, err := number(cell(raw, col))
			if err != nil || (ok && v < 0) {
				warn("INVALID_UNIT_COST", "Некорректная себестоимость; оценка стоимости недоступна.", false)
			} else if ok {
				p.UnitCost = &v
			}
		}
	}
	if err := rows.Error(); err != nil {
		return errors.New("Ошибка чтения листа.")
	}
	if err := rawRows.Error(); err != nil {
		return errors.New("Ошибка чтения значений листа.")
	}
	return nil
}

func findHeader(ctx context.Context, f *excelize.File, field string, supplier string) (header, error) {
	schema := schemas[field]
	if supplier == "IEK" {
		switch field {
		case "moq":
			schema = map[string][]string{"code": {"код 1с", "код 1c"}, "article": {"артикул поставщика"}, "name": {"наименование"}, "multiple": {"мин. разр. к отгр."}}
		case "monthly_sales":
			schema = schemas["monthly_stock"]
		case "in_transit":
			schema = map[string][]string{"code": {"код 1с", "код 1c"}, "article": {"артикул иэк"}, "name": {"наименование"}}
		}
	}
	props, err := f.GetWorkbookProps()
	if err != nil {
		return header{}, errors.New("Не удалось прочитать свойства книги.")
	}
	date1904 := props.Date1904 != nil && *props.Date1904
	for _, sheet := range f.GetSheetList() {
		if err := ctx.Err(); err != nil {
			return header{}, err
		}
		rows, err := f.Rows(sheet)
		if err != nil {
			continue
		}
		preview := [][]string{}
		for len(preview) < 30 && rows.Next() {
			row, e := rows.Columns()
			if e != nil {
				break
			}
			preview = append(preview, row)
		}
		rows.Close()
		for i, row := range preview {
			columns := map[string]int{}
			for c, value := range row {
				for key, aliases := range schema {
					for _, alias := range aliases {
						if normalize(value) == alias {
							columns[key] = c
						}
					}
				}
			}
			if len(columns) != len(schema) {
				continue
			}
			h := header{sheet: sheet, row: i + 1, columns: columns, months: map[int]string{}, date1904: date1904}
			if supplier == "IEK" && field == "in_transit" {
				for c, v := range row {
					if strings.Contains(normalize(v), "поступление до") {
						h.transitColumns = append(h.transitColumns, c)
					}
				}
				if len(h.transitColumns) == 0 {
					continue
				}
			}
			if field == "monthly_sales" || field == "monthly_stock" {
				// Support a single header row, or merged year headers above months.
				monthRow := i
				for attempt := 0; attempt < 2 && monthRow < len(preview); attempt++ {
					yearHint := 0
					months := map[int]string{}
					duplicate := false
					used := map[string]bool{}
					for c, value := range preview[monthRow] {
						if monthRow > 0 {
							above := cell(preview[monthRow-1], c)
							if y, e := strconv.Atoi(above); e == nil && y >= 2000 && y <= 2100 {
								yearHint = y
							}
						}
						key := monthKey(value, yearHint, date1904)
						if key == "" {
							address, _ := excelize.CoordinatesToCellName(c+1, monthRow+1)
							if raw, e := f.GetCellValue(sheet, address, excelize.Options{RawCellValue: true}); e == nil {
								key = monthKey(raw, yearHint, date1904)
							}
						}
						if key != "" {
							if used[key] {
								duplicate = true
							}
							used[key] = true
							months[c] = key
						}
					}
					if len(months) > 0 {
						if duplicate {
							return header{}, errors.New("Найдены повторные колонки одного месяца.")
						}
						h.months = months
						h.row = monthRow + 1
						break
					}
					monthRow++
				}
				if len(h.months) == 0 {
					continue
				}
			}
			return h, nil
		}
	}
	return header{}, errors.New("Не найден лист со всеми обязательными заголовками (поиск в первых 30 строках).")
}

func finishDiagnostics(d *domain.Dataset) {
	codes := make([]string, 0, len(d.Products))
	for code := range d.Products {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		p := d.Products[code]
		if p.OrderMultiple <= 0 {
			d.Diagnostics.ProductsWithoutMOQ = append(d.Diagnostics.ProductsWithoutMOQ, code)
		}
		hasSales := false
		for m := range p.MonthlySales {
			if m < d.AsOf.Format("2006-01") {
				hasSales = true
				break
			}
		}
		if !hasSales {
			d.Diagnostics.ProductsWithoutSales = append(d.Diagnostics.ProductsWithoutSales, code)
		}
		if !p.PresentFields["freeStock"] {
			d.Diagnostics.ProductsWithoutCurrentStock = append(d.Diagnostics.ProductsWithoutCurrentStock, code)
		}
		matched := true
		for _, field := range Fields {
			if field != "seasonality" && !p.Sources[field] {
				matched = false
			}
		}
		if matched {
			d.Diagnostics.FullyMatchedProducts++
		}
		conflicts := anomaly.Reconcile(p, d.AsOf)
		d.Diagnostics.SourceConflicts = append(d.Diagnostics.SourceConflicts, conflicts...)
		d.Diagnostics.Warnings = append(d.Diagnostics.Warnings, conflicts...)
	}
	d.Diagnostics.UniqueProducts = len(d.Products)
}
