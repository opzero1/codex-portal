package portal

import (
	"bytes"
	"strings"
	"testing"
)

func TestAnthropicStreamToResponsesSSEShapes(t *testing.T) {
	tools := NewToolNameMap([]any{
		map[string]any{"type": "freeform", "name": "apply_patch", "format": map[string]any{"type": "grammar"}},
	})
	stream := strings.NewReader(strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg-1"}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call-patch","name":"apply_patch","input":{"input":"*** Begin Patch\n*** End Patch"}}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":1}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n"))
	var out bytes.Buffer
	if err := AnthropicStreamToResponses(stream, &out, "claude-code-default", tools); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"event: response.created",
		"event: response.output_text.delta",
		`"type":"custom_tool_call"`,
		`"name":"apply_patch"`,
		"event: response.completed",
		"data: [DONE]",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("SSE output missing %q:\n%s", want, text)
		}
	}
}

func TestAnthropicStreamToResponsesStreamsFunctionArgumentDeltas(t *testing.T) {
	tools := NewToolNameMap([]any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "shell",
				"parameters": map[string]any{"type": "object", "additionalProperties": true},
			},
		},
	})
	stream := strings.NewReader(strings.Join([]string{
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-shell","name":"shell","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"pwd\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n"))
	var out bytes.Buffer
	if err := AnthropicStreamToResponses(stream, &out, "claude-code-default", tools); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"event: response.function_call_arguments.delta",
		`"item_id":"call-shell"`,
		`"type":"function_call"`,
		`"arguments":"{\"command\":\"pwd\"}"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("SSE output missing %q:\n%s", want, text)
		}
	}
}
