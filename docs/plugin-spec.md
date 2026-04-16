# Plugin Spec

`web2api` uses Go-written WebAssembly plugins to describe and implement different Web sources.

## Goals

- One platform manages multiple Web-to-API adapters
- Adapters are isolated in `.wasm` plugins
- All outbound `http/https/wss` access is brokered by the platform
- Plugin discovery is automatic from the `plugins/` directory

## Discovery

- Platform scans `plugins/*.wasm`
- Each module is executed as a WASI program
- Platform sends `{"action":"plugin_info"}` to stdin
- Plugin writes one JSON object to stdout

## Required Entry

```go
func main()
```

The plugin dispatches by `action` from stdin and prints one JSON object to stdout:

```json
{
  "id": "grok-web",
  "name": "Grok Web",
  "version": "0.1.0",
  "description": "Convert Grok Web to OpenAI chat completions",
  "entry": "chat_completions",
  "capabilities": ["chat", "stream", "thinking"],
  "models": [{"id": "grok-4", "name": "Grok 4"}],
  "account_fields": [{"key": "access_token", "required": true, "secret": true}],
  "author": "your-name"
}
```

## Runtime ABI

Current platform runtime supports these actions:

- `plugin_info`
- `chat_completions`

Contract:

- platform writes one JSON invocation object to plugin stdin
- platform executes the WASI module
- plugin writes one JSON output object to stdout
- if plugin needs network access, it returns `type: "continue"` plus host requests
- platform executes those requests and reinvokes the plugin with `host_results`

This keeps plugins focused on source translation while the platform owns networking, retries, cookies, accounts, and scheduling.

### Invocation Shape

```json
{
  "version": "web2api.plugin.v1",
  "action": "chat_completions",
  "step": 0,
  "input": {
    "request": {
      "model": "grok-4",
      "stream": true,
      "thinking": true,
      "messages": [
        {"role": "user", "content": "hello"}
      ]
    },
    "source": {
      "id": "grok",
      "name": "Grok",
      "base_url": "https://grok.com",
      "api_key": "optional",
      "metadata": {}
    },
    "account": {
      "id": "acc-1",
      "name": "primary",
      "fields": {
        "access_token": "..."
      }
    }
  }
}
```

### Output Shape

Plugin may return a final response:

```json
{
  "type": "response",
  "response": {
    "content": "final assistant content",
    "thinking": "optional reasoning text",
    "usage": {
      "prompt_tokens": 10,
      "completion_tokens": 20,
      "total_tokens": 30
    }
  }
}
```

Or request host work and continue:

```json
{
  "type": "continue",
  "state": {"phase": "after-http"},
  "requests": [
    {
      "id": "bootstrap",
      "kind": "http",
      "http": {
        "method": "GET",
        "url": "https://example.com/health",
        "headers": {
          "accept": "application/json"
        },
        "timeout_ms": 10000
      }
    }
  ]
}
```

Platform then invokes the plugin again with:

```json
{
  "version": "web2api.plugin.v1",
  "action": "chat_completions",
  "step": 1,
  "state": {"phase": "after-http"},
  "host_results": [
    {
      "id": "bootstrap",
      "kind": "http",
      "http": {
        "status_code": 200,
        "headers": {},
        "body": "..."
      }
    }
  ]
}
```

## Network Brokerage

Because plugins run in WASM, the platform should provide all real network capabilities.

Currently implemented host-side service:

- `http`

Planned next host-side services:

- `ws.connect`
- `ws.send`
- `ws.recv`
- `ws.close`
- `cookie.jar.get`
- `cookie.jar.set`
- `account.acquire`
- `account.report`

The plugin should request these capabilities through this RPC loop instead of opening sockets directly.

## Account Pooling

Account pool logic stays in the platform, not in plugins.

Platform responsibilities:

- multiple accounts per source
- quota sync
- account health state
- failure feedback and circuit breaking
- automatic rotation and refresh

Plugin responsibilities:

- declare what account fields are needed
- declare what models are supported
- describe how usage/quota is extracted from upstream responses
- map upstream protocol to the platform response model
