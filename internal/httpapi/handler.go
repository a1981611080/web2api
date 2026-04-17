package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"web2api/internal/account"
	"web2api/internal/consumer"
	"web2api/internal/plugin"
	"web2api/internal/source"
)

type Handler struct {
	plugins   *plugin.Manager
	accounts  *account.Store
	consumers *consumer.Store
	webDir    string
	toolLogMu sync.Mutex
	toolLogs  []toolCallLogEntry
}

func NewHandler(plugins *plugin.Manager, accounts *account.Store, consumers *consumer.Store, webDir string) *Handler {
	return &Handler{plugins: plugins, accounts: accounts, consumers: consumers, webDir: webDir}
}

type toolCallLogEntry struct {
	At         string `json:"at"`
	RequestID  string `json:"request_id,omitempty"`
	Model      string `json:"model"`
	Stream     bool   `json:"stream"`
	ToolChoice string `json:"tool_choice,omitempty"`
	ToolCount  int    `json:"tool_count"`
	Request    string `json:"request_preview,omitempty"`
	Finish     string `json:"finish_reason,omitempty"`
	ToolCalls  int    `json:"tool_calls"`
	Content    string `json:"content_preview,omitempty"`
	Raw        string `json:"raw_preview,omitempty"`
	Error      string `json:"error,omitempty"`
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
	r.Get("/admin/accounts", h.serveFile("admin/accounts.html"))
	r.Get("/admin/clients", h.serveFile("admin/clients.html"))
	r.Get("/admin/test", h.serveFile("admin/test.html"))
	r.Get("/admin/status", h.serveFile("admin/status.html"))
	r.Get("/admin/runtime", h.serveFile("admin/runtime.html"))
	r.Get("/admin/tool-calls", h.serveFile("admin/tool-calls.html"))
	r.Get("/webui", h.serveFile("webui/index.html"))
	r.Get("/webui/test", h.serveFile("webui/test.html"))
	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(h.webDir, "assets")))))

	r.Route("/api/admin", func(r chi.Router) {
		r.Get("/status", h.adminStatus)
		r.Get("/runtime", h.runtimeStatus)
		r.Get("/tool-calls", h.toolCallLogs)
		r.Get("/plugins", h.listPlugins)
		r.Post("/plugins/scan", h.scanPlugins)
		r.Get("/accounts", h.listAccounts)
		r.Post("/accounts", h.upsertAccount)
		r.Delete("/accounts/{accountID}", h.deleteAccount)
		r.Post("/accounts/{accountID}/validate", h.validateAccount)
		r.Post("/accounts/{accountID}/success", h.markAccountSuccess)
		r.Post("/accounts/{accountID}/failure", h.markAccountFailure)
		r.Get("/consumers", h.listConsumers)
		r.Post("/consumers", h.upsertConsumer)
		r.Delete("/consumers/{consumerID}", h.deleteConsumer)
		r.Get("/catalog/models", h.listCatalogModels)
	})

	r.Get("/v1/models", h.listModels)
	r.Get("/v1/models/{modelID}", h.getModel)
	r.Post("/v1/completions", h.completions)
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
	readyPlugins := 0
	for _, item := range plugins {
		if item.Status == "ready" {
			readyPlugins++
		}
	}
	accounts := h.accounts.List()
	consumers := h.consumers.List()
	catalogModels := h.collectCatalogModels()
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
	writeJSON(w, http.StatusOK, map[string]any{
		"service": map[string]any{
			"name":   "web2api",
			"status": "ok",
		},
		"plugins": map[string]any{
			"total": readyPlugins,
			"items": plugins,
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
		"catalog_models": map[string]any{
			"total": len(catalogModels),
			"items": catalogModels,
		},
		"routes": map[string]string{
			"plugins":    "/admin/plugins",
			"accounts":   "/admin/accounts",
			"clients":    "/admin/clients",
			"models":     "/api/admin/catalog/models",
			"test":       "/admin/test",
			"status":     "/admin/status",
			"runtime":    "/admin/runtime",
			"tool_calls": "/admin/tool-calls",
		},
	})
}

func (h *Handler) toolCallLogs(w http.ResponseWriter, r *http.Request) {
	h.toolLogMu.Lock()
	items := append([]toolCallLogEntry(nil), h.toolLogs...)
	h.toolLogMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"total": len(items), "items": items})
}

func (h *Handler) runtimeStatus(w http.ResponseWriter, r *http.Request) {
	items := h.plugins.ProcessPoolSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"now":   time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"total": len(items),
		"items": items,
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
		req.APIKey = fmt.Sprintf("sk-web2api-%d", time.Now().UnixNano())
	}
	if len(req.AllowedModels) > 0 {
		allowed := map[string]bool{}
		for _, item := range h.collectCatalogModels() {
			allowed[item.ID] = true
		}
		for _, modelID := range req.AllowedModels {
			if !allowed[modelID] {
				writeError(w, http.StatusBadRequest, fmt.Errorf("model %q not found in plugin catalog", modelID))
				return
			}
		}
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

func (h *Handler) listCatalogModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": h.collectCatalogModels()})
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
	if strings.TrimSpace(req.PluginID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("plugin_id is required"))
		return
	}
	if !h.plugins.Exists(req.PluginID) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("plugin %q not found", req.PluginID))
		return
	}
	req.SourceID = sourceIDForPlugin(req.PluginID)
	if strings.TrimSpace(req.Name) == "" {
		req.Name = req.ID
	}
	if strings.TrimSpace(req.ValidationMessage) == "" {
		req.ValidationMessage = "你好，请回复ok"
	}
	if err := h.validateAccountModels(req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := h.validateAccountFields(req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := h.accounts.Upsert(req); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) validateAccountFields(req account.Config) error {
	if strings.TrimSpace(req.PluginID) == "" {
		return nil
	}
	var manifest *plugin.Manifest
	for _, item := range h.plugins.List() {
		if item.Manifest.ID == req.PluginID {
			m := item.Manifest
			manifest = &m
			break
		}
	}
	if manifest == nil {
		return nil
	}
	for _, field := range manifest.AccountFields {
		if !field.Required {
			continue
		}
		if strings.TrimSpace(req.Fields[field.Key]) == "" {
			return fmt.Errorf("missing required account field: %s", field.Key)
		}
	}
	return nil
}

func (h *Handler) validateAccountModels(req account.Config) error {
	if len(req.Models) == 0 {
		return nil
	}
	var manifest *plugin.Manifest
	for _, item := range h.plugins.List() {
		if item.Manifest.ID == req.PluginID {
			m := item.Manifest
			manifest = &m
			break
		}
	}
	if manifest == nil {
		return nil
	}
	allowed := map[string]bool{}
	for _, m := range manifest.Models {
		allowed[m.ID] = true
	}
	for _, model := range req.Models {
		if !allowed[model] {
			return fmt.Errorf("model %q not declared by plugin %q", model, req.PluginID)
		}
	}
	return nil
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

func (h *Handler) validateAccount(w http.ResponseWriter, r *http.Request) {
	accountID := chi.URLParam(r, "accountID")
	item, err := h.accounts.Find(accountID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(item.PluginID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("account plugin_id is empty"))
		return
	}
	src, srcErr := h.resolveSourceForPlugin(item.PluginID)
	if srcErr != nil {
		writeError(w, http.StatusBadRequest, srcErr)
		return
	}
	var body struct {
		Model   string `json:"model"`
		Message string `json:"message"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	model := strings.TrimSpace(body.Model)
	if model == "" {
		if len(item.Models) > 0 {
			model = item.Models[0]
		} else {
			for _, m := range h.collectCatalogModels() {
				if m.PluginID == item.PluginID {
					model = m.ID
					break
				}
			}
		}
	}
	if model == "" {
		writeError(w, http.StatusBadRequest, errors.New("no model available for account validation"))
		return
	}
	msg := strings.TrimSpace(body.Message)
	if msg == "" {
		msg = strings.TrimSpace(item.ValidationMessage)
	}
	if msg == "" {
		msg = "你好，请回复ok"
	}
	req := chatCompletionRequest{Model: model, Stream: false, Thinking: false, Messages: []chatMessage{{Role: "user", Content: msg}}, Metadata: map[string]string{"debug_validate": "1"}}
	result, execErr := h.runChatPlugin(r, src, req, item)
	if execErr != nil {
		errMsg := execErr.Error()
		if strings.Contains(errMsg, "upstream status 403") && !strings.Contains(strings.ToLower(errMsg), "check access_token") {
			errMsg = errMsg + " (请检查该账号 access_token/cookie/user_agent 是否有效)"
		}
		writeError(w, http.StatusBadRequest, errors.New(errMsg))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "account_id": item.ID, "plugin_id": item.PluginID, "model": model, "preview": buildValidationPreview(result), "debug": result.Raw})
}

func sourceIDForPlugin(pluginID string) string {
	return "plugin:" + strings.TrimSpace(pluginID)
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

func buildValidationPreview(result plugin.ChatCompletionResult) string {
	if strings.TrimSpace(result.Content) != "" {
		return truncatePreview(result.Content, 220)
	}
	if strings.TrimSpace(result.Thinking) != "" {
		return truncatePreview("[thinking] "+result.Thinking, 220)
	}
	if result.Raw != nil {
		b, err := json.Marshal(result.Raw)
		if err == nil && strings.TrimSpace(string(b)) != "" {
			return truncatePreview("[raw] "+string(b), 220)
		}
	}
	return "(上游响应成功，但未返回可展示文本)"
}

func truncatePreview(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "..."
}

func (h *Handler) listModels(w http.ResponseWriter, r *http.Request) {
	consumerCfg, authErr := h.authenticateConsumer(r)
	if authErr != nil {
		writeError(w, http.StatusUnauthorized, authErr)
		return
	}

	catalog := h.collectCatalogModels()
	items := make([]map[string]any, 0, len(catalog))
	for _, item := range catalog {
		if !isModelAllowedForConsumer(consumerCfg, item.ID) {
			continue
		}
		items = append(items, buildModelObject(item))
	}
	sort.Slice(items, func(i, j int) bool {
		return fmt.Sprint(items[i]["id"]) < fmt.Sprint(items[j]["id"])
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   items,
	})
}

func (h *Handler) getModel(w http.ResponseWriter, r *http.Request) {
	consumerCfg, authErr := h.authenticateConsumer(r)
	if authErr != nil {
		writeError(w, http.StatusUnauthorized, authErr)
		return
	}
	modelID := chi.URLParam(r, "modelID")
	for _, item := range h.collectCatalogModels() {
		if item.ID == modelID {
			if !isModelAllowedForConsumer(consumerCfg, item.ID) {
				writeError(w, http.StatusForbidden, fmt.Errorf("model %q not allowed for this client", item.ID))
				return
			}
			writeJSON(w, http.StatusOK, buildModelObject(item))
			return
		}
	}
	writeError(w, http.StatusNotFound, fmt.Errorf("model %q not found", modelID))
}

func buildModelObject(item catalogModel) map[string]any {
	return map[string]any{
		"id":         item.ID,
		"object":     "model",
		"created":    0,
		"owned_by":   "web2api",
		"permission": []any{},
		"web2api": map[string]any{
			"name":          item.Name,
			"plugin_id":     item.PluginID,
			"plugin_source": sourceIDForPlugin(item.PluginID),
			"modes":         item.Modes,
			"plugin_model":  item.SourceModel,
		},
	}
}

type chatCompletionRequest struct {
	Model      string            `json:"model"`
	Messages   []chatMessage     `json:"messages"`
	Stream     bool              `json:"stream"`
	Thinking   bool              `json:"thinking"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Tools      []chatTool        `json:"tools,omitempty"`
	ToolChoice any               `json:"tool_choice,omitempty"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type completionsRequest struct {
	Model    string            `json:"model"`
	Prompt   any               `json:"prompt"`
	Stream   bool              `json:"stream"`
	Thinking bool              `json:"thinking"`
	Metadata map[string]string `json:"metadata,omitempty"`
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
	consumerCfg, authErr := h.authenticateConsumer(r)
	if authErr != nil {
		writeError(w, http.StatusUnauthorized, authErr)
		return
	}
	effectiveReq := req
	if len(req.Tools) > 0 {
		instruction := buildToolSystemInstruction(req.Tools, req.ToolChoice)
		if strings.TrimSpace(instruction) != "" {
			effectiveReq.Messages = append([]chatMessage{{Role: "system", Content: instruction}}, req.Messages...)
		}
	}
	if effectiveReq.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
			return
		}

		streamedContent := ""
		streamedThinking := ""
		hasChunk := false
		src, selection, result, err := h.executeChatPipelineStream(r, effectiveReq, consumerCfg, func(chunk plugin.ChatCompletionChunk) error {
			hasChunk = true
			if chunk.Thinking != "" {
				streamedThinking += chunk.Thinking
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"id":"thinking","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning":%q}}]}`, chunk.Thinking))
				flusher.Flush()
			}
			if chunk.Content != "" {
				streamedContent += chunk.Content
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"id":"chunk","object":"chat.completion.chunk","model":%q,"choices":[{"index":0,"delta":{"content":%q}}]}`, effectiveReq.Model, chunk.Content))
				flusher.Flush()
			}
			return nil
		})
		if err != nil {
			if len(req.Tools) > 0 {
				h.appendToolCallLog(r, effectiveReq, "", nil, plugin.ChatCompletionResult{}, err)
			}
			if !hasChunk {
				writeError(w, http.StatusBadRequest, err)
			} else {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"id":"chunk","object":"chat.completion.chunk","model":%q,"choices":[{"index":0,"delta":{"content":%q}}]}`, effectiveReq.Model, "[stream error] "+err.Error()))
				_, _ = fmt.Fprint(w, "data: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
				flusher.Flush()
			}
			return
		}
		_ = src
		_ = selection
		toolCalls := extractToolCalls(result.Raw)
		if len(req.Tools) > 0 {
			finishLog := "stop"
			if len(toolCalls) > 0 {
				finishLog = "tool_calls"
			}
			h.appendToolCallLog(r, effectiveReq, finishLog, toolCalls, result, nil)
		}
		if len(req.Tools) > 0 && isToolCallRequired(req.ToolChoice) && len(toolCalls) == 0 {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"id":"chunk","object":"chat.completion.chunk","model":%q,"choices":[{"index":0,"delta":{"content":%q}}]}`, effectiveReq.Model, "[tool error] model did not return required tool_calls"))
			_, _ = fmt.Fprint(w, "data: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
		if !hasChunk {
			if len(toolCalls) > 0 {
				streamChatCompletionToolCalls(w, effectiveReq.Model, toolCalls)
				return
			}
			streamChatCompletion(w, effectiveReq.Model, result.Content, result.Thinking)
			return
		}

		if strings.HasPrefix(result.Thinking, streamedThinking) {
			tail := strings.TrimPrefix(result.Thinking, streamedThinking)
			if tail != "" {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"id":"thinking","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning":%q}}]}`, tail))
				flusher.Flush()
			}
		}
		if strings.HasPrefix(result.Content, streamedContent) {
			tail := strings.TrimPrefix(result.Content, streamedContent)
			if tail != "" {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"id":"chunk","object":"chat.completion.chunk","model":%q,"choices":[{"index":0,"delta":{"content":%q}}]}`, effectiveReq.Model, tail))
				flusher.Flush()
			}
		}
		finish := "stop"
		if len(toolCalls) > 0 && strings.TrimSpace(result.Content) == "" {
			finish = "tool_calls"
		}
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\n", finish)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	src, selection, result, err := h.executeChatPipeline(r, effectiveReq, consumerCfg)
	if err != nil {
		if len(req.Tools) > 0 {
			h.appendToolCallLog(r, effectiveReq, "", nil, plugin.ChatCompletionResult{}, err)
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	toolCalls := extractToolCalls(result.Raw)
	if len(req.Tools) > 0 {
		finishLog := "stop"
		if len(toolCalls) > 0 {
			finishLog = "tool_calls"
		}
		h.appendToolCallLog(r, effectiveReq, finishLog, toolCalls, result, nil)
	}
	if len(req.Tools) > 0 && isToolCallRequired(req.ToolChoice) && len(toolCalls) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("model did not return required tool_calls"))
		return
	}
	finishReason := "stop"
	message := map[string]any{"role": "assistant", "content": result.Content}
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
		message["content"] = ""
		message["tool_calls"] = formatChatToolCalls(toolCalls)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   effectiveReq.Model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
		}},
		"usage": result.Usage,
		"web2api": map[string]any{
			"thinking":   effectiveReq.Thinking,
			"plugin_id":  src.PluginID,
			"account_id": selection.Account.ID,
			"mode":       "plugin",
		},
	})
}

func (h *Handler) completions(w http.ResponseWriter, r *http.Request) {
	var req completionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	consumerCfg, authErr := h.authenticateConsumer(r)
	if authErr != nil {
		writeError(w, http.StatusUnauthorized, authErr)
		return
	}
	prompt := parsePrompt(req.Prompt)
	if strings.TrimSpace(prompt) == "" {
		writeError(w, http.StatusBadRequest, errors.New("prompt is required"))
		return
	}
	chatReq := chatCompletionRequest{
		Model:    req.Model,
		Stream:   req.Stream,
		Thinking: req.Thinking,
		Metadata: req.Metadata,
		Messages: []chatMessage{{Role: "user", Content: prompt}},
	}
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
			return
		}

		streamedText := ""
		hasChunk := false
		_, _, result, err := h.executeChatPipelineStream(r, chatReq, consumerCfg, func(chunk plugin.ChatCompletionChunk) error {
			hasChunk = true
			if chunk.Content != "" {
				streamedText += chunk.Content
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"id":"cmpl-chunk","object":"text_completion","model":%q,"choices":[{"text":%q,"index":0,"finish_reason":null}]}`, req.Model, chunk.Content))
				flusher.Flush()
			}
			return nil
		})
		if err != nil {
			if !hasChunk {
				writeError(w, http.StatusBadRequest, err)
			} else {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"id":"cmpl-chunk","object":"text_completion","model":%q,"choices":[{"text":%q,"index":0,"finish_reason":null}]}`, req.Model, "[stream error] "+err.Error()))
				_, _ = fmt.Fprint(w, "data: {\"id\":\"cmpl-done\",\"object\":\"text_completion\",\"choices\":[{\"text\":\"\",\"index\":0,\"finish_reason\":\"stop\"}]}\n\n")
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
				flusher.Flush()
			}
			return
		}
		if !hasChunk {
			streamCompletions(w, req.Model, result.Content)
			return
		}
		if strings.HasPrefix(result.Content, streamedText) {
			tail := strings.TrimPrefix(result.Content, streamedText)
			if tail != "" {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"id":"cmpl-chunk","object":"text_completion","model":%q,"choices":[{"text":%q,"index":0,"finish_reason":null}]}`, req.Model, tail))
				flusher.Flush()
			}
		}
		_, _ = fmt.Fprint(w, "data: {\"id\":\"cmpl-done\",\"object\":\"text_completion\",\"choices\":[{\"text\":\"\",\"index\":0,\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	src, selection, result, err := h.executeChatPipeline(r, chatReq, consumerCfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      fmt.Sprintf("cmpl-%d", time.Now().UnixNano()),
		"object":  "text_completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{{"text": result.Content, "index": 0, "finish_reason": "stop"}},
		"usage":   result.Usage,
		"web2api": map[string]any{"plugin_id": src.PluginID, "account_id": selection.Account.ID, "mode": "plugin"},
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

	consumerCfg, authErr := h.authenticateConsumer(r)
	if authErr != nil {
		writeError(w, http.StatusUnauthorized, authErr)
		return
	}

	chatReq := chatCompletionRequest{
		Model:    req.Model,
		Messages: messages,
		Stream:   req.Stream,
		Thinking: req.Thinking,
		Metadata: req.Metadata,
	}

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
			return
		}

		streamedText := ""
		streamedThinking := ""
		hasChunk := false
		_, _, result, err := h.executeChatPipelineStream(r, chatReq, consumerCfg, func(chunk plugin.ChatCompletionChunk) error {
			hasChunk = true
			if chunk.Thinking != "" {
				streamedThinking += chunk.Thinking
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"type":"response.reasoning.delta","model":%q,"delta":%q}`, req.Model, chunk.Thinking))
				flusher.Flush()
			}
			if chunk.Content != "" {
				streamedText += chunk.Content
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"type":"response.output_text.delta","model":%q,"delta":%q}`, req.Model, chunk.Content))
				flusher.Flush()
			}
			return nil
		})
		if err != nil {
			if !hasChunk {
				writeError(w, http.StatusBadRequest, err)
			} else {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"type":"response.output_text.delta","model":%q,"delta":%q}`, req.Model, "[stream error] "+err.Error()))
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\"}\n\n")
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
				flusher.Flush()
			}
			return
		}
		if !hasChunk {
			streamResponses(w, req.Model, result.Content, result.Thinking)
			return
		}
		if strings.HasPrefix(result.Thinking, streamedThinking) {
			tail := strings.TrimPrefix(result.Thinking, streamedThinking)
			if tail != "" {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"type":"response.reasoning.delta","model":%q,"delta":%q}`, req.Model, tail))
				flusher.Flush()
			}
		}
		if strings.HasPrefix(result.Content, streamedText) {
			tail := strings.TrimPrefix(result.Content, streamedText)
			if tail != "" {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"type":"response.output_text.delta","model":%q,"delta":%q}`, req.Model, tail))
				flusher.Flush()
			}
		}
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\"}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	src, selection, result, err := h.executeChatPipeline(r, chatReq, consumerCfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	toolCalls := extractToolCalls(result.Raw)
	outputItems := make([]map[string]any, 0, len(toolCalls)+1)
	if len(toolCalls) > 0 {
		for _, tc := range toolCalls {
			outputItems = append(outputItems, map[string]any{
				"type":      "function_call",
				"id":        tc.ID,
				"call_id":   tc.ID,
				"name":      tc.Name,
				"arguments": tc.Arguments,
				"status":    "completed",
			})
		}
	}
	if strings.TrimSpace(result.Content) != "" || len(outputItems) == 0 {
		outputItems = append(outputItems, map[string]any{
			"type": "message",
			"id":   fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			"role": "assistant",
			"content": []map[string]any{{
				"type": "output_text",
				"text": result.Content,
			}},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":          fmt.Sprintf("resp-%d", time.Now().UnixNano()),
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      "completed",
		"model":       req.Model,
		"output":      outputItems,
		"output_text": result.Content,
		"usage":       result.Usage,
		"web2api": map[string]any{
			"thinking":   req.Thinking,
			"plugin_id":  src.PluginID,
			"account_id": selection.Account.ID,
			"mode":       "plugin",
		},
	})
}

func (h *Handler) executeChatPipeline(r *http.Request, req chatCompletionRequest, consumerCfg *consumer.Config) (source.Config, account.Selection, plugin.ChatCompletionResult, error) {
	src, err := h.resolveSourceForModel(req.Model)
	if err != nil {
		return source.Config{}, account.Selection{}, plugin.ChatCompletionResult{}, err
	}
	if !isModelAllowedForConsumer(consumerCfg, req.Model) {
		return source.Config{}, account.Selection{}, plugin.ChatCompletionResult{}, fmt.Errorf("model %q not allowed for this client", req.Model)
	}
	requiredFields := h.requiredAccountFields(src.PluginID)

	pluginReq := req

	now := time.Now().UTC()
	selection, err := h.accounts.SelectByPluginModel(src.PluginID, req.Model, now)
	if err != nil {
		selection, err = h.accounts.SelectByPlugin(src.PluginID, now)
	}
	if err != nil {
		return src, account.Selection{}, plugin.ChatCompletionResult{}, fmt.Errorf("no available account for plugin %q model %q", src.PluginID, req.Model)
	}
	if missing := missingRequiredFields(selection.Account.Fields, requiredFields); len(missing) > 0 {
		return src, selection, plugin.ChatCompletionResult{}, fmt.Errorf("selected account %q missing required field(s): %s", selection.Account.ID, strings.Join(missing, ","))
	}
	if pluginReq.Metadata == nil {
		pluginReq.Metadata = map[string]string{}
	}
	if rid := strings.TrimSpace(middleware.GetReqID(r.Context())); rid != "" {
		pluginReq.Metadata["request_trace_id"] = rid
	}
	pluginReq.Metadata["account_id"] = selection.Account.ID

	result, err := h.runChatPlugin(r, src, pluginReq, selection.Account)
	if err != nil {
		if selection.Account.ID != "" && shouldMarkAccountFailure(err) {
			_ = h.accounts.MarkFailure(selection.Account.ID, err.Error(), 5*time.Minute, time.Now().UTC())
		}
		return src, selection, plugin.ChatCompletionResult{}, err
	}
	if selection.Account.ID != "" {
		_ = h.accounts.MarkSuccess(selection.Account.ID, time.Now().UTC())
	}
	return src, selection, result, nil
}

func (h *Handler) executeChatPipelineStream(r *http.Request, req chatCompletionRequest, consumerCfg *consumer.Config, onChunk func(plugin.ChatCompletionChunk) error) (source.Config, account.Selection, plugin.ChatCompletionResult, error) {
	src, err := h.resolveSourceForModel(req.Model)
	if err != nil {
		return source.Config{}, account.Selection{}, plugin.ChatCompletionResult{}, err
	}
	if !isModelAllowedForConsumer(consumerCfg, req.Model) {
		return source.Config{}, account.Selection{}, plugin.ChatCompletionResult{}, fmt.Errorf("model %q not allowed for this client", req.Model)
	}
	requiredFields := h.requiredAccountFields(src.PluginID)

	pluginReq := req

	now := time.Now().UTC()
	selection, err := h.accounts.SelectByPluginModel(src.PluginID, req.Model, now)
	if err != nil {
		selection, err = h.accounts.SelectByPlugin(src.PluginID, now)
	}
	if err != nil {
		return src, account.Selection{}, plugin.ChatCompletionResult{}, fmt.Errorf("no available account for plugin %q model %q", src.PluginID, req.Model)
	}
	if missing := missingRequiredFields(selection.Account.Fields, requiredFields); len(missing) > 0 {
		return src, selection, plugin.ChatCompletionResult{}, fmt.Errorf("selected account %q missing required field(s): %s", selection.Account.ID, strings.Join(missing, ","))
	}
	if pluginReq.Metadata == nil {
		pluginReq.Metadata = map[string]string{}
	}
	if rid := strings.TrimSpace(middleware.GetReqID(r.Context())); rid != "" {
		pluginReq.Metadata["request_trace_id"] = rid
	}
	pluginReq.Metadata["account_id"] = selection.Account.ID

	result, err := h.runChatPluginStream(r, src, pluginReq, selection.Account, onChunk)
	if err != nil {
		if selection.Account.ID != "" && shouldMarkAccountFailure(err) {
			_ = h.accounts.MarkFailure(selection.Account.ID, err.Error(), 5*time.Minute, time.Now().UTC())
		}
		return src, selection, plugin.ChatCompletionResult{}, err
	}
	if selection.Account.ID != "" {
		_ = h.accounts.MarkSuccess(selection.Account.ID, time.Now().UTC())
	}
	return src, selection, result, nil
}

func (h *Handler) requiredAccountFields(pluginID string) []string {
	for _, item := range h.plugins.List() {
		if item.Manifest.ID != pluginID {
			continue
		}
		out := make([]string, 0, len(item.Manifest.AccountFields))
		for _, field := range item.Manifest.AccountFields {
			if field.Required {
				out = append(out, field.Key)
			}
		}
		return out
	}
	return nil
}

func missingRequiredFields(values map[string]string, required []string) []string {
	if len(required) == 0 {
		return nil
	}
	missing := make([]string, 0, len(required))
	for _, key := range required {
		if strings.TrimSpace(values[key]) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

func buildToolSystemInstruction(tools []chatTool, toolChoice any) string {
	if len(tools) == 0 {
		return ""
	}
	compact := compactToolsForPrompt(tools)
	b, _ := json.Marshal(compact)
	choice := toolChoiceString(toolChoice)
	if strings.TrimSpace(choice) == "" {
		choice = "auto"
	}
	return "[Tool Calling Contract]\nYou MUST only use tools listed in available_tools; never invent tool names.\nIf tool is NOT needed, answer normally in plain text.\nIf tool IS needed, output ONLY XML and nothing else: <tool_calls><tool_call><tool_name>NAME</tool_name><parameters>{JSON}</parameters></tool_call></tool_calls>.\nDo not claim tool execution in natural language.\nFor tool_choice=required you must return tool XML; for tool_choice=auto choose based on necessity.\ntool_choice=" + choice + "\navailable_tools=" + string(b)
}

func compactToolsForPrompt(tools []chatTool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		name := strings.TrimSpace(t.Function.Name)
		if name == "" {
			continue
		}
		entry := map[string]any{"name": name}
		desc := strings.TrimSpace(t.Function.Description)
		if desc != "" {
			entry["description"] = truncatePreview(desc, 180)
		}
		if params := compactToolParameters(t.Function.Parameters); len(params) > 0 {
			entry["parameters"] = params
		}
		out = append(out, entry)
	}
	return out
}

func compactToolParameters(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return nil
	}
	out := map[string]any{}
	if typ, ok := schema["type"].(string); ok && strings.TrimSpace(typ) != "" {
		out["type"] = strings.TrimSpace(typ)
	}
	if req, ok := schema["required"].([]any); ok && len(req) > 0 {
		names := make([]string, 0, len(req))
		for _, item := range req {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				names = append(names, s)
			}
		}
		if len(names) > 0 {
			out["required"] = names
		}
	}
	if props, ok := schema["properties"].(map[string]any); ok && len(props) > 0 {
		fields := make([]map[string]any, 0, len(props))
		for k, raw := range props {
			item := map[string]any{"name": k}
			if p, ok := raw.(map[string]any); ok {
				if t, ok := p["type"].(string); ok && strings.TrimSpace(t) != "" {
					item["type"] = strings.TrimSpace(t)
				}
				if d, ok := p["description"].(string); ok && strings.TrimSpace(d) != "" {
					item["description"] = truncatePreview(d, 120)
				}
			}
			fields = append(fields, item)
		}
		sort.Slice(fields, func(i, j int) bool {
			return fmt.Sprint(fields[i]["name"]) < fmt.Sprint(fields[j]["name"])
		})
		out["fields"] = fields
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (h *Handler) appendToolCallLog(r *http.Request, req chatCompletionRequest, finish string, calls []toolCall, result plugin.ChatCompletionResult, err error) {
	entry := toolCallLogEntry{
		At:         time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		RequestID:  strings.TrimSpace(middleware.GetReqID(r.Context())),
		Model:      req.Model,
		Stream:     req.Stream,
		ToolChoice: toolChoiceString(req.ToolChoice),
		ToolCount:  len(req.Tools),
		Finish:     finish,
		ToolCalls:  len(calls),
	}
	if b, marshalErr := json.Marshal(req); marshalErr == nil {
		entry.Request = string(b)
	}
	if err != nil {
		entry.Error = err.Error()
	}
	if text := strings.TrimSpace(result.Content); text != "" {
		entry.Content = text
	}
	if result.Raw != nil {
		if b, marshalErr := json.Marshal(result.Raw); marshalErr == nil {
			entry.Raw = string(b)
		}
	}

	h.toolLogMu.Lock()
	h.toolLogs = append([]toolCallLogEntry{entry}, h.toolLogs...)
	if len(h.toolLogs) > 200 {
		h.toolLogs = h.toolLogs[:200]
	}
	h.toolLogMu.Unlock()

	log.Printf("tool-call trace=%s model=%s stream=%v choice=%s tools=%d finish=%s calls=%d request=%q err=%s content=%q raw=%q",
		entry.RequestID, entry.Model, entry.Stream, entry.ToolChoice, entry.ToolCount, entry.Finish, entry.ToolCalls, entry.Request, entry.Error, entry.Content, entry.Raw)
}

func toolChoiceString(choice any) string {
	if choice == nil {
		return ""
	}
	switch v := choice.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func isToolCallRequired(choice any) bool {
	if choice == nil {
		return false
	}
	switch v := choice.(type) {
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "required"
	case map[string]any:
		if t, ok := v["type"].(string); ok && strings.EqualFold(strings.TrimSpace(t), "function") {
			return true
		}
		if fn, ok := v["function"].(map[string]any); ok {
			if _, ok := fn["name"].(string); ok {
				return true
			}
		}
	}
	return false
}

func shouldMarkAccountFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	internalSignals := []string{
		"plugin exceeded max steps",
		"plugin step timeout",
		"decode plugin output",
		"unsupported output type",
		"empty stdout",
		"stream error",
	}
	for _, s := range internalSignals {
		if strings.Contains(msg, s) {
			return false
		}
	}
	upstreamSignals := []string{
		"upstream status",
		"forbidden",
		"rate limit",
		"too many requests",
		"invalid credentials",
		"check access_token",
		"cookie",
		"user_agent",
	}
	for _, s := range upstreamSignals {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func (h *Handler) authenticateConsumer(r *http.Request) (*consumer.Config, error) {
	if !h.consumers.HasAny() {
		return nil, nil
	}
	apiKey := extractAPIKey(r)
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("missing client api key")
	}
	item, err := h.consumers.FindByAPIKey(apiKey)
	if err != nil {
		return nil, errors.New("invalid client api key")
	}
	if !item.Enabled {
		return nil, errors.New("client account is disabled")
	}
	return &item, nil
}

func extractAPIKey(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

func isModelAllowedForConsumer(cfg *consumer.Config, modelID string) bool {
	if cfg == nil || len(cfg.AllowedModels) == 0 {
		return true
	}
	for _, item := range cfg.AllowedModels {
		if item == modelID {
			return true
		}
	}
	return false
}

type catalogModel struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	PluginID    string   `json:"plugin_id"`
	SourceModel string   `json:"source_model"`
	Modes       []string `json:"modes,omitempty"`
}

func (h *Handler) collectCatalogModels() []catalogModel {
	registry := map[string]catalogModel{}
	for _, desc := range h.plugins.List() {
		if desc.Status != "ready" || strings.TrimSpace(desc.Manifest.ID) == "" {
			continue
		}
		for _, model := range desc.Manifest.Models {
			if strings.TrimSpace(model.ID) == "" {
				continue
			}
			item, exists := registry[model.ID]
			if !exists {
				item = catalogModel{ID: model.ID, Name: model.Name, PluginID: desc.Manifest.ID, SourceModel: model.ID, Modes: model.Modes}
			}
			registry[model.ID] = item
		}
	}
	out := make([]catalogModel, 0, len(registry))
	for _, item := range registry {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (h *Handler) resolveSourceForModel(modelID string) (source.Config, error) {
	for _, item := range h.collectCatalogModels() {
		if item.ID != modelID {
			continue
		}
		srcID := sourceIDForPlugin(item.PluginID)
		return source.Config{
			ID:       srcID,
			Name:     item.PluginID,
			PluginID: item.PluginID,
			Enabled:  true,
			Models:   []string{item.ID},
		}, nil
	}
	return source.Config{}, fmt.Errorf("no enabled plugin model matched %q", modelID)
}

func (h *Handler) resolveSourceForPlugin(pluginID string) (source.Config, error) {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return source.Config{}, errors.New("plugin_id is required")
	}
	srcID := sourceIDForPlugin(pluginID)
	return source.Config{ID: srcID, Name: pluginID, PluginID: pluginID, Enabled: true}, nil
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

func parsePrompt(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []string:
		return strings.Join(v, "\n")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, parsePrompt(item))
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text
		}
	}
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
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
		Tools:    make([]plugin.Tool, 0, len(req.Tools)),
		Messages: make([]plugin.ChatMessage, 0, len(req.Messages)),
	}
	if req.ToolChoice != nil {
		if b, err := json.Marshal(req.ToolChoice); err == nil {
			pluginReq.ToolChoice = b
		}
	}
	for _, t := range req.Tools {
		pluginReq.Tools = append(pluginReq.Tools, plugin.Tool{Type: t.Type, Function: plugin.ToolFunction{Name: t.Function.Name, Description: t.Function.Description, Parameters: t.Function.Parameters}})
	}
	for _, msg := range req.Messages {
		content := parseResponseContent(msg.Content)
		if strings.TrimSpace(content) == "" {
			continue
		}
		pluginReq.Messages = append(pluginReq.Messages, plugin.ChatMessage{Role: msg.Role, Content: content})
	}

	return h.plugins.ExecuteChatCompletion(r.Context(), src.PluginID, pluginReq, src, accountCfg.ID, accountCfg.Name, accountCfg.Fields)
}

func (h *Handler) runChatPluginStream(r *http.Request, src source.Config, req chatCompletionRequest, accountCfg account.Config, onChunk func(plugin.ChatCompletionChunk) error) (plugin.ChatCompletionResult, error) {
	if src.PluginID == "" {
		return fallbackChatResult(src, req), nil
	}

	pluginReq := plugin.ChatCompletionRequest{
		Model:    req.Model,
		Stream:   req.Stream,
		Thinking: req.Thinking,
		Metadata: req.Metadata,
		Tools:    make([]plugin.Tool, 0, len(req.Tools)),
		Messages: make([]plugin.ChatMessage, 0, len(req.Messages)),
	}
	if req.ToolChoice != nil {
		if b, err := json.Marshal(req.ToolChoice); err == nil {
			pluginReq.ToolChoice = b
		}
	}
	for _, t := range req.Tools {
		pluginReq.Tools = append(pluginReq.Tools, plugin.Tool{Type: t.Type, Function: plugin.ToolFunction{Name: t.Function.Name, Description: t.Function.Description, Parameters: t.Function.Parameters}})
	}
	for _, msg := range req.Messages {
		content := parseResponseContent(msg.Content)
		if strings.TrimSpace(content) == "" {
			continue
		}
		pluginReq.Messages = append(pluginReq.Messages, plugin.ChatMessage{Role: msg.Role, Content: content})
	}

	return h.plugins.ExecuteChatCompletionStream(r.Context(), src.PluginID, pluginReq, src, accountCfg.ID, accountCfg.Name, accountCfg.Fields, onChunk)
}

func fallbackChatResult(src source.Config, req chatCompletionRequest) plugin.ChatCompletionResult {
	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			last = parseResponseContent(req.Messages[i].Content)
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
	for _, token := range splitTextForStream(answer, 24) {
		chunks = append(chunks, fmt.Sprintf(`{"id":"chunk","object":"chat.completion.chunk","model":%q,"choices":[{"index":0,"delta":{"content":%q}}]}`, model, token))
	}
	chunks = append(chunks, `{"id":"done","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)

	for _, chunk := range chunks {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func streamChatCompletionToolCalls(w http.ResponseWriter, model string, calls []toolCall) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	for i, tc := range calls {
		chunk := map[string]any{
			"id":     "chunk",
			"object": "chat.completion.chunk",
			"model":  model,
			"choices": []map[string]any{{
				"index": i,
				"delta": map[string]any{
					"tool_calls": []map[string]any{{
						"index": i,
						"id":    tc.ID,
						"type":  "function",
						"function": map[string]any{
							"name":      tc.Name,
							"arguments": tc.Arguments,
						},
					}},
				},
			}},
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", mustJSON(chunk))
		flusher.Flush()
	}
	_, _ = fmt.Fprint(w, "data: {\"id\":\"done\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

type toolCall struct {
	ID        string
	Name      string
	Arguments string
}

func extractToolCalls(raw any) []toolCall {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	list, ok := obj["tool_calls"].([]any)
	if !ok {
		if typed, ok2 := obj["tool_calls"].([]map[string]any); ok2 {
			list = make([]any, 0, len(typed))
			for _, v := range typed {
				list = append(list, v)
			}
		} else {
			return nil
		}
	}
	out := make([]toolCall, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(m["name"]))
		if name == "" {
			continue
		}
		id := strings.TrimSpace(fmt.Sprint(m["id"]))
		if id == "" {
			id = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), i)
		}
		arguments := "{}"
		if v, ok := m["arguments"]; ok {
			switch arg := v.(type) {
			case string:
				if strings.TrimSpace(arg) != "" {
					arguments = arg
				}
			default:
				b, err := json.Marshal(arg)
				if err == nil {
					arguments = string(b)
				}
			}
		}
		out = append(out, toolCall{ID: id, Name: name, Arguments: arguments})
	}
	return out
}

func formatChatToolCalls(calls []toolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, tc := range calls {
		out = append(out, map[string]any{
			"id":   tc.ID,
			"type": "function",
			"function": map[string]any{
				"name":      tc.Name,
				"arguments": tc.Arguments,
			},
		})
	}
	return out
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func splitTextForStream(text string, maxRunes int) []string {
	if text == "" {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = 24
	}
	runes := []rune(text)
	out := make([]string, 0, (len(runes)+maxRunes-1)/maxRunes)
	for i := 0; i < len(runes); i += maxRunes {
		end := i + maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
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
	for _, token := range splitTextForStream(answer, 24) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"type":"response.output_text.delta","model":%q,"delta":%q}`, model, token))
		flusher.Flush()
	}
	_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\"}\n\n")
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func streamCompletions(w http.ResponseWriter, model string, answer string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	for _, token := range splitTextForStream(answer, 24) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"id":"cmpl-chunk","object":"text_completion","model":%q,"choices":[{"text":%q,"index":0,"finish_reason":null}]}`, model, token))
		flusher.Flush()
	}
	_, _ = fmt.Fprint(w, "data: {\"id\":\"cmpl-done\",\"object\":\"text_completion\",\"choices\":[{\"text\":\"\",\"index\":0,\"finish_reason\":\"stop\"}]}\n\n")
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	typeName := "invalid_request_error"
	var code any
	if status == http.StatusUnauthorized {
		typeName = "authentication_error"
		code = "invalid_api_key"
	} else if status == http.StatusForbidden {
		typeName = "permission_error"
	} else if status >= 500 {
		typeName = "server_error"
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": err.Error(),
			"type":    typeName,
			"param":   nil,
			"code":    code,
		},
	})
}

func init() {
	_ = os.MkdirAll("plugins", 0o755)
}
