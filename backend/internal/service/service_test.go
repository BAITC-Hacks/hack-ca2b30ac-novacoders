package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/demo"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/importer"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/store"
)

func setup(t *testing.T, modify func(*domain.Dataset)) (*Service, domain.RunConfig) {
	t.Helper()
	files, err := demo.Workbooks()
	if err != nil {
		t.Fatal(err)
	}
	readers := map[string]io.Reader{}
	for k, v := range files {
		readers[k] = bytes.NewReader(v)
	}
	d, err := importer.Import(context.Background(), readers)
	if err != nil {
		t.Fatal(err)
	}
	if modify != nil {
		modify(d)
	}
	s := &Service{Store: store.New()}
	d, err = s.Store.AddDataset(d)
	if err != nil {
		t.Fatal(err)
	}
	return s, domain.RunConfig{DatasetID: d.ID, Supplier: domain.Supplier, LeadTimeDays: 30, SafetyDays: 14}
}

func TestUnconfirmedAnomalyRequiresReview(t *testing.T) {
	s, cfg := setup(t, nil)
	r, err := s.CreateRun(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	item := r.Items[3]
	if item.Decision != "REVIEW" || len(item.Anomalies) != 1 || item.Anomalies[0].Excluded {
		t.Fatalf("%+v", item)
	}
}

func TestExclusionRequiresManagerAndConsistentSources(t *testing.T) {
	for _, tc := range []struct {
		decision           string
		conflict, excluded bool
	}{{"EXCLUDE", false, true}, {"KEEP", false, false}, {"EXCLUDE", true, false}, {"KEEP", true, false}} {
		t.Run(tc.decision+map[bool]string{true: "_conflict", false: ""}[tc.conflict], func(t *testing.T) {
			s, cfg := setup(t, func(d *domain.Dataset) {
				if tc.conflict {
					d.Products["030200131_"].MonthlySales["2026-05"] = 421
				}
			})
			r, err := s.CreateRun(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			r, err = s.Patch(context.Background(), r.ID, "030200131_", domain.ManagerPatch{AnomalyDecision: &tc.decision})
			if err != nil {
				t.Fatal(err)
			}
			item := r.Items[3]
			if item.Anomalies[0].Excluded != tc.excluded {
				t.Fatalf("%+v", item)
			}
			if (item.Decision == "REVIEW") != tc.conflict {
				t.Fatal("only the source conflict should remain blocking after manager review")
			}
			if tc.conflict {
				found := false
				for _, w := range item.Warnings {
					if w.Code == "SOURCE_CONFLICT" {
						found = true
					}
				}
				if !found {
					t.Fatal("source conflict not guarded")
				}
			}
		})
	}
}

func TestManagerRecalculatesAndInvalidatesApproval(t *testing.T) {
	s, cfg := setup(t, nil)
	r, err := s.CreateRun(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	qty := 100.0
	r, err = s.Patch(context.Background(), r.ID, "030200131_", domain.ManagerPatch{ApprovedQuantity: &qty})
	if err != nil {
		t.Fatal(err)
	}
	decision := "EXCLUDE"
	r, err = s.Patch(context.Background(), r.ID, "030200131_", domain.ManagerPatch{AnomalyDecision: &decision})
	if err != nil {
		t.Fatal(err)
	}
	i := r.Items[3]
	// Exclusion restores a flat history of 100/month. A robust median may
	// already have ignored the event; recalculation need not change the quantity.
	if !i.Anomalies[0].Excluded || i.Decision == "REVIEW" || i.ApprovedQuantity != nil || i.Breakdown.ForecastDemand != 100 || r.Summary.ApprovedItems != 0 {
		t.Fatalf("after=%+v", i)
	}
	decision = "KEEP"
	r, err = s.Patch(context.Background(), r.ID, "030200131_", domain.ManagerPatch{AnomalyDecision: &decision, ApprovedQuantity: &qty})
	if err != nil {
		t.Fatal(err)
	}
	if r.Items[3].Anomalies[0].Excluded || r.Items[3].Decision == "REVIEW" || r.Items[3].ApprovedQuantity == nil {
		t.Fatalf("%+v", r.Items[3])
	}
}

func TestCanceledRunNotStored(t *testing.T) {
	s, cfg := setup(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.CreateRun(ctx, cfg); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Store.Run("run-001"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("canceled run persisted")
	}
}
