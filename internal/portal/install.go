package portal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const savedConfigPrefix = "# CODEX PORTAL SAVED "

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
	next := replaceManagedBlock(string(current), cfg)
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

func replaceManagedBlock(current string, cfg Config) string {
	without := removeManagedBlock(current)
	cleaned, saved := removeRootModelSelection(without)
	block := printConfigBlockWithSaved(cfg, saved)
	if strings.TrimSpace(cleaned) == "" {
		return block
	}
	return insertBeforeFirstTable(cleaned, block)
}

func removeManagedBlock(current string) string {
	start := strings.Index(current, ManagedStart)
	if start < 0 {
		return current
	}
	saved := savedConfigLines(current[start:])
	end := strings.Index(current[start:], ManagedEnd)
	if end < 0 {
		return current
	}
	endAbs := start + end + len(ManagedEnd)
	if endAbs < len(current) && current[endAbs] == '\n' {
		endAbs++
	}
	next := strings.TrimRight(current[:start], "\n") + "\n" + strings.TrimLeft(current[endAbs:], "\n")
	if len(saved) == 0 {
		return next
	}
	return insertBeforeFirstTable(next, strings.Join(saved, "\n")+"\n")
}

func printConfigBlockWithSaved(cfg Config, saved []string) string {
	block := PrintConfigBlock(cfg)
	if len(saved) == 0 {
		return block
	}
	insert := "\n"
	for _, line := range saved {
		insert += savedConfigPrefix + line + "\n"
	}
	return strings.Replace(block, "\n\n[model_providers.", insert+"\n[model_providers.", 1)
}

func savedConfigLines(text string) []string {
	lines := strings.Split(text, "\n")
	saved := []string{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, savedConfigPrefix) {
			saved = append(saved, strings.TrimPrefix(trimmed, savedConfigPrefix))
		}
	}
	return saved
}

func removeRootModelSelection(current string) (string, []string) {
	lines := strings.Split(current, "\n")
	out := make([]string, 0, len(lines))
	saved := []string{}
	inRoot := true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inRoot && strings.HasPrefix(trimmed, "[") {
			inRoot = false
		}
		if inRoot && isModelSelectionLine(trimmed) {
			saved = append(saved, strings.TrimSpace(line))
			continue
		}
		out = append(out, line)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n", saved
}

func isModelSelectionLine(trimmed string) bool {
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	key, _, ok := strings.Cut(trimmed, "=")
	if !ok {
		return false
	}
	key = strings.TrimSpace(key)
	return key == "model" || key == "model_provider"
}

func insertBeforeFirstTable(current string, block string) string {
	current = strings.TrimRight(current, "\n")
	if strings.TrimSpace(current) == "" {
		return block
	}
	lines := strings.Split(current, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			prefix := strings.TrimRight(strings.Join(lines[:i], "\n"), "\n")
			suffix := strings.TrimLeft(strings.Join(lines[i:], "\n"), "\n")
			if prefix == "" {
				return strings.TrimRight(block, "\n") + "\n\n" + suffix + "\n"
			}
			return prefix + "\n\n" + strings.TrimRight(block, "\n") + "\n\n" + suffix + "\n"
		}
	}
	return current + "\n\n" + block
}
