# Plugin Prompt Template

Use this template when you ask AI to generate a new `web2api` plugin.

## How To Use

1. Copy this full prompt.
2. Fill placeholders in `<>`.
3. Send to AI code generator.
4. Validate with `docs/plugin-checklist.md`.

## Prompt

You are a senior Go engineer. Generate a production-oriented `web2api` WASI plugin.

### Target

- Plugin ID: `<plugin_id>`
- Plugin Name: `<plugin_name>`
- Source Website: `<source_name>`
- Use Case: `<what this plugin does>`

### Must Follow

- Language: Go
- Build target: `GOOS=wasip1 GOARCH=wasm`
- Runtime style: WASI executable with `main()`
- Input/Output: read one JSON invocation from stdin, write one JSON output to stdout
- Action dispatch: support `plugin_info` and `chat_completions`
- Do not open network sockets directly in plugin
- Use continue/resume host RPC pattern for HTTP flow

### Manifest Requirements

Return manifest via action `plugin_info` with:

- `id`, `name`, `version`, `entry`, `capabilities`, `models`, `account_fields`

`models` must include all supported model IDs.

`account_fields` must include all required account inputs with:

- `key`, `label`, `type`, `required`, `secret`, optional `placeholder`, optional `help`

### Chat Requirements

For action `chat_completions`:

- Parse model and messages from invocation input
- Parse account data from `input.account.fields`
- Validate required account fields
- If network is needed, return `type: "continue"` with `requests`
- On resume (`host_results` present), continue state machine and eventually return `type: "response"`
- Include `thinking` when request has thinking enabled
- Parse and preserve `input.request.tools` and `input.request.tool_choice` when present
- Do not add plugin-owned tool policy prompts; platform owns tool instruction prompts
- Preserve upstream tool-call hints in `response.raw` so platform can output OpenAI `tool_calls`

### Output Constraints

- Return stable JSON only
- No logs to stdout except JSON response
- If error, return structured plugin error response

### Project Structure

Generate these files:

- `main.go` (dispatch)
- `manifest.go` (manifest builder)
- `chat.go` (chat handler)
- `host_requests.go` (continue/resume host request builders)
- `types.go` (invocation/output structs)
- `README.md` (plugin-specific notes)

### Include In Output

1. Complete source code files.
2. Build command.
3. Example `plugin_info` output JSON.
4. Example `chat_completions` invocation and output JSON.
5. What account fields operator must configure.

### Platform Contract Reference

Align with these docs:

- `docs/plugin-spec.md`
- `docs/plugin-dev.md`
- `docs/plugin-ai-guide.md`

### Additional Source Details

- Login/session flow: `<describe cookies/token/bootstrap flow>`
- Request endpoint(s): `<url list>`
- Streaming behavior: `<sse/ws/none>`
- Anti-bot or headers: `<required headers/cookies>`
- Failure patterns: `<rate limit, 403, captcha, etc>`

Now generate the full plugin implementation.
