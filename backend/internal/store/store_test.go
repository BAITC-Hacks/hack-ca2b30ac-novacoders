package store

import (
	"errors"
	"testing"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
)

func TestIsolationAndAtomicRollback(t *testing.T) {
	s := New()
	r, err := s.AddRun(&domain.CalculationRun{Items: []domain.Item{{Code1C: "sku", Warnings: []domain.Diagnostic{{Code: "original"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	r.Items[0].Warnings[0].Code = "changed"
	stored, err := s.Run(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Items[0].Warnings[0].Code != "original" {
		t.Fatal("shared mutable state")
	}
	_, err = s.UpdateRun(r.ID, func(run *domain.CalculationRun) error { run.Items[0].Code1C = "broken"; return errors.New("cancel") })
	if err == nil {
		t.Fatal("expected rollback")
	}
	stored, err = s.Run(r.ID)
	if err != nil || stored.Items[0].Code1C != "sku" {
		t.Fatal("partial write escaped")
	}
}
