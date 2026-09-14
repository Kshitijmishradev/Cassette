# Portable behavior-test fixture

`cassettes/case-01` is a small recording made against the MCP Everything
reference server. Its manifest launches the repository's deterministic
protocol driver and names the tape explicitly, so it replays on Linux or
macOS without the original server or Node.js being present.

The GitHub Action uses this case as its own end-to-end smoke test. The public
UI demo is built from the richer frontend fixture suite in `web/fixtures`.
