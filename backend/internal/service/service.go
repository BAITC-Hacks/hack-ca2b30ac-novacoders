package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/anomaly"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/forecast"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/store"
)

var ErrInvalid = errors.New("invalid input")

type Service struct {
	Store *store.Store
}

func ValidateConfig(cfg domain.RunConfig) error {
	if cfg.DatasetID == "" || cfg.Supplier != domain.Supplier {
		return fmt.Errorf("%w: datasetId обязателен, supplier должен быть SystemElectric", ErrInvalid)
	}
	if err := forecast.ValidateConfig(cfg); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}

func (s *Service) CreateRun(ctx context.Context, cfg domain.RunConfig) (*domain.CalculationRun, error) {
	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}
	d, err := s.Store.Dataset(cfg.DatasetID)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(d.Products))
	for code, p := range d.Products {
		if p.Supplier == cfg.Supplier {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	r := &domain.CalculationRun{DatasetID: d.ID, CreatedAt: time.Now().UTC(), Config: cfg, Items: []domain.Item{}}
	for _, code := range codes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p := d.Products[code]
		candidates, _ := anomaly.Detect(p, d.AsOf)
		item, err := forecast.Calculate(ctx, p, d.AsOf, d.Seasonality, cfg, candidates, "")
		if err != nil {
			return nil, err
		}
		r.Items = append(r.Items, item)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.Summary = forecast.Summarize(r.Items)
	return s.Store.AddRun(r)
}

func (s *Service) Patch(ctx context.Context, id, code string, patch domain.ManagerPatch) (*domain.CalculationRun, error) {
	if patch.ApprovedQuantity == nil && patch.AnomalyDecision == nil && patch.Comment == nil {
		return nil, fmt.Errorf("%w: пустое изменение", ErrInvalid)
	}
	if patch.ApprovedQuantity != nil {
		v := *patch.ApprovedQuantity
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1e12 || math.Trunc(v) != v {
			return nil, fmt.Errorf("%w: approvedQuantity — целое число 0–1e12", ErrInvalid)
		}
	}
	if patch.AnomalyDecision != nil && *patch.AnomalyDecision != "EXCLUDE" && *patch.AnomalyDecision != "KEEP" {
		return nil, fmt.Errorf("%w: anomalyDecision должен быть EXCLUDE или KEEP", ErrInvalid)
	}
	if patch.Comment != nil && len(*patch.Comment) > 4000 {
		return nil, fmt.Errorf("%w: комментарий слишком длинный", ErrInvalid)
	}
	r, err := s.Store.Run(id)
	if err != nil {
		return nil, err
	}
	d, err := s.Store.Dataset(r.DatasetID)
	if err != nil {
		return nil, err
	}
	return s.Store.UpdateRun(id, func(r *domain.CalculationRun) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i, old := range r.Items {
			if old.Code1C != code {
				continue
			}
			decision := old.AnomalyDecision
			if patch.AnomalyDecision != nil {
				decision = *patch.AnomalyDecision
			}
			if patch.AnomalyDecision != nil && len(old.Anomalies) == 0 {
				return fmt.Errorf("%w: у позиции нет кандидатов на аномалии", ErrInvalid)
			}
			item, err := forecast.Calculate(ctx, d.Products[code], d.AsOf, d.Seasonality, r.Config, old.Anomalies, decision)
			if err != nil {
				return err
			}
			item.ManagerComment = old.ManagerComment
			if patch.Comment != nil {
				item.ManagerComment = *patch.Comment
			}
			item.ApprovedQuantity = old.ApprovedQuantity
			if patch.AnomalyDecision != nil && decision != old.AnomalyDecision {
				item.ApprovedQuantity = nil
			} // Changed evidence invalidates an old approval.
			if patch.ApprovedQuantity != nil {
				quantity := *patch.ApprovedQuantity
				item.ApprovedQuantity = &quantity
			}
			if item.ApprovedQuantity != nil {
				item.FinalQuantity = *item.ApprovedQuantity
				if item.Breakdown.OrderMultiple > 0 && math.Mod(item.FinalQuantity, float64(item.Breakdown.OrderMultiple)) != 0 {
					item.Warnings = append(item.Warnings, domain.Diagnostic{Code: "MANAGER_QUANTITY_NOT_MULTIPLE", Message: "Подтверждённое менеджером количество не кратно поставке."})
				}
			}
			forecast.SetCost(&item)
			r.Items[i] = item
			r.Summary = forecast.Summarize(r.Items)
			return nil
		}
		return store.ErrNotFound
	})
}
