package portal

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	if len(catalog.Models) != 1 {
		t.Fatalf("expected one model, got %+v", catalog.Models)
	}
	if catalog.Aliases["claude-code-default"] != "claude-sonnet-test" {
		t.Fatalf("default alias not built: %+v", catalog.Aliases)
	}
	if _, ok := catalog.Resolve("claude-code-default"); !ok {
		t.Fatalf("alias did not resolve")
	}
}
