package portal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigLoopbackAndPrintConfig(t *testing.T) {
	cfg := DefaultConfig()
	if err := validateLoopbackHost(cfg.Host); err != nil {
		t.Fatalf("default host should be loopback: %v", err)
	}
	if IsLoopbackHost("0.0.0.0") {
		t.Fatalf("0.0.0.0 must not be accepted as loopback")
	}
	block := PrintConfigBlock(cfg)
	for _, want := range []string{
		ManagedStart,
		`model_provider = "codex-portal"`,
		`wire_api = "responses"`,
		`base_url = "http://127.0.0.1:8766/v1"`,
		ManagedEnd,
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("config block missing %q:\n%s", want, block)
		}
	}
}

func TestSaveConfigRejectsNonLoopbackHost(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Host = "0.0.0.0"
	if err := SaveConfig(t.TempDir(), cfg); err == nil {
		t.Fatalf("expected non-loopback host to be rejected")
	}
}

func TestInstallAndUninstallManagedBlock(t *testing.T) {
	home := t.TempDir()
	codexHome := t.TempDir()
	configPath := filepath.Join(codexHome, "config.toml")
	initial := "approval_policy = \"never\"\n"
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := InstallCodexConfig(home, DefaultConfig(), configPath); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(installed)
	if !strings.Contains(text, initial) || !strings.Contains(text, ManagedStart) {
		t.Fatalf("install did not preserve existing config and add managed block:\n%s", text)
	}
	if err := UninstallCodexConfig(home, configPath); err != nil {
		t.Fatal(err)
	}
	removed, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(removed), ManagedStart) {
		t.Fatalf("uninstall left managed block:\n%s", string(removed))
	}
	if !strings.Contains(string(removed), initial) {
		t.Fatalf("uninstall removed unrelated config:\n%s", string(removed))
	}
}
