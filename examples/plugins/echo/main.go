package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type Invocation struct {
	Action string          `json:"action"`
	Step   int             `json:"step"`
	Input  json.RawMessage `json:"input,omitempty"`
}

type Output struct {
	Type     string        `json:"type"`
	Manifest *Manifest     `json:"manifest,omitempty"`
	Response *ChatResponse `json:"response,omitempty"`
}

type Manifest struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Version       string           `json:"version"`
	Description   string           `json:"description,omitempty"`
	Entry         string           `json:"entry,omitempty"`
	Capabilities  []string         `json:"capabilities,omitempty"`
	Models        []map[string]any `json:"models,omitempty"`
	AccountFields []map[string]any `json:"account_fields,omitempty"`
	Author        string           `json:"author,omitempty"`
}

type ChatResponse struct {
	Content  string `json:"content"`
	Thinking string `json:"thinking,omitempty"`
}

type ChatInput struct {
	Request struct {
		Model    string `json:"model"`
		Thinking bool   `json:"thinking"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	} `json:"request"`
	Account struct {
		ID     string            `json:"id"`
		Fields map[string]string `json:"fields"`
	} `json:"account"`
}

func pluginInfo() {
	write(Output{
		Type: "response",
		Manifest: &Manifest{
			ID:            "echo",
			Name:          "Echo Plugin",
			Version:       "0.1.0",
			Description:   "Example Go WASM plugin for web2api",
			Entry:         "chat_completions",
			Capabilities:  []string{"chat", "stream", "thinking"},
			Models:        []map[string]any{{"id": "grok-test-model", "name": "Echo Test Model"}},
			AccountFields: []map[string]any{{"key": "access_token", "label": "Access Token", "type": "text", "required": true, "secret": true, "placeholder": "paste access token"}},
			Author:        "web2api",
		},
	})
}

func chatCompletions(inv Invocation) {
	var input ChatInput
	_ = json.Unmarshal(inv.Input, &input)

	last := ""
	for i := len(input.Request.Messages) - 1; i >= 0; i-- {
		if input.Request.Messages[i].Role == "user" {
			last = input.Request.Messages[i].Content
			break
		}
	}

	resp := &ChatResponse{Content: fmt.Sprintf("echo plugin: %s", last)}
	if input.Account.ID != "" {
		resp.Content += fmt.Sprintf(" [account=%s]", input.Account.ID)
	}
	if input.Request.Thinking {
		resp.Thinking = "echo plugin reasoning"
	}

	write(Output{Type: "response", Response: resp})
}

func readInvocation() Invocation {
	data, _ := io.ReadAll(os.Stdin)
	var inv Invocation
	_ = json.Unmarshal(data, &inv)
	return inv
}

func write(v any) {
	b, _ := json.Marshal(v)
	fmt.Print(string(b))
}

func main() {
	inv := readInvocation()
	switch inv.Action {
	case "plugin_info":
		pluginInfo()
	case "chat_completions":
		chatCompletions(inv)
	default:
		write(map[string]any{"type": "error", "error": "unsupported action: " + inv.Action})
	}
}
