# Plugin Development

## Directory Layout

```text
plugins/
  grok-web.wasm
```

## Minimal Example

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type Invocation struct {
	Action string `json:"action"`
	Step   int    `json:"step"`
}

type Output struct {
	Type     string    `json:"type"`
	Manifest *Manifest `json:"manifest,omitempty"`
	Response *Response `json:"response,omitempty"`
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

type Response struct {
	Content  string `json:"content"`
	Thinking string `json:"thinking,omitempty"`
	Raw      any    `json:"raw,omitempty"`
}

func pluginInfo() {
	write(Output{
		Type: "response",
		Manifest: &Manifest{
			ID:           "example",
			Name:         "Example Plugin",
			Version:      "0.1.0",
			Entry:        "chat_completions",
			Capabilities: []string{"chat", "stream", "thinking"},
			Models:       []map[string]any{{"id": "example-model", "name": "Example Model"}},
			AccountFields: []map[string]any{{"key": "access_token", "required": true, "secret": true}},
		},
	})
}

func chatCompletions(inv Invocation) {
	write(Output{
		Type: "response",
		Response: &Response{
			Content:  fmt.Sprintf("handled action=%s step=%d", inv.Action, inv.Step),
			Thinking: "example plugin reasoning",
		},
	})
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
```

## Build

Use WASI so the platform can run the plugin with `wasmtime`:

```bash
GOOS=wasip1 GOARCH=wasm go build -o plugins/example.wasm ./plugin
```

## Plugin Metadata Fields

- `id`: unique source id
- `name`: display name in admin UI
- `version`: plugin version
- `description`: short summary
- `entry`: primary exported handler name
- `capabilities`: `chat`, `stream`, `thinking`, `images`, `audio`, `video`, `models`
- `models`: source-supported model list declared by the plugin
- `account_fields`: account fields required by the plugin
- `author`: plugin author or organization

## Design Rules

- Do not open network connections directly inside plugins
- Treat platform RPC as the only network path
- Keep account scheduling out of plugins
- Return deterministic JSON for admin discovery
- Keep source-specific protocol logic inside the plugin
- Do not inject tool policy/system prompts from plugin code
- Read `input.request.tools` and `input.request.tool_choice` when present
- If upstream has tool-call structure, keep it in `response.raw` for platform conversion

## Suggested Next Exports

- `chat_completions`
- `list_models`
- `sync_usage`
- `validate_account`

## HTTP Host Call Pattern

If the plugin needs the platform to perform HTTP, return `type: "continue"` with an HTTP request list.
The platform executes them and reinvokes the plugin with `host_results`.

That pattern is the current host ABI foundation and will later be extended to websocket and account operations.
