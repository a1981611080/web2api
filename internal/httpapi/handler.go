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
	"web2api/internal/plugin"
	"web2api/internal/source"
)

type Handler struct {
	plugins   *plugin.Manager
	sources   *source.Store
	accounts  *account.Store
	consumers *consumer.Store
	webDir    string
}

func NewHandler(plugins *plugin.Manager, sources *source.Store, accounts *account.Store, consumers *consumer.Store, webDir string) *Handler {
	return &Handler{plugins: plugins, sources: sources, accounts: accounts, consumers: consumers, webDir: webDir}
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
	r.Get("/webui", h.serveFile("webui/index.html"))
	r.Get("/webui/test", h.serveFile("webui/test.html"))
	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(h.webDir, "assets")))))

	r.Route("/api/admin", func(r chi.Router) {
		r.Get("/status", h.adminStatus)
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
		"catalog_models": map[string]any{
			"total": len(catalogModels),
			"items": catalogModels,
		},
		"routes": map[string]string{
			"plugins":  "/admin/plugins",
			"accounts": "/admin/accounts",
			"clients":  "/admin/clients",
			"models":   "/api/admin/catalog/models",
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
	if err := h.syncSourceForPlugin(req.PluginID); err != nil {
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
	item, findErr := h.accounts.Find(accountID)
	if err := h.accounts.Delete(accountID); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if findErr == nil && strings.TrimSpace(item.PluginID) != "" {
		_ = h.syncSourceForPlugin(item.PluginID)
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
	src, srcErr := findSourceByID(h.sources.List(), sourceIDForPlugin(item.PluginID))
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
		} else if len(src.Models) > 0 {
			model = src.Models[0]
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

func (h *Handler) syncSourceForPlugin(pluginID string) error {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return nil
	}
	modelSet := map[string]bool{}
	validation := ""
	enabled := false
	for _, acc := range h.accounts.List() {
		if strings.TrimSpace(acc.PluginID) != pluginID {
			continue
		}
		if acc.Status != account.StatusDisabled {
			enabled = true
		}
		for _, m := range acc.Models {
			modelSet[m] = true
		}
		if validation == "" && strings.TrimSpace(acc.ValidationMessage) != "" {
			validation = strings.TrimSpace(acc.ValidationMessage)
		}
	}
	models := make([]string, 0, len(modelSet))
	for m := range modelSet {
		models = append(models, m)
	}
	sort.Strings(models)
	src := source.Config{
		ID:                sourceIDForPlugin(pluginID),
		Name:              pluginID,
		PluginID:          pluginID,
		Enabled:           enabled,
		Models:            models,
		ValidationMessage: validation,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	return h.sources.Upsert(src)
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
	if strings.TrimSpace(req.ValidationMessage) == "" {
		req.ValidationMessage = "你好，请回复ok"
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

func (h *Handler) validateSource(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")
	if strings.TrimSpace(sourceID) == "" {
		writeError(w, http.StatusBadRequest, errors.New("sourceID is required"))
		return
	}
	src, err := findSourceByID(h.sources.List(), sourceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Model   string `json:"model"`
		Message string `json:"message"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		if len(src.Models) == 0 {
			writeError(w, http.StatusBadRequest, errors.New("source has no enabled models"))
			return
		}
		model = src.Models[0]
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		message = strings.TrimSpace(src.ValidationMessage)
	}
	if message == "" {
		message = "你好，请回复ok"
	}
	chatReq := chatCompletionRequest{Model: model, Stream: false, Thinking: false, Messages: []chatMessage{{Role: "user", Content: message}}, Metadata: map[string]string{"debug_validate": "1"}}
	_, selection, result, execErr := h.executeChatPipeline(r, chatReq, nil)
	if execErr != nil {
		msg := execErr.Error()
		if strings.Contains(msg, "upstream status 403") {
			msg = msg + " (请检查该来源账号的 access_token/cookie/user_agent 是否有效)"
		}
		writeError(w, http.StatusBadRequest, errors.New(msg))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                 true,
		"source_id":          sourceID,
		"model":              model,
		"account_id":         selection.Account.ID,
		"preview":            buildValidationPreview(result),
		"debug":              result.Raw,
		"validation_message": message,
	})
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
			"name":         item.Name,
			"plugin_id":    item.PluginID,
			"source_ids":   item.SourceIDs,
			"modes":        item.Modes,
			"source_model": item.SourceModel,
		},
	}
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

	src, selection, result, err := h.executeChatPipeline(r, req, consumerCfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	toolCalls := extractToolCalls(result.Raw)

	if req.Stream {
		if len(toolCalls) > 0 {
			streamChatCompletionToolCalls(w, req.Model, toolCalls)
			return
		}
		streamChatCompletion(w, req.Model, result.Content, result.Thinking)
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
		"model":   req.Model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
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
	src, selection, result, err := h.executeChatPipeline(r, chatReq, consumerCfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Stream {
		streamCompletions(w, req.Model, result.Content)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      fmt.Sprintf("cmpl-%d", time.Now().UnixNano()),
		"object":  "text_completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{{"text": result.Content, "index": 0, "finish_reason": "stop"}},
		"usage":   result.Usage,
		"web2api": map[string]any{"source_id": src.ID, "plugin_id": src.PluginID, "account_id": selection.Account.ID, "mode": "plugin"},
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

	src, selection, result, err := h.executeChatPipeline(r, chatReq, consumerCfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if req.Stream {
		streamResponses(w, req.Model, result.Content, result.Thinking)
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
			"source_id":  src.ID,
			"thinking":   req.Thinking,
			"plugin_id":  src.PluginID,
			"account_id": selection.Account.ID,
			"mode":       "plugin",
		},
	})
}

func (h *Handler) executeChatPipeline(r *http.Request, req chatCompletionRequest, consumerCfg *consumer.Config) (source.Config, account.Selection, plugin.ChatCompletionResult, error) {
	src, err := h.sources.FindByModel(req.Model)
	if err != nil {
		return source.Config{}, account.Selection{}, plugin.ChatCompletionResult{}, err
	}
	if !isModelAllowedForConsumer(consumerCfg, req.Model) {
		return source.Config{}, account.Selection{}, plugin.ChatCompletionResult{}, fmt.Errorf("model %q not allowed for this client", req.Model)
	}
	requiredFields := h.requiredAccountFields(src.PluginID)

	pluginReq := req

	now := time.Now().UTC()
	selection, err := h.accounts.Select(src.ID, now)
	if err != nil {
		if strings.TrimSpace(src.PluginID) != "" {
			fallback, fallbackErr := h.accounts.SelectByPlugin(src.PluginID, now)
			if fallbackErr == nil {
				selection = fallback
				err = nil
			}
		}
	}
	if err != nil {
		return src, account.Selection{}, plugin.ChatCompletionResult{}, fmt.Errorf("no available account for source %q", src.ID)
	}
	if missing := missingRequiredFields(selection.Account.Fields, requiredFields); len(missing) > 0 {
		return src, selection, plugin.ChatCompletionResult{}, fmt.Errorf("selected account %q missing required field(s): %s", selection.Account.ID, strings.Join(missing, ","))
	}
	if pluginReq.Metadata == nil {
		pluginReq.Metadata = map[string]string{}
	}
	pluginReq.Metadata["account_id"] = selection.Account.ID

	result, err := h.runChatPlugin(r, src, pluginReq, selection.Account)
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

func findSourceByID(items []source.Config, id string) (source.Config, error) {
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return source.Config{}, fmt.Errorf("source %q not found", id)
}

type catalogModel struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	PluginID    string   `json:"plugin_id"`
	SourceIDs   []string `json:"source_ids,omitempty"`
	SourceModel string   `json:"source_model"`
	Modes       []string `json:"modes,omitempty"`
}

func (h *Handler) collectCatalogModels() []catalogModel {
	registry := map[string]catalogModel{}
	for _, src := range h.sources.List() {
		if !strings.HasPrefix(src.ID, "plugin:") {
			continue
		}
		if !src.Enabled || src.PluginID == "" {
			continue
		}
		desc, ok := h.plugins.Get(src.PluginID)
		if !ok || desc.Status != "ready" {
			continue
		}
		selected := map[string]bool{}
		for _, modelID := range src.Models {
			selected[modelID] = true
		}
		for _, model := range desc.Manifest.Models {
			if len(selected) > 0 && !selected[model.ID] {
				continue
			}
			item, exists := registry[model.ID]
			if !exists {
				item = catalogModel{ID: model.ID, Name: model.Name, PluginID: src.PluginID, SourceModel: model.ID, Modes: model.Modes, SourceIDs: []string{src.ID}}
			} else {
				item.SourceIDs = append(item.SourceIDs, src.ID)
			}
			registry[model.ID] = item
		}
	}
	out := make([]catalogModel, 0, len(registry))
	for _, item := range registry {
		sort.Strings(item.SourceIDs)
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
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
		Messages: make([]plugin.ChatMessage, 0, len(req.Messages)),
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

func streamCompletions(w http.ResponseWriter, model string, answer string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	for _, token := range strings.Fields(answer) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"id":"cmpl-chunk","object":"text_completion","model":%q,"choices":[{"text":%q,"index":0,"finish_reason":null}]}`, model, token+" "))
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
