package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"web2api/internal/source"
)

const (
	protocolVersion = "web2api.plugin.v1"
	maxPluginSteps  = 8
)

var wsBridge = newWSBridge()

func loadManifest(path string) (Manifest, error) {
	invocation := Invocation{
		Version: protocolVersion,
		Action:  "plugin_info",
	}

	out, err := executeModule(context.Background(), path, "plugin_info", invocation)
	if err == nil {
		if out.Error != "" {
			return Manifest{}, errors.New(out.Error)
		}
		if out.Manifest == nil {
			return Manifest{}, fmt.Errorf("plugin_info returned no manifest")
		}
		manifest := *out.Manifest
		if manifest.ID == "" {
			return Manifest{}, fmt.Errorf("plugin manifest missing id")
		}
		if manifest.Name == "" {
			manifest.Name = manifest.ID
		}
		return manifest, nil
	}

	manifest, legacyErr := loadLegacyManifest(path)
	if legacyErr != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m *Manager) ExecuteChatCompletion(ctx context.Context, pluginID string, req ChatCompletionRequest, src source.Config, accountID string, accountName string, accountFields map[string]string) (ChatCompletionResult, error) {
	desc, ok := m.plugins[pluginID]
	if !ok {
		return ChatCompletionResult{}, fmt.Errorf("plugin %q not found", pluginID)
	}

	invocation := Invocation{
		Version: protocolVersion,
		Action:  "chat_completions",
		Input: &ChatInput{
			Request: req,
			Source: SourceConfig{
				ID:       src.ID,
				Name:     src.Name,
				BaseURL:  src.BaseURL,
				APIKey:   src.APIKey,
				Metadata: src.Metadata,
			},
			Account: accountConfig(accountID, accountName, accountFields),
		},
	}

	for step := 0; step < maxPluginSteps; step++ {
		invocation.Step = step
		out, err := executeModule(ctx, desc.Path, "chat_completions", invocation)
		if err != nil {
			return ChatCompletionResult{}, err
		}
		if out.Error != "" {
			return ChatCompletionResult{}, errors.New(out.Error)
		}

		switch out.Type {
		case "response":
			if out.Response == nil {
				return ChatCompletionResult{}, fmt.Errorf("plugin returned empty response")
			}
			return *out.Response, nil
		case "continue":
			results, err := executeHostRequests(ctx, out.Requests)
			if err != nil {
				return ChatCompletionResult{}, err
			}
			invocation.State = out.State
			invocation.HostResults = results
		default:
			return ChatCompletionResult{}, fmt.Errorf("plugin returned unsupported output type %q", out.Type)
		}
	}

	return ChatCompletionResult{}, fmt.Errorf("plugin exceeded max steps")
}

func accountConfig(id string, name string, fields map[string]string) *AccountConfig {
	if id == "" && len(fields) == 0 && name == "" {
		return nil
	}
	return &AccountConfig{ID: id, Name: name, Fields: fields}
}

func executeModule(ctx context.Context, path string, export string, invocation Invocation) (Output, error) {
	in, err := json.Marshal(invocation)
	if err != nil {
		return Output{}, err
	}

	stdout, stderr, err := runWasmtime(ctx, path, in)
	if err != nil {
		return Output{}, fmt.Errorf("run action %s: %w stderr=%s", export, err, strings.TrimSpace(stderr))
	}

	var out Output
	raw := strings.TrimSpace(stdout)
	if raw == "" {
		return Output{}, fmt.Errorf("plugin returned empty stdout")
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return Output{}, fmt.Errorf("decode plugin output: %w raw=%s", err, raw)
	}
	return out, nil
}

func loadLegacyManifest(path string) (Manifest, error) {
	invocation := Invocation{Version: protocolVersion, Action: "plugin_info"}
	in, err := json.Marshal(invocation)
	if err != nil {
		return Manifest{}, err
	}
	stdout, stderr, err := runWasmtime(context.Background(), path, in)
	if err != nil {
		return Manifest{}, fmt.Errorf("run plugin_info: %w stderr=%s", err, strings.TrimSpace(stderr))
	}
	raw := strings.TrimSpace(stdout)
	if raw == "" {
		return Manifest{}, fmt.Errorf("plugin_info returned empty stdout")
	}

	var manifest Manifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode plugin info: %w", err)
	}
	if manifest.ID == "" {
		return Manifest{}, fmt.Errorf("plugin manifest missing id")
	}
	if manifest.Name == "" {
		manifest.Name = manifest.ID
	}
	return manifest, nil
}

func runWasmtime(ctx context.Context, path string, stdin []byte) (string, string, error) {
	bin, err := exec.LookPath("wasmtime")
	if err != nil {
		return "", "", fmt.Errorf("wasmtime not found in PATH")
	}
	cmd := exec.CommandContext(ctx, bin, path)
	cmd.Dir = filepath.Dir(path)
	cmd.Stdin = strings.NewReader(string(stdin))
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	return stdout.String(), stderr.String(), err
}

func executeHostRequests(ctx context.Context, requests []HostRequest) ([]HostResult, error) {
	results := make([]HostResult, 0, len(requests))
	for _, req := range requests {
		switch req.Kind {
		case "http":
			result, err := executeHTTPRequest(ctx, req)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		case "ws":
			result, err := wsBridge.Execute(ctx, req)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		default:
			results = append(results, HostResult{ID: req.ID, Kind: req.Kind, Error: "unsupported host request kind"})
		}
	}
	return results, nil
}

type wsBridgeRuntime struct {
	mu    sync.Mutex
	conns map[string]*websocket.Conn
}

func newWSBridge() *wsBridgeRuntime {
	return &wsBridgeRuntime{conns: map[string]*websocket.Conn{}}
}

func (b *wsBridgeRuntime) Execute(ctx context.Context, req HostRequest) (HostResult, error) {
	if req.WS == nil {
		return HostResult{}, fmt.Errorf("ws request payload missing")
	}
	action := strings.ToLower(strings.TrimSpace(req.WS.Action))
	sessionID := strings.TrimSpace(req.WS.SessionID)
	if sessionID == "" {
		sessionID = fmt.Sprintf("ws-%d", time.Now().UnixNano())
	}
	switch action {
	case "connect":
		dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
		headers := http.Header{}
		for k, v := range req.WS.Headers {
			headers.Set(k, v)
		}
		conn, _, err := dialer.DialContext(ctx, req.WS.URL, headers)
		if err != nil {
			return HostResult{ID: req.ID, Kind: req.Kind, Error: err.Error()}, nil
		}
		b.mu.Lock()
		b.conns[sessionID] = conn
		b.mu.Unlock()
		return HostResult{ID: req.ID, Kind: req.Kind, WS: &WSResult{SessionID: sessionID, Connected: true}}, nil
	case "send":
		conn, ok := b.get(sessionID)
		if !ok {
			return HostResult{ID: req.ID, Kind: req.Kind, Error: "ws session not found"}, nil
		}
		msgType := websocket.TextMessage
		if strings.EqualFold(req.WS.MessageType, "binary") {
			msgType = websocket.BinaryMessage
		}
		if err := conn.WriteMessage(msgType, []byte(req.WS.Message)); err != nil {
			_ = b.close(sessionID)
			return HostResult{ID: req.ID, Kind: req.Kind, Error: err.Error()}, nil
		}
		outType := "text"
		if msgType == websocket.BinaryMessage {
			outType = "binary"
		}
		return HostResult{ID: req.ID, Kind: req.Kind, WS: &WSResult{SessionID: sessionID, MessageType: outType}}, nil
	case "recv":
		conn, ok := b.get(sessionID)
		if !ok {
			return HostResult{ID: req.ID, Kind: req.Kind, Error: "ws session not found"}, nil
		}
		timeout := 30 * time.Second
		if req.WS.TimeoutMS > 0 {
			timeout = time.Duration(req.WS.TimeoutMS) * time.Millisecond
		}
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			_ = b.close(sessionID)
			return HostResult{ID: req.ID, Kind: req.Kind, Error: err.Error()}, nil
		}
		outType := "text"
		if messageType == websocket.BinaryMessage {
			outType = "binary"
		}
		return HostResult{ID: req.ID, Kind: req.Kind, WS: &WSResult{SessionID: sessionID, Message: string(payload), MessageType: outType}}, nil
	case "close":
		if err := b.close(sessionID); err != nil {
			return HostResult{ID: req.ID, Kind: req.Kind, Error: err.Error()}, nil
		}
		return HostResult{ID: req.ID, Kind: req.Kind, WS: &WSResult{SessionID: sessionID}}, nil
	default:
		return HostResult{ID: req.ID, Kind: req.Kind, Error: "unsupported ws action"}, nil
	}
}

func (b *wsBridgeRuntime) get(sessionID string) (*websocket.Conn, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	conn, ok := b.conns[sessionID]
	return conn, ok
}

func (b *wsBridgeRuntime) close(sessionID string) error {
	b.mu.Lock()
	conn, ok := b.conns[sessionID]
	if ok {
		delete(b.conns, sessionID)
	}
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("ws session not found")
	}
	return conn.Close()
}

func executeHTTPRequest(ctx context.Context, req HostRequest) (HostResult, error) {
	if req.HTTP == nil {
		return HostResult{}, fmt.Errorf("http request payload missing")
	}
	method := req.HTTP.Method
	if method == "" {
		method = http.MethodGet
	}
	timeout := 30 * time.Second
	if req.HTTP.TimeoutMS > 0 {
		timeout = time.Duration(req.HTTP.TimeoutMS) * time.Millisecond
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, req.HTTP.URL, strings.NewReader(req.HTTP.Body))
	if err != nil {
		return HostResult{}, err
	}
	for k, v := range req.HTTP.Headers {
		httpReq.Header.Set(k, v)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(httpReq)
	if err != nil {
		return HostResult{ID: req.ID, Kind: req.Kind, Error: err.Error()}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return HostResult{}, err
	}

	return HostResult{
		ID:   req.ID,
		Kind: req.Kind,
		HTTP: &HTTPResult{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       string(body),
		},
	}, nil
}
