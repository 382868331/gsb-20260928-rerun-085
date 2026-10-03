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

// RouteStore persists staged route plans so a saved route can be reopened and
// still explain which steps were executed and what remains.
type RouteStore struct {
	path string
	mu   sync.Mutex
}

func NewRoutes(path string) (*RouteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, []byte("[]\n"), 0o644); err != nil {
			return nil, err
		}
	}
	return &RouteStore{path: path}, nil
}

func (s *RouteStore) List() ([]domain.SavedRoute, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	routes := []domain.SavedRoute{}
	if len(content) == 0 {
		return routes, nil
	}
	if err := json.Unmarshal(content, &routes); err != nil {
		return nil, err
	}
	return routes, nil
}

func (s *RouteStore) Save(route domain.SavedRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	routes := []domain.SavedRoute{}
	content, err := os.ReadFile(s.path)
	if err == nil && len(content) > 0 {
		if err := json.Unmarshal(content, &routes); err != nil {
			return err
		}
	}
	routes = append(routes, route)
	encoded, err := json.MarshalIndent(routes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(encoded, '\n'), 0o644)
}

// Update replaces the stored route with the same ID. The caller is
// responsible for carrying executed steps and their check records over
// verbatim — Update itself never merges or drops history.
func (s *RouteStore) Update(route domain.SavedRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	routes := []domain.SavedRoute{}
	content, err := os.ReadFile(s.path)
	if err == nil && len(content) > 0 {
		if err := json.Unmarshal(content, &routes); err != nil {
			return err
		}
	}
	found := false
	for i := range routes {
		if routes[i].ID == route.ID {
			routes[i] = route
			found = true
		}
	}
	if !found {
		routes = append(routes, route)
	}
	encoded, err := json.MarshalIndent(routes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(encoded, '\n'), 0o644)
}
