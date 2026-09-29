package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sync"

	"dependency-workbench/internal/model"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Store {
	return &Store{path: path}
}

func (store *Store) List() ([]model.Plan, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.read()
}

func (store *Store) Get(id string) (model.Plan, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	plans, err := store.read()
	if err != nil {
		return model.Plan{}, err
	}
	for _, plan := range plans {
		if plan.ID == id {
			return plan, nil
		}
	}
	return model.Plan{}, errors.New("plan not found")
}

func (store *Store) Save(plan model.Plan) (model.Plan, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	plans, err := store.read()
	if err != nil {
		return model.Plan{}, err
	}
	if plan.ID == "" {
		plan.ID = newID()
	}
	for index, existing := range plans {
		if existing.ID == plan.ID {
			plans[index] = plan
			return plan, store.write(plans)
		}
	}
	return plan, store.write(append(plans, plan))
}

func (store *Store) read() ([]model.Plan, error) {
	data, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) || len(data) == 0 {
		return []model.Plan{}, nil
	}
	if err != nil {
		return nil, err
	}
	var plans []model.Plan
	if err := json.Unmarshal(data, &plans); err != nil {
		return nil, err
	}
	return plans, nil
}

func (store *Store) write(plans []model.Plan) error {
	if err := os.MkdirAll("data", 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(plans, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(store.path, data, 0o644)
}

func newID() string {
	var bytes [8]byte
	_, _ = rand.Read(bytes[:])
	return hex.EncodeToString(bytes[:])
}
