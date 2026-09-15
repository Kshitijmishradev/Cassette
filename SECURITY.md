# Security

## Scope and supported use

Cassette is a local development and testing tool. The public Cloudflare Pages
site is a static introduction, documentation, and demonstration with reviewed
fixture data. It does not need a live Cassette backend.

**Hermetic replay is not an agent sandbox.** It prevents wrapped MCP shims from
starting live fallback servers. It cannot stop the agent's own shell commands,
native tools, network requests, unwrapped MCP servers, or model API calls.
Use a disposable container or VM, restricted network access, and minimal
credentials when testing an agent that could perform unwanted actions.

Only replay suites and agents you trust. `manifest.json` contains an executable
agent command; `replay` and `test` can launch it. Treat a downloaded suite like
source code you intend to execute. Tape contents can also influence an agent.
Cassette does not provide cryptographic tape authenticity or an independent
verdict against a malicious agent that can modify its own tapes/reports.

## Safer defaults

- Live MCP fall-through is **off** by default.
- Tool-name heuristics are **off** by default. Unknown tools are writes.
- Explicit read allowlists use exact, case-sensitive tool names.
- `--hermetic` overrides any configured fall-through permission via the shim
  environment, without writing a policy file into the recording directory.
- New recording directories use mode `0700`; new tapes and replay reports use
  `0600`. Existing files, Git checkouts, and copied/shared exports may have
  different permissions. There is no automatic encryption or redaction.

For an explicitly reviewed exploratory session only:

```json
{
  "tools": { "read": ["get_customer", "get_order", "get_delivery_status"] },
  "replay": { "fallThrough": true }
}
```

Review the actual implementation and destinations of an allowlisted tool.
Even a read can expose data or trigger side effects on a poorly designed API.
Use `--hermetic` in CI regardless of the local configuration.

These defaults are stricter than earlier versions. Users relying on inferred
read classes must configure `tools.read`; otherwise differences in those tools
are conservatively treated as write differences. Re-run comparisons after
upgrading: normalization now preserves large numeric identifiers and exact
numeric spellings, and keys are rebuilt from recorded requests for old tapes.

## Publishing and local access

- Publish only the static build plus reviewed demonstration fixtures. Never
  point a public reverse proxy or tunnel at `cassette serve`.
- The local viewer binds to loopback and rejects non-local Host headers and
  cross-origin/cross-site requests. It has no authentication and does not
  isolate other processes/users on the same machine.
- Recordings, manifests (including command-line arguments), reports, exports,
  and logs can contain secrets. Review them before sharing. A static export
  contains full payloads and is readable by anyone with access to its host.
- `--no-payloads` applies to ClickHouse exports only and is rejected for static
  or fixture exports. It is not a general redactor: metadata can still contain
  sensitive values. Export into a fresh directory to avoid stale payloads.
- Keep suites and output directories private and stable while processing them.
  Path validation rejects traversal and symlink components at the checked
  boundaries, but does not promise isolation from concurrent hostile local
  filesystem modification or resource exhaustion by very large inputs.
- Browser security headers are emitted for the local server and in the Pages
  `_headers` build artifact. Inline **styles** remain allowed for the waterfall;
  inline scripts and cross-origin scripts/connections are blocked.
  Other static hosts must configure their own equivalent response headers.

Cloudflare parses `_headers` in the build output for static responses; Pages
Functions need their own response headers. See the
[Cloudflare header documentation](https://developers.cloudflare.com/pages/configuration/headers/).

## CI credentials

The repository's pull-request replay workflow uses read-only permissions and
checkout without persisted credentials. Do not run untrusted PR code, suite
manifests, or agents with repository write tokens, production secrets, or on
persistent self-hosted runners. Do not use `pull_request_target` to check out
and execute an untrusted head revision. Optional comment tokens are appropriate
only where all executed code is trusted or in a separate trusted reporting job.

Third-party Actions are pinned to immutable upstream commits. Branch and
environment protection remain repository administration work. The audit
does not verify GitHub branch rules, Cloudflare account permissions, or stored
secret scope. CI includes npm advisory and Go vulnerability scans; clean scans
only describe known advisories at the time of scanning.

## Reporting a vulnerability

Do not post credentials, private tapes, or weaponized samples in a public issue.
Use [private vulnerability reporting](https://github.com/Kshitijmishradev/Cassette/security/advisories/new),
which is enabled for this repository.

The September 2026 review and validation results are in
[docs/SECURITY_AUDIT.md](https://github.com/Kshitijmishradev/Cassette/blob/main/docs/SECURITY_AUDIT.md).
