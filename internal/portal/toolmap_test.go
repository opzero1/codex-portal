package portal

import "testing"

func TestToolNameMapPreservesNamespaceAndFreeform(t *testing.T) {
	tools := []any{
		map[string]any{
			"type":        "namespace",
			"name":        "mcp__codex_apps__gmail",
			"description": "Gmail tools",
			"tools": []any{
				map[string]any{
					"type":        "function",
					"name":        "send-message",
					"description": "Send",
					"parameters":  map[string]any{"type": "object"},
				},
			},
		},
		map[string]any{
			"type":        "freeform",
			"name":        "apply_patch",
			"description": "Patch",
			"format":      map[string]any{"type": "grammar"},
		},
	}
	m := NewToolNameMap(tools)
	ns := m.ForOriginal("mcp__codex_apps__gmail", "send-message")
	if ns.SafeName == "" || ns.Namespace != "mcp__codex_apps__gmail" || ns.Name != "send-message" {
		t.Fatalf("namespace tool did not round-trip: %+v", ns)
	}
	freeform := m.ForOriginal("", "apply_patch")
	if !freeform.Freeform {
		t.Fatalf("apply_patch should be freeform: %+v", freeform)
	}
	if len(m.AnthropicTools()) != 2 {
		t.Fatalf("expected two Anthropic tools")
	}
}
