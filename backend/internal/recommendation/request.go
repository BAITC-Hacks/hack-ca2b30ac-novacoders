// Package recommendation implements only the HTTP contract with the separate
// calculation service. It contains no LLM clients or demand forecasting.
package recommendation

import (
	"crypto/rand"
	"fmt"
	"sort"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
)

type Source struct {
	FileName string `json:"fileName"`
	RowCount int    `json:"rowCount"`
}
type Product struct {
	Code1C   string   `json:"code1C"`
	Article  string   `json:"article"`
	Name     string   `json:"name"`
	Supplier string   `json:"supplier"`
	Category string   `json:"category"`
	MOQ      *int     `json:"moq"`
	UnitCost *float64 `json:"unitCost"`
}
type Monthly struct {
	Code1C   string   `json:"code1C"`
	Month    string   `json:"month"`
	Quantity *float64 `json:"quantity"`
}
type Transaction struct {
	Code1C        string   `json:"code1C"`
	TransactionID string   `json:"transactionId"`
	Date          string   `json:"date"`
	Warehouse     string   `json:"warehouse"`
	Quantity      *float64 `json:"quantity"`
}
type Inventory struct {
	Code1C        string   `json:"code1C"`
	TotalStock    *float64 `json:"totalStock"`
	ReservedStock *float64 `json:"reservedStock"`
	FreeStock     *float64 `json:"freeStock"`
	InTransit     *float64 `json:"inTransit"`
}
type Seasonality struct {
	Month       int     `json:"month"`
	Coefficient float64 `json:"coefficient"`
}
type Request struct {
	SchemaVersion string                        `json:"schemaVersion"`
	RequestID     string                        `json:"requestId"`
	AsOfDate      string                        `json:"asOfDate"`
	Currency      string                        `json:"currency"`
	Settings      domain.RecommendationSettings `json:"settings"`
	SourceMeta    map[string]Source             `json:"sourceMeta"`
	Products      []Product                     `json:"products"`
	MonthlySales  []Monthly                     `json:"monthlySales"`
	MonthlyStock  []Monthly                     `json:"monthlyStock"`
	Transactions  []Transaction                 `json:"transactions"`
	Inventory     []Inventory                   `json:"inventory"`
	Seasonality   []Seasonality                 `json:"seasonality"`
}

func Settings(cfg domain.RunConfig) (domain.RecommendationSettings, error) {
	s := domain.RecommendationSettings{HistoryMonths: 12, ForecastHorizonMonths: 2, LeadTimeDays: 30, SafetyStockDays: 14, ExcludePartialMonth: true, AvailableStockPolicy: "FREE_PLUS_IN_TRANSIT", AnomalyReviewEnabled: true, ExplanationMode: "IMPORTANT_ONLY", MaxAIExplanations: 50}
	if cfg.Settings != nil {
		s = *cfg.Settings
	} else if cfg.LeadTimeDays != 0 || cfg.SafetyDays != 0 {
		if cfg.LeadTimeDays != 0 {
			s.LeadTimeDays = cfg.LeadTimeDays
		}
		s.SafetyStockDays = cfg.SafetyDays
	}
	if cfg.IncludeShowcase || cfg.IncludeTZStock || cfg.IncludeRetailStock {
		return s, fmt.Errorf("контракт v1 поддерживает только FREE_PLUS_IN_TRANSIT")
	}
	if cfg.Settings != nil && (cfg.LeadTimeDays != 0 || cfg.SafetyDays != 0) {
		return s, fmt.Errorf("задайте сроки в settings либо в leadTimeDays/safetyDays, без смешивания")
	}
	if s.HistoryMonths < 1 || s.HistoryMonths > 120 || s.ForecastHorizonMonths < 1 || s.ForecastHorizonMonths > 36 || s.LeadTimeDays < 1 || s.LeadTimeDays > 365 || s.SafetyStockDays < 0 || s.SafetyStockDays > 365 || s.AvailableStockPolicy != "FREE_PLUS_IN_TRANSIT" || s.ExplanationMode != "IMPORTANT_ONLY" || s.MaxAIExplanations < 0 || s.MaxAIExplanations > 1000 {
		return s, fmt.Errorf("некорректные settings: historyMonths 1–120, forecastHorizonMonths 1–36, leadTimeDays 1–365, safetyStockDays 0–365, maxAIExplanations 0–1000, availableStockPolicy FREE_PLUS_IN_TRANSIT, explanationMode IMPORTANT_ONLY")
	}
	return s, nil
}

func Build(d *domain.Dataset, cfg domain.RunConfig) (*Request, error) {
	settings, err := Settings(cfg)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	r := &Request{SchemaVersion: "1.0", RequestID: fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), AsOfDate: d.AsOf.Format("2006-01-02"), Currency: "KZT", Settings: settings, SourceMeta: map[string]Source{}, Products: []Product{}, MonthlySales: []Monthly{}, MonthlyStock: []Monthly{}, Transactions: []Transaction{}, Inventory: []Inventory{}, Seasonality: []Seasonality{}}
	for field, key := range map[string]string{"moq": "moq", "monthly_sales": "monthlySales", "sales_transactions": "detailedSales", "monthly_stock": "monthlyStock", "seasonality": "seasonality", "in_transit": "inventoryTransit"} {
		r.SourceMeta[key] = Source{FileName: d.SourceFiles[field], RowCount: d.Diagnostics.ProcessedRows[field]}
	}
	codes := sortedKeys(d.Products)
	for _, code := range codes {
		p := d.Products[code]
		if p.Supplier != cfg.Supplier {
			continue
		}
		var moq *int
		if p.OrderMultiple > 0 {
			n := p.OrderMultiple
			moq = &n
		}
		r.Products = append(r.Products, Product{code, p.Article, p.Name, p.Supplier, p.Category, moq, p.UnitCost})
		sales := map[string]*float64{}
		for m, q := range p.MonthlySales {
			sales[m] = ptr(q)
		}
		for _, m := range p.BlankSalesMonths {
			sales[m] = nil
		}
		for _, m := range sortedKeys(sales) {
			r.MonthlySales = append(r.MonthlySales, Monthly{code, m, sales[m]})
		}
		for _, m := range sortedKeys(p.MonthlyStock) {
			r.MonthlyStock = append(r.MonthlyStock, Monthly{code, m, p.MonthlyStock[m]})
		}
		for _, t := range p.Transactions {
			var q *float64
			if !t.QuantityMissing {
				q = ptr(t.Quantity)
			}
			r.Transactions = append(r.Transactions, Transaction{code, t.DocumentID, t.Date.Format("2006-01-02"), t.Warehouse, q})
		}
		known := func(key string, value float64) *float64 {
			if !p.PresentFields[key] {
				return nil
			}
			return ptr(value)
		}
		r.Inventory = append(r.Inventory, Inventory{code, known("totalStock", p.TotalStock), known("reservedStock", p.ReservedStock), known("freeStock", p.FreeStock), known("inTransit", p.InTransit)})
	}
	for m := 1; m <= 12; m++ {
		if k, ok := d.Seasonality[m]; ok {
			r.Seasonality = append(r.Seasonality, Seasonality{m, k})
		}
	}
	if len(r.Products) == 0 {
		return nil, fmt.Errorf("в dataset нет товаров поставщика %s", cfg.Supplier)
	}
	return r, nil
}
func ptr(v float64) *float64 { return &v }
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
