# Plugin AI Guide

This document is written for AI agents or developers generating `web2api` plugins.

## Goal

Write a Go plugin that compiles to `wasip1/wasm` and runs as a WASI program under the platform.

The plugin must:

- describe itself through `plugin_info`
- declare supported models
- declare required account fields
- translate source-specific Web protocol into platform responses
- ask the platform to perform `http/https/wss` work instead of opening sockets directly
- accept tool context (`tools`, `tool_choice`) from request input without owning prompt policy

## Required Actions

Your plugin reads one JSON invocation from stdin and dispatches by `action`.

Supported actions today:

- `plugin_info`
- `chat_completions`

Your `main()` should:

1. read stdin JSON
2. inspect `action`
3. call the appropriate handler
4. write one JSON object to stdout

## Manifest Requirements

Your `plugin_info` response must include:

- `id`
- `name`
- `version`
- `entry`
- `capabilities`
- `models`
- `account_fields`

Example:

```json
{
  "type": "response",
  "manifest": {
    "id": "grok-web",
    "name": "Grok Web",
    "version": "0.1.0",
    "entry": "chat_completions",
    "capabilities": ["chat", "stream", "thinking"],
    "models": [
      {"id": "grok-4", "name": "Grok 4"}
    ],
    "account_fields": [
      {
        "key": "access_token",
        "label": "Access Token",
        "type": "text",
        "required": true,
        "secret": true,
        "placeholder": "paste your token"
      }
    ]
  }
}
```

## Account Rules

Do not assume a fixed account shape like `identifier` or `secret`.

Instead:

- declare the fields you need in `manifest.account_fields`
- read the selected account from `input.account.fields`
- validate required fields inside the plugin

Example:

```json
{
  "input": {
    "account": {
      "id": "acc-1",
      "name": "primary",
      "fields": {
        "access_token": "...",
        "cookie": "..."
      }
    }
  }
}
```

## Model Rules

Do not expect the platform operator to hand-type your models.

Instead:

- declare models in `manifest.models`
- the platform will read them
- the source config chooses which declared models are enabled

For Grok plugin alignment, current model IDs are:

- `auto`
- `fast`
- `expert`

## Tool Calling Rules

Tool policy prompting is platform-owned. Plugin should not inject its own tool policy/system prompt.

Plugin behavior should be:

- read `input.request.tools` and `input.request.tool_choice` when present
- preserve upstream tool-call signal in `response.raw` (for platform parser)
- avoid rewriting tool-call JSON into explanatory text
- for stream mode, emit chunks normally; the platform decides what is forwarded to API clients

## Host Networking

Plugins must not open sockets directly.

Use the continue/resume pattern:

1. return `type: "continue"`
2. include a request list
3. platform executes requests
4. platform reinvokes your plugin with `host_results`

### HTTP/HTTPS

Request example:

```json
{
  "type": "continue",
  "requests": [
    {
      "id": "bootstrap",
      "kind": "http",
      "http": {
        "method": "GET",
        "url": "https://example.com/api/session",
        "headers": {
          "authorization": "Bearer ..."
        },
        "body": "",
        "timeout_ms": 10000
      }
    }
  ]
}
```

Result example:

```json
{
  "host_results": [
    {
      "id": "bootstrap",
      "kind": "http",
      "http": {
        "status_code": 200,
        "headers": {
          "content-type": ["application/json"]
        },
        "body": "..."
      }
    }
  ]
}
```

### WSS

Planned host methods:

- `ws.connect`
- `ws.send`
- `ws.recv`
- `ws.close`

When writing plugins now, structure your code so websocket logic can be split into these host operations later.

## Plugin Structure Advice

Use this internal layout in the plugin source:

- `main.go`: stdin/stdout dispatch
- `manifest.go`: plugin manifest
- `chat.go`: chat translation logic
- `protocol.go`: request/response mapping
- `host.go`: continue/resume request builders

## What AI Should Avoid

- do not hardcode platform-internal account field names
- do not assume direct TCP, TLS, or browser APIs are available
- do not return random JSON shapes
- do not bypass `plugin_info`
- do not invent models that are not actually supported by the upstream source

## What AI Should Produce

When generating a plugin, include:

1. complete `plugin_info`
2. declared models
3. declared account fields
4. `chat_completions` handler
5. host request builders for HTTP flow
6. comments only where protocol steps are not obvious
