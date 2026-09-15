# Cassette: test your agent

## Install

Download your platform's `.tar.gz` and `checksums.txt` from
[GitHub Releases](https://github.com/Kshitijmishradev/Cassette/releases).
Choose `darwin` for macOS or `linux`, and `arm64` for Apple Silicon/ARM or
`amd64` for Intel/AMD. GitHub's source-code archives are for contributors.

Verify the downloaded archive against its entry in `checksums.txt` using
`shasum -a 256 <archive>` (macOS) or `sha256sum <archive>` (Linux), then extract
it. These checksums detect corruption; they are not independent signatures.
Place `cassette` in a directory on your PATH, then run `cassette --help`.
Cassette needs no Go, Node, database, or separate web server at runtime.
Your own agent and MCP servers still need their usual dependencies.

The archive contains only `cassette`, `LICENSE`, this guide, and `SECURITY.md`.
The local trace viewer is embedded. No recorded sessions or credentials ship
in these archives. The public story/docs website is built separately.

## Connect your MCP server

In your agent's MCP configuration, use your installed binary's absolute path:

```json
{
  "mcpServers": {
    "my-server": {
      "command": "/absolute/path/to/cassette",
      "args": ["wrap", "--", "/absolute/path/to/your-mcp-server"]
    }
  }
}
```

Append your server's usual arguments after its path. Use the same configuration
when recording and replaying. Without a Cassette session, `wrap` passes through
to the live server, so normal agent use can still perform real actions.

## Record, replay, inspect

Replace the example agent executable and arguments with your installed agent.
Record only against a test environment with permissions you intend to grant.

```sh
cassette record first-test -- your-agent "perform my test task"
cassette replay first-test --hermetic -- your-agent "perform my revised task"
cassette test --suite ./cassettes --hermetic
cassette serve --suite ./cassettes --open
```

Recordings go into `./cassettes`. The viewer runs on loopback. Hermetic replay
blocks live fallback through wrapped MCP servers; it does not sandbox your
agent's shell, network, or native tools. Only execute trusted agents and suites.
Read [SECURITY.md](SECURITY.md) before sharing recordings or testing risky agents.

## Upgrade

Replace the binary with the new release after checking its checksum. Back up
private suites first. Older tapes remain readable. Live fallback and tool-name
heuristics now default to off. Configure exact, reviewed `tools.read` names if
you need read/write classification; unknown tools count as writes. Re-run your
suite and regenerate reports because numeric matching and classification are
stricter. Exports require a fresh destination. Do not reuse old verdicts as
validation of the new version.

[Full documentation](https://cassette-agent-replay.pages.dev/#/docs)
