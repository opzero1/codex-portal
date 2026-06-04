package portal

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

func RunCLI(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printUsage(stdout)
		return nil
	}
	home, err := DefaultHome()
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch args[0] {
	case "serve":
		return runServe(ctx, home, args[1:], stdout)
	case "login":
		store := AuthStore{Home: home}
		return RunLogin(ctx, store, stdin, stdout)
	case "auth":
		return runAuth(ctx, home, args[1:], stdout)
	case "install":
		cfg, err := LoadConfig(home)
		if err != nil {
			return err
		}
		if err := InstallCodexConfig(home, cfg, ""); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Installed managed Codex config block.")
		return nil
	case "uninstall":
		if err := UninstallCodexConfig(home, ""); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Removed managed Codex config block.")
		return nil
	case "print-config":
		cfg, err := LoadConfig(home)
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, PrintConfigBlock(cfg))
		return nil
	case "models":
		return runModels(ctx, home, args[1:], stdout)
	case "status":
		return runStatus(home, stdout)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: codex-portal <command>")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Commands:")
	fmt.Fprintln(out, "  serve [--host 127.0.0.1] [--port 8766]")
	fmt.Fprintln(out, "  login")
	fmt.Fprintln(out, "  auth status")
	fmt.Fprintln(out, "  auth logout")
	fmt.Fprintln(out, "  install")
	fmt.Fprintln(out, "  uninstall")
	fmt.Fprintln(out, "  print-config")
	fmt.Fprintln(out, "  models refresh")
	fmt.Fprintln(out, "  status")
}

func runServe(ctx context.Context, home string, args []string, out io.Writer) error {
	cfg, err := LoadConfig(home)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	host := fs.String("host", cfg.Host, "loopback host")
	port := fs.Int("port", cfg.Port, "port")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg.Host = *host
	cfg.Port = *port
	if err := validateLoopbackHost(cfg.Host); err != nil {
		return err
	}
	if err := EnsureHome(home); err != nil {
		return err
	}
	catalog, err := LoadCatalog(home)
	if err != nil {
		return err
	}
	server := NewServer(home, cfg, catalog, nil)
	if err := WriteRuntimeStatus(home, cfg, server.Started); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.ListenAddr())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Codex Portal listening on %s\n", cfg.BaseURL())
	httpServer := &http.Server{Handler: server.Handler(), ReadHeaderTimeout: 15 * time.Second}
	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	return err
}

func runAuth(ctx context.Context, home string, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("auth command requires status or logout")
	}
	store := AuthStore{Home: home}
	switch args[0] {
	case "status":
		tok, err := store.Load()
		if err != nil {
			fmt.Fprintln(out, "not logged in")
			return nil
		}
		state := "valid"
		if tok.ExpiresAt.Before(time.Now()) {
			state = "expired"
		}
		fmt.Fprintf(out, "logged in: %s, expires_at=%s\n", state, tok.ExpiresAt.UTC().Format(time.RFC3339))
		return nil
	case "logout":
		if err := store.Logout(); err != nil {
			return err
		}
		fmt.Fprintln(out, "logged out")
		return nil
	case "refresh":
		_, err := store.Refresh(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "refreshed")
		return nil
	default:
		return fmt.Errorf("unknown auth command %q", args[0])
	}
}

func runModels(ctx context.Context, home string, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "refresh" {
		return errors.New("models command requires refresh")
	}
	cfg, err := LoadConfig(home)
	if err != nil {
		return err
	}
	catalog, err := RefreshCatalog(ctx, http.DefaultClient, cfg.ModelsURL)
	if err != nil {
		return err
	}
	if err := SaveCatalog(home, catalog); err != nil {
		return err
	}
	fmt.Fprintf(out, "cached %d Anthropic Claude tool-capable models\n", len(catalog.Models))
	return nil
}

func runStatus(home string, out io.Writer) error {
	cfg, err := LoadConfig(home)
	if err != nil {
		return err
	}
	catalog, catalogErr := LoadCatalog(home)
	_, authErr := AuthStore{Home: home}.Load()
	status, statusErr := ReadRuntimeStatus(home)
	payload := map[string]any{
		"home":         home,
		"base_url":     cfg.BaseURL(),
		"auth":         authErr == nil,
		"models":       len(catalog.Models),
		"catalog_ok":   catalogErr == nil,
		"claude_probe": claudeProbe(),
	}
	if statusErr == nil {
		payload["runtime"] = status
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(data))
	return nil
}

func claudeProbe() string {
	path, err := exec.LookPath("claude")
	if err != nil {
		return "not_found"
	}
	if strings.TrimSpace(path) == "" {
		return "not_found"
	}
	return "available"
}
