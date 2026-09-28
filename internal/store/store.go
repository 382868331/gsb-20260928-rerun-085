package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"dependency-workbench/internal/domain"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, []byte("[]\n"), 0o644); err != nil {
			return nil, err
		}
	}
	return &Store{path: path}, nil
}

func (s *Store) List() ([]domain.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var plans []domain.Plan
	if len(content) == 0 {
		return plans, nil
	}
	if err := json.Unmarshal(content, &plans); err != nil {
		return nil, err
	}
	return plans, nil
}

func (s *Store) Save(plan domain.Plan) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	plans := []domain.Plan{}
	content, err := os.ReadFile(s.path)
	if err == nil && len(content) > 0 {
		if err := json.Unmarshal(content, &plans); err != nil {
			return err
		}
	}
	plans = append(plans, plan)
	encoded, err := json.MarshalIndent(plans, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(encoded, '\n'), 0o644)
}
