package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/forecast"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/recommendation"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/store"
)

func (s *Service) createRemoteRun(ctx context.Context, cfg domain.RunConfig) (*domain.CalculationRun, error) {
	if cfg.DatasetID == "" || (cfg.Supplier != domain.Supplier && cfg.Supplier != "IEK") {
		return nil, fmt.Errorf("%w: datasetId обязателен, supplier: SystemElectric или IEK", ErrInvalid)
	}
	d, err := s.Store.Dataset(cfg.DatasetID)
	if err != nil {
		return nil, err
	}
	req, err := recommendation.Build(d, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	cfg.Settings = &req.Settings.RecommendationSettings
	cfg.LeadTimeDays, cfg.SafetyDays = 0, 0
	// Do not hold the store lock while making a network request. No run is stored
	// on failure, and the uploaded dataset remains available for a retry.
	result, err := s.Recommender.Recommend(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	run := &domain.CalculationRun{DatasetID: d.ID, AsOf: d.AsOf, CreatedAt: time.Now().UTC(), Config: cfg, AIResponse: result.Raw, Items: []domain.Item{}}
	var raw struct {
		Recommendations []struct {
			Calculation     json.RawMessage `json:"calculation"`
			Explanation     json.RawMessage `json:"explanation"`
			AnomalyAnalysis json.RawMessage `json:"anomalyAnalysis"`
		} `json:"recommendations"`
	}
	if err := json.Unmarshal(result.Raw, &raw); err != nil {
		return nil, err
	}
	conflicts := map[string][]domain.Diagnostic{}
	for _, w := range d.Diagnostics.SourceConflicts {
		conflicts[w.Code1C] = append(conflicts[w.Code1C], w)
	}
	for n, r := range result.Response.Recommendations {
		p := d.Products[r.Code1C]
		item := domain.Item{Code1C: p.Code1C, Article: p.Article, Name: p.Name, Supplier: p.Supplier, Decision: r.Action, Urgency: r.Urgency, RecommendedQuantity: *r.RecommendedQuantity, FinalQuantity: *r.RecommendedQuantity, EstimatedCost: r.EstimatedCost, UnitCost: r.EstimatedUnitCost, RequiresManualReview: r.RequiresManualReview, Confidence: r.Confidence, Calculation: raw.Recommendations[n].Calculation, Explanation: raw.Recommendations[n].Explanation, AnomalyAnalysis: raw.Recommendations[n].AnomalyAnalysis, Anomalies: []domain.Anomaly{}}
		item.Warnings = historyWarnings(p, d.AsOf, req.Settings.RecommendationSettings, conflicts[p.Code1C])
		item.Category, item.UpdatedAt = p.Category, run.CreatedAt
		review := func(code, msg string) {
			item.Warnings = append(item.Warnings, domain.Diagnostic{Code: code, Message: msg, Blocking: true, Code1C: p.Code1C})
			item.RequiresManualReview = true
		}
		for _, w := range r.Warnings {
			item.Warnings = append(item.Warnings, domain.Diagnostic{Code: w.Code, Message: w.Message, Severity: w.Severity})
		}
		if !p.PresentFields["freeStock"] || !p.PresentFields["inTransit"] {
			review("UNKNOWN_INVENTORY", "Неизвестен свободный остаток или полный объём товара в пути.")
		}
		if p.OrderMultiple <= 0 {
			review("UNKNOWN_MOQ", "Кратность поставки неизвестна.")
		}
		if r.Calculation.RoundedRequirement == nil {
			review("AI_UNKNOWN_REQUIREMENT", "AI Service не указал округлённую потребность.")
		}
		if r.Calculation.FreeStock == nil || r.Calculation.InTransit == nil || r.Calculation.AvailableStock == nil {
			review("AI_UNKNOWN_INVENTORY", "В расчёте AI Service остаток или товар в пути неизвестен.")
		}
		if r.EstimatedUnitCost != nil && r.EstimatedCost != nil && math.Abs(*r.EstimatedCost-item.RecommendedQuantity**r.EstimatedUnitCost) > .01 {
			review("AI_COST_MISMATCH", "Стоимость не соответствует количеству и цене из ответа AI Service.")
		}
		if r.Calculation.RoundedRequirement != nil && math.Abs(*r.Calculation.RoundedRequirement-item.RecommendedQuantity) > 1e-6 {
			review("AI_QUANTITY_MISMATCH", "recommendedQuantity не совпадает с calculation.roundedRequirement; требуется ручная проверка.")
		}
		if r.Action == "NO_BUY" && item.RecommendedQuantity > 0 || r.Action == "BUY" && item.RecommendedQuantity == 0 {
			review("AI_ACTION_MISMATCH", "Действие не соответствует рекомендованному количеству.")
		}
		if p.OrderMultiple > 0 && math.Mod(item.RecommendedQuantity, float64(p.OrderMultiple)) != 0 {
			review("AI_QUANTITY_NOT_MULTIPLE", "Рекомендация AI Service не кратна MOQ из источника.")
		}
		for _, a := range r.AnomalyAnalysis.Candidates {
			item.Anomalies = append(item.Anomalies, domain.Anomaly{DocumentID: a.TransactionID, Month: a.Date[:7], Quantity: a.Quantity, MedianTransaction: a.MedianTransactionQuantity, Deterministic: a.NvidiaVerdict, Excluded: a.SystemDecision == "EXCLUDED"})
			if a.SystemDecision == "PENDING_REVIEW" || a.SystemDecision == "EXCLUDED" {
				review("ANOMALY_REQUIRES_CONFIRMATION", "Решение об исключении аномалии требует подтверждения менеджера.")
			}
		}
		for _, w := range item.Warnings {
			if w.Blocking {
				item.RequiresManualReview = true
			}
		}
		if item.RequiresManualReview || item.Decision == "REVIEW" {
			item.Decision = "REVIEW"
			item.RequiresManualReview = true
		}
		run.Items = append(run.Items, item)
	}
	run.Summary = forecast.Summarize(run.Items)
	return s.Store.AddRun(run)
}

// v1 has no manager-decision input. Record the decision separately without
// claiming that the upstream formula has been recalculated. An explicit final
// quantity remains necessary for export, even after an anomaly decision.
func (s *Service) patchRemote(ctx context.Context, id, code string, patch domain.ManagerPatch, d *domain.Dataset) (*domain.CalculationRun, error) {
	return s.Store.UpdateRun(id, func(run *domain.CalculationRun) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for n := range run.Items {
			item := &run.Items[n]
			if item.Code1C != code {
				continue
			}
			// This warning describes the current approval, not its edit history.
			warnings := item.Warnings[:0]
			for _, warning := range item.Warnings {
				if warning.Code != "MANAGER_QUANTITY_NOT_MULTIPLE" {
					warnings = append(warnings, warning)
				}
			}
			item.Warnings = warnings
			if patch.AnomalyDecision != nil {
				if len(item.Anomalies) == 0 {
					return fmt.Errorf("%w: у позиции нет кандидатов на аномалии", ErrInvalid)
				}
				if item.AnomalyDecision != *patch.AnomalyDecision {
					item.AnomalyDecision = *patch.AnomalyDecision
					item.ApprovedQuantity = nil
					item.Approved = false
					item.FinalQuantity = item.RecommendedQuantity
					item.Decision = "REVIEW"
					item.RequiresManualReview = true
					found := false
					for _, warning := range item.Warnings {
						if warning.Code == "ANOMALY_DECISION_RECORDED" {
							found = true
						}
					}
					if !found {
						item.Warnings = append(item.Warnings, domain.Diagnostic{Code: "ANOMALY_DECISION_RECORDED", Message: "Решение сохранено отдельно. Контракт AI v1 не принимает решения менеджера для пересчёта; явно подтвердите окончательное количество.", Blocking: true})
					}
				}
			}
			if patch.Comment != nil {
				item.ManagerComment = *patch.Comment
			}
			if patch.ApprovedQuantity != nil {
				v := *patch.ApprovedQuantity
				item.ApprovedQuantity = &v
				item.Approved = true // Legacy endpoint implicitly approves a quantity.
			}
			if patch.Approved != nil {
				item.Approved = *patch.Approved
			}
			item.UpdatedAt = time.Now().UTC()
			item.FinalQuantity = item.RecommendedQuantity
			if item.Approved && item.ApprovedQuantity != nil {
				item.FinalQuantity = *item.ApprovedQuantity
				if moq := d.Products[code].OrderMultiple; moq > 0 && math.Mod(item.FinalQuantity, float64(moq)) != 0 {
					item.Warnings = append(item.Warnings, domain.Diagnostic{Code: "MANAGER_QUANTITY_NOT_MULTIPLE", Message: "Подтверждённое количество не кратно MOQ."})
				}
				item.EstimatedCost = nil
				if item.UnitCost != nil {
					cost := math.Round(item.FinalQuantity**item.UnitCost*100) / 100
					item.EstimatedCost = &cost
				}
			} else {
				// Restore the service's original estimate when an old approval is reset.
				var original recommendation.Response
				if err := json.Unmarshal(run.AIResponse, &original); err != nil {
					return err
				}
				for _, r := range original.Recommendations {
					if r.Code1C == code {
						item.EstimatedCost = r.EstimatedCost
						break
					}
				}
			}
			run.Summary = forecast.Summarize(run.Items)
			return nil
		}
		return store.ErrNotFound
	})
}
