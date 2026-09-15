# Cassette security review — 15 September 2026

## Decision

The local source has been hardened for **trusted local development and a public
static documentation/demo site containing reviewed fixtures**. This is not a
certification of safety or approval to expose the live viewer as a hosted service.
It is not safe to treat hermetic replay as a sandbox for arbitrary agents or
third-party suite manifests.

The changes have not been pushed, released, or deployed by this review.
The existing Cloudflare deployment still needs the rebuilt bundle and a
post-deployment header check. Private vulnerability reporting was enabled and verified through the GitHub API.
Other account permissions and repository protection settings were not inspected.

## Scope

Reviewed the Go replay/matching/classification boundary, tape parser and writer,
manifest/report paths, local HTTP API, static and ClickHouse export handling,
React rendering, repository workflows, and current frontend/Go advisories.
Pre-existing website changes and concurrent viewer refactoring were preserved.

## Findings addressed

| Finding | Impact before the change | Fix and regression evidence |
|---|---|---|
| Hermetic suite isolation depended on a successful policy-file write | A failed write could silently leave live fall-through available | Pass `CASSETTE_HERMETIC=1` directly to the agent/shims; tests cover an unusable `.hermetic.json` and explicit live config overridden without starting a sentinel server |
| Default live fall-through plus inferred read names | A tool named like a read could perform a real side effect | Both options are off by default; tests require explicit permission |
| Read allowlists ignored case | A differently named MCP tool could inherit permission | Exact read-name matching; conservative write overrides retained |
| Matching omitted tool identity | Identical arguments to different tools could receive the wrong response | Method/tool-separated indexes, verified arguments, and cross-tool regression test |
| Numeric normalization used float64 | Distinct IDs/amounts above 2^53 could match | Preserve JSON numeric spellings; rebuild keys from recorded requests and test adjacent large IDs |
| `--no-diff` suppressed enforcement | Changed writes could escape the configured outcome check | Always calculate verdicts; only suppress rendering; regression covers a removed write |
| Tape arithmetic could wrap before unsafe memory access | A crafted offset or string length could panic or access beyond the mapping | Subtraction-based bounds checks, regular-file check, targeted tests and parser fuzzing |
| Unvalidated names and manifest/report references | Traversal, unsafe `record --force` deletion, or reads outside the intended case | Single-component names; reject checked symlinks, glob characters, mismatched reports, and duplicate tapes; traversal/symlink tests |
| Predictable tape temporary path and broad permissions | A `.cas.tmp` symlink could overwrite another file; local users could read new recordings | Random private temporary file; private new recording directories, tapes, manifests and reports; sentinel and permission tests |
| Local viewer lacked browser-origin defenses | A rebinding domain could read a loopback service under its own browser origin | Reject non-local Host, cross-origin and cross-site requests; unit coverage plus successful production UI smoke check |
| Browser hardening headers were absent | Weaker defense against framing and injected content | CSP, framing, MIME and referrer headers locally; Pages `_headers` artifact generated and embedded |
| `--no-payloads` was silently ignored for web exports | A user expecting omission could publish full payloads | Reject the unsupported combination before writing |
| Re-exporting over old data retained stale payloads | Removed cases/payloads could remain in an output directory | Refuse nonempty API/ClickHouse destinations; regression ensures no partial new API export |
| PR replay workflow supplied write access to checked-out code | A malicious agent/manifest or PR source could use privileged credentials | Repository workflow now uses read-only permission, no comment token, and no persisted checkout credential |

## Validation performed

- Full `go test -race ./...`: passed.
- Final targeted tests for commands, replay and suite: passed after the final policy edits.
- `go vet ./...`: passed.
- Production Go binary and Vite web build: passed.
- Tape parser fuzzing: **893,468 executions in 10 seconds**, no crash found.
  This is bounded fuzz coverage, not a proof of parser correctness.
- `npm audit --prefix web --json`: **0 known advisories reported** across 44 dependencies.
- Official `govulncheck` v1.8.0: **no vulnerabilities found** in the scanned Go usage.
  CI now repeats Go vulnerability and npm advisory checks.
- Pattern scan of 224 tracked files at audit start: no matches for the tested
  private-key, GitHub-token, OpenAI-key and AWS-access-key patterns. This was
  supplemented by Gitleaks v8.30.1 over all locally reachable Git refs: **92
  commits, no leaks found**. This does not guarantee the absence of sensitive data.
- Production local server: story, docs, runs and waterfall loaded under the CSP;
  no browser error/warning logs were observed during the smoke check.
- `git diff --check`: passed.

The Go scanner detects known vulnerabilities in reachable usage; it cannot
replace code review. See [Go vulnerability management](https://go.dev/doc/security/vuln/).

## Deployed-state observation

A read-only HEAD request to `https://cassette-agent-replay.pages.dev/` returned
HTTP 200 on 15 September 2026. It included `X-Content-Type-Options: nosniff`, but
no Content-Security-Policy or X-Frame-Options. It used
`Referrer-Policy: strict-origin-when-cross-origin` and public cross-origin access.
This observation describes the old deployment, not the locally tested build.

After publishing, verify the root, an asset, and `/api/suite.json` receive the
new headers and that the demo works. `_headers` affects static responses on
Cloudflare Pages; other hosts and Pages Functions require their own handling.
[Cloudflare documentation](https://developers.cloudflare.com/pages/configuration/headers/).

## Remaining boundaries and release work

1. **Deploy/release the fixes.** Existing installations and the live site do not
   gain protections from local edits alone.
2. **Trust execution inputs.** The agent and manifest command execute with the
   user's privileges. A malicious agent can bypass a wrapper or forge files.
   Isolate untrusted agents outside Cassette with OS/container controls.
3. **Review publication data.** No encryption/redaction is automatic. Static
   payloads and agent command arguments may contain secrets. Remote account
   secrets were outside this scan.
4. **Keep the viewer local.** There is no authentication or tenant isolation.
   Do not tunnel or reverse-proxy it publicly.
5. **Use private, stable directories and bounded workloads.** Path validation
   is not a complete defense against concurrent malicious local filesystem
   changes, mutation of memory-mapped tapes, or resource exhaustion. Live
   fallback subprocesses and arbitrary agent execution are not fully sandboxed
   or guaranteed to finish within a hard deadline.
6. **Harden repository administration.** Review branch/environment protections,
   and secret scopes. Private vulnerability reporting is now enabled; third-party
   Actions are pinned to upstream commit IDs and Dependabot checks for updates.
   Do not attach privileged tokens to untrusted PR execution.
7. **Expect stricter behavior after upgrading.** Configure reviewed `tools.read`
   names to distinguish read drift from write changes. Regenerate replay reports
   after upgrading normalization/classification semantics. Existing tape files
   remain readable. Exports now require fresh destinations.

Operational guidance is in [SECURITY.md](../SECURITY.md).

## Release packaging follow-up

- Separate viewer and public-site builds. Only the viewer is embedded in Go;
  demo API fixtures and the public storytelling/docs bundle are excluded.
- Archives explicitly allow only `cassette`, `LICENSE`, `QUICKSTART.md`, and
  `SECURITY.md`. A validation script checks all four platforms, SHA-256 sums,
  executable permissions, and rejects extra files or symlinks.
- GoReleaser is pinned to v2.18.1. Release CI repeats source/security checks,
  verifies the embedded viewer is current, and validates snapshot archives
  before publishing a tagged release. Missing optional Homebrew credentials
  skip tap publication without blocking binary releases.
- CI scans fetched Git history using Gitleaks v8.30.1. The local tool downloads
  for this review were verified against upstream release SHA-256 checksums.
- The quick-start guide includes installation, MCP wrapping, hermetic replay,
  local viewing, trust boundaries, and upgrade behavior changes.
