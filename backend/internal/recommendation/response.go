package recommendation

import (
	"fmt"
	"math"
	"time"
)

// The complete upstream JSON is retained separately, including explanation,
// provider details and future additive fields. Typed fields validate quantities
// and map recommendations to the existing frontend/approval API.
type Response struct {
	SchemaVersion    string           `json:"schemaVersion"`
	RequestID        string           `json:"requestId"`
	RunID            string           `json:"runId"`
	Status           string           `json:"status"`
	GeneratedAt      time.Time        `json:"generatedAt"`
	ProcessingTimeMs int64            `json:"processingTimeMs"`
	Recommendations  []Recommendation `json:"recommendations"`
}
type Warning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}
type Recommendation struct {
	Code1C               string       `json:"code1C"`
	Article              string       `json:"article"`
	Name                 string       `json:"name"`
	Supplier             string       `json:"supplier"`
	Action               string       `json:"action"`
	Urgency              string       `json:"urgency"`
	Confidence           *float64     `json:"confidence"`
	RequiresManualReview bool         `json:"requiresManualReview"`
	RecommendedQuantity  *float64     `json:"recommendedQuantity"`
	EstimatedUnitCost    *float64     `json:"estimatedUnitCost"`
	EstimatedCost        *float64     `json:"estimatedCost"`
	Calculation          *Calculation `json:"calculation"`
	AnomalyAnalysis      struct {
		Status     string      `json:"status"`
		Candidates []Candidate `json:"candidates"`
	} `json:"anomalyAnalysis"`
	Warnings []Warning `json:"warnings"`
}
type Calculation struct {
	HistoryPeriod struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"historyPeriod"`
	BaseMonthlyDemand     float64  `json:"baseMonthlyDemand"`
	StockoutCompensation  float64  `json:"stockoutCompensation"`
	GrowthFactor          float64  `json:"growthFactor"`
	SeasonalityFactor     float64  `json:"seasonalityFactor"`
	ForecastHorizonMonths float64  `json:"forecastHorizonMonths"`
	ForecastDemand        float64  `json:"forecastDemand"`
	SafetyStock           float64  `json:"safetyStock"`
	FreeStock             *float64 `json:"freeStock"`
	InTransit             *float64 `json:"inTransit"`
	AvailableStock        *float64 `json:"availableStock"`
	RawRequirement        float64  `json:"rawRequirement"`
	MOQ                   *float64 `json:"moq"`
	RoundedRequirement    *float64 `json:"roundedRequirement"`
}
type Candidate struct {
	TransactionID             string  `json:"transactionId"`
	Date                      string  `json:"date"`
	Quantity                  float64 `json:"quantity"`
	MedianTransactionQuantity float64 `json:"medianTransactionQuantity"`
	NvidiaVerdict             string  `json:"nvidiaVerdict"`
	SystemDecision            string  `json:"systemDecision"`
}

func (r *Response) Validate(input *Request) error {
	if r.SchemaVersion != "1.0" || r.RequestID != input.RequestID || r.RunID == "" || r.GeneratedAt.IsZero() || r.ProcessingTimeMs < 0 {
		return fmt.Errorf("Не совпадает версия, requestId или отсутствует метаинформация ответа AI Service.")
	}
	if r.Status != "COMPLETED" && r.Status != "COMPLETED_WITH_WARNINGS" {
		return fmt.Errorf("AI Service не завершил расчёт успешно (status=%s).", r.Status)
	}
	expected := map[string]bool{}
	for _, p := range input.Products {
		expected[p.Code1C] = true
	}
	seen := map[string]bool{}
	for _, item := range r.Recommendations {
		if !expected[item.Code1C] || seen[item.Code1C] {
			return fmt.Errorf("AI Service вернул неизвестный или повторный code1C.")
		}
		seen[item.Code1C] = true
		if item.Action != "BUY" && item.Action != "NO_BUY" && item.Action != "REVIEW" {
			return fmt.Errorf("Неизвестное action в ответе AI Service.")
		}
		if item.Urgency != "HIGH" && item.Urgency != "MEDIUM" && item.Urgency != "LOW" && item.Urgency != "NONE" {
			return fmt.Errorf("Неизвестное urgency в ответе AI Service.")
		}
		if item.RecommendedQuantity == nil || *item.RecommendedQuantity < 0 || *item.RecommendedQuantity > 1e12 || math.Trunc(*item.RecommendedQuantity) != *item.RecommendedQuantity || item.Calculation == nil {
			return fmt.Errorf("Отсутствует calculation или некорректно recommendedQuantity в ответе AI Service.")
		}
		if item.Confidence != nil && (*item.Confidence < 0 || *item.Confidence > 1) {
			return fmt.Errorf("confidence вне диапазона 0–1.")
		}
		for _, cost := range []*float64{item.EstimatedUnitCost, item.EstimatedCost} {
			if cost != nil && (*cost < 0 || *cost > 1e24) {
				return fmt.Errorf("Некорректная стоимость в ответе AI Service.")
			}
		}
		for _, a := range item.AnomalyAnalysis.Candidates {
			if _, err := time.Parse("2006-01-02", a.Date); err != nil {
				return fmt.Errorf("Некорректная дата кандидата на аномалию.")
			}
		}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("AI Service вернул неполный список рекомендаций: %d из %d.", len(seen), len(expected))
	}
	return nil
}
