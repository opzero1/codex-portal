# Homebrew Tap

Codex Portal can be installed from this repository as a Homebrew tap.

Because the repository is named `opzero1/codex-portal`, use Homebrew's custom
remote form:

```sh
brew tap opzero1/codex-portal https://github.com/opzero1/codex-portal.git
brew install --HEAD codex-portal
```

The one-argument form, `brew tap opzero1/codex-portal`, is not equivalent: by
default Homebrew resolves that to `https://github.com/opzero1/homebrew-codex-portal`.

## First Run

```sh
codex-portal login
codex-portal models refresh
codex-portal install
brew services start codex-portal
codex-portal status
```

`codex-portal login` shows the unofficial OAuth/TOS/account-risk notice, opens an
Anthropic OAuth URL, and asks you to paste the redirected URL or `code#state`.

`codex-portal install` writes only the marked Codex Portal block in
`~/.codex/config.toml`. Homebrew does not run it automatically because that would
be a hidden config mutation.

The Homebrew service runs:

```sh
codex-portal serve
```

which binds `127.0.0.1:8766` and exposes:

- `http://127.0.0.1:8766/health`
- `http://127.0.0.1:8766/v1/models`
- `http://127.0.0.1:8766/v1/responses`

## Smoke Test

```sh
curl http://127.0.0.1:8766/health
curl http://127.0.0.1:8766/v1/models
curl -N http://127.0.0.1:8766/v1/responses \
  -H 'content-type: application/json' \
  -d '{"model":"claude-code-default","input":"Say ok.","stream":true}'
```

## Upgrade

```sh
brew update
brew reinstall --HEAD codex-portal
brew services restart codex-portal
```

## Uninstall

```sh
brew services stop codex-portal
codex-portal uninstall
codex-portal auth logout
brew uninstall codex-portal
brew untap opzero1/codex-portal
```

## Stable Formula Path

The formula is HEAD-only for now. That is the correct shape when the source repo
also acts as the tap repo.

For `brew install codex-portal` without `--HEAD`, create a separate tap repository
named `opzero1/homebrew-codex-portal`, or publish release assets from this repo
and update `Formula/codex-portal.rb` to use a versioned URL with a fixed SHA-256.
