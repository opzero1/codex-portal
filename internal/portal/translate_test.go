package portal

import (
	"strings"
	"testing"
)

func TestBuildAnthropicRequestPreservesToolOutputsAndReasoning(t *testing.T) {
	req := map[string]any{
		"model": "claude-code-default",
		"reasoning": map[string]any{
			"effort": "medium",
		},
		"tools": []any{
			map[string]any{"type": "function", "name": "exec_command", "parameters": map[string]any{"type": "object"}},
		},
		"input": []any{
			map[string]any{"type": "message", "role": "developer", "content": "be precise"},
			map[string]any{"type": "message", "role": "user", "content": "run pwd"},
			map[string]any{"type": "function_call", "call_id": "call-1", "name": "exec_command", "arguments": `{"cmd":"pwd"}`},
			map[string]any{"type": "function_call_output", "call_id": "call-1", "output": "/tmp/project"},
		},
	}
	tr, err := BuildAnthropicRequest(req, FallbackCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if tr.Anthropic["system"] != "be precise" {
		t.Fatalf("developer instructions not moved to system: %+v", tr.Anthropic)
	}
	if tr.Anthropic["thinking"] == nil {
		t.Fatalf("reasoning effort did not enable Anthropic thinking")
	}
	messages := tr.Anthropic["messages"].([]map[string]any)
	last := messages[len(messages)-1]["content"].([]map[string]any)[0]
	if last["type"] != "tool_result" || last["content"] != "/tmp/project" {
		t.Fatalf("tool output not preserved as tool_result: %+v", last)
	}
}

func TestBuildAnthropicRequestPreservesToolSearchOutputTools(t *testing.T) {
	req := map[string]any{
		"model": "claude-code-default",
		"tools": []any{
			map[string]any{"type": "function", "name": "tool_search", "parameters": map[string]any{"type": "object"}},
		},
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "find gmail"},
			map[string]any{"type": "tool_search_call", "call_id": "search-1", "execution": "client", "arguments": map[string]any{"query": "gmail"}},
			map[string]any{"type": "tool_search_output", "call_id": "search-1", "status": "completed", "execution": "client", "tools": []any{
				map[string]any{"type": "namespace", "name": "mcp__codex_apps__gmail"},
			}},
		},
	}
	tr, err := BuildAnthropicRequest(req, FallbackCatalog())
	if err != nil {
		t.Fatal(err)
	}
	messages := tr.Anthropic["messages"].([]map[string]any)
	last := messages[len(messages)-1]["content"].([]map[string]any)[0]
	if !strings.Contains(last["content"].(string), "mcp__codex_apps__gmail") {
		t.Fatalf("tool_search output tools not preserved: %+v", last)
	}
}

func TestAnthropicToolUseToResponsesItems(t *testing.T) {
	tools := NewToolNameMap([]any{
		map[string]any{"type": "function", "name": "exec_command"},
		map[string]any{"type": "freeform", "name": "apply_patch", "format": map[string]any{"type": "grammar"}},
		map[string]any{"type": "function", "name": "tool_search"},
	})
	for _, tc := range []struct {
		name string
		want string
	}{
		{"exec_command", "function_call"},
		{"apply_patch", "custom_tool_call"},
		{"tool_search", "tool_search_call"},
	} {
		item := toolUseToResponseItem(map[string]any{
			"type":  "tool_use",
			"id":    "call-1",
			"name":  tc.name,
			"input": map[string]any{"input": "payload"},
		}, tools)
		if item["type"] != tc.want {
			t.Fatalf("%s produced %+v", tc.name, item)
		}
	}
}
