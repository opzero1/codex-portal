package portal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type ModelInfo struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Family          string   `json:"family"`
	Reasoning       bool     `json:"reasoning"`
	ToolCall        bool     `json:"tool_call"`
	InputModalities []string `json:"input_modalities"`
	ContextWindow   int64    `json:"context_window"`
	MaxOutput       int64    `json:"max_output"`
	ReleaseDate     string   `json:"release_date,omitempty"`
	LastUpdated     string   `json:"last_updated,omitempty"`
}

type ModelCatalog struct {
	UpdatedAt time.Time         `json:"updated_at"`
	Models    []ModelInfo       `json:"models"`
	Aliases   map[string]string `json:"aliases"`
}

func LoadCatalog(home string) (ModelCatalog, error) {
	data, err := os.ReadFile(catalogPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return FallbackCatalog(), nil
	}
	if err != nil {
		return ModelCatalog{}, err
	}
	var catalog ModelCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return ModelCatalog{}, err
	}
	if len(catalog.Models) == 0 {
		return FallbackCatalog(), nil
	}
	if catalog.Aliases == nil {
		catalog.Aliases = BuildAliases(catalog.Models)
	}
	return catalog, nil
}

func SaveCatalog(home string, catalog ModelCatalog) error {
	if err := EnsureHome(home); err != nil {
		return err
	}
	data, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(catalogPath(home), append(data, '\n'), 0o600)
}

func RefreshCatalog(ctx context.Context, client *http.Client, modelsURL string) (ModelCatalog, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return ModelCatalog{}, err
	}
	res, err := client.Do(req)
	if err != nil {
		return ModelCatalog{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ModelCatalog{}, fmt.Errorf("models refresh failed: HTTP %d", res.StatusCode)
	}
	var root map[string]struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if err := json.NewDecoder(res.Body).Decode(&root); err != nil {
		return ModelCatalog{}, err
	}
	provider, ok := root["anthropic"]
	if !ok {
		return ModelCatalog{}, errors.New("models.dev response has no anthropic provider")
	}
	models := make([]ModelInfo, 0, len(provider.Models))
	for id, raw := range provider.Models {
		info, ok := parseModelsDevModel(id, raw)
		if ok {
			models = append(models, info)
		}
	}
	if len(models) == 0 {
		return ModelCatalog{}, errors.New("models.dev response had no Anthropic Claude tool-capable models")
	}
	sort.Slice(models, func(i, j int) bool {
		return modelSortKey(models[i]) > modelSortKey(models[j])
	})
	return ModelCatalog{
		UpdatedAt: time.Now().UTC(),
		Models:    models,
		Aliases:   BuildAliases(models),
	}, nil
}

func parseModelsDevModel(id string, raw json.RawMessage) (ModelInfo, bool) {
	var m struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Family     string `json:"family"`
		Reasoning  bool   `json:"reasoning"`
		ToolCall   bool   `json:"tool_call"`
		Modalities struct {
			Input []string `json:"input"`
		} `json:"modalities"`
		Limit struct {
			Context int64 `json:"context"`
			Output  int64 `json:"output"`
		} `json:"limit"`
		ReleaseDate string `json:"release_date"`
		LastUpdated string `json:"last_updated"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return ModelInfo{}, false
	}
	if m.ID == "" {
		m.ID = id
	}
	if !m.ToolCall || !strings.Contains(strings.ToLower(m.ID+" "+m.Name), "claude") {
		return ModelInfo{}, false
	}
	return ModelInfo{
		ID:              m.ID,
		Name:            m.Name,
		Family:          m.Family,
		Reasoning:       m.Reasoning,
		ToolCall:        m.ToolCall,
		InputModalities: append([]string(nil), m.Modalities.Input...),
		ContextWindow:   m.Limit.Context,
		MaxOutput:       m.Limit.Output,
		ReleaseDate:     m.ReleaseDate,
		LastUpdated:     m.LastUpdated,
	}, true
}

func BuildAliases(models []ModelInfo) map[string]string {
	aliases := map[string]string{}
	best := func(family string) string {
		var candidates []ModelInfo
		for _, model := range models {
			if strings.Contains(model.Family, family) || strings.Contains(model.ID, family) {
				candidates = append(candidates, model)
			}
		}
		if len(candidates) == 0 {
			return ""
		}
		sort.Slice(candidates, func(i, j int) bool {
			return modelSortKey(candidates[i]) > modelSortKey(candidates[j])
		})
		return candidates[0].ID
	}
	opus := best("opus")
	sonnet := best("sonnet")
	haiku := best("haiku")
	if sonnet == "" && len(models) > 0 {
		sonnet = models[0].ID
	}
	for alias, target := range map[string]string{
		"claude-code-default": sonnet,
		"claude-code-sonnet":  sonnet,
		"claude-sonnet":       sonnet,
		"claude-code-opus":    opus,
		"claude-opus":         opus,
		"claude-code-haiku":   haiku,
		"claude-haiku":        haiku,
	} {
		if target != "" {
			aliases[alias] = target
		}
	}
	return aliases
}

func modelSortKey(m ModelInfo) string {
	key := m.LastUpdated
	if key == "" {
		key = m.ReleaseDate
	}
	return key + "/" + m.ID
}

func (c ModelCatalog) Resolve(id string) (ModelInfo, bool) {
	if target, ok := c.Aliases[id]; ok {
		id = target
	}
	for _, model := range c.Models {
		if model.ID == id {
			return model, true
		}
	}
	return ModelInfo{}, false
}

func (c ModelCatalog) OpenAIModels(created int64) []map[string]any {
	out := make([]map[string]any, 0, len(c.Models)+len(c.Aliases))
	seen := map[string]bool{}
	for _, model := range c.Models {
		out = append(out, openAIModelEntry(model.ID, created, model))
		seen[model.ID] = true
	}
	aliases := make([]string, 0, len(c.Aliases))
	for alias := range c.Aliases {
		if !seen[alias] {
			aliases = append(aliases, alias)
		}
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		target, _ := c.Resolve(alias)
		out = append(out, openAIModelEntry(alias, created, target))
	}
	return out
}

func openAIModelEntry(id string, created int64, model ModelInfo) map[string]any {
	return map[string]any{
		"id":                             id,
		"object":                         "model",
		"created":                        created,
		"owned_by":                       "anthropic-oauth",
		"display_name":                   model.Name,
		"context_window":                 model.ContextWindow,
		"max_output_tokens":              model.MaxOutput,
		"supports_parallel_tool_calls":   true,
		"supports_reasoning_summaries":   false,
		"reasoning_summary_format":       "none",
		"apply_patch_tool_type":          "freeform",
		"input_modalities":               model.InputModalities,
		"supports_image_detail_original": modelSupportsInput(model, "image"),
		"supported_in_api":               true,
	}
}

func FallbackCatalog() ModelCatalog {
	models := []ModelInfo{
		{
			ID:              "claude-sonnet-4-6",
			Name:            "Claude Sonnet 4.6",
			Family:          "claude-sonnet",
			Reasoning:       true,
			ToolCall:        true,
			InputModalities: []string{"text", "image", "pdf"},
			ContextWindow:   1000000,
			MaxOutput:       64000,
			ReleaseDate:     "2026-02-17",
			LastUpdated:     "2026-03-13",
		},
		{
			ID:              "claude-opus-4-5",
			Name:            "Claude Opus 4.5",
			Family:          "claude-opus",
			Reasoning:       true,
			ToolCall:        true,
			InputModalities: []string{"text", "image", "pdf"},
			ContextWindow:   200000,
			MaxOutput:       64000,
			ReleaseDate:     "2025-11-24",
			LastUpdated:     "2025-11-24",
		},
		{
			ID:              "claude-haiku-4-5",
			Name:            "Claude Haiku 4.5",
			Family:          "claude-haiku",
			Reasoning:       true,
			ToolCall:        true,
			InputModalities: []string{"text", "image", "pdf"},
			ContextWindow:   200000,
			MaxOutput:       64000,
			ReleaseDate:     "2025-10-15",
			LastUpdated:     "2025-10-15",
		},
	}
	return ModelCatalog{
		UpdatedAt: time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC),
		Models:    models,
		Aliases:   BuildAliases(models),
	}
}
