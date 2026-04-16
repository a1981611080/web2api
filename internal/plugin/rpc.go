package plugin

import "encoding/json"

type Invocation struct {
	Version     string          `json:"version"`
	Action      string          `json:"action"`
	Step        int             `json:"step"`
	Input       *ChatInput      `json:"input,omitempty"`
	State       json.RawMessage `json:"state,omitempty"`
	HostResults []HostResult    `json:"host_results,omitempty"`
}

type ChatInput struct {
	Request ChatCompletionRequest `json:"request"`
	Source  SourceConfig          `json:"source"`
	Account *AccountConfig        `json:"account,omitempty"`
}

type SourceConfig struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	BaseURL  string            `json:"base_url,omitempty"`
	APIKey   string            `json:"api_key,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type AccountConfig struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Fields map[string]string `json:"fields,omitempty"`
}

type ChatCompletionRequest struct {
	Model    string            `json:"model"`
	Messages []ChatMessage     `json:"messages"`
	Stream   bool              `json:"stream"`
	Thinking bool              `json:"thinking"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Output struct {
	Type     string                `json:"type"`
	Manifest *Manifest             `json:"manifest,omitempty"`
	Response *ChatCompletionResult `json:"response,omitempty"`
	Requests []HostRequest         `json:"requests,omitempty"`
	State    json.RawMessage       `json:"state,omitempty"`
	Error    string                `json:"error,omitempty"`
}

type ChatCompletionResult struct {
	Content  string `json:"content"`
	Thinking string `json:"thinking,omitempty"`
	Usage    Usage  `json:"usage,omitempty"`
	Raw      any    `json:"raw,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

type HostRequest struct {
	ID   string       `json:"id"`
	Kind string       `json:"kind"`
	HTTP *HTTPRequest `json:"http,omitempty"`
	WS   *WSRequest   `json:"ws,omitempty"`
}

type HTTPRequest struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      string            `json:"body,omitempty"`
	TimeoutMS int               `json:"timeout_ms,omitempty"`
}

type WSRequest struct {
	Action      string            `json:"action"`
	SessionID   string            `json:"session_id,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Message     string            `json:"message,omitempty"`
	MessageType string            `json:"message_type,omitempty"`
	TimeoutMS   int               `json:"timeout_ms,omitempty"`
}

type HostResult struct {
	ID    string      `json:"id"`
	Kind  string      `json:"kind"`
	HTTP  *HTTPResult `json:"http,omitempty"`
	WS    *WSResult   `json:"ws,omitempty"`
	Error string      `json:"error,omitempty"`
}

type HTTPResult struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       string              `json:"body,omitempty"`
}

type WSResult struct {
	SessionID   string `json:"session_id,omitempty"`
	Connected   bool   `json:"connected,omitempty"`
	Message     string `json:"message,omitempty"`
	MessageType string `json:"message_type,omitempty"`
}
