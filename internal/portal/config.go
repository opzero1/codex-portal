package portal

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultHost         = "127.0.0.1"
	DefaultPort         = 8766
	DefaultAnthropicURL = "https://api.anthropic.com"
	DefaultModelsURL    = "https://models.dev/api.json"
	ProviderID          = "codex-portal"
	ProviderName        = "Codex Portal"
	ManagedStart        = "# BEGIN CODEX PORTAL MANAGED"
	ManagedEnd          = "# END CODEX PORTAL MANAGED"
)

type Config struct {
	Host                     string `json:"host"`
	Port                     int    `json:"port"`
	AnthropicBaseURL         string `json:"anthropic_base_url"`
	ModelsURL                string `json:"models_url"`
	DefaultModel             string `json:"default_model"`
	CatalogParallelToolCalls bool   `json:"catalog_parallel_tool_calls"`
}

func DefaultConfig() Config {
	return Config{
		Host:                     DefaultHost,
		Port:                     DefaultPort,
		AnthropicBaseURL:         DefaultAnthropicURL,
		ModelsURL:                DefaultModelsURL,
		DefaultModel:             "claude-code-default",
		CatalogParallelToolCalls: true,
	}
}

func DefaultHome() (string, error) {
	if override := strings.TrimSpace(os.Getenv("CODEX_PORTAL_HOME")); override != "" {
		return filepath.Clean(override), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex-portal"), nil
}

func EnsureHome(home string) error {
	if strings.TrimSpace(home) == "" {
		return errors.New("portal home is empty")
	}
	return os.MkdirAll(home, 0o700)
}

func configPath(home string) string {
	return filepath.Join(home, "config.json")
}

func authPath(home string) string {
	return filepath.Join(home, "auth.json")
}

func catalogPath(home string) string {
	return filepath.Join(home, "models.json")
}

func statusPath(home string) string {
	return filepath.Join(home, "status.json")
}

func LoadConfig(home string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(configPath(home))
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Host == "" {
		cfg.Host = DefaultHost
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.AnthropicBaseURL == "" {
		cfg.AnthropicBaseURL = DefaultAnthropicURL
	}
	if cfg.ModelsURL == "" {
		cfg.ModelsURL = DefaultModelsURL
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "claude-code-default"
	}
	return cfg, nil
}

func SaveConfig(home string, cfg Config) error {
	if err := EnsureHome(home); err != nil {
		return err
	}
	if err := validateLoopbackHost(cfg.Host); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(home), append(data, '\n'), 0o600)
}

func (c Config) ListenAddr() string {
	host := c.Host
	if host == "" {
		host = DefaultHost
	}
	port := c.Port
	if port == 0 {
		port = DefaultPort
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func (c Config) BaseURL() string {
	host := c.Host
	if host == "" {
		host = DefaultHost
	}
	if host == "::1" {
		host = "[::1]"
	}
	port := c.Port
	if port == 0 {
		port = DefaultPort
	}
	return fmt.Sprintf("http://%s:%d/v1", host, port)
}

func validateLoopbackHost(host string) error {
	if !IsLoopbackHost(host) {
		return fmt.Errorf("host %q is not loopback; Codex Portal only binds to loopback", host)
	}
	return nil
}

func IsLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.Contains(host, ":") {
		if split, _, err := net.SplitHostPort(host); err == nil {
			host = split
		}
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func hostFromHeader(value string) string {
	host := strings.TrimSpace(value)
	if split, _, err := net.SplitHostPort(host); err == nil {
		return split
	}
	return strings.Trim(host, "[]")
}

func WriteRuntimeStatus(home string, cfg Config, started time.Time) error {
	if err := EnsureHome(home); err != nil {
		return err
	}
	payload := map[string]any{
		"ok":         true,
		"pid":        os.Getpid(),
		"started_at": started.UTC().Format(time.RFC3339),
		"base_url":   cfg.BaseURL(),
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statusPath(home), append(data, '\n'), 0o600)
}

func ReadRuntimeStatus(home string) (map[string]any, error) {
	data, err := os.ReadFile(statusPath(home))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func PrintConfigBlock(cfg Config) string {
	var b strings.Builder
	b.WriteString(ManagedStart)
	b.WriteByte('\n')
	b.WriteString("model_provider = \"")
	b.WriteString(ProviderID)
	b.WriteString("\"\n")
	b.WriteString("model = \"")
	b.WriteString(cfg.DefaultModel)
	b.WriteString("\"\n\n")
	b.WriteString("[model_providers.")
	b.WriteString(ProviderID)
	b.WriteString("]\n")
	b.WriteString("name = \"")
	b.WriteString(ProviderName)
	b.WriteString("\"\n")
	b.WriteString("base_url = \"")
	b.WriteString(cfg.BaseURL())
	b.WriteString("\"\n")
	b.WriteString("wire_api = \"responses\"\n")
	b.WriteString("request_max_retries = 0\n")
	b.WriteString("stream_max_retries = 0\n")
	b.WriteString("stream_idle_timeout_ms = 300000\n")
	b.WriteString(ManagedEnd)
	b.WriteByte('\n')
	return b.String()
}
