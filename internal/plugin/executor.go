package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	maxPluginSteps  = 4096
)

var wsBridge = newWSBridge()
var httpStreamBridge = newHTTPStreamBridge()
var pluginProcPool = newPluginProcessPool()

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
	return m.executeChatCompletion(ctx, pluginID, req, src, accountID, accountName, accountFields, nil)
}

func (m *Manager) ExecuteChatCompletionStream(ctx context.Context, pluginID string, req ChatCompletionRequest, src source.Config, accountID string, accountName string, accountFields map[string]string, onChunk func(ChatCompletionChunk) error) (ChatCompletionResult, error) {
	return m.executeChatCompletion(ctx, pluginID, req, src, accountID, accountName, accountFields, onChunk)
}

func (m *Manager) executeChatCompletion(ctx context.Context, pluginID string, req ChatCompletionRequest, src source.Config, accountID string, accountName string, accountFields map[string]string, onChunk func(ChatCompletionChunk) error) (ChatCompletionResult, error) {
	desc, ok := m.plugins[pluginID]
	if !ok {
		return ChatCompletionResult{}, fmt.Errorf("plugin %q not found", pluginID)
	}
	invoker, releaseInvoker, err := pluginProcPool.Acquire(desc.Path, hasCapability(desc.Manifest.Capabilities, "persistent_process"))
	if err != nil {
		return ChatCompletionResult{}, err
	}
	defer releaseInvoker()

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

	aggregated := ChatCompletionResult{}
	traceID := strings.TrimSpace(req.Metadata["request_trace_id"])
	for step := 0; step < maxPluginSteps; step++ {
		invocation.Step = step
		started := time.Now()
		out, err := executeModuleWithInvoker(ctx, invoker, "chat_completions", invocation)
		if err != nil {
			log.Printf("plugin step trace=%s plugin=%s step=%d err=%v duration_ms=%d", traceID, pluginID, step, err, time.Since(started).Milliseconds())
			return ChatCompletionResult{}, err
		}
		if out.Error != "" {
			log.Printf("plugin step trace=%s plugin=%s step=%d type=%s plugin_error=%s duration_ms=%d", traceID, pluginID, step, out.Type, out.Error, time.Since(started).Milliseconds())
			return ChatCompletionResult{}, errors.New(out.Error)
		}
		log.Printf("plugin step trace=%s plugin=%s step=%d type=%s duration_ms=%d", traceID, pluginID, step, out.Type, time.Since(started).Milliseconds())

		switch out.Type {
		case "response":
			if out.Response == nil {
				return ChatCompletionResult{}, fmt.Errorf("plugin returned empty response")
			}
			if out.Response.Content == "" && aggregated.Content != "" {
				out.Response.Content = aggregated.Content
			}
			if out.Response.Thinking == "" && aggregated.Thinking != "" {
				out.Response.Thinking = aggregated.Thinking
			}
			if out.Response.Raw == nil && aggregated.Raw != nil {
				out.Response.Raw = aggregated.Raw
			}
			if out.Response.Usage == (Usage{}) && aggregated.Usage != (Usage{}) {
				out.Response.Usage = aggregated.Usage
			}
			return *out.Response, nil
		case "chunk":
			if out.Chunk == nil {
				return ChatCompletionResult{}, fmt.Errorf("plugin returned empty chunk")
			}
			aggregated.Content += out.Chunk.Content
			aggregated.Thinking += out.Chunk.Thinking
			if out.Chunk.Raw != nil {
				aggregated.Raw = out.Chunk.Raw
			}
			if onChunk != nil {
				if err := onChunk(*out.Chunk); err != nil {
					return ChatCompletionResult{}, err
				}
			}
			invocation.State = out.State
			invocation.HostResults = nil
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

func hasCapability(items []string, target string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	for _, item := range items {
		if strings.TrimSpace(strings.ToLower(item)) == target {
			return true
		}
	}
	return false
}

func executeModule(ctx context.Context, path string, export string, invocation Invocation) (Output, error) {
	return executeModuleWithInvoker(ctx, &ephemeralInvoker{path: path}, export, invocation)
}

func executeModuleWithInvoker(ctx context.Context, invoker pluginInvoker, export string, invocation Invocation) (Output, error) {
	return invoker.Invoke(ctx, export, invocation)
}

type pluginInvoker interface {
	Invoke(ctx context.Context, export string, invocation Invocation) (Output, error)
}

type ephemeralInvoker struct {
	path string
}

func (e *ephemeralInvoker) Invoke(ctx context.Context, export string, invocation Invocation) (Output, error) {
	in, err := json.Marshal(invocation)
	if err != nil {
		return Output{}, err
	}
	stdout, stderr, err := runWasmtime(ctx, e.path, in, false)
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
	stdout, stderr, err := runWasmtime(context.Background(), path, in, false)
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

func runWasmtime(ctx context.Context, path string, stdin []byte, loop bool) (string, string, error) {
	bin, err := exec.LookPath("wasmtime")
	if err != nil {
		return "", "", fmt.Errorf("wasmtime not found in PATH")
	}
	args := []string{}
	if loop {
		args = append(args, "--env", "WEB2API_LOOP=1")
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, bin, args...)
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
	if req.HTTP.Stream {
		return httpStreamBridge.Execute(ctx, req)
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

	const maxHTTPBodyBytes = 2 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyBytes))
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

type httpStreamSession struct {
	body    io.ReadCloser
	status  int
	headers map[string][]string
	cancel  context.CancelFunc
}

type httpStreamRuntime struct {
	mu       sync.Mutex
	sessions map[string]*httpStreamSession
}

func newHTTPStreamBridge() *httpStreamRuntime {
	return &httpStreamRuntime{sessions: map[string]*httpStreamSession{}}
}

func (b *httpStreamRuntime) Execute(ctx context.Context, req HostRequest) (HostResult, error) {
	if req.HTTP == nil {
		return HostResult{}, fmt.Errorf("http stream request payload missing")
	}
	action := strings.ToLower(strings.TrimSpace(req.HTTP.Action))
	if action == "" {
		action = "open"
	}
	sessionID := strings.TrimSpace(req.HTTP.SessionID)
	if sessionID == "" {
		sessionID = fmt.Sprintf("http-%d", time.Now().UnixNano())
	}
	chunkSize := req.HTTP.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 4096
	}
	if chunkSize > 64*1024 {
		chunkSize = 64 * 1024
	}

	switch action {
	case "open":
		streamCtx := ctx
		cancel := func() {}
		if req.HTTP.TimeoutMS > 0 {
			var c context.CancelFunc
			streamCtx, c = context.WithTimeout(ctx, time.Duration(req.HTTP.TimeoutMS)*time.Millisecond)
			cancel = c
		}
		method := req.HTTP.Method
		if method == "" {
			method = http.MethodGet
		}
		httpReq, err := http.NewRequestWithContext(streamCtx, method, req.HTTP.URL, strings.NewReader(req.HTTP.Body))
		if err != nil {
			cancel()
			return HostResult{}, err
		}
		for k, v := range req.HTTP.Headers {
			httpReq.Header.Set(k, v)
		}
		client := &http.Client{}
		resp, err := client.Do(httpReq)
		if err != nil {
			cancel()
			return HostResult{ID: req.ID, Kind: req.Kind, Error: err.Error()}, nil
		}
		sess := &httpStreamSession{body: resp.Body, status: resp.StatusCode, headers: resp.Header, cancel: cancel}
		b.mu.Lock()
		b.sessions[sessionID] = sess
		b.mu.Unlock()
		piece, done, err := b.readChunk(sessionID, chunkSize)
		if err != nil {
			return HostResult{ID: req.ID, Kind: req.Kind, Error: err.Error()}, nil
		}
		return HostResult{ID: req.ID, Kind: req.Kind, HTTP: &HTTPResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: piece, Chunks: []string{piece}, StreamID: sessionID, Done: done}}, nil
	case "recv":
		piece, done, err := b.readChunk(sessionID, chunkSize)
		if err != nil {
			return HostResult{ID: req.ID, Kind: req.Kind, Error: err.Error()}, nil
		}
		status, headers, _ := b.sessionMeta(sessionID)
		return HostResult{ID: req.ID, Kind: req.Kind, HTTP: &HTTPResult{StatusCode: status, Headers: headers, Body: piece, Chunks: []string{piece}, StreamID: sessionID, Done: done}}, nil
	case "close":
		if err := b.close(sessionID); err != nil {
			return HostResult{ID: req.ID, Kind: req.Kind, Error: err.Error()}, nil
		}
		return HostResult{ID: req.ID, Kind: req.Kind, HTTP: &HTTPResult{StreamID: sessionID, Done: true}}, nil
	default:
		return HostResult{ID: req.ID, Kind: req.Kind, Error: "unsupported http stream action"}, nil
	}
}

func (b *httpStreamRuntime) sessionMeta(sessionID string) (int, map[string][]string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.sessions[sessionID]
	if !ok {
		return 0, nil, false
	}
	return s.status, s.headers, true
}

func (b *httpStreamRuntime) readChunk(sessionID string, size int) (string, bool, error) {
	b.mu.Lock()
	sess, ok := b.sessions[sessionID]
	b.mu.Unlock()
	if !ok {
		return "", true, fmt.Errorf("http stream session not found")
	}
	buf := make([]byte, size)
	n, err := sess.body.Read(buf)
	if n > 0 {
		if err != nil && !errors.Is(err, io.EOF) {
			_ = b.close(sessionID)
			return "", true, err
		}
		if errors.Is(err, io.EOF) {
			_ = b.close(sessionID)
			return string(buf[:n]), true, nil
		}
		return string(buf[:n]), false, nil
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			_ = b.close(sessionID)
			return "", true, nil
		}
		_ = b.close(sessionID)
		return "", true, err
	}
	return "", false, nil
}

func (b *httpStreamRuntime) close(sessionID string) error {
	b.mu.Lock()
	sess, ok := b.sessions[sessionID]
	if ok {
		delete(b.sessions, sessionID)
	}
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("http stream session not found")
	}
	if sess.cancel != nil {
		sess.cancel()
	}
	if sess.body != nil {
		return sess.body.Close()
	}
	return nil
}
