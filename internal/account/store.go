package account

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
	sort.Slice(items, func(i, j int) bool {
		if items[i].SourceID == items[j].SourceID {
			if items[i].Priority == items[j].Priority {
				return items[i].ID < items[j].ID
			}
			return items[i].Priority > items[j].Priority
		}
		return items[i].SourceID < items[j].SourceID
	})
	return items
}

func (s *Store) Upsert(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if cfg.Status == "" {
		cfg.Status = StatusActive
	}
	if cfg.CreatedAt.IsZero() {
		cfg.CreatedAt = now
	}
	cfg.UpdatedAt = now

	for i := range s.items {
		if s.items[i].ID == cfg.ID {
			if cfg.Fields == nil {
				cfg.Fields = map[string]string{}
			}
			for k, v := range s.items[i].Fields {
				if _, ok := cfg.Fields[k]; !ok || cfg.Fields[k] == "" {
					cfg.Fields[k] = v
				}
			}
			cfg.CreatedAt = s.items[i].CreatedAt
			s.items[i] = cfg
			return s.saveLocked()
		}
	}
	s.items = append(s.items, cfg)
	return s.saveLocked()
}

func (s *Store) Select(sourceID string, now time.Time) (Selection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	best := selectBest(s.items, func(item Config) bool { return item.SourceID == sourceID }, now)
	if best == nil {
		return Selection{}, fmt.Errorf("no available account for route %q", sourceID)
	}
	return Selection{Account: *best, Reason: "selected available account"}, nil
}

func (s *Store) SelectByPlugin(pluginID string, now time.Time) (Selection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	best := selectBest(s.items, func(item Config) bool { return item.PluginID == pluginID }, now)
	if best == nil {
		return Selection{}, fmt.Errorf("no available account for plugin %q", pluginID)
	}
	return Selection{Account: *best, Reason: "selected available plugin account"}, nil
}

func (s *Store) SelectByPluginModel(pluginID string, model string, now time.Time) (Selection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	best := selectBest(s.items, func(item Config) bool {
		if item.PluginID != pluginID {
			return false
		}
		if len(item.Models) == 0 {
			return true
		}
		for _, enabled := range item.Models {
			if enabled == model {
				return true
			}
		}
		return false
	}, now)
	if best == nil {
		return Selection{}, fmt.Errorf("no available account for plugin %q and model %q", pluginID, model)
	}
	return Selection{Account: *best, Reason: "selected available plugin account by model"}, nil
}

func selectBest(items []Config, filter func(Config) bool, now time.Time) *Config {
	var best *Config
	for i := range items {
		item := items[i]
		if !filter(item) {
			continue
		}
		if item.Status == StatusDisabled {
			continue
		}
		if item.CoolingUntil != nil && item.CoolingUntil.After(now) {
			continue
		}
		if item.MaxRequests > 0 && item.UsedRequests >= item.MaxRequests {
			continue
		}
		if best == nil || item.Priority > best.Priority || (item.Priority == best.Priority && lessUsed(item, *best)) {
			candidate := item
			best = &candidate
		}
	}
	return best
}

func lessUsed(a, b Config) bool {
	if a.UsedRequests != b.UsedRequests {
		return a.UsedRequests < b.UsedRequests
	}
	if a.LastUsedAt == nil {
		return true
	}
	if b.LastUsedAt == nil {
		return false
	}
	return a.LastUsedAt.Before(*b.LastUsedAt)
}

func (s *Store) MarkSuccess(id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID != id {
			continue
		}
		s.items[i].Status = StatusActive
		s.items[i].FailureCount = 0
		s.items[i].LastError = ""
		s.items[i].CoolingUntil = nil
		s.items[i].UsedRequests++
		t := now.UTC()
		s.items[i].LastUsedAt = &t
		s.items[i].UpdatedAt = t
		return s.saveLocked()
	}
	return fmt.Errorf("account %q not found", id)
}

func (s *Store) MarkFailure(id string, errMsg string, cooldown time.Duration, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID != id {
			continue
		}
		s.items[i].FailureCount++
		s.items[i].LastError = errMsg
		if cooldown > 0 {
			until := now.UTC().Add(cooldown)
			s.items[i].CoolingUntil = &until
			s.items[i].Status = StatusCooling
		}
		s.items[i].UpdatedAt = now.UTC()
		return s.saveLocked()
	}
	return fmt.Errorf("account %q not found", id)
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
	return fmt.Errorf("account %q not found", id)
}

func (s *Store) Find(id string) (Config, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.items {
		if item.ID == id {
			return item, nil
		}
	}
	return Config{}, fmt.Errorf("account %q not found", id)
}
