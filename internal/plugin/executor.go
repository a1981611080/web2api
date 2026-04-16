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
	"time"

	"web2api/internal/source"
)

const (
	protocolVersion = "web2api.plugin.v1"
	maxPluginSteps  = 8
)

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
			results = append(results, HostResult{ID: req.ID, Kind: req.Kind, Error: "ws host bridge not implemented yet"})
		default:
			results = append(results, HostResult{ID: req.ID, Kind: req.Kind, Error: "unsupported host request kind"})
		}
	}
	return results, nil
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
