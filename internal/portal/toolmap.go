package portal

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var unsafeToolName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

type ToolIdentity struct {
	SafeName     string
	OriginalType string
	Namespace    string
	Name         string
	Description  string
	Parameters   map[string]any
	Freeform     bool
	ToolSearch   bool
	Raw          map[string]any
}

type ToolNameMap struct {
	bySafe map[string]ToolIdentity
	byKey  map[string]ToolIdentity
	order  []ToolIdentity
}

func NewToolNameMap(tools any) ToolNameMap {
	m := ToolNameMap{
		bySafe: map[string]ToolIdentity{},
		byKey:  map[string]ToolIdentity{},
	}
	list, _ := tools.([]any)
	for _, item := range list {
		tool, _ := item.(map[string]any)
		if tool == nil {
			continue
		}
		m.addTool(tool, "")
	}
	return m
}

func (m *ToolNameMap) addTool(tool map[string]any, namespace string) {
	toolType := stringValue(tool["type"])
	if toolType == "namespace" {
		ns := stringValue(tool["name"])
		children, _ := tool["tools"].([]any)
		for _, child := range children {
			childTool, _ := child.(map[string]any)
			if childTool != nil {
				m.addTool(childTool, ns)
			}
		}
		return
	}
	name := toolName(tool)
	if name == "" {
		return
	}
	params := parametersForTool(tool)
	identity := ToolIdentity{
		OriginalType: toolType,
		Namespace:    namespace,
		Name:         name,
		Description:  descriptionForTool(tool),
		Parameters:   params,
		Freeform:     isFreeformTool(tool),
		ToolSearch:   name == "tool_search" && namespace == "",
		Raw:          cloneMap(tool),
	}
	identity.SafeName = m.uniqueSafeName(namespace, name)
	m.bySafe[identity.SafeName] = identity
	m.byKey[identity.key()] = identity
	m.order = append(m.order, identity)
}

func (m *ToolNameMap) uniqueSafeName(namespace string, name string) string {
	baseParts := []string{}
	if namespace != "" {
		baseParts = append(baseParts, namespace)
	}
	baseParts = append(baseParts, name)
	base := sanitizeToolName(strings.Join(baseParts, "__"))
	if base == "" {
		base = "tool"
	}
	if len(base) > 64 {
		sum := sha1.Sum([]byte(strings.Join(baseParts, "\x00")))
		base = strings.TrimRight(base[:51], "_-") + "_" + hex.EncodeToString(sum[:])[:12]
	}
	candidate := base
	for i := 2; ; i++ {
		if _, exists := m.bySafe[candidate]; !exists {
			return candidate
		}
		suffix := fmt.Sprintf("_%d", i)
		head := base
		if len(head)+len(suffix) > 64 {
			head = head[:64-len(suffix)]
		}
		candidate = head + suffix
	}
}

func (m ToolNameMap) AnthropicTools() []map[string]any {
	out := make([]map[string]any, 0, len(m.order))
	for _, identity := range m.order {
		out = append(out, map[string]any{
			"name":         identity.SafeName,
			"description":  identity.Description,
			"input_schema": identity.Parameters,
		})
	}
	return out
}

func (m ToolNameMap) BySafe(name string) (ToolIdentity, bool) {
	identity, ok := m.bySafe[name]
	return identity, ok
}

func (m ToolNameMap) ForOriginal(namespace string, name string) ToolIdentity {
	if identity, ok := m.byKey[toolKey(namespace, name)]; ok {
		return identity
	}
	identity := ToolIdentity{
		SafeName:     sanitizeToolName(joinOriginal(namespace, name)),
		OriginalType: "function",
		Namespace:    namespace,
		Name:         name,
		Description:  "Previously requested Codex tool.",
		Parameters:   map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": true},
	}
	if identity.SafeName == "" {
		identity.SafeName = "tool"
	}
	return identity
}

func (t ToolIdentity) key() string {
	return toolKey(t.Namespace, t.Name)
}

func toolKey(namespace string, name string) string {
	return namespace + "\x00" + name
}

func joinOriginal(namespace string, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "__" + name
}

func sanitizeToolName(name string) string {
	clean := unsafeToolName.ReplaceAllString(strings.TrimSpace(name), "_")
	clean = strings.Trim(clean, "_-")
	if len(clean) > 64 {
		clean = clean[:64]
	}
	return clean
}

func toolName(tool map[string]any) string {
	if fn, _ := tool["function"].(map[string]any); fn != nil {
		if name := stringValue(fn["name"]); name != "" {
			return name
		}
	}
	if name := stringValue(tool["name"]); name != "" {
		return name
	}
	switch stringValue(tool["type"]) {
	case "apply_patch":
		return "apply_patch"
	case "tool_search":
		return "tool_search"
	}
	return ""
}

func descriptionForTool(tool map[string]any) string {
	if fn, _ := tool["function"].(map[string]any); fn != nil {
		if description := stringValue(fn["description"]); description != "" {
			return description
		}
	}
	if description := stringValue(tool["description"]); description != "" {
		return description
	}
	name := toolName(tool)
	if name == "" {
		name = stringValue(tool["type"])
	}
	if isFreeformTool(tool) {
		return "Invoke Codex freeform/custom tool " + name + "."
	}
	return "Invoke Codex-owned tool " + name + "."
}

func parametersForTool(tool map[string]any) map[string]any {
	if fn, _ := tool["function"].(map[string]any); fn != nil {
		if params, _ := fn["parameters"].(map[string]any); params != nil {
			return params
		}
	}
	if params, _ := tool["parameters"].(map[string]any); params != nil {
		return params
	}
	if isFreeformTool(tool) {
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{"type": "string"},
			},
			"required":             []any{"input"},
			"additionalProperties": false,
		}
	}
	return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": true}
}

func isFreeformTool(tool map[string]any) bool {
	toolType := stringValue(tool["type"])
	if toolType == "freeform" || toolType == "custom" || toolType == "custom_tool" || toolType == "apply_patch" {
		return true
	}
	if format, _ := tool["format"].(map[string]any); format != nil {
		return true
	}
	return false
}

func stringValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func cloneMap(in map[string]any) map[string]any {
	data, err := json.Marshal(in)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}
