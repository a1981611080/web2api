# web2api

Go-based Web-to-OpenAI API platform with WASM plugin support.

## Current Scaffold

- Go service entrypoint
- `plugins/` auto scan for `.wasm`
- WASM metadata loading through `action=plugin_info` on stdin
- WASM RPC execution through stdin/stdout JSON ABI
- platform-owned HTTP host calls for plugins via continue/resume loop
- source registry in `data/sources.json`
- account registry in `data/accounts.json`
- client registry in `data/consumers.json`
- plugin model catalog endpoint at `/api/admin/catalog/models`
- admin page at `/admin`
- admin subpages at `/admin/plugins`, `/admin/sources`, `/admin/accounts`, `/admin/clients`, `/admin/test`, `/admin/status`
- web UI at `/webui`
- API test page at `/webui/test`
- OpenAI-style `GET /v1/models`
- OpenAI-style `GET /v1/models/{id}`
- OpenAI-style `POST /v1/completions`
- OpenAI-style `POST /v1/responses`
- OpenAI-style `POST /v1/chat/completions`
- streaming SSE placeholder and thinking placeholder
- plugin spec and plugin development docs (`docs/plugin-spec.md`, `docs/plugin-dev.md`, `docs/plugin-ai-guide.md`, `docs/plugin-prompt-template.md`, `docs/plugin-checklist.md`)

## Run

```bash
go mod tidy
go run ./cmd/web2api
```

Open these pages:

- `http://localhost:8080/admin`
- `http://localhost:8080/webui`
- `http://localhost:8080/webui/test`

## Notes

This repository now contains the platform skeleton.
Actual source-specific Web conversion logic should be implemented as Go-to-WASM plugins following `docs/plugin-spec.md`.

Example plugin source is in `examples/plugins/echo`.
