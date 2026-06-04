# typed: strict
# frozen_string_literal: true

# Homebrew formula for Codex Portal.
class CodexPortal < Formula
  desc "Loopback Responses provider for Codex backed by Anthropic OAuth"
  homepage "https://github.com/opzero1/codex-portal"
  license :cannot_represent
  head "https://github.com/opzero1/codex-portal.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(
      output:  bin/"codex-portal",
      ldflags: "-s -w",
    ), "./cmd/codex-portal"
  end

  service do
    run [opt_bin/"codex-portal", "serve"]
    keep_alive true
    log_path var/"log/codex-portal.log"
    error_log_path var/"log/codex-portal.log"
  end

  def caveats
    <<~EOS
      Codex Portal stores its own files under ~/.codex-portal.

      First-time setup:
        codex-portal login
        codex-portal models refresh
        codex-portal install
        brew services start codex-portal

      codex-portal install writes only the marked Codex Portal block in
      ~/.codex/config.toml. It is not run automatically by Homebrew.

      To undo Codex config and auth:
        codex-portal uninstall
        codex-portal auth logout
    EOS
  end

  test do
    assert_match "Usage: codex-portal", shell_output("#{bin}/codex-portal --help")
    assert_match "model_provider = \"codex-portal\"", shell_output("#{bin}/codex-portal print-config")
  end
end
