package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

func (s *Store) List() []Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.items)
}

func (s *Store) Upsert(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID == cfg.ID {
			cfg.CreatedAt = s.items[i].CreatedAt
			s.items[i] = cfg
			return s.save()
		}
	}
	s.items = append(s.items, cfg)
	return s.save()
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("source %q not found", id)
}

func (s *Store) FindByModel(model string) (Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, item := range s.items {
		if !item.Enabled {
			continue
		}
		for _, enabledModel := range item.Models {
			if model == enabledModel {
				return item, nil
			}
		}
		for _, prefix := range item.ModelPrefixes {
			if strings.HasPrefix(model, prefix) {
				return item, nil
			}
		}
	}
	return Config{}, fmt.Errorf("no enabled source matched model %q", model)
}
