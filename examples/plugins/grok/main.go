package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"time"
)

type invocation struct {
	Action      string       `json:"action"`
	Step        int          `json:"step"`
	Input       *input       `json:"input,omitempty"`
	HostResults []hostResult `json:"host_results,omitempty"`
}

type input struct {
	Request request `json:"request"`
	Source  source  `json:"source"`
	Account account `json:"account"`
}

type request struct {
	Model    string            `json:"model"`
	Thinking bool              `json:"thinking"`
	Metadata map[string]string `json:"metadata"`
	Messages []message         `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type source struct {
	BaseURL  string            `json:"base_url"`
	Metadata map[string]string `json:"metadata"`
}

type account struct {
	ID     string            `json:"id"`
	Fields map[string]string `json:"fields"`
}

type output struct {
	Type     string      `json:"type"`
	Manifest *manifest   `json:"manifest,omitempty"`
	Requests []hostReq   `json:"requests,omitempty"`
	Response *chatResp   `json:"response,omitempty"`
	Error    string      `json:"error,omitempty"`
	State    interface{} `json:"state,omitempty"`
}

type manifest struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Version       string           `json:"version"`
	Description   string           `json:"description,omitempty"`
	Entry         string           `json:"entry,omitempty"`
	Capabilities  []string         `json:"capabilities,omitempty"`
	Models        []map[string]any `json:"models,omitempty"`
	AccountFields []map[string]any `json:"account_fields,omitempty"`
	Author        string           `json:"author,omitempty"`
}

type hostReq struct {
	ID   string    `json:"id"`
	Kind string    `json:"kind"`
	HTTP *httpReq  `json:"http,omitempty"`
	WS   *struct{} `json:"ws,omitempty"`
}

type httpReq struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      string            `json:"body,omitempty"`
	TimeoutMS int               `json:"timeout_ms,omitempty"`
}

type hostResult struct {
	ID    string      `json:"id"`
	Kind  string      `json:"kind"`
	Error string      `json:"error,omitempty"`
	HTTP  *httpResult `json:"http,omitempty"`
}

type httpResult struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body,omitempty"`
}

type chatResp struct {
	Content  string `json:"content"`
	Thinking string `json:"thinking,omitempty"`
	Raw      any    `json:"raw,omitempty"`
}

var (
	xmlRootRE  = regexp.MustCompile(`(?is)<tool_calls\s*>(.*?)</tool_calls\s*>`)
	xmlCallRE  = regexp.MustCompile(`(?is)<tool_call\s*>(.*?)</tool_call\s*>`)
	xmlNameRE  = regexp.MustCompile(`(?is)<tool_name\s*>(.*?)</tool_name\s*>`)
	xmlParamRE = regexp.MustCompile(`(?is)<parameters\s*>(.*?)</parameters\s*>`)
)

func main() {
	inv := readInvocation()
	switch inv.Action {
	case "plugin_info":
		pluginInfo()
	case "chat_completions":
		chatCompletions(inv)
	default:
		write(output{Type: "error", Error: "unsupported action"})
	}
}

func pluginInfo() {
	write(output{Type: "response", Manifest: &manifest{
		ID:          "grok-web",
		Name:        "Grok Web Plugin",
		Version:     "0.2.0",
		Description: "Translate Grok Web app-chat to OpenAI-compatible outputs",
		Entry:       "chat_completions",
		Capabilities: []string{
			"chat", "stream", "thinking",
		},
		Models: []map[string]any{
			{"id": "grok-3", "name": "Grok 3"},
			{"id": "grok-3-mini", "name": "Grok 3 Mini"},
			{"id": "grok-3-reasoning", "name": "Grok 3 Reasoning"},
		},
		AccountFields: []map[string]any{
			{"key": "access_token", "label": "SSO Access Token", "type": "text", "required": true, "secret": true, "placeholder": "sso token value"},
			{"key": "cookie", "label": "Extra Cookie", "type": "text", "required": false, "secret": true, "placeholder": "optional cookie string"},
			{"key": "user_agent", "label": "User Agent", "type": "text", "required": false, "secret": false, "placeholder": "optional user-agent"},
		},
		Author: "web2api",
	}})
}

func chatCompletions(inv invocation) {
	if inv.Step == 0 {
		if inv.Input == nil {
			write(output{Type: "error", Error: "missing input"})
			return
		}
		accessToken := strings.TrimSpace(inv.Input.Account.Fields["access_token"])
		if accessToken == "" {
			write(output{Type: "error", Error: "missing account field: access_token"})
			return
		}

		baseURL := "https://grok.com"
		url := strings.TrimRight(baseURL, "/") + "/rest/app-chat/conversations/new"

		message := extractUserMessage(inv.Input.Request.Messages)
		payload := map[string]any{
			"collectionIds":               []string{},
			"connectors":                  []string{},
			"deviceEnvInfo":               map[string]any{"darkModeEnabled": false, "devicePixelRatio": 2, "screenHeight": 1329, "screenWidth": 2056, "viewportHeight": 1083, "viewportWidth": 2056},
			"message":                     message,
			"modeId":                      resolveModeID(inv.Input.Request, inv.Input.Source.Metadata),
			"isReasoning":                 inv.Input.Request.Thinking,
			"fileAttachments":             []string{},
			"imageAttachments":            []string{},
			"temporary":                   true,
			"sendFinalMetadata":           true,
			"disableSearch":               false,
			"disableMemory":               true,
			"enableImageGeneration":       false,
			"enableImageStreaming":        false,
			"returnRawGrokInXaiRequest":   false,
			"disableTextFollowUps":        false,
			"disableSelfHarmShortCircuit": false,
			"searchAllConnectors":         false,
			"responseMetadata":            map[string]any{},
			"forceConcise":                false,
			"forceSideBySide":             false,
			"returnImageBytes":            false,
			"toolOverrides": map[string]bool{
				"gmailSearch":           false,
				"googleCalendarSearch":  false,
				"outlookSearch":         false,
				"outlookCalendarSearch": false,
				"googleDriveSearch":     false,
			},
		}
		body, _ := json.Marshal(payload)

		headers := map[string]string{
			"Content-Type":     "application/json",
			"Accept":           "text/event-stream",
			"Accept-Language":  "zh-CN,zh;q=0.9,en;q=0.8",
			"Baggage":          "sentry-environment=production,sentry-release=d6add6fb0460641fd482d767a335ef72b9b6abb8,sentry-public_key=b311e0f2690c81f25e2c4cf6d4f7ce1c",
			"Origin":           strings.TrimRight(baseURL, "/"),
			"Priority":         "u=1, i",
			"Referer":          strings.TrimRight(baseURL, "/") + "/",
			"Cookie":           buildCookie(accessToken, inv.Input.Account.Fields["cookie"]),
			"Sec-Fetch-Dest":   "empty",
			"Sec-Fetch-Mode":   "cors",
			"Sec-Fetch-Site":   "same-site",
			"x-statsig-id":     "ZTpUeXBlRXJyb3I6IENhbm5vdCByZWFkIHByb3BlcnRpZXMgb2YgdW5kZWZpbmVkIChyZWFkaW5nICdjaGlsZE5vZGVzJyk=",
			"x-xai-request-id": buildRequestID(),
		}
		if ua := strings.TrimSpace(inv.Input.Account.Fields["user_agent"]); ua != "" {
			headers["User-Agent"] = ua
		} else {
			headers["User-Agent"] = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
		}

		write(output{Type: "continue", Requests: []hostReq{{
			ID:   "grok-chat",
			Kind: "http",
			HTTP: &httpReq{
				Method:    "POST",
				URL:       url,
				Headers:   headers,
				Body:      string(body),
				TimeoutMS: 120000,
			},
		}}})
		return
	}

	if len(inv.HostResults) == 0 || inv.HostResults[0].HTTP == nil {
		write(output{Type: "error", Error: "missing host http result"})
		return
	}
	result := inv.HostResults[0]
	if result.Error != "" {
		write(output{Type: "error", Error: result.Error})
		return
	}
	mode := resolveModeID(inv.Input.Request, inv.Input.Source.Metadata)
	if result.HTTP.StatusCode >= 400 {
		if result.HTTP.StatusCode == 403 {
			write(output{Type: "error", Error: fmt.Sprintf("upstream status 403: forbidden (check access_token/cookie/user_agent validity) mode=%s body=%s", mode, sanitizePreview(result.HTTP.Body, 320))})
			return
		}
		write(output{Type: "error", Error: fmt.Sprintf("upstream status %d mode=%s body=%s", result.HTTP.StatusCode, mode, sanitizePreview(result.HTTP.Body, 220))})
		return
	}

	text, thinking, toolCalls := parseGrokSSE(result.HTTP.Body)
	if strings.TrimSpace(text) == "" {
		text = "(empty grok response)"
	}
	resp := &chatResp{Content: text, Thinking: thinking}
	if strings.TrimSpace(inv.Input.Request.Metadata["debug_validate"]) == "1" {
		resp.Raw = map[string]any{
			"mode_id":      mode,
			"status_code":  result.HTTP.StatusCode,
			"body_preview": sanitizePreview(result.HTTP.Body, 420),
			"content_len":  len(text),
			"thinking_len": len(thinking),
		}
	}
	if len(toolCalls) > 0 {
		if resp.Raw == nil {
			resp.Raw = map[string]any{}
		}
		resp.Raw.(map[string]any)["tool_calls"] = toolCalls
	}
	write(output{Type: "response", Response: resp})
}

func extractUserMessage(messages []message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		content := parseAnyContent(messages[i].Content)
		if messages[i].Role == "user" && strings.TrimSpace(content) != "" {
			return strings.TrimSpace(content)
		}
	}
	if len(messages) > 0 {
		return strings.TrimSpace(parseAnyContent(messages[len(messages)-1].Content))
	}
	return ""
}

func resolveModeID(req request, metadata map[string]string) string {
	if metadata != nil {
		if raw := strings.TrimSpace(metadata["mode_map_json"]); raw != "" {
			var modeMap map[string]string
			if json.Unmarshal([]byte(raw), &modeMap) == nil {
				if mode, ok := modeMap[req.Model]; ok && strings.TrimSpace(mode) != "" {
					return normalizeModeID(mode)
				}
			}
		}
	}
	model := strings.ToLower(strings.TrimSpace(req.Model))
	if strings.Contains(model, "heavy") || strings.Contains(model, "multi-agent") {
		return "heavy"
	}
	if strings.Contains(model, "reason") || strings.Contains(model, "expert") {
		return "expert"
	}
	if strings.Contains(model, "mini") {
		return "fast"
	}
	if strings.Contains(model, "fast") || strings.Contains(model, "non-reasoning") {
		return "fast"
	}
	if model == "" {
		return "auto"
	}
	return "auto"
}

func normalizeModeID(mode string) string {
	v := strings.ToLower(strings.TrimSpace(mode))
	switch v {
	case "auto", "fast", "expert", "heavy":
		return v
	default:
		if strings.Contains(v, "heavy") {
			return "heavy"
		}
		if strings.Contains(v, "reason") || strings.Contains(v, "expert") {
			return "expert"
		}
		if strings.Contains(v, "mini") || strings.Contains(v, "fast") || strings.Contains(v, "non-reasoning") {
			return "fast"
		}
		return "auto"
	}
}

func buildCookie(accessToken string, extra string) string {
	value := strings.TrimSpace(strings.TrimPrefix(accessToken, "sso="))
	base := "sso=" + value + "; sso-rw=" + value
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return base
	}
	return base + "; " + strings.Trim(extra, "; ")
}

func buildRequestID() string {
	letters := "abcdef0123456789"
	randPart := make([]byte, 12)
	for i := range randPart {
		randPart[i] = letters[rand.Intn(len(letters))]
	}
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), string(randPart))
}

func sanitizePreview(text string, max int) string {
	v := strings.ReplaceAll(text, "\n", "\\n")
	v = strings.ReplaceAll(v, "\r", "")
	if max > 0 && len(v) > max {
		v = v[:max] + "..."
	}
	return v
}

func parseGrokSSE(raw string) (string, string, []map[string]any) {
	textParts := make([]string, 0, 64)
	thinkingParts := make([]string, 0, 32)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if line == "" || line == "[DONE]" {
			continue
		}

		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		result, _ := obj["result"].(map[string]any)
		resp, _ := result["response"].(map[string]any)
		token, _ := resp["token"].(string)
		if strings.TrimSpace(token) == "" {
			continue
		}
		if isThinking, _ := resp["isThinking"].(bool); isThinking {
			thinkingParts = append(thinkingParts, token)
			continue
		}
		textParts = append(textParts, token)
	}
	text := strings.TrimSpace(strings.Join(textParts, ""))
	toolCalls, cleaned := parseToolCalls(text)
	if cleaned != "" {
		text = cleaned
	}
	return text, strings.TrimSpace(strings.Join(thinkingParts, "")), toolCalls
}

func parseAnyContent(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			switch block := item.(type) {
			case map[string]any:
				if text, ok := block["text"].(string); ok {
					parts = append(parts, text)
				}
			case string:
				parts = append(parts, block)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text
		}
	}
	return ""
}

func parseToolCalls(text string) ([]map[string]any, string) {
	root := xmlRootRE.FindStringSubmatch(text)
	if len(root) < 2 {
		return nil, text
	}
	calls := make([]map[string]any, 0, 4)
	for _, callMatch := range xmlCallRE.FindAllStringSubmatch(root[1], -1) {
		if len(callMatch) < 2 {
			continue
		}
		nameMatch := xmlNameRE.FindStringSubmatch(callMatch[1])
		if len(nameMatch) < 2 {
			continue
		}
		name := strings.TrimSpace(nameMatch[1])
		if name == "" {
			continue
		}
		args := map[string]any{}
		if paramMatch := xmlParamRE.FindStringSubmatch(callMatch[1]); len(paramMatch) >= 2 {
			_ = json.Unmarshal([]byte(strings.TrimSpace(paramMatch[1])), &args)
		}
		calls = append(calls, map[string]any{
			"id":        fmt.Sprintf("call_%d", time.Now().UnixNano()),
			"name":      name,
			"arguments": args,
		})
	}
	cleaned := strings.TrimSpace(xmlRootRE.ReplaceAllString(text, ""))
	return calls, cleaned
}

func readInvocation() invocation {
	data, _ := io.ReadAll(os.Stdin)
	var inv invocation
	_ = json.Unmarshal(data, &inv)
	return inv
}

func write(v any) {
	b, _ := json.Marshal(v)
	fmt.Print(string(b))
}
