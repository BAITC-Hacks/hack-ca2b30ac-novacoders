package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
)

var ErrNotFound = errors.New("not found")

// Store owns all stored objects. Reads and writes copy their arguments so a
// caller cannot mutate maps/slices outside the lock. Runs update atomically.
type Store struct {
	mu                   sync.RWMutex
	datasets             map[string]*domain.Dataset
	runs                 map[string]*domain.CalculationRun
	nextDataset, nextRun uint64
}

func New() *Store {
	return &Store{datasets: map[string]*domain.Dataset{}, runs: map[string]*domain.CalculationRun{}}
}

func clone[T any](value *T) (*T, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var copy T
	if err := json.Unmarshal(data, &copy); err != nil {
		return nil, err
	}
	return &copy, nil
}

func (s *Store) AddDataset(d *domain.Dataset) (*domain.Dataset, error) {
	copy, err := clone(d)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextDataset++
	copy.ID = fmt.Sprintf("import-%s-%03d", time.Now().UTC().Format("20060102"), s.nextDataset)
	s.datasets[copy.ID] = copy
	return clone(copy)
}

func (s *Store) Dataset(id string) (*domain.Dataset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.datasets[id]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(d)
}

func (s *Store) AddRun(r *domain.CalculationRun) (*domain.CalculationRun, error) {
	copy, err := clone(r)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextRun++
	copy.ID = fmt.Sprintf("run-%03d", s.nextRun)
	s.runs[copy.ID] = copy
	return clone(copy)
}

func (s *Store) Run(id string) (*domain.CalculationRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(r)
}

// update must only perform local calculations and must not call Store methods.
func (s *Store) UpdateRun(id string, update func(*domain.CalculationRun) error) (*domain.CalculationRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, ErrNotFound
	}
	copy, err := clone(r)
	if err != nil {
		return nil, err
	}
	if err := update(copy); err != nil {
		return nil, err
	}
	result, err := clone(copy)
	if err != nil {
		return nil, err
	}
	s.runs[id] = copy
	return result, nil
}
