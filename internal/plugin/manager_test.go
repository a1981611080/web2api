package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"web2api/internal/source"
	"web2api/internal/testutil"
)

func TestManagerScanAndExecuteChatCompletion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.BuildExampleEchoPlugin(t, dir)

	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := mgr.Scan(); err != nil {
		t.Fatalf("scan plugins: %v", err)
	}

	plugins := mgr.List()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Manifest.ID != "echo" {
		t.Fatalf("expected echo manifest, got %#v", plugins[0].Manifest)
	}

	result, err := mgr.ExecuteChatCompletion(context.Background(), "echo", ChatCompletionRequest{
		Model:    "grok-test-model",
		Stream:   false,
		Thinking: true,
		Messages: []ChatMessage{{Role: "user", Content: "hello plugin"}},
	}, source.Config{ID: "grok", Name: "Grok"}, "acc-1", "Primary", map[string]string{"access_token": "token-1"})
	if err != nil {
		t.Fatalf("execute chat completion: %v", err)
	}
	if result.Content != "echo plugin: hello plugin [account=acc-1]" {
		t.Fatalf("unexpected content: %q", result.Content)
	}
	if result.Thinking != "echo plugin reasoning" {
		t.Fatalf("unexpected thinking: %q", result.Thinking)
	}
}

func TestExecuteChatCompletionWithHostHTTPContinue(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bridge" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"bridge ok"}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	testutil.BuildExampleHTTPContinuePlugin(t, dir)

	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := mgr.Scan(); err != nil {
		t.Fatalf("scan plugins: %v", err)
	}

	result, err := mgr.ExecuteChatCompletion(context.Background(), "http-continue", ChatCompletionRequest{
		Model:    "grok-http-model",
		Thinking: true,
		Messages: []ChatMessage{{Role: "user", Content: "call upstream"}},
	}, source.Config{ID: "grok", Name: "Grok", BaseURL: upstream.URL}, "acc-http", "HTTP Account", map[string]string{"session_cookie": "cookie-1"})
	if err != nil {
		t.Fatalf("execute continue chat completion: %v", err)
	}
	if !strings.Contains(result.Content, "bridge ok") {
		t.Fatalf("expected upstream body in content, got %q", result.Content)
	}
	if result.Thinking != "http continue reasoning" {
		t.Fatalf("unexpected thinking: %q", result.Thinking)
	}
}

func TestExecuteChatCompletionWithHostWSBridge(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read websocket: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			t.Fatalf("write websocket: %v", err)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	testutil.BuildExampleWSBridgePlugin(t, dir)

	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if err := mgr.Scan(); err != nil {
		t.Fatalf("scan plugins: %v", err)
	}

	result, err := mgr.ExecuteChatCompletion(context.Background(), "ws-bridge", ChatCompletionRequest{
		Model:    "ws-test-model",
		Thinking: false,
		Messages: []ChatMessage{{Role: "user", Content: "ws test"}},
	}, source.Config{ID: "grok", Name: "Grok", BaseURL: server.URL}, "acc-ws", "WS Account", map[string]string{})
	if err != nil {
		t.Fatalf("execute ws bridge chat completion: %v", err)
	}
	if !strings.Contains(result.Content, "ping") {
		t.Fatalf("expected websocket echoed message, got %q", result.Content)
	}
}
