package account

import "time"

type Status string

const (
	StatusActive   Status = "active"
	StatusCooling  Status = "cooling"
	StatusDisabled Status = "disabled"
)

type Config struct {
	ID                string            `json:"id"`
	SourceID          string            `json:"source_id"`
	PluginID          string            `json:"plugin_id,omitempty"`
	Models            []string          `json:"models,omitempty"`
	ValidationMessage string            `json:"validation_message,omitempty"`
	Name              string            `json:"name"`
	Fields            map[string]string `json:"fields,omitempty"`
	Status            Status            `json:"status"`
	Priority          int               `json:"priority,omitempty"`
	MaxRequests       int               `json:"max_requests,omitempty"`
	UsedRequests      int               `json:"used_requests,omitempty"`
	FailureCount      int               `json:"failure_count,omitempty"`
	LastError         string            `json:"last_error,omitempty"`
	LastUsedAt        *time.Time        `json:"last_used_at,omitempty"`
	CoolingUntil      *time.Time        `json:"cooling_until,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type Selection struct {
	Account Config
	Reason  string
}
