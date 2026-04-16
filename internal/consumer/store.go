package consumer

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
	items []Config
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
		s.items = []Config{}
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		s.items = []Config{}
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

func (s *Store) List() []Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := slices.Clone(s.items)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (s *Store) Upsert(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if cfg.CreatedAt.IsZero() {
		cfg.CreatedAt = now
	}
	cfg.UpdatedAt = now

	for i := range s.items {
		if s.items[i].ID == cfg.ID {
			cfg.CreatedAt = s.items[i].CreatedAt
			s.items[i] = cfg
			return s.saveLocked()
		}
	}
	s.items = append(s.items, cfg)
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
	return fmt.Errorf("consumer %q not found", id)
}

func (s *Store) FindByAPIKey(key string) (Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.items {
		if item.APIKey == key {
			return item, nil
		}
	}
	return Config{}, fmt.Errorf("consumer not found")
}

func (s *Store) HasAny() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items) > 0
}
