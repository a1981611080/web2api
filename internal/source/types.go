package source

import "time"

type Config struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	PluginID          string            `json:"plugin_id"`
	Enabled           bool              `json:"enabled"`
	Models            []string          `json:"models,omitempty"`
	ValidationMessage string            `json:"validation_message,omitempty"`
	ModelPrefixes     []string          `json:"model_prefixes"`
	BaseURL           string            `json:"base_url,omitempty"`
	APIKey            string            `json:"api_key,omitempty"`
	MockReplyPrefix   string            `json:"mock_reply_prefix,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}
