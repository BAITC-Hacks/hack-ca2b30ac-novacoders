package httptransport

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/importer"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/recommendation"
)

var uploadFields = map[string]string{"moq": "moqFile", "monthly_sales": "monthlySalesFile", "sales_transactions": "detailedSalesFile", "monthly_stock": "monthlyStockFile", "seasonality": "seasonalityFile", "in_transit": "inventoryTransitFile"}
var fileTypes = map[string]string{"moq": "MOQ", "monthly_sales": "MONTHLY_SALES", "sales_transactions": "DETAILED_SALES", "monthly_stock": "MONTHLY_STOCK", "seasonality": "SEASONALITY", "in_transit": "INVENTORY_TRANSIT"}

func uploadField(field string, canonical bool) string {
	if canonical {
		return uploadFields[field]
	}
	return field
}

type importFileResult struct {
	Type     string `json:"type"`
	FileName string `json:"fileName"`
	Status   string `json:"status"`
	Rows     int    `json:"rows"`
}
type importSummary struct {
	ProductsFound        int `json:"productsFound"`
	ProductsReady        int `json:"productsReady"`
	ProductsWithWarnings int `json:"productsWithWarnings"`
}
type importDTO struct {
	ImportID string              `json:"importId"`
	Status   string              `json:"status"`
	Supplier string              `json:"supplier"`
	AsOf     string              `json:"asOf"`
	Files    []importFileResult  `json:"files"`
	Summary  importSummary       `json:"summary"`
	Warnings []domain.Diagnostic `json:"warnings"`
}

func normalizeWarning(w domain.Diagnostic) domain.Diagnostic {
	w.Severity = strings.ToUpper(w.Severity)
	if w.Severity != "INFO" && w.Severity != "WARNING" && w.Severity != "ERROR" {
		w.Severity = "WARNING"
		if w.Blocking {
			w.Severity = "ERROR"
		}
	}
	return w
}
func importResult(d *domain.Dataset) importDTO {
	out := importDTO{ImportID: d.ID, Status: "READY", Supplier: d.Supplier, AsOf: d.AsOf.Format("2006-01-02"), Files: []importFileResult{}, Warnings: []domain.Diagnostic{}}
	affected, files := map[string]bool{}, map[string]bool{}
	seen := map[string]bool{}
	add := func(w domain.Diagnostic) {
		w = normalizeWarning(w)
		b, _ := json.Marshal(w)
		if seen[string(b)] {
			return
		}
		seen[string(b)] = true
		out.Warnings = append(out.Warnings, w)
		if w.Code1C != "" {
			affected[w.Code1C] = true
		}
		if w.File != "" {
			files[w.File] = true
		}
	}
	for _, list := range [][]domain.Diagnostic{d.Diagnostics.Warnings, d.Diagnostics.Errors} {
		for _, w := range list {
			add(w)
		}
	}
	for code, p := range d.Products {
		for _, w := range p.Warnings {
			add(w)
		}
		for _, check := range []struct {
			missing       bool
			code, message string
		}{
			{p.OrderMultiple <= 0, "UNKNOWN_MOQ", "Кратность поставки неизвестна."},
			{!p.PresentFields["freeStock"], "UNKNOWN_FREE_STOCK", "Текущий свободный остаток неизвестен."},
			{!p.PresentFields["inTransit"], "UNKNOWN_IN_TRANSIT", "Общий объём товара в пути неизвестен."},
			{p.UnitCost == nil, "UNKNOWN_UNIT_COST", "Себестоимость неизвестна."},
		} {
			if check.missing {
				add(domain.Diagnostic{Code: check.code, Message: check.message, Code1C: code})
			}
		}
		for _, field := range importer.Fields {
			if field != "seasonality" && !p.Sources[field] {
				affected[code] = true
			}
		}
	}
	for _, code := range d.Diagnostics.ProductsWithoutSales {
		affected[code] = true
	}
	for _, field := range importer.Fields {
		status := "VALID"
		if files[field] {
			status = "WARNING"
		}
		out.Files = append(out.Files, importFileResult{fileTypes[field], d.SourceFiles[field], status, d.Diagnostics.ProcessedRows[field]})
	}
	out.Summary = importSummary{ProductsFound: len(d.Products), ProductsReady: len(d.Products) - len(affected), ProductsWithWarnings: len(affected)}
	return out
}

type orderSummary struct {
	Positions     int      `json:"positions"`
	TotalUnits    float64  `json:"totalUnits"`
	EstimatedCost *float64 `json:"estimatedCost"`
}
type runDTO struct {
	RunID        string                         `json:"runId"`
	ImportID     string                         `json:"importId"`
	Supplier     string                         `json:"supplier"`
	AsOf         string                         `json:"asOf"`
	CreatedAt    time.Time                      `json:"createdAt"`
	Status       string                         `json:"status"`
	Settings     *domain.RecommendationSettings `json:"settings"`
	Summary      domain.Summary                 `json:"summary"`
	OrderSummary orderSummary                   `json:"orderSummary"`
	Items        []domain.Item                  `json:"items"`
	Warnings     []domain.Diagnostic            `json:"warnings"`
	Providers    json.RawMessage                `json:"providers"`
}

func runResult(r *domain.CalculationRun) runDTO {
	out := runDTO{RunID: r.ID, ImportID: r.DatasetID, Supplier: r.Config.Supplier, AsOf: r.AsOf.Format("2006-01-02"), CreatedAt: r.CreatedAt, Status: "COMPLETED", Settings: r.Config.Settings, Summary: r.Summary, Items: r.Items, Warnings: []domain.Diagnostic{}, Providers: json.RawMessage(`{}`)}
	var ai struct {
		Status    string              `json:"status"`
		Warnings  []domain.Diagnostic `json:"warnings"`
		Providers json.RawMessage     `json:"providers"`
	}
	if json.Unmarshal(r.AIResponse, &ai) == nil {
		out.Status = ai.Status
		if len(ai.Providers) > 0 {
			out.Providers = ai.Providers
		}
		for _, w := range ai.Warnings {
			out.Warnings = append(out.Warnings, normalizeWarning(w))
		}
	}
	cost, unpriced := 0.0, false
	for n := range out.Items {
		i := &out.Items[n]
		for k, w := range i.Warnings {
			i.Warnings[k] = normalizeWarning(w)
		}
		switch i.AnomalyDecision {
		case "EXCLUDE":
			i.AnomalyDecision = "EXCLUDED"
		case "KEEP":
			i.AnomalyDecision = "INCLUDED"
		default:
			i.AnomalyDecision = "PENDING_REVIEW"
		}
		if len(i.Warnings) > 0 {
			out.Status = "COMPLETED_WITH_WARNINGS"
		}
		if i.Approved && i.ApprovedQuantity != nil && *i.ApprovedQuantity > 0 {
			out.OrderSummary.Positions++
			out.OrderSummary.TotalUnits += *i.ApprovedQuantity
			if i.EstimatedCost == nil {
				unpriced = true
			} else {
				cost += *i.EstimatedCost
			}
		}
	}
	if !unpriced {
		cost = math.Round(cost*100) / 100
		out.OrderSummary.EstimatedCost = &cost
	}
	return out
}

func (a *API) createRecommendations(w http.ResponseWriter, r *http.Request, id string) {
	var params struct {
		ForecastHorizonMonths *int  `json:"forecastHorizonMonths"`
		LeadTimeDays          *int  `json:"leadTimeDays"`
		SafetyStockDays       *int  `json:"safetyStockDays"`
		ExcludePartialMonth   *bool `json:"excludePartialMonth"`
	}
	if !decode(w, r, &params) {
		return
	}
	if params.ForecastHorizonMonths == nil || params.LeadTimeDays == nil || params.SafetyStockDays == nil || params.ExcludePartialMonth == nil {
		jsonError(w, 400, "INVALID_INPUT", "Укажите forecastHorizonMonths, leadTimeDays, safetyStockDays и excludePartialMonth.", nil)
		return
	}
	d, err := a.service.Store.Dataset(id)
	if err != nil {
		handleError(w, err)
		return
	}
	settings, _ := recommendation.Settings(domain.RunConfig{})
	settings.ForecastHorizonMonths = *params.ForecastHorizonMonths
	settings.LeadTimeDays = *params.LeadTimeDays
	settings.SafetyStockDays = *params.SafetyStockDays
	settings.ExcludePartialMonth = *params.ExcludePartialMonth
	if a.service.Recommender == nil {
		jsonError(w, 503, "AI_SERVICE_UNAVAILABLE", "AI Service не настроен.", nil)
		return
	}
	run, err := a.service.CreateRun(r.Context(), domain.RunConfig{DatasetID: id, Supplier: d.Supplier, Settings: &settings})
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, 201, runResult(run))
}
func (a *API) patchRecommendation(w http.ResponseWriter, r *http.Request, id, code string) {
	var patch domain.ManagerPatch
	if !decode(w, r, &patch) {
		return
	}
	if patch.AnomalyDecision != nil {
		mapped, ok := map[string]string{"EXCLUDED": "EXCLUDE", "INCLUDED": "KEEP", "PENDING_REVIEW": ""}[*patch.AnomalyDecision]
		if !ok {
			jsonError(w, 400, "INVALID_INPUT", "anomalyDecision: EXCLUDED, INCLUDED или PENDING_REVIEW.", nil)
			return
		}
		patch.AnomalyDecision = &mapped
	}
	// Entering a draft quantity must not silently approve a REVIEW item.
	if patch.ApprovedQuantity != nil && patch.Approved == nil {
		v := false
		patch.Approved = &v
	}
	run, err := a.service.Patch(r.Context(), id, code, patch)
	if err != nil {
		handleError(w, err)
		return
	}
	for _, item := range run.Items {
		if item.Code1C == code {
			writeJSON(w, 200, struct {
				Code1C           string    `json:"code1C"`
				ApprovedQuantity *float64  `json:"approvedQuantity"`
				Approved         bool      `json:"approved"`
				UpdatedAt        time.Time `json:"updatedAt"`
			}{item.Code1C, item.ApprovedQuantity, item.Approved, item.UpdatedAt})
			return
		}
	}
}
