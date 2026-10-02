package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"dependency-workbench/internal/domain"
)

// RouteStore persists staged migration routes so a reopened workspace can
// still explain executed steps and remaining choices.
type RouteStore struct {
	path string
	mu   sync.Mutex
}

func NewRouteStore(path string) (*RouteStore, error) {
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

func (s *RouteStore) List() ([]domain.Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

func (s *RouteStore) read() ([]domain.Route, error) {
	content, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	routes := []domain.Route{}
	if len(content) == 0 {
		return routes, nil
	}
	if err := json.Unmarshal(content, &routes); err != nil {
		return nil, err
	}
	return routes, nil
}

func (s *RouteStore) write(routes []domain.Route) error {
	encoded, err := json.MarshalIndent(routes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(encoded, '\n'), 0o644)
}

func (s *RouteStore) Save(route domain.Route) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	routes, err := s.read()
	if err != nil {
		return err
	}
	routes = append(routes, route)
	return s.write(routes)
}

// Update replaces the route with the same ID, returning false when missing.
func (s *RouteStore) Update(route domain.Route) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	routes, err := s.read()
	if err != nil {
		return false, err
	}
	for i := range routes {
		if routes[i].ID == route.ID {
			routes[i] = route
			return true, s.write(routes)
		}
	}
	return false, nil
}

func (s *RouteStore) Get(id string) (domain.Route, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	routes, err := s.read()
	if err != nil {
		return domain.Route{}, false, err
	}
	for _, item := range routes {
		if item.ID == id {
			return item, true, nil
		}
	}
	return domain.Route{}, false, nil
}
