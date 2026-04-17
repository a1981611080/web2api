package plugin

import (
	"os"
	"path/filepath"
	"sort"
)

type RuntimeProcess struct {
	PluginID      string `json:"plugin_id"`
	Path          string `json:"path"`
	PID           int    `json:"pid"`
	Refs          int    `json:"refs"`
	Closed        bool   `json:"closed"`
	IdleReleaseAt string `json:"idle_release_at,omitempty"`
	LastTraceID   string `json:"last_trace_id,omitempty"`
	LastAction    string `json:"last_action,omitempty"`
	LastStep      int    `json:"last_step"`
	LastType      string `json:"last_type,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	LastDuration  int64  `json:"last_duration_ms"`
	TotalCalls    int64  `json:"total_calls"`
	LastInvokeAt  string `json:"last_invoke_at,omitempty"`
}

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

func (m *Manager) ProcessPoolSnapshot() []RuntimeProcess {
	items := pluginProcPool.Snapshot()
	out := make([]RuntimeProcess, 0, len(items))
	for _, item := range items {
		pluginID := "(unknown)"
		for id, desc := range m.plugins {
			if desc.Path == item.Path {
				pluginID = id
				break
			}
		}
		entry := RuntimeProcess{
			PluginID:     pluginID,
			Path:         item.Path,
			PID:          item.PID,
			Refs:         item.Refs,
			Closed:       item.Closed,
			LastTraceID:  item.LastTraceID,
			LastAction:   item.LastAction,
			LastStep:     item.LastStep,
			LastType:     item.LastType,
			LastError:    item.LastError,
			LastDuration: item.LastDuration,
			TotalCalls:   item.TotalCalls,
		}
		if item.IdleReleaseAt != nil {
			entry.IdleReleaseAt = item.IdleReleaseAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		if item.LastInvokeAt != nil {
			entry.LastInvokeAt = item.LastInvokeAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PluginID == out[j].PluginID {
			return out[i].Path < out[j].Path
		}
		return out[i].PluginID < out[j].PluginID
	})
	return out
}
