package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"web2api/internal/account"
	"web2api/internal/plugin"
	"web2api/internal/source"
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

func TestDeleteSource(t *testing.T) {
	t.Parallel()
	router := newTestRouter(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/sources/grok", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("delete source status=%d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/sources", nil)
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("list sources status=%d", res.Code)
	}
	var payload map[string][]map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode sources payload: %v", err)
	}
	if len(payload["data"]) != 0 {
		t.Fatalf("expected 0 source after delete, got %d", len(payload["data"]))
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

	store, err := source.NewStore(filepath.Join(t.TempDir(), "sources.json"))
	if err != nil {
		t.Fatalf("new source store: %v", err)
	}
	if err := store.Upsert(source.Config{
		ID:            "grok",
		Name:          "Grok",
		PluginID:      "echo",
		Enabled:       true,
		Models:        []string{"grok-test-model"},
		ModelPrefixes: []string{"grok-"},
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert source: %v", err)
	}
	accounts, err := account.NewStore(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("new account store: %v", err)
	}
	if err := accounts.Upsert(account.Config{ID: "acc-1", SourceID: "grok", Name: "Primary", Status: account.StatusActive, Fields: map[string]string{"access_token": "token-1"}}); err != nil {
		t.Fatalf("upsert account: %v", err)
	}

	webDir := filepath.Join(testutil.ProjectRoot(t), "web")
	return NewHandler(mgr, store, accounts, webDir).Router()
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
