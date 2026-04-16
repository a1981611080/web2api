package modelroute

import "time"

type Route struct {
	ID          string            `json:"id"`
	Name        string            `json:"name,omitempty"`
	SourceID    string            `json:"source_id"`
	PluginID    string            `json:"plugin_id"`
	SourceModel string            `json:"source_model"`
	Modes       []string          `json:"modes,omitempty"`
	Enabled     bool              `json:"enabled"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}
