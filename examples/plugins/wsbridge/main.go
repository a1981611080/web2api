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
	State  struct {
		Message string `json:"message"`
	} `json:"state,omitempty"`
	Input struct {
		Source struct {
			BaseURL string `json:"base_url"`
		} `json:"source"`
	} `json:"input,omitempty"`
	HostResults []struct {
		WS *struct {
			SessionID string `json:"session_id"`
			Message   string `json:"message"`
		} `json:"ws,omitempty"`
	} `json:"host_results,omitempty"`
}

type Output struct {
	Type     string      `json:"type"`
	Manifest *Manifest   `json:"manifest,omitempty"`
	Requests []HostReq   `json:"requests,omitempty"`
	Response *Resp       `json:"response,omitempty"`
	State    interface{} `json:"state,omitempty"`
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

type HostReq struct {
	ID   string    `json:"id"`
	Kind string    `json:"kind"`
	WS   *WSAction `json:"ws,omitempty"`
}

type WSAction struct {
	Action    string `json:"action"`
	SessionID string `json:"session_id,omitempty"`
	URL       string `json:"url,omitempty"`
	Message   string `json:"message,omitempty"`
}

type Resp struct {
	Content  string `json:"content"`
	Thinking string `json:"thinking,omitempty"`
}

func pluginInfo() {
	write(Output{Type: "response", Manifest: &Manifest{ID: "ws-bridge", Name: "WS Bridge Plugin", Version: "0.1.0", Entry: "chat_completions", Capabilities: []string{"chat"}, Models: []map[string]any{{"id": "ws-test-model", "name": "WS Test Model"}}}})
}

func chatCompletions(inv Invocation) {
	base := strings.TrimRight(inv.Input.Source.BaseURL, "/")
	wsURL := strings.Replace(base, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL += "/ws"

	session := ""
	if len(inv.HostResults) > 0 && inv.HostResults[0].WS != nil {
		session = inv.HostResults[0].WS.SessionID
	}

	switch inv.Step {
	case 0:
		write(Output{Type: "continue", Requests: []HostReq{{ID: "conn", Kind: "ws", WS: &WSAction{Action: "connect", URL: wsURL}}}})
	case 1:
		write(Output{Type: "continue", Requests: []HostReq{{ID: "send", Kind: "ws", WS: &WSAction{Action: "send", SessionID: session, Message: "ping"}}}})
	case 2:
		write(Output{Type: "continue", Requests: []HostReq{{ID: "recv", Kind: "ws", WS: &WSAction{Action: "recv", SessionID: session}}}})
	case 3:
		msg := ""
		if len(inv.HostResults) > 0 && inv.HostResults[0].WS != nil {
			msg = inv.HostResults[0].WS.Message
		}
		write(Output{Type: "continue", State: map[string]string{"message": msg}, Requests: []HostReq{{ID: "close", Kind: "ws", WS: &WSAction{Action: "close", SessionID: session}}}})
	default:
		write(Output{Type: "response", Response: &Resp{Content: "ws message: " + inv.State.Message}})
	}
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
		write(map[string]any{"type": "error", "error": "unsupported action"})
	}
}
