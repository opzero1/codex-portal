package portal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func defaultCodexConfigPath() (string, error) {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return filepath.Join(home, "config.toml"), nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userHome, ".codex", "config.toml"), nil
}

func InstallCodexConfig(portalHome string, cfg Config, codexConfig string) error {
	if codexConfig == "" {
		var err error
		codexConfig, err = defaultCodexConfigPath()
		if err != nil {
			return err
		}
	}
	if err := EnsureHome(portalHome); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(codexConfig), 0o700); err != nil {
		return err
	}
	current, err := os.ReadFile(codexConfig)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(current) > 0 {
		if err := backupCodexConfig(portalHome, current); err != nil {
			return err
		}
	}
	next := replaceManagedBlock(string(current), PrintConfigBlock(cfg))
	return os.WriteFile(codexConfig, []byte(next), 0o600)
}

func UninstallCodexConfig(portalHome string, codexConfig string) error {
	if codexConfig == "" {
		var err error
		codexConfig, err = defaultCodexConfigPath()
		if err != nil {
			return err
		}
	}
	current, err := os.ReadFile(codexConfig)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(current) > 0 {
		if err := backupCodexConfig(portalHome, current); err != nil {
			return err
		}
	}
	next := removeManagedBlock(string(current))
	return os.WriteFile(codexConfig, []byte(next), 0o600)
}

func backupCodexConfig(portalHome string, data []byte) error {
	dir := filepath.Join(portalHome, "codex-config-backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name := fmt.Sprintf("config.toml.%s.bak", time.Now().UTC().Format("20060102T150405Z"))
	return os.WriteFile(filepath.Join(dir, name), data, 0o600)
}

func replaceManagedBlock(current string, block string) string {
	without := removeManagedBlock(current)
	if strings.TrimSpace(without) == "" {
		return block
	}
	return strings.TrimRight(without, "\n") + "\n\n" + block
}

func removeManagedBlock(current string) string {
	start := strings.Index(current, ManagedStart)
	if start < 0 {
		return current
	}
	end := strings.Index(current[start:], ManagedEnd)
	if end < 0 {
		return current
	}
	endAbs := start + end + len(ManagedEnd)
	if endAbs < len(current) && current[endAbs] == '\n' {
		endAbs++
	}
	return strings.TrimRight(current[:start], "\n") + "\n" + strings.TrimLeft(current[endAbs:], "\n")
}
