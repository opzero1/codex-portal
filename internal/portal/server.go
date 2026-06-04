package portal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	Home      string
	Config    Config
	Catalog   ModelCatalog
	Anthropic AnthropicClient
	Started   time.Time
}

func NewServer(home string, cfg Config, catalog ModelCatalog, httpClient *http.Client) Server {
	auth := AuthStore{Home: home, HTTPClient: httpClient}
	return Server{
		Home:    home,
		Config:  cfg,
		Catalog: catalog,
		Anthropic: AnthropicClient{
			BaseURL:    cfg.AnthropicBaseURL,
			HTTPClient: httpClient,
			Auth:       auth,
		},
		Started: time.Now().UTC(),
	}
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/responses", s.handleResponses)
	return s.hostGuard(mux)
}

func (s Server) hostGuard(next http.Handler) http.Handler {
	allowed := map[string]bool{
		"localhost":                   true,
		"127.0.0.1":                   true,
		"::1":                         true,
		hostFromHeader(s.Config.Host): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := hostFromHeader(r.Host)
		if host == "" || !allowed[host] || !IsLoopbackHost(host) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": "Codex Portal rejects non-loopback Host headers", "type": "loopback_required"}})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_, authErr := AuthStore{Home: s.Home}.Load()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"provider":   ProviderID,
		"base_url":   s.Config.BaseURL(),
		"models":     len(s.Catalog.Models),
		"auth":       authErr == nil,
		"started_at": s.Started.Format(time.RFC3339),
	})
}

func (s Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   s.Catalog.OpenAIModels(time.Now().Unix()),
	})
}

func (s Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var body map[string]any
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64*1024*1024))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid_request_error", "invalid JSON request body"))
		return
	}
	requestedModel := stringValue(body["model"])
	translation, err := BuildAnthropicRequest(body, s.Catalog)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorPayload("invalid_request_error", err.Error()))
		return
	}
	stream := boolValue(body["stream"])
	res, err := s.Anthropic.DoMessages(r.Context(), translation.Anthropic, stream)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorPayload("anthropic_error", err.Error()))
		return
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		status := http.StatusBadGateway
		if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusNotFound {
			status = res.StatusCode
		}
		message := modelDeniedError(res.StatusCode, translation.Model.ID)
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		if strings.TrimSpace(string(detail)) != "" {
			message = fmt.Sprintf("%s Upstream body: %s", message, strings.TrimSpace(string(detail)))
		}
		writeJSON(w, status, errorPayload("anthropic_error", message))
		return
	}
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		if err := AnthropicStreamToResponses(res.Body, w, requestedModel, translation.Tools); err != nil {
			return
		}
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadGateway, errorPayload("anthropic_error", "invalid Anthropic JSON response"))
		return
	}
	writeJSON(w, http.StatusOK, AnthropicMessageToResponse(payload, requestedModel, translation.Tools))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func errorPayload(kind string, message string) map[string]any {
	return map[string]any{"error": map[string]any{"type": kind, "message": message}}
}
