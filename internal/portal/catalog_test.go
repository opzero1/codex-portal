package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRefreshCatalogFiltersAnthropicClaudeToolModelsAndAliases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"anthropic": {
				"models": {
					"claude-sonnet-test": {
						"id": "claude-sonnet-test",
						"name": "Claude Sonnet Test",
						"family": "claude-sonnet",
						"reasoning": true,
						"tool_call": true,
						"modalities": {"input": ["text", "image"]},
						"limit": {"context": 200000, "output": 64000},
						"release_date": "2026-01-01",
						"last_updated": "2026-01-02"
					},
					"claude-opus-test": {
						"id": "claude-opus-test",
						"name": "Claude Opus Test",
						"family": "claude-opus",
						"reasoning": true,
						"tool_call": true,
						"modalities": {"input": ["text", "image"]},
						"limit": {"context": 200000, "output": 64000},
						"release_date": "2026-01-01",
						"last_updated": "2026-01-03"
					},
					"claude-haiku-test": {
						"id": "claude-haiku-test",
						"name": "Claude Haiku Test",
						"family": "claude-haiku",
						"reasoning": true,
						"tool_call": true,
						"modalities": {"input": ["text", "image"]},
						"limit": {"context": 200000, "output": 64000},
						"release_date": "2026-01-01",
						"last_updated": "2026-01-04"
					},
					"claude-text-only": {
						"id": "claude-text-only",
						"name": "Claude Text",
						"family": "claude",
						"tool_call": false
					}
				}
			}
		}`))
	}))
	defer server.Close()

	catalog, err := RefreshCatalog(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 3 {
		t.Fatalf("expected three models, got %+v", catalog.Models)
	}
	if catalog.Aliases["claude-code-default"] != "claude-sonnet-test" {
		t.Fatalf("default alias not built: %+v", catalog.Aliases)
	}
	if _, ok := catalog.Resolve("claude-code-default"); !ok {
		t.Fatalf("alias did not resolve")
	}
	if model, ok := catalog.Resolve("5.5"); !ok || model.ID != "claude-opus-test" {
		t.Fatalf("Codex built-in model alias did not resolve")
	}
	if model, ok := catalog.Resolve("GPT-5.4"); !ok || model.ID != "claude-sonnet-test" {
		t.Fatalf("Codex built-in standard model alias did not resolve to Sonnet")
	}
	if model, ok := catalog.Resolve("GPT-5.4-Mini"); !ok || model.ID != "claude-haiku-test" {
		t.Fatalf("Codex built-in mini model alias did not resolve case-insensitively")
	}
}

func TestLoadCatalogMergesGeneratedAliasesIntoExistingCatalog(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{
		"updated_at": "2026-06-04T00:00:00Z",
		"models": [
			{
				"id": "claude-sonnet-test",
				"name": "Claude Sonnet Test",
				"family": "claude-sonnet",
				"reasoning": true,
				"tool_call": true,
				"input_modalities": ["text"],
				"context_window": 200000,
				"max_output": 64000
			}
		],
		"aliases": {
			"custom-default": "claude-sonnet-test"
		}
	}`)
	if err := os.WriteFile(filepath.Join(home, "models.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	catalog, err := LoadCatalog(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := catalog.Resolve("custom-default"); !ok {
		t.Fatalf("existing alias was not preserved")
	}
	if _, ok := catalog.Resolve("5.5"); !ok {
		t.Fatalf("generated Codex model alias was not merged")
	}
}
