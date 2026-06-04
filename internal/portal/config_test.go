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

func TestInstallPlacesManagedBlockBeforeFirstTableAndRestoresModel(t *testing.T) {
	home := t.TempDir()
	codexHome := t.TempDir()
	configPath := filepath.Join(codexHome, "config.toml")
	initial := strings.Join([]string{
		`model = "gpt-5.5"`,
		`model_reasoning_effort = "xhigh"`,
		`approval_policy = "never"`,
		``,
		`[mcp_servers.figma]`,
		`url = "https://mcp.figma.com/mcp"`,
		``,
	}, "\n")
	if err := os.WriteFile(configPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := InstallCodexConfig(home, DefaultConfig(), configPath); err != nil {
		t.Fatal(err)
	}
	installedData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	installed := string(installedData)
	rootOldModel := `model = "gpt-5.5"`
	if strings.Contains(strings.ReplaceAll(installed, savedConfigPrefix+rootOldModel, ""), rootOldModel) {
		t.Fatalf("old root model was not removed:\n%s", installed)
	}
	if strings.Index(installed, ManagedStart) > strings.Index(installed, `[mcp_servers.figma]`) {
		t.Fatalf("managed block must be before first table:\n%s", installed)
	}
	if !strings.Contains(installed, savedConfigPrefix+rootOldModel) {
		t.Fatalf("old root model was not saved for uninstall:\n%s", installed)
	}
	if strings.Index(installed, `model_reasoning_effort = "xhigh"`) > strings.Index(installed, ManagedStart) {
		t.Fatalf("existing root keys should remain before provider table:\n%s", installed)
	}

	if err := UninstallCodexConfig(home, configPath); err != nil {
		t.Fatal(err)
	}
	removedData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	removed := string(removedData)
	if !strings.Contains(removed, rootOldModel) {
		t.Fatalf("uninstall did not restore previous model:\n%s", removed)
	}
	if strings.Contains(removed, ManagedStart) || strings.Contains(removed, savedConfigPrefix) {
		t.Fatalf("uninstall left managed metadata:\n%s", removed)
	}
}
