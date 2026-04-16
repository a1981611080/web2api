package plugin

import (
	"os"
	"path/filepath"
	"sort"
)

type Manager struct {
	dir     string
	plugins map[string]Descriptor
}

func NewManager(dir string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Manager{dir: dir, plugins: map[string]Descriptor{}}, nil
}

func (m *Manager) Scan() error {
	pattern := filepath.Join(m.dir, "*.wasm")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	plugins := map[string]Descriptor{}
	for _, path := range paths {
		desc := Descriptor{Path: path, Status: "error"}
		manifest, loadErr := loadManifest(path)
		if loadErr != nil {
			desc.Error = loadErr.Error()
			plugins[filepath.Base(path)] = desc
			continue
		}
		desc.Manifest = manifest
		desc.Status = "ready"
		plugins[manifest.ID] = desc
	}
	m.plugins = plugins
	return nil
}

func (m *Manager) List() []Descriptor {
	items := make([]Descriptor, 0, len(m.plugins))
	for _, item := range m.plugins {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Manifest.ID < items[j].Manifest.ID
	})
	return items
}

func (m *Manager) Exists(id string) bool {
	_, ok := m.plugins[id]
	return ok
}

func (m *Manager) Get(id string) (Descriptor, bool) {
	item, ok := m.plugins[id]
	return item, ok
}
