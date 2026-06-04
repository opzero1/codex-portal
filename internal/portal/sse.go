package portal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type sseWriter struct {
	w       io.Writer
	flusher interface{ Flush() }
}

func newSSEWriter(w io.Writer) sseWriter {
	flusher, _ := w.(interface{ Flush() })
	return sseWriter{w: w, flusher: flusher}
}

func (s sseWriter) event(name string, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

func (s sseWriter) done() error {
	if _, err := fmt.Fprint(s.w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

type streamBlock struct {
	Type     string
	Text     strings.Builder
	Thinking strings.Builder
	Tool     map[string]any
	JSON     strings.Builder
}

func AnthropicStreamToResponses(r io.Reader, w io.Writer, requestedModel string, tools ToolNameMap) error {
	writer := newSSEWriter(w)
	responseID := fmt.Sprintf("resp_%d", time.Now().UnixMilli())
	createdAt := time.Now().Unix()
	if err := writer.event("response.created", map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":         responseID,
			"object":     "response",
			"created_at": createdAt,
			"model":      requestedModel,
			"status":     "in_progress",
		},
	}); err != nil {
		return err
	}
	blocks := map[int]*streamBlock{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		dataLine := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if dataLine == "" || dataLine == "[DONE]" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(dataLine), &ev); err != nil {
			continue
		}
		index := intValue(ev["index"], 0)
		switch stringValue(ev["type"]) {
		case "content_block_start":
			block, _ := ev["content_block"].(map[string]any)
			if block == nil {
				continue
			}
			kind := stringValue(block["type"])
			sb := &streamBlock{Type: kind}
			if kind == "tool_use" {
				sb.Tool = block
			}
			if kind == "text" {
				_ = writer.event("response.output_item.added", map[string]any{
					"type": "response.output_item.added",
					"item": map[string]any{"id": fmt.Sprintf("msg_%d", index), "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}},
				})
			}
			blocks[index] = sb
		case "content_block_delta":
			sb := blocks[index]
			if sb == nil {
				continue
			}
			delta, _ := ev["delta"].(map[string]any)
			switch stringValue(delta["type"]) {
			case "text_delta":
				text := stringValue(delta["text"])
				sb.Text.WriteString(text)
				_ = writer.event("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "delta": text})
			case "thinking_delta":
				sb.Thinking.WriteString(stringValue(delta["thinking"]))
			case "input_json_delta":
				partial := stringValue(delta["partial_json"])
				sb.JSON.WriteString(partial)
				if partial != "" {
					emitToolArgumentsDelta(writer, sb, partial, tools)
				}
			}
		case "content_block_stop":
			sb := blocks[index]
			if sb == nil {
				continue
			}
			switch sb.Type {
			case "text":
				text := sb.Text.String()
				_ = writer.event("response.output_text.done", map[string]any{"type": "response.output_text.done", "text": text})
				_ = writer.event("response.output_item.done", map[string]any{
					"type": "response.output_item.done",
					"item": messageOutputItem(fmt.Sprintf("msg_%d", index), text),
				})
			case "thinking":
				if text := sb.Thinking.String(); text != "" {
					_ = writer.event("response.output_item.done", map[string]any{
						"type": "response.output_item.done",
						"item": reasoningItem(fmt.Sprintf("rsn_%d", index), text),
					})
				}
			case "tool_use":
				input := map[string]any{}
				if raw := strings.TrimSpace(sb.JSON.String()); raw != "" {
					_ = json.Unmarshal([]byte(raw), &input)
				} else if existing, _ := sb.Tool["input"].(map[string]any); existing != nil {
					input = existing
				}
				sb.Tool["input"] = input
				_ = writer.event("response.output_item.done", map[string]any{
					"type": "response.output_item.done",
					"item": toolUseToResponseItem(sb.Tool, tools),
				})
			}
		case "error":
			return fmt.Errorf("anthropic stream error: %s", contentToText(ev["error"]))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := writer.event("response.completed", map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":     responseID,
			"status": "completed",
			"usage":  normalizeUsage(nil),
		},
	}); err != nil {
		return err
	}
	return writer.done()
}

func emitToolArgumentsDelta(writer sseWriter, block *streamBlock, partial string, tools ToolNameMap) {
	if block == nil || block.Tool == nil {
		return
	}
	safeName := stringValue(block.Tool["name"])
	identity, ok := tools.BySafe(safeName)
	if !ok {
		identity = ToolIdentity{Name: safeName, SafeName: safeName}
	}
	if identity.Freeform {
		return
	}
	itemID := stringValue(block.Tool["id"])
	if itemID == "" {
		itemID = "call_0"
	}
	_ = writer.event("response.function_call_arguments.delta", map[string]any{
		"type":    "response.function_call_arguments.delta",
		"item_id": itemID,
		"delta":   partial,
	})
}
