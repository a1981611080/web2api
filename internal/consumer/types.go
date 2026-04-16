package consumer

import "time"

type Config struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	APIKey        string            `json:"api_key"`
	Enabled       bool              `json:"enabled"`
	AllowedModels []string          `json:"allowed_models,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}
