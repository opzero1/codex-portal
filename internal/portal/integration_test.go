package portal

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResponsesEndpointFakeAnthropicToolStreams(t *testing.T) {
	tests := []struct {
		name      string
		tools     []any
		toolName  string
		input     map[string]any
		wantSnips []string
	}{
		{
			name:      "shell function call",
			tools:     []any{map[string]any{"type": "function", "name": "exec_command", "parameters": map[string]any{"type": "object"}}},
			toolName:  "exec_command",
			input:     map[string]any{"cmd": "pwd"},
			wantSnips: []string{`"type":"function_call"`, `"name":"exec_command"`, `\"cmd\":\"pwd\"`},
		},
		{
			name:      "apply_patch freeform call",
			tools:     []any{map[string]any{"type": "freeform", "name": "apply_patch", "format": map[string]any{"type": "grammar"}}},
			toolName:  "apply_patch",
			input:     map[string]any{"input": "*** Begin Patch\n*** End Patch"},
			wantSnips: []string{`"type":"custom_tool_call"`, `"name":"apply_patch"`, `*** Begin Patch`},
		},
		{
			name: "namespaced MCP plugin call",
			tools: []any{map[string]any{
				"type": "namespace",
				"name": "mcp__codex_apps__gmail",
				"tools": []any{
					map[string]any{"type": "function", "name": "send_message", "parameters": map[string]any{"type": "object"}},
				},
			}},
			toolName:  "mcp__codex_apps__gmail__send_message",
			input:     map[string]any{"to": "a@example.com"},
			wantSnips: []string{`"type":"function_call"`, `"namespace":"mcp__codex_apps__gmail"`, `"name":"send_message"`},
		},
		{
			name:      "tool_search call",
			tools:     []any{map[string]any{"type": "function", "name": "tool_search", "parameters": map[string]any{"type": "object"}}},
			toolName:  "tool_search",
			input:     map[string]any{"query": "gmail"},
			wantSnips: []string{`"type":"tool_search_call"`, `"execution":"client"`, `"query":"gmail"`},
		},
		{
			name: "multi-tool response",
			tools: []any{
				map[string]any{"type": "function", "name": "exec_command", "parameters": map[string]any{"type": "object"}},
				map[string]any{"type": "function", "name": "tool_search", "parameters": map[string]any{"type": "object"}},
			},
			toolName:  "exec_command",
			input:     map[string]any{"cmd": "pwd"},
			wantSnips: []string{`"type":"function_call"`, `"type":"tool_search_call"`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			saveTestAuth(t, home)
			var captured map[string]any
			anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") == "" {
					t.Fatalf("missing authorization header")
				}
				if r.Header.Get("x-api-key") != "" {
					t.Fatalf("x-api-key must not be sent for OAuth")
				}
				if r.URL.Query().Get("beta") != "true" {
					t.Fatalf("missing beta=true query")
				}
				if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
					t.Fatal(err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, fakeToolStream(tc.toolName, tc.input, tc.name == "multi-tool response"))
			}))
			defer anthropic.Close()

			cfg := DefaultConfig()
			cfg.AnthropicBaseURL = anthropic.URL
			server := httptest.NewServer(NewServer(home, cfg, FallbackCatalog(), anthropic.Client()).Handler())
			defer server.Close()

			body := map[string]any{
				"model":  "claude-code-default",
				"stream": true,
				"input":  "use a tool",
				"tools":  tc.tools,
			}
			data, _ := json.Marshal(body)
			res, err := http.Post(server.URL+"/v1/responses", "application/json", bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			out, _ := io.ReadAll(res.Body)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("portal status %d: %s", res.StatusCode, string(out))
			}
			text := string(out)
			for _, want := range tc.wantSnips {
				if !strings.Contains(text, want) {
					t.Fatalf("stream missing %q:\n%s", want, text)
				}
			}
			if len(captured["tools"].([]any)) == 0 {
				t.Fatalf("Portal did not forward tool catalog to Anthropic")
			}
		})
	}
}

func TestServerRejectsNonLoopbackHostAndHasNoCompactEndpoint(t *testing.T) {
	home := t.TempDir()
	saveTestAuth(t, home)
	server := httptest.NewServer(NewServer(home, DefaultConfig(), FallbackCatalog(), nil).Handler())
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "example.com"
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden non-loopback Host, got %d", res.StatusCode)
	}
	_ = res.Body.Close()

	compactReq, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses/compact", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	compactRes, err := http.DefaultClient.Do(compactReq)
	if err != nil {
		t.Fatal(err)
	}
	defer compactRes.Body.Close()
	if compactRes.StatusCode != http.StatusNotFound {
		t.Fatalf("compact endpoint should not exist for Portal provider, got %d", compactRes.StatusCode)
	}
}

func TestResponsesEndpointMapsModelDeniedErrorsClearly(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			home := t.TempDir()
			saveTestAuth(t, home)
			anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "denied", status)
			}))
			defer anthropic.Close()
			cfg := DefaultConfig()
			cfg.AnthropicBaseURL = anthropic.URL
			server := httptest.NewServer(NewServer(home, cfg, FallbackCatalog(), anthropic.Client()).Handler())
			defer server.Close()

			body := map[string]any{
				"model": "claude-code-default",
				"input": "hello",
			}
			data, _ := json.Marshal(body)
			res, err := http.Post(server.URL+"/v1/responses", "application/json", bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			out, _ := io.ReadAll(res.Body)
			if res.StatusCode != status {
				t.Fatalf("expected status %d, got %d: %s", status, res.StatusCode, string(out))
			}
			if !strings.Contains(string(out), "model") || !strings.Contains(string(out), "OAuth account") {
				t.Fatalf("model denied error was not clear: %s", string(out))
			}
		})
	}
}

func saveTestAuth(t *testing.T, home string) {
	t.Helper()
	store := AuthStore{Home: home}
	if err := store.Save(TokenSet{
		AccessToken:  "test-access",
		RefreshToken: "test-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
}

func fakeToolStream(toolName string, input map[string]any, multi bool) string {
	firstInput, _ := json.Marshal(input)
	var b strings.Builder
	b.WriteString("event: message_start\n")
	b.WriteString(`data: {"type":"message_start","message":{"id":"resp-fake"}}` + "\n\n")
	b.WriteString("event: content_block_start\n")
	b.WriteString(`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-1","name":"` + toolName + `","input":` + string(firstInput) + `}}` + "\n\n")
	b.WriteString("event: content_block_stop\n")
	b.WriteString(`data: {"type":"content_block_stop","index":0}` + "\n\n")
	if multi {
		b.WriteString("event: content_block_start\n")
		b.WriteString(`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call-2","name":"tool_search","input":{"query":"more tools"}}}` + "\n\n")
		b.WriteString("event: content_block_stop\n")
		b.WriteString(`data: {"type":"content_block_stop","index":1}` + "\n\n")
	}
	b.WriteString("event: message_stop\n")
	b.WriteString(`data: {"type":"message_stop"}` + "\n\n")
	return b.String()
}
