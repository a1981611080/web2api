package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"web2api/internal/account"
	"web2api/internal/consumer"
	"web2api/internal/modelroute"
	"web2api/internal/plugin"
	"web2api/internal/source"
)

type Handler struct {
	plugins   *plugin.Manager
	sources   *source.Store
	accounts  *account.Store
	consumers *consumer.Store
	routes    *modelroute.Store
	webDir    string
}

func NewHandler(plugins *plugin.Manager, sources *source.Store, accounts *account.Store, consumers *consumer.Store, routes *modelroute.Store, webDir string) *Handler {
	return &Handler{plugins: plugins, sources: sources, accounts: accounts, consumers: consumers, routes: routes, webDir: webDir}
}

func (h *Handler) Router() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	r.Get("/admin", h.serveFile("admin/index.html"))
	r.Get("/admin/plugins", h.serveFile("admin/plugins.html"))
	r.Get("/admin/sources", h.serveFile("admin/sources.html"))
	r.Get("/admin/accounts", h.serveFile("admin/accounts.html"))
	r.Get("/admin/clients", h.serveFile("admin/clients.html"))
	r.Get("/admin/model-routes", h.serveFile("admin/model-routes.html"))
	r.Get("/admin/test", h.serveFile("admin/test.html"))
	r.Get("/admin/status", h.serveFile("admin/status.html"))
	r.Get("/webui", h.serveFile("webui/index.html"))
	r.Get("/webui/test", h.serveFile("webui/test.html"))
	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(h.webDir, "assets")))))

	r.Route("/api/admin", func(r chi.Router) {
		r.Get("/status", h.adminStatus)
		r.Get("/plugins", h.listPlugins)
		r.Post("/plugins/scan", h.scanPlugins)
		r.Get("/sources", h.listSources)
		r.Post("/sources", h.upsertSource)
		r.Delete("/sources/{sourceID}", h.deleteSource)
		r.Get("/accounts", h.listAccounts)
		r.Post("/accounts", h.upsertAccount)
		r.Delete("/accounts/{accountID}", h.deleteAccount)
		r.Post("/accounts/{accountID}/success", h.markAccountSuccess)
		r.Post("/accounts/{accountID}/failure", h.markAccountFailure)
		r.Get("/consumers", h.listConsumers)
		r.Post("/consumers", h.upsertConsumer)
		r.Delete("/consumers/{consumerID}", h.deleteConsumer)
		r.Get("/model-routes", h.listModelRoutes)
		r.Post("/model-routes", h.upsertModelRoute)
		r.Delete("/model-routes/{routeID}", h.deleteModelRoute)
	})

	r.Get("/v1/models", h.listModels)
	r.Post("/v1/responses", h.responses)
	r.Post("/v1/chat/completions", h.chatCompletions)

	return r
}

func (h *Handler) serveFile(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(h.webDir, name))
	}
}

func (h *Handler) listPlugins(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": h.plugins.List()})
}

func (h *Handler) adminStatus(w http.ResponseWriter, r *http.Request) {
	plugins := h.plugins.List()
	sources := h.sources.List()
	readyPlugins := 0
	for _, item := range plugins {
		if item.Status == "ready" {
			readyPlugins++
		}
	}
	enabledSources := 0
	accounts := h.accounts.List()
	consumers := h.consumers.List()
	modelRoutes := h.routes.List()
	activeAccounts := 0
	coolingAccounts := 0
	disabledAccounts := 0
	for _, item := range accounts {
		switch item.Status {
		case account.StatusCooling:
			coolingAccounts++
		case account.StatusDisabled:
			disabledAccounts++
		default:
			activeAccounts++
		}
	}
	for _, item := range sources {
		if item.Enabled {
			enabledSources++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": map[string]any{
			"name":   "web2api",
			"status": "ok",
		},
		"plugins": map[string]any{
			"total": readyPlugins,
			"items": plugins,
		},
		"sources": map[string]any{
			"total":   len(sources),
			"enabled": enabledSources,
			"items":   sources,
		},
		"accounts": map[string]any{
			"total":    activeAccounts + coolingAccounts + disabledAccounts,
			"active":   activeAccounts,
			"cooling":  coolingAccounts,
			"disabled": disabledAccounts,
			"items":    accounts,
		},
		"consumers": map[string]any{
			"total": len(consumers),
			"items": consumers,
		},
		"model_routes": map[string]any{
			"total": len(modelRoutes),
			"items": modelRoutes,
		},
		"routes": map[string]string{
			"plugins":  "/admin/plugins",
			"sources":  "/admin/sources",
			"accounts": "/admin/accounts",
			"clients":  "/admin/clients",
			"models":   "/admin/model-routes",
			"test":     "/admin/test",
			"status":   "/admin/status",
		},
	})
}

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": h.accounts.List()})
}

func (h *Handler) listConsumers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": h.consumers.List()})
}

func (h *Handler) upsertConsumer(w http.ResponseWriter, r *http.Request) {
	var req consumer.Config
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("id is required"))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = req.ID
	}
	if strings.TrimSpace(req.APIKey) == "" {
		writeError(w, http.StatusBadRequest, errors.New("api_key is required"))
		return
	}
	if err := h.consumers.Upsert(req); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) deleteConsumer(w http.ResponseWriter, r *http.Request) {
	consumerID := chi.URLParam(r, "consumerID")
	if strings.TrimSpace(consumerID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("consumerID is required"))
		return
	}
	if err := h.consumers.Delete(consumerID); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) listModelRoutes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": h.routes.List()})
}

func (h *Handler) upsertModelRoute(w http.ResponseWriter, r *http.Request) {
	var req modelroute.Route
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.SourceID) == "" || strings.TrimSpace(req.PluginID) == "" || strings.TrimSpace(req.SourceModel) == "" {
		writeError(w, http.StatusBadRequest, errors.New("id, source_id, plugin_id, source_model are required"))
		return
	}
	if !sourceExists(h.sources.List(), req.SourceID) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("source %q not found", req.SourceID))
		return
	}
	if !h.plugins.Exists(req.PluginID) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("plugin %q not found", req.PluginID))
		return
	}
	if req.Name == "" {
		req.Name = req.ID
	}
	if err := h.routes.Upsert(req); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) deleteModelRoute(w http.ResponseWriter, r *http.Request) {
	routeID := chi.URLParam(r, "routeID")
	if strings.TrimSpace(routeID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("routeID is required"))
		return
	}
	if err := h.routes.Delete(routeID); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) upsertAccount(w http.ResponseWriter, r *http.Request) {
	var req account.Config
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("id is required"))
		return
	}
	if strings.TrimSpace(req.SourceID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("source_id is required"))
		return
	}
	if !sourceExists(h.sources.List(), req.SourceID) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("source %q not found", req.SourceID))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = req.ID
	}
	if err := h.accounts.Upsert(req); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "accountID")
	if strings.TrimSpace(accountID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("accountID is required"))
		return
	}
	if err := h.accounts.Delete(accountID); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func sourceExists(items []source.Config, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func (h *Handler) markAccountSuccess(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "accountID")
	if err := h.accounts.MarkSuccess(accountID, time.Now().UTC()); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) markAccountFailure(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "accountID")
	var req struct {
		Error           string `json:"error"`
		CooldownSeconds int    `json:"cooldown_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.CooldownSeconds <= 0 {
		req.CooldownSeconds = 300
	}
	if err := h.accounts.MarkFailure(accountID, req.Error, time.Duration(req.CooldownSeconds)*time.Second, time.Now().UTC()); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) scanPlugins(w http.ResponseWriter, r *http.Request) {
	if err := h.plugins.Scan(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": h.plugins.List()})
}

func (h *Handler) listSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": h.sources.List()})
}

func (h *Handler) upsertSource(w http.ResponseWriter, r *http.Request) {
	var req source.Config
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("id is required"))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = req.ID
	}
	if req.PluginID != "" && !h.plugins.Exists(req.PluginID) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("plugin %q not found", req.PluginID))
		return
	}
	if req.PluginID != "" {
		var manifest *plugin.Manifest
		for _, item := range h.plugins.List() {
			if item.Manifest.ID == req.PluginID {
				manifest = &item.Manifest
				break
			}
		}
		if manifest != nil && len(req.Models) > 0 {
			allowed := map[string]bool{}
			for _, m := range manifest.Models {
				allowed[m.ID] = true
			}
			for _, modelID := range req.Models {
				if !allowed[modelID] {
					writeError(w, http.StatusBadRequest, fmt.Errorf("model %q not declared by plugin %q", modelID, req.PluginID))
					return
				}
			}
		}
		if manifest != nil && len(req.Models) == 0 && len(manifest.Models) > 0 {
			req.Models = make([]string, 0, len(manifest.Models))
			for _, model := range manifest.Models {
				req.Models = append(req.Models, model.ID)
			}
		}
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	}
	req.UpdatedAt = time.Now().UTC()
	if err := h.sources.Upsert(req); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) deleteSource(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")
	if strings.TrimSpace(sourceID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("sourceID is required"))
		return
	}
	if err := h.sources.Delete(sourceID); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) listModels(w http.ResponseWriter, r *http.Request) {
	type modelItem struct {
		ID       string
		Name     string
		PluginID string
		Sources  map[string]bool
		Modes    []string
		Desc     string
	}

	registry := map[string]*modelItem{}
	for _, src := range h.sources.List() {
		if !src.Enabled || src.PluginID == "" {
			continue
		}
		desc, ok := h.plugins.Get(src.PluginID)
		if !ok || desc.Status != "ready" {
			continue
		}

		selected := map[string]bool{}
		for _, m := range src.Models {
			selected[m] = true
		}
		for _, model := range desc.Manifest.Models {
			if len(selected) > 0 && !selected[model.ID] {
				continue
			}
			item, exists := registry[model.ID]
			if !exists {
				item = &modelItem{
					ID:       model.ID,
					Name:     model.Name,
					PluginID: src.PluginID,
					Sources:  map[string]bool{},
					Modes:    append([]string(nil), model.Modes...),
					Desc:     model.Description,
				}
				registry[model.ID] = item
			}
			item.Sources[src.ID] = true
		}
	}

	items := make([]map[string]any, 0, len(registry))
	for _, item := range registry {
		sources := make([]string, 0, len(item.Sources))
		for sourceID := range item.Sources {
			sources = append(sources, sourceID)
		}
		sort.Strings(sources)
		items = append(items, map[string]any{
			"id":         item.ID,
			"object":     "model",
			"created":    0,
			"owned_by":   "web2api",
			"permission": []any{},
			"web2api": map[string]any{
				"name":        item.Name,
				"plugin_id":   item.PluginID,
				"source_ids":  sources,
				"modes":       item.Modes,
				"description": item.Desc,
			},
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return fmt.Sprint(items[i]["id"]) < fmt.Sprint(items[j]["id"])
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   items,
	})
}

type chatCompletionRequest struct {
	Model    string            `json:"model"`
	Messages []chatMessage     `json:"messages"`
	Stream   bool              `json:"stream"`
	Thinking bool              `json:"thinking"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responsesRequest struct {
	Model    string            `json:"model"`
	Input    json.RawMessage   `json:"input"`
	Stream   bool              `json:"stream"`
	Thinking bool              `json:"thinking"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

func (h *Handler) chatCompletions(w http.ResponseWriter, r *http.Request) {
	var req chatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	src, selection, result, err := h.executeChatPipeline(r, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if req.Stream {
		streamChatCompletion(w, req.Model, result.Content, result.Thinking)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": result.Content,
			},
			"finish_reason": "stop",
		}},
		"usage": result.Usage,
		"web2api": map[string]any{
			"source_id":  src.ID,
			"thinking":   req.Thinking,
			"plugin_id":  src.PluginID,
			"account_id": selection.Account.ID,
			"mode":       "plugin",
		},
	})
}

func (h *Handler) responses(w http.ResponseWriter, r *http.Request) {
	var req responsesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	messages, err := parseResponsesInput(req.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	chatReq := chatCompletionRequest{
		Model:    req.Model,
		Messages: messages,
		Stream:   req.Stream,
		Thinking: req.Thinking,
		Metadata: req.Metadata,
	}

	src, selection, result, err := h.executeChatPipeline(r, chatReq)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if req.Stream {
		streamResponses(w, req.Model, result.Content, result.Thinking)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         fmt.Sprintf("resp-%d", time.Now().UnixNano()),
		"object":     "response",
		"created_at": time.Now().Unix(),
		"status":     "completed",
		"model":      req.Model,
		"output": []map[string]any{{
			"type": "message",
			"id":   fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			"role": "assistant",
			"content": []map[string]any{{
				"type": "output_text",
				"text": result.Content,
			}},
		}},
		"output_text": result.Content,
		"usage":       result.Usage,
		"web2api": map[string]any{
			"source_id":  src.ID,
			"thinking":   req.Thinking,
			"plugin_id":  src.PluginID,
			"account_id": selection.Account.ID,
			"mode":       "plugin",
		},
	})
}

func (h *Handler) executeChatPipeline(r *http.Request, req chatCompletionRequest) (source.Config, account.Selection, plugin.ChatCompletionResult, error) {
	src, err := h.sources.FindByModel(req.Model)
	if err != nil {
		return source.Config{}, account.Selection{}, plugin.ChatCompletionResult{}, err
	}

	selection, err := h.accounts.Select(src.ID, time.Now().UTC())
	if err == nil {
		if req.Metadata == nil {
			req.Metadata = map[string]string{}
		}
		req.Metadata["account_id"] = selection.Account.ID
	}

	result, err := h.runChatPlugin(r, src, req, selection.Account)
	if err != nil {
		if selection.Account.ID != "" {
			_ = h.accounts.MarkFailure(selection.Account.ID, err.Error(), 5*time.Minute, time.Now().UTC())
		}
		return src, selection, plugin.ChatCompletionResult{}, err
	}
	if selection.Account.ID != "" {
		_ = h.accounts.MarkSuccess(selection.Account.ID, time.Now().UTC())
	}
	return src, selection, result, nil
}

func parseResponsesInput(raw json.RawMessage) ([]chatMessage, error) {
	if len(raw) == 0 {
		return nil, errors.New("input is required")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if strings.TrimSpace(text) == "" {
			return nil, errors.New("input text cannot be empty")
		}
		return []chatMessage{{Role: "user", Content: text}}, nil
	}

	var anyValue any
	if err := json.Unmarshal(raw, &anyValue); err != nil {
		return nil, err
	}
	array, ok := anyValue.([]any)
	if !ok {
		return nil, errors.New("input must be string or array")
	}

	messages := make([]chatMessage, 0, len(array))
	for _, item := range array {
		switch v := item.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				messages = append(messages, chatMessage{Role: "user", Content: v})
			}
		case map[string]any:
			role, _ := v["role"].(string)
			if role == "" {
				role = "user"
			}
			content := parseResponseContent(v["content"])
			if strings.TrimSpace(content) != "" {
				messages = append(messages, chatMessage{Role: role, Content: content})
			}
		}
	}
	if len(messages) == 0 {
		return nil, errors.New("no usable message found in input")
	}
	return messages, nil
}

func parseResponseContent(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		if t, ok := v["text"].(string); ok {
			return t
		}
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			switch c := item.(type) {
			case string:
				parts = append(parts, c)
			case map[string]any:
				if text, ok := c["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func (h *Handler) runChatPlugin(r *http.Request, src source.Config, req chatCompletionRequest, accountCfg account.Config) (plugin.ChatCompletionResult, error) {
	if src.PluginID == "" {
		return fallbackChatResult(src, req), nil
	}

	pluginReq := plugin.ChatCompletionRequest{
		Model:    req.Model,
		Stream:   req.Stream,
		Thinking: req.Thinking,
		Metadata: req.Metadata,
		Messages: make([]plugin.ChatMessage, 0, len(req.Messages)),
	}
	for _, msg := range req.Messages {
		pluginReq.Messages = append(pluginReq.Messages, plugin.ChatMessage{Role: msg.Role, Content: msg.Content})
	}

	return h.plugins.ExecuteChatCompletion(r.Context(), src.PluginID, pluginReq, src, accountCfg.ID, accountCfg.Name, accountCfg.Fields)
}

func fallbackChatResult(src source.Config, req chatCompletionRequest) plugin.ChatCompletionResult {
	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			last = req.Messages[i].Content
			break
		}
	}

	answer := fmt.Sprintf("source=%s plugin=%s model=%s echo=%s", src.ID, src.PluginID, req.Model, last)
	if src.MockReplyPrefix != "" {
		answer = src.MockReplyPrefix + last
	}

	result := plugin.ChatCompletionResult{
		Content: answer,
		Usage: plugin.Usage{
			PromptTokens:     len(req.Messages),
			CompletionTokens: len(strings.Fields(answer)),
			TotalTokens:      len(req.Messages) + len(strings.Fields(answer)),
		},
	}
	if req.Thinking {
		result.Thinking = "fallback reasoning placeholder"
	}
	return result
}

func streamChatCompletion(w http.ResponseWriter, model string, answer string, thinking string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}

	chunks := []string{}
	if thinking != "" {
		chunks = append(chunks, fmt.Sprintf(`{"id":"thinking","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning":%q}}]}`, thinking))
	}
	for _, token := range strings.Fields(answer) {
		chunks = append(chunks, fmt.Sprintf(`{"id":"chunk","object":"chat.completion.chunk","model":%q,"choices":[{"index":0,"delta":{"content":%q}}]}`, model, token+" "))
	}
	chunks = append(chunks, `{"id":"done","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)

	for _, chunk := range chunks {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func streamResponses(w http.ResponseWriter, model string, answer string, thinking string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}

	if thinking != "" {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"type":"response.reasoning.delta","model":%q,"delta":%q}`, model, thinking))
		flusher.Flush()
	}
	for _, token := range strings.Fields(answer) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"type":"response.output_text.delta","model":%q,"delta":%q}`, model, token+" "))
		flusher.Flush()
	}
	_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\"}\n\n")
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": err.Error(),
			"type":    http.StatusText(status),
		},
	})
}

func init() {
	_ = os.MkdirAll("plugins", 0o755)
}
