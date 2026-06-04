# Codex Portal

Codex Portal is a local loopback OpenAI Responses-compatible provider for Codex,
backed by direct Anthropic OAuth subscription access. Codex owns state, tools,
approvals, plugins, MCP/apps, and UI; Portal only translates model I/O.

## Homebrew

```sh
brew tap opzero1/codex-portal https://github.com/opzero1/codex-portal.git
brew install --HEAD codex-portal
codex-portal login
codex-portal models refresh
codex-portal install
brew services start opzero1/codex-portal/codex-portal
```

See [docs/homebrew.md](docs/homebrew.md) for smoke tests, upgrade, and uninstall
commands.

## Development

```sh
mise exec -- go test ./...
mise exec -- go run ./cmd/codex-portal --help
```
