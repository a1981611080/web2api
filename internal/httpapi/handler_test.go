package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"web2api/internal/account"
	"web2api/internal/consumer"
	"web2api/internal/plugin"
	"web2api/internal/testutil"
)

func TestChatCompletionsJSONResponseFromPlugin(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	body := map[string]any{
		"model":    "grok-test-model",
		"stream":   false,
		"thinking": true,
		"messages": []map[string]string{{"role": "user", "content": "hello from api"}},
	}
	res := performJSONRequest(t, router, body)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	choices := payload["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "echo plugin: hello from api [account=acc-1]" {
		t.Fatalf("unexpected content: %#v", message["content"])
	}
	meta := payload["web2api"].(map[string]any)
	if meta["mode"] != "plugin" {
		t.Fatalf("unexpected mode: %#v", meta["mode"])
	}
	if meta["account_id"] != "acc-1" {
		t.Fatalf("unexpected account id: %#v", meta["account_id"])
	}
}

func TestChatCompletionsStreamResponseFromPlugin(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)
	body := map[string]any{
		"model":    "grok-test-model",
		"stream":   true,
		"thinking": true,
		"messages": []map[string]string{{"role": "user", "content": "stream this please"}},
	}
	res := performJSONRequest(t, router, body)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	contentType := res.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("expected event stream content type, got %q", contentType)
	}
	bodyText := res.Body.String()
	if !strings.Contains(bodyText, "echo plugin reasoning") {
		t.Fatalf("expected reasoning in stream, got %s", bodyText)
	}
	if !strings.Contains(bodyText, "echo ") {
		t.Fatalf("expected streamed content chunks, got %s", bodyText)
	}
	if !strings.Contains(bodyText, "data: [DONE]") {
		t.Fatalf("expected done marker, got %s", bodyText)
	}
}

func TestListModelsFromPluginManifest(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode models response: %v", err)
	}
	if payload["object"] != "list" {
		t.Fatalf("unexpected object: %#v", payload["object"])
	}
	items := payload["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 model, got %d", len(items))
	}
	model := items[0].(map[string]any)
	if model["id"] != "grok-test-model" {
		t.Fatalf("unexpected model id: %#v", model["id"])
	}
	meta := model["web2api"].(map[string]any)
	if meta["plugin_id"] != "echo" {
		t.Fatalf("unexpected plugin id: %#v", meta["plugin_id"])
	}
}

func TestGetModelByID(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/models/grok-test-model", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode model response: %v", err)
	}
	if payload["id"] != "grok-test-model" {
		t.Fatalf("unexpected model id: %#v", payload["id"])
	}
}

func TestCompletionsEndpoint(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t)
	payload, _ := json.Marshal(map[string]any{
		"model":  "grok-test-model",
		"prompt": "hello completion",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode completions body: %v", err)
	}
	if body["object"] != "text_completion" {
		t.Fatalf("unexpected object: %#v", body["object"])
	}
}

func TestChatCompletionsContentArrayCompatibility(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t)
	body := map[string]any{
		"model":  "grok-test-model",
		"stream": false,
		"messages": []map[string]any{{
			"role":    "user",
			"content": []map[string]any{{"type": "text", "text": "hello array content"}},
		}},
	}
	res := performJSONRequest(t, router, body)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestResponsesJSONResponseFromPlugin(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t)
	payload, err := json.Marshal(map[string]any{
		"model":    "grok-test-model",
		"input":    "hello responses",
		"stream":   false,
		"thinking": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode responses body: %v", err)
	}
	if body["object"] != "response" {
		t.Fatalf("unexpected object: %#v", body["object"])
	}
	if body["output_text"] != "echo plugin: hello responses [account=acc-1]" {
		t.Fatalf("unexpected output_text: %#v", body["output_text"])
	}
	meta := body["web2api"].(map[string]any)
	if meta["account_id"] != "acc-1" {
		t.Fatalf("unexpected account_id: %#v", meta["account_id"])
	}
}

func TestClientAPIKeyAndModelActivation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.BuildExampleEchoPlugin(t, dir)
	mgr, err := plugin.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Scan(); err != nil {
		t.Fatal(err)
	}

	accounts, _ := account.NewStore(filepath.Join(t.TempDir(), "accounts.json"))
	if err := accounts.Upsert(account.Config{ID: "acc-1", PluginID: "echo", Name: "Primary", Status: account.StatusActive, Fields: map[string]string{"access_token": "token-1"}}); err != nil {
		t.Fatal(err)
	}
	consumers, _ := consumer.NewStore(filepath.Join(t.TempDir(), "consumers.json"))
	if err := consumers.Upsert(consumer.Config{ID: "c1", Name: "Client One", APIKey: "sk-test-1", Enabled: true, AllowedModels: []string{"grok-test-model"}}); err != nil {
		t.Fatal(err)
	}
	router := NewHandler(mgr, accounts, consumers, filepath.Join(testutil.ProjectRoot(t), "web")).Router()

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without api key, got %d", res.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-test-1")
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 with api key, got %d body=%s", res.Code, res.Body.String())
	}

	payload, _ := json.Marshal(map[string]any{"model": "grok-test-model", "messages": []map[string]string{{"role": "user", "content": "route test"}}})
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer sk-test-1")
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for routed model, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestChatCompletionsToolCallsCompatibility(t *testing.T) {
	t.Parallel()
	router := newToolCallsRouter(t)
	body := map[string]any{
		"model":    "tool-model",
		"stream":   false,
		"thinking": false,
		"messages": []map[string]string{{"role": "user", "content": "search something"}},
	}
	res := performJSONRequest(t, router, body)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	choices := payload["choices"].([]any)
	choice := choices[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("unexpected finish_reason: %#v", choice["finish_reason"])
	}
	message := choice["message"].(map[string]any)
	if _, ok := message["tool_calls"]; !ok {
		t.Fatalf("expected tool_calls in message, got %#v", message)
	}
}

func TestResponsesToolCallsCompatibility(t *testing.T) {
	t.Parallel()
	router := newToolCallsRouter(t)
	payload, _ := json.Marshal(map[string]any{
		"model":  "tool-model",
		"input":  "search",
		"stream": false,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	output := body["output"].([]any)
	if len(output) == 0 {
		t.Fatalf("expected output items, got %#v", body)
	}
	first := output[0].(map[string]any)
	if first["type"] != "function_call" {
		t.Fatalf("expected function_call, got %#v", first["type"])
	}
}

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	testutil.BuildExampleEchoPlugin(t, dir)

	mgr, err := plugin.NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := mgr.Scan(); err != nil {
		t.Fatalf("scan plugins: %v", err)
	}
	if !mgr.Exists("echo") {
		t.Fatalf("expected echo plugin after scan, got %#v", mgr.List())
	}

	accounts, err := account.NewStore(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("new account store: %v", err)
	}
	if err := accounts.Upsert(account.Config{ID: "acc-1", PluginID: "echo", Name: "Primary", Status: account.StatusActive, Fields: map[string]string{"access_token": "token-1"}}); err != nil {
		t.Fatalf("upsert account: %v", err)
	}
	consumers, err := consumer.NewStore(filepath.Join(t.TempDir(), "consumers.json"))
	if err != nil {
		t.Fatalf("new consumer store: %v", err)
	}
	webDir := filepath.Join(testutil.ProjectRoot(t), "web")
	return NewHandler(mgr, accounts, consumers, webDir).Router()
}

func newToolCallsRouter(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	testutil.BuildWASM(t, "./examples/plugins/toolcalls", filepath.Join(dir, "toolcalls.wasm"))

	mgr, err := plugin.NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := mgr.Scan(); err != nil {
		t.Fatalf("scan plugins: %v", err)
	}
	accounts, err := account.NewStore(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("new account store: %v", err)
	}
	if err := accounts.Upsert(account.Config{ID: "acc-1", PluginID: "toolcalls", Name: "Primary", Status: account.StatusActive}); err != nil {
		t.Fatalf("upsert account: %v", err)
	}
	consumers, err := consumer.NewStore(filepath.Join(t.TempDir(), "consumers.json"))
	if err != nil {
		t.Fatalf("new consumer store: %v", err)
	}
	return NewHandler(mgr, accounts, consumers, filepath.Join(testutil.ProjectRoot(t), "web")).Router()
}

func performJSONRequest(t *testing.T, router http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}
