package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"web2api/internal/account"
	"web2api/internal/plugin"
	"web2api/internal/source"
	"web2api/internal/testutil"
)

func TestAdminAccountsLifecycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.BuildExampleEchoPlugin(t, dir)
	mgr, _ := plugin.NewManager(dir)
	if err := mgr.Scan(); err != nil {
		t.Fatal(err)
	}
	sources, _ := source.NewStore(filepath.Join(t.TempDir(), "sources.json"))
	if err := sources.Upsert(source.Config{ID: "grok", Name: "Grok", Enabled: true, Models: []string{"grok-test-model"}, ModelPrefixes: []string{"grok-"}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	accounts, _ := account.NewStore(filepath.Join(t.TempDir(), "accounts.json"))
	router := NewHandler(mgr, sources, accounts, filepath.Join(testutil.ProjectRoot(t), "web")).Router()

	body, _ := json.Marshal(map[string]any{"id": "acc-1", "source_id": "grok", "name": "Primary", "status": "active", "fields": map[string]string{"access_token": "token-1"}})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("upsert account status=%d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/accounts", nil)
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("list accounts status=%d", res.Code)
	}
	var payload map[string][]map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload["data"]) != 1 {
		t.Fatalf("expected 1 account, got %d", len(payload["data"]))
	}

	body, _ = json.Marshal(map[string]any{"error": "rate limit", "cooldown_seconds": 60})
	req = httptest.NewRequest(http.MethodPost, "/api/admin/accounts/acc-1/failure", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("mark failure status=%d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/admin/accounts/acc-1", nil)
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("delete account status=%d body=%s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/accounts", nil)
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("list accounts after delete status=%d", res.Code)
	}
	payload = map[string][]map[string]any{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload["data"]) != 0 {
		t.Fatalf("expected 0 account after delete, got %d", len(payload["data"]))
	}
}
