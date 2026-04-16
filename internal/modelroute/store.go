package modelroute

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

type Store struct {
	path  string
	mu    sync.RWMutex
	items []Route
}

func NewStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.items = []Route{}
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		s.items = []Route{}
		return nil
	}
	return json.Unmarshal(data, &s.items)
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

func (s *Store) List() []Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := slices.Clone(s.items)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (s *Store) Upsert(route Route) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if route.CreatedAt.IsZero() {
		route.CreatedAt = now
	}
	route.UpdatedAt = now

	for i := range s.items {
		if s.items[i].ID == route.ID {
			route.CreatedAt = s.items[i].CreatedAt
			s.items[i] = route
			return s.saveLocked()
		}
	}
	s.items = append(s.items, route)
	return s.saveLocked()
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("model route %q not found", id)
}

func (s *Store) Find(id string) (Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.items {
		if item.ID == id {
			return item, nil
		}
	}
	return Route{}, fmt.Errorf("model route %q not found", id)
}
