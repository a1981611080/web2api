# Plugin Checklist

Use this checklist to validate any generated plugin before putting it in `plugins/`.

## A. Build And Basic Runtime

- [ ] `GOOS=wasip1 GOARCH=wasm go build -o plugins/<name>.wasm ./<plugin_dir>` succeeds
- [ ] `{"action":"plugin_info"}` invocation returns valid JSON
- [ ] No extra stdout noise beyond one JSON response

## B. Manifest Contract

- [ ] `manifest.id` is unique and stable
- [ ] `manifest.name` and `manifest.version` are present
- [ ] `manifest.entry` is `chat_completions`
- [ ] `manifest.capabilities` is accurate
- [ ] `manifest.models` includes all supported model IDs
- [ ] `manifest.account_fields` includes required operator inputs

## C. Account Field Design

- [ ] No hardcoded platform account keys outside declared `account_fields`
- [ ] Required fields are marked `required: true`
- [ ] Sensitive fields are marked `secret: true`
- [ ] Field labels/placeholders are operator-friendly

## D. Invocation/Output Rules

- [ ] `main()` dispatches by `action`
- [ ] Supports `plugin_info`
- [ ] Supports `chat_completions`
- [ ] Parses `input.request` and `input.account.fields`
- [ ] Returns `type: "response"` on completion
- [ ] Returns `type: "continue"` when waiting for host work

## E. Host RPC Usage

- [ ] Plugin does not directly open TCP/TLS/WebSocket connections
- [ ] HTTP work is requested via `requests` with `kind: "http"`
- [ ] Continue/resume state is deterministic across `step`
- [ ] Host result parsing is robust to non-200 responses

## F. Model Behavior

- [ ] Incoming `model` is validated against supported models
- [ ] Unsupported models return clear plugin error
- [ ] Thinking output behavior matches capability declaration
- [ ] Tool-context fields (`tools`, `tool_choice`) are accepted without plugin-side prompt hijacking
- [ ] Upstream tool-call hints are preserved in `response.raw` for platform parser

## G. Error Quality

- [ ] Upstream auth failures are distinguishable from rate limits
- [ ] Plugin returns actionable error messages for operator
- [ ] Temporary failures can trigger platform cooldown/rotation logic

## H. Security And Hygiene

- [ ] No secret values hardcoded in source
- [ ] No debug dumps of cookies/tokens in stdout
- [ ] Input parsing safely handles missing fields

## I. Platform Integration

- [ ] Plugin visible in `/admin/plugins` as `ready`
- [ ] Declared models visible via `/api/admin/catalog/models`
- [ ] Declared account fields rendered in `/admin/accounts`
- [ ] `GET /v1/models` includes enabled plugin models
- [ ] `POST /v1/chat/completions` succeeds with configured account
- [ ] In tools stream mode, output is `delta.tool_calls` + `finish_reason=tool_calls` without leaked thinking/content chunks

## J. Regression Tests (Recommended)

- [ ] Add plugin unit tests for invocation parsing and response building
- [ ] Add integration test for at least one happy path
- [ ] Add integration test for one failure path (e.g., 401/429)

## Release Decision

- [ ] All mandatory checks pass (A-I)
- [ ] Optional tests (J) completed or consciously deferred
- [ ] Plugin `.wasm` copied into `plugins/` and rescanned
