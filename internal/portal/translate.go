package portal

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type Translation struct {
	Anthropic map[string]any
	Tools     ToolNameMap
	Model     ModelInfo
}

func BuildAnthropicRequest(body map[string]any, catalog ModelCatalog) (Translation, error) {
	requestedModel := stringValue(body["model"])
	if requestedModel == "" {
		return Translation{}, errors.New("request missing model")
	}
	model, ok := catalog.Resolve(requestedModel)
	if !ok {
		return Translation{}, fmt.Errorf("model %q is not in Portal model catalog; run `codex-portal models refresh` or choose /v1/models entry", requestedModel)
	}
	tools := NewToolNameMap(body["tools"])
	maxTokens := intValue(body["max_output_tokens"], 0)
	if maxTokens == 0 {
		maxTokens = intValue(body["max_tokens"], 0)
	}
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	if model.MaxOutput > 0 && int64(maxTokens) > model.MaxOutput {
		maxTokens = int(model.MaxOutput)
	}

	systemParts := []string{}
	if instructions := contentToText(body["instructions"]); instructions != "" {
		systemParts = append(systemParts, instructions)
	}
	messages := []map[string]any{}
	appendMessage := func(role string, blocks []map[string]any) {
		if len(blocks) == 0 {
			return
		}
		if n := len(messages); n > 0 && messages[n-1]["role"] == role {
			if existing, ok := messages[n-1]["content"].([]map[string]any); ok {
				messages[n-1]["content"] = append(existing, blocks...)
				return
			}
		}
		messages = append(messages, map[string]any{"role": role, "content": blocks})
	}

	for _, item := range responsesInputItems(body["input"]) {
		itemType := stringValue(item["type"])
		role := stringValue(item["role"])
		switch {
		case itemType == "message" || (itemType == "" && role != ""):
			if role == "" {
				role = "user"
			}
			if role == "system" || role == "developer" {
				if text := contentToText(item["content"]); text != "" {
					systemParts = append(systemParts, text)
				}
				continue
			}
			if role != "assistant" {
				role = "user"
			}
			appendMessage(role, contentBlocks(item["content"], model))
		case itemType == "input_text" || itemType == "text" || itemType == "input_image":
			appendMessage("user", contentBlocks(item, model))
		case itemType == "function_call":
			name := stringValue(item["name"])
			namespace := stringValue(item["namespace"])
			callID := callID(item)
			identity := tools.ForOriginal(namespace, name)
			appendMessage("assistant", []map[string]any{toolUseBlock(identity, callID, parseArguments(stringValue(item["arguments"])))})
		case itemType == "custom_tool_call":
			name := stringValue(item["name"])
			callID := callID(item)
			identity := tools.ForOriginal("", name)
			appendMessage("assistant", []map[string]any{toolUseBlock(identity, callID, map[string]any{"input": stringValue(item["input"])})})
		case itemType == "tool_search_call":
			callID := callID(item)
			identity := tools.ForOriginal("", "tool_search")
			appendMessage("assistant", []map[string]any{toolUseBlock(identity, callID, valueMap(item["arguments"]))})
		case itemType == "function_call_output" || itemType == "custom_tool_call_output" || itemType == "tool_search_output":
			output := contentToText(item["output"])
			if itemType == "tool_search_output" {
				output = contentToText(item["tools"])
			}
			appendMessage("user", []map[string]any{{
				"type":        "tool_result",
				"tool_use_id": callID(item),
				"content":     output,
			}})
		case itemType == "reasoning" || itemType == "compaction" || itemType == "context_compaction":
			if text := contentToText(item["summary"]); text != "" {
				appendMessage("assistant", []map[string]any{{"type": "thinking", "thinking": text}})
			}
		}
	}
	if len(messages) == 0 {
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": []map[string]any{{"type": "text", "text": ""}},
		})
	}

	out := map[string]any{
		"model":      model.ID,
		"max_tokens": maxTokens,
		"messages":   messages,
		"stream":     boolValue(body["stream"]),
	}
	if len(systemParts) > 0 {
		out["system"] = strings.Join(systemParts, "\n\n")
	}
	if temperature, ok := numericValue(body["temperature"]); ok {
		out["temperature"] = temperature
	}
	if topP, ok := numericValue(body["top_p"]); ok {
		out["top_p"] = topP
	}
	if anthropicTools := tools.AnthropicTools(); len(anthropicTools) > 0 {
		out["tools"] = anthropicTools
	}
	if thinking := thinkingConfig(body, model, maxTokens); thinking != nil {
		out["thinking"] = thinking
	}
	return Translation{Anthropic: out, Tools: tools, Model: model}, nil
}

func responsesInputItems(input any) []map[string]any {
	switch v := input.(type) {
	case nil:
		return nil
	case string:
		return []map[string]any{{"type": "message", "role": "user", "content": v}}
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			switch t := item.(type) {
			case string:
				out = append(out, map[string]any{"type": "message", "role": "user", "content": t})
			case map[string]any:
				out = append(out, t)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{v}
	default:
		return []map[string]any{{"type": "message", "role": "user", "content": fmt.Sprint(v)}}
	}
}

func contentBlocks(content any, model ModelInfo) []map[string]any {
	switch v := content.(type) {
	case nil:
		return nil
	case string:
		if v == "" {
			return nil
		}
		return []map[string]any{{"type": "text", "text": v}}
	case []any:
		out := []map[string]any{}
		for _, item := range v {
			out = append(out, contentBlocks(item, model)...)
		}
		return out
	case map[string]any:
		itemType := stringValue(v["type"])
		switch itemType {
		case "input_text", "output_text", "text":
			if text := stringValue(v["text"]); text != "" {
				return []map[string]any{{"type": "text", "text": text}}
			}
		case "input_image", "image_url":
			return imageBlock(v, model)
		default:
			if _, ok := v["image_url"]; ok {
				return imageBlock(v, model)
			}
			if inner, ok := v["content"]; ok {
				return contentBlocks(inner, model)
			}
			if text := stringValue(v["text"]); text != "" {
				return []map[string]any{{"type": "text", "text": text}}
			}
		}
	}
	text := contentToText(content)
	if text == "" {
		return nil
	}
	return []map[string]any{{"type": "text", "text": text}}
}

func imageBlock(part map[string]any, model ModelInfo) []map[string]any {
	if !modelSupportsInput(model, "image") {
		return []map[string]any{{"type": "text", "text": "[image omitted: selected model metadata does not list image input]"}}
	}
	imageURL := stringValue(part["image_url"])
	if imageURL == "" {
		if imageObj, _ := part["image_url"].(map[string]any); imageObj != nil {
			imageURL = stringValue(imageObj["url"])
		}
	}
	if imageURL == "" {
		imageURL = stringValue(part["url"])
	}
	if imageURL == "" {
		return []map[string]any{{"type": "text", "text": "[image omitted: missing image URL]"}}
	}
	if strings.HasPrefix(imageURL, "data:") {
		mediaType, data, ok := parseDataURL(imageURL)
		if !ok {
			return []map[string]any{{"type": "text", "text": "[image omitted: unsupported data URL]"}}
		}
		return []map[string]any{{"type": "image", "source": map[string]any{"type": "base64", "media_type": mediaType, "data": data}}}
	}
	if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
		return []map[string]any{{"type": "image", "source": map[string]any{"type": "url", "url": imageURL}}}
	}
	return []map[string]any{{"type": "text", "text": "[image omitted: unsupported image URL scheme]"}}
}

func modelSupportsInput(model ModelInfo, modality string) bool {
	for _, item := range model.InputModalities {
		if item == modality {
			return true
		}
	}
	return false
}

func parseDataURL(value string) (mediaType string, data string, ok bool) {
	withoutPrefix := strings.TrimPrefix(value, "data:")
	parts := strings.SplitN(withoutPrefix, ",", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	meta := parts[0]
	if !strings.HasSuffix(meta, ";base64") {
		return "", "", false
	}
	mediaType = strings.TrimSuffix(meta, ";base64")
	if mediaType == "" || parts[1] == "" {
		return "", "", false
	}
	return mediaType, parts[1], true
}

func toolUseBlock(identity ToolIdentity, callID string, input map[string]any) map[string]any {
	if input == nil {
		input = map[string]any{}
	}
	return map[string]any{
		"type":  "tool_use",
		"id":    callID,
		"name":  identity.SafeName,
		"input": input,
	}
}

func parseArguments(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out
	}
	return map[string]any{"_raw": raw}
}

func valueMap(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func callID(item map[string]any) string {
	for _, key := range []string{"call_id", "id"} {
		if value := stringValue(item[key]); value != "" {
			return value
		}
	}
	return "call_0"
}

func contentToText(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := []string{}
		for _, item := range v {
			if text := contentToText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case []map[string]any:
		parts := []string{}
		for _, item := range v {
			if text := contentToText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		itemType := stringValue(v["type"])
		switch itemType {
		case "input_text", "output_text", "text", "summary_text":
			return stringValue(v["text"])
		case "input_image", "image_url":
			return "[image]"
		}
		if output, ok := v["output"]; ok {
			return contentToText(output)
		}
		if content, ok := v["content"]; ok {
			return contentToText(content)
		}
		if text := stringValue(v["text"]); text != "" {
			return text
		}
		data, err := json.Marshal(v)
		if err == nil {
			return string(data)
		}
	}
	return fmt.Sprint(content)
}

func thinkingConfig(body map[string]any, model ModelInfo, maxTokens int) map[string]any {
	if !model.Reasoning || maxTokens < 2048 {
		return nil
	}
	effort := ""
	if reasoning, _ := body["reasoning"].(map[string]any); reasoning != nil {
		effort = stringValue(reasoning["effort"])
	}
	if effort == "" {
		effort = stringValue(body["reasoning_effort"])
	}
	if effort == "" || effort == "none" {
		return nil
	}
	budget := map[string]int{"low": 1024, "medium": 4096, "high": 8192, "max": 16384}[effort]
	if budget == 0 {
		budget = 4096
	}
	budget = int(math.Min(float64(budget), float64(maxTokens/2)))
	if budget < 1024 {
		return nil
	}
	return map[string]any{"type": "enabled", "budget_tokens": budget}
}

func AnthropicMessageToResponse(payload map[string]any, requestedModel string, tools ToolNameMap) map[string]any {
	responseID := stringValue(payload["id"])
	if responseID == "" {
		responseID = fmt.Sprintf("resp_%d", time.Now().UnixMilli())
	}
	output := []any{}
	textParts := []string{}
	for _, block := range contentBlockList(payload["content"]) {
		switch stringValue(block["type"]) {
		case "text":
			if text := stringValue(block["text"]); text != "" {
				textParts = append(textParts, text)
			}
		case "thinking":
			if thinking := stringValue(block["thinking"]); thinking != "" {
				output = append(output, reasoningItem("rsn_"+responseID, thinking))
			}
		case "tool_use":
			output = append(output, toolUseToResponseItem(block, tools))
		}
	}
	if len(textParts) > 0 {
		output = append([]any{messageOutputItem("msg_"+responseID, strings.Join(textParts, ""))}, output...)
	}
	return map[string]any{
		"id":         responseID,
		"object":     "response",
		"created_at": time.Now().Unix(),
		"status":     "completed",
		"model":      requestedModel,
		"output":     output,
		"usage":      normalizeUsage(payload["usage"]),
	}
}

func contentBlockList(content any) []map[string]any {
	list, _ := content.([]any)
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		block, _ := item.(map[string]any)
		if block != nil {
			out = append(out, block)
		}
	}
	return out
}

func messageOutputItem(id string, text string) map[string]any {
	return map[string]any{
		"id":     id,
		"type":   "message",
		"status": "completed",
		"role":   "assistant",
		"content": []any{map[string]any{
			"type":        "output_text",
			"text":        text,
			"annotations": []any{},
		}},
	}
}

func reasoningItem(id string, text string) map[string]any {
	return map[string]any{
		"id":      id,
		"type":    "reasoning",
		"status":  "completed",
		"summary": []any{map[string]any{"type": "summary_text", "text": text}},
	}
}

func toolUseToResponseItem(block map[string]any, tools ToolNameMap) map[string]any {
	callID := stringValue(block["id"])
	if callID == "" {
		callID = "call_0"
	}
	safeName := stringValue(block["name"])
	identity, ok := tools.BySafe(safeName)
	if !ok {
		identity = ToolIdentity{Name: safeName, SafeName: safeName, Parameters: map[string]any{}}
	}
	input := valueMap(block["input"])
	if identity.ToolSearch {
		return map[string]any{
			"type":      "tool_search_call",
			"call_id":   callID,
			"execution": "client",
			"arguments": input,
		}
	}
	if identity.Freeform {
		return map[string]any{
			"type":    "custom_tool_call",
			"call_id": callID,
			"name":    identity.Name,
			"input":   freeformInput(input),
		}
	}
	item := map[string]any{
		"type":      "function_call",
		"call_id":   callID,
		"name":      identity.Name,
		"arguments": jsonString(input),
	}
	if identity.Namespace != "" {
		item["namespace"] = identity.Namespace
	}
	return item
}

func freeformInput(input map[string]any) string {
	if text := stringValue(input["input"]); text != "" {
		return text
	}
	return jsonString(input)
}

func jsonString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func normalizeUsage(value any) map[string]any {
	usage, _ := value.(map[string]any)
	input := intValue(usage["input_tokens"], 0)
	output := intValue(usage["output_tokens"], 0)
	total := input + output
	return map[string]any{
		"input_tokens":  input,
		"output_tokens": output,
		"total_tokens":  total,
	}
}

func intValue(value any, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, err := strconv.Atoi(v.String())
		if err == nil {
			return i
		}
	case string:
		i, err := strconv.Atoi(v)
		if err == nil {
			return i
		}
	}
	return fallback
}

func boolValue(value any) bool {
	v, _ := value.(bool)
	return v
}

func numericValue(value any) (any, bool) {
	switch value.(type) {
	case float64, float32, int, int64, json.Number:
		return value, true
	default:
		return nil, false
	}
}
