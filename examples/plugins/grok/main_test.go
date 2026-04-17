package main

import "testing"

func TestParseGrokSSE_ConcatenatedJSONObjects(t *testing.T) {
	raw := `{"result":{"response":{"token":"你","isThinking":false}}}{"result":{"response":{"token":"好","isThinking":false}}}{"result":{"response":{"token":"！","isThinking":false}}}`
	text, thinking, toolCalls := parseGrokSSE(raw)
	if text != "你好！" {
		t.Fatalf("expected concatenated token text, got %q", text)
	}
	if thinking != "" {
		t.Fatalf("expected empty thinking, got %q", thinking)
	}
	if len(toolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(toolCalls))
	}
}

func TestParseGrokSSE_FallbackToModelResponseMessage(t *testing.T) {
	raw := "data: [DONE]\n\n" +
		`{"result":{"response":{"modelResponse":{"message":"final answer"}}}}`
	text, thinking, _ := parseGrokSSE(raw)
	if text != "final answer" {
		t.Fatalf("expected modelResponse fallback text, got %q", text)
	}
	if thinking != "" {
		t.Fatalf("expected empty thinking, got %q", thinking)
	}
}

func TestParseGrokStreamChunks_PreservesNewlines(t *testing.T) {
	raw := `{"result":{"response":{"token":"### 标题\n","isThinking":false}}}{"result":{"response":{"token":"- 列表项\n","isThinking":false}}}`
	state := pendingStreamState{}
	applyStreamBody(&state, raw, 64)
	finalizePendingStreamState(&state)
	if len(state.Pending) == 0 {
		t.Fatalf("expected at least one chunk")
	}
	combined := ""
	for _, chunk := range state.Pending {
		combined += chunk.Content
	}
	if combined != "### 标题\n- 列表项\n" {
		t.Fatalf("unexpected combined content: %q", combined)
	}
}
