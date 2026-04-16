package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type Invocation struct {
	Action string `json:"action"`
	Step   int    `json:"step"`
	Input  struct {
		Source struct {
			BaseURL string `json:"base_url"`
		} `json:"source"`
	} `json:"input,omitempty"`
	HostResults []struct {
		HTTP *struct {
			Body string `json:"body"`
		} `json:"http,omitempty"`
	} `json:"host_results,omitempty"`
}

type Output struct {
	Type     string            `json:"type"`
	Manifest *Manifest         `json:"manifest,omitempty"`
	Requests []Request         `json:"requests,omitempty"`
	Response *Response         `json:"response,omitempty"`
	State    map[string]string `json:"state,omitempty"`
}

type Manifest struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Version       string           `json:"version"`
	Entry         string           `json:"entry"`
	Capabilities  []string         `json:"capabilities"`
	Models        []map[string]any `json:"models,omitempty"`
	AccountFields []map[string]any `json:"account_fields,omitempty"`
}

type Request struct {
	ID   string       `json:"id"`
	Kind string       `json:"kind"`
	HTTP *HTTPRequest `json:"http,omitempty"`
}

type HTTPRequest struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

type Response struct {
	Content  string `json:"content"`
	Thinking string `json:"thinking,omitempty"`
}

func pluginInfo() {
	write(Output{Type: "response", Manifest: &Manifest{ID: "http-continue", Name: "HTTP Continue", Version: "0.1.0", Entry: "chat_completions", Capabilities: []string{"chat"}, Models: []map[string]any{{"id": "grok-http-model", "name": "HTTP Continue Model"}}, AccountFields: []map[string]any{{"key": "session_cookie", "label": "Session Cookie", "type": "text", "required": true, "secret": true}}}})
}

func chatCompletions(inv Invocation) {
	if inv.Step == 0 {
		write(Output{Type: "continue", State: map[string]string{"phase": "after-http"}, Requests: []Request{{ID: "bridge", Kind: "http", HTTP: &HTTPRequest{Method: "GET", URL: strings.TrimRight(inv.Input.Source.BaseURL, "/") + "/bridge"}}}})
		return
	}
	body := ""
	if len(inv.HostResults) > 0 && inv.HostResults[0].HTTP != nil {
		body = inv.HostResults[0].HTTP.Body
	}
	write(Output{Type: "response", Response: &Response{Content: "upstream=" + body, Thinking: "http continue reasoning"}})
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
