package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type invocation struct {
	Action string `json:"action"`
}

type output struct {
	Type     string    `json:"type"`
	Manifest *manifest `json:"manifest,omitempty"`
	Response *response `json:"response,omitempty"`
	Error    string    `json:"error,omitempty"`
}

type manifest struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Version       string           `json:"version"`
	Entry         string           `json:"entry"`
	Capabilities  []string         `json:"capabilities"`
	Models        []map[string]any `json:"models,omitempty"`
	AccountFields []map[string]any `json:"account_fields,omitempty"`
}

type response struct {
	Content string `json:"content"`
	Raw     any    `json:"raw,omitempty"`
}

func main() {
	data, _ := io.ReadAll(os.Stdin)
	var inv invocation
	_ = json.Unmarshal(data, &inv)
	if inv.Action == "plugin_info" {
		write(output{Type: "response", Manifest: &manifest{ID: "toolcalls", Name: "Tool Calls", Version: "0.1.0", Entry: "chat_completions", Capabilities: []string{"chat"}, Models: []map[string]any{{"id": "tool-model"}}}})
		return
	}
	write(output{Type: "response", Response: &response{Content: "", Raw: map[string]any{"tool_calls": []map[string]any{{"id": "call_1", "name": "web_search", "arguments": map[string]any{"query": "hello"}}}}}})
}

func write(v any) {
	b, _ := json.Marshal(v)
	fmt.Print(string(b))
}
