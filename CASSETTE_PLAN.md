# Cassette: implementation plan and session context

> **Purpose of this file.** This is the resume document. If a session ends,
> hits a limit, or a fresh Claude picks this up cold, read this file top to
> bottom first. It contains the product premise, every locked decision, the
> file format spec, the phase plan, and a live status section. Update the
> STATUS section at the end of every working session.

Last updated: 2026-09-13
Owner: Kshitij (github.com/Kshitijmishradev)
Status: planning complete, implementation not started

---

## 1. What we are building, in one paragraph

Cassette is a record/replay proxy for MCP that turns real agent runs into
deterministic regression tests. It sits in the middle of the MCP wire
protocol between an agent and its tools, records every tool call and its
response into a "cassette" file, and can later replay those recorded
responses back to the agent so you can change a prompt or swap a model and
measure exactly what changed, without touching the real world, without
paying for the tool calls, and without the environment having drifted.

## 2. The problem, stated precisely

An agent run has two independent sources of variance:

- the **brain**: the model samples tokens, so it can act differently each run
- the **world**: tool responses drift as data changes and APIs move

Today both vary at once, so a difference between two runs is uninterpretable.
Cassette pins the world. That converts agent testing from a single noisy
sample into a controlled experiment where exactly one variable moved. Replay
being free is what makes running it N times (and getting a distribution)
possible at all.

Secondary problems it solves along the way:
- you cannot rerun a real historical run because it would re-trigger side
  effects (re-refunding a customer, reopening a PR)
- you cannot rerun it because the world moved (shipping status changed)
- you cannot rerun it cheaply (real API latency and model cost per iteration)

## 3. Locked decisions

| Decision | Choice | Why |
|---|---|---|
| Proxy language | **Go** | Single static binary, trivial stdio pumping, mmap, cheap concurrency for the suite runner |
| Analytics engine | **chDB (embedded ClickHouse) via chdb-go** | Real ClickHouse SQL and MergeTree with zero ops, zero cost, works offline, ships in the binary. Accepts a cgo dependency |
| Demo subject | **Coding agent (Claude Code on a real OSS repo)** | Instantly legible to any engineer, no fake data to invent, recognizable tool calls |
| Product shape | **Local-first CLI, not a SaaS** | Cassettes contain production data and secrets. Nobody uploads those. Local-first is the only adoptable architecture, and it is free to run |
| Cassette storage | **Committed to git alongside the code** | They are test fixtures. Reviewable in PRs, diffable, and CI needs zero infrastructure |
| Public demo | **Static export to Cloudflare Pages** | `cassette export --static` precomputes JSON, SPA reads it, no backend, free forever, cannot break or bill |
| Web UI delivery | **React build embedded with go:embed** | Preserves the one-binary story. `cassette serve` needs no node at runtime |
| Workspace | **Build directly on Kshitij's Mac via a connected folder** | Needs to run against real Claude Code locally |

**Not building:** a metrics dashboard with time-series charts. That space is
saturated (Dynatrace, SigNoz, Dash0 all shipped Claude Code OTel dashboards
in 2026). Building one would make this look like a clone.

**Open:** the name. "Cassette" is clear but generic as a repo name. Revisit
before the README is written.

## 4. Architecture

```
                    ┌──────────────────────────────┐
   agent process    │  cassette wrap / replay      │    real MCP servers
   (Claude Code) ──►│  stdio JSON-RPC proxy        │──► (filesystem, github,
                    │                              │     bash, postgres...)
                    │  record mode: tee to writer  │
                    │  replay mode: serve from tape│
                    └──────┬───────────────┬───────┘
                           │               │
                    .cassette/*.cas    chDB (embedded)
                    (git-committed      .cassette/analytics/
                     test fixtures)     cross-run queries
                           │               │
                    ┌──────▼───────────────▼───────┐
                    │ cassette test  (CLI, CI)     │
                    │ cassette serve (localhost UI)│
                    │ cassette export --static     │
                    └──────────────────────────────┘
```

Two storage systems on purpose, because the access patterns are opposite:

| | cassette file | chDB |
|---|---|---|
| pattern | point lookup by key | scan across millions of rows |
| scope | one run | every run ever |
| latency budget | microseconds | ~1 second |
| answers | "what did this exact call return" | "which calls appear in failing trajectories but not passing ones" |

## 5. Cassette file format (CAS1)

One file per recorded run. Laid out like a mini SSTable so the index loads
with a single cast and payloads are never parsed.

```
┌─────────────────────────────────────────────────┐
│ header   magic "CAS1" | version u32 | n u32     │  16 B
├─────────────────────────────────────────────────┤
│ index    n × 32 B, grouped by tool_id           │  200 × 32 = 6.4 KB
│          { KeyHash u64, NormHash u64,           │
│            ToolID u32, VecIdx u32,              │
│            BlobOff u32, BlobLen u32 }           │
├─────────────────────────────────────────────────┤
│ vectors  n × 128 × float32, contiguous          │  200 × 512 B = 100 KB
├─────────────────────────────────────────────────┤
│ strings  tool name table, arg text for diffing  │
├─────────────────────────────────────────────────┤
│ blobs    PRE-FRAMED response bytes              │  ~1.6 MB
└─────────────────────────────────────────────────┘
```

Three invariants that must not be broken:

1. **Blobs are stored pre-framed** exactly as they go back on the wire, which
   on the MCP stdio transport means the JSON message plus its trailing
   newline. Replay is one `write()` of an mmap slice. Zero parse, zero
   allocation. The JSON-RPC `id` is padded to fixed width at record time and
   patched in place.
2. **The proxy only parses the envelope** (`id`, `method`, `params.name`,
   `params.arguments`). It never unmarshals a response payload.
3. **Embeddings are computed at RECORD time**, not replay time. Recording is
   offline and already slow. Replay is the hot path.

Budget check that justifies all of this: a lookup is ~80ns, a model turn is
~1.5s. Ratio ~20,000,000:1. Reads are not the bottleneck, so the format only
has to avoid allocating. Do not build a filesystem. Do not put the replay hot
path in a database.

### Correction: transport framing

The plan originally specified Content-Length framing. That is **wrong**, and
it is LSP's framing, not MCP's. The MCP stdio transport is newline-delimited
JSON: one message per line, and messages **MUST NOT** contain embedded
newlines. Verified against the spec before writing phase 1.

Consequences, all of them good:
- A "pre-framed blob" is just the message bytes plus `\n`. Simpler than planned.
- Line splitting is safe precisely because embedded newlines are forbidden.
- No header layer to parse, so the reader is a bufio loop rather than a state
  machine.

Other normative rules from the same spec page that constrain the proxy:
- The server MAY write anything to stderr, and the client MUST NOT assume
  stderr means an error. So stderr is passed through untouched, never parsed.
- The server MUST NOT write non-MCP output to stdout.
- Shutdown is: close the child's stdin, wait, then escalate SIGTERM to
  SIGKILL. Servers SHOULD exit when stdin reaches EOF. The proxy has
  interposed itself, so it must honor this discipline in both directions:
  the real client will do it to us, and we must do it to the child.

Source: https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio

## 6. Matching ladder

```
L0  exact      hash(tool_id, canonical_args)        ~80ns   hashmap
L1  normalized hash(tool_id, normalize(args))       ~200ns  hashmap
    (sort keys, strip volatile fields, canonical paths)
L2  fuzzy      dot product vs vectors in SAME tool  ~2µs    brute force
                bucket only; embed incoming args
                once; threshold ~0.94
--  miss       policy depends on tool class
```

Bucket by tool before comparing: a `stripe_refund` can never match a
`postgres_query`. Buckets are 5-30 entries, so brute force beats HNSW
(index traversal overhead exceeds the loop on a set that fits in L1 cache).

**Miss policy is the core safety decision.** It depends on tool class:
- **read-class** (query, read_file, grep, list): fall through to the real
  tool, append to the cassette, mark the call `live-fallthrough`
- **write-class** (refund, send_email, create_pr, bash): **halt the replay
  immediately**. Falling through means performing a real side effect.

Classification: default deny (treat unknown tools as write-class), with an
allowlist in `cassette.yaml` plus heuristics on MCP tool annotations where
the server provides `readOnlyHint`.

## 7. Phases

Each phase has an explicit "done when" that is empirically checkable. Do not
advance until it passes. Verify with real runs, not assertions on paper.

### Phase 0 — Scaffold
- Go module, `cmd/cassette` with cobra, internal package layout
- goreleaser config (darwin/arm64, linux/amd64), GitHub Actions CI
- `cassette.yaml` config loader
- **Done when:** `cassette --help` runs from a release binary on macOS.

### Phase 1 — Transparent proxy
- stdio JSON-RPC framing reader/writer (newline-delimited, per the MCP spec)
- bidirectional pump, spawn child MCP server, forward both directions
- envelope-only parsing, everything else passes through untouched
- **Done when:** Claude Code configured to use `cassette wrap -- <server>`
  behaves identically to using the server directly, across a full real task.
  Zero behavioral difference is the bar.

### Phase 2 — Recording and the CAS1 format
- writer: pre-framed blobs, index, string table
- reader: mmap, zero-copy blob access, index built at load
- embedding at record time (start with a local model or a cheap API; make it
  pluggable and optional via config)
- benchmark harness vs a naive JSON-lines implementation
- **Done when:** a real Claude Code session records to a `.cas` file, and
  `go test -bench` shows lookup latency and the memory delta vs JSONL. Record
  the actual numbers in this file's STATUS section.

### Phase 3 — Replay and the matching ladder
- L0, L1, L2 tiers with per-call tier reporting
- tool classification (read vs write) and the miss policy
- `id` patching, ordering tolerance
- **Done when:** a recorded session replays end to end with no network access
  at all (verify by pulling the network interface or blocking egress), the
  agent completes the task, and each call reports which tier served it.

### Phase 4 — Trajectory diff
- weighted edit-distance alignment over two tool-call sequences; substitution
  cost from tool equivalence, not name equality
- verdict classification: identical / same-outcome-different-path /
  outcome-changed
- terminal output (the CLI is the primary interface, make it good)
- **Done when:** changing the system prompt on a real recorded task produces
  a correct, readable diff that a human agrees with.

### Phase 5 — Suite runner and parallelism
- worker pool, one proxy process per cassette, shared page cache
- aggregate reporting, `--fail-on` flag, nonzero exit
- **Done when:** N cassettes run in parallel and we have a measured
  serial-vs-parallel wall clock number for the README.

### Phase 6 — chDB analytics
- schema: narrow spans table ordered by (project, tool_name, started_at),
  payloads out of line keyed by span_id, materialized view for daily rollups
- the queries that matter: per-tool p95, tier hit rates, and "calls present
  in failing trajectories but absent from passing ones"
- **Done when:** those three queries run against real recorded data and
  return something a human finds interesting.

### Phase 7 — Web UI
- React + Vite + design tokens, embedded via go:embed, served by
  `cassette serve` on localhost:7070
- Screen 1 runs list, Screen 2 run waterfall with tier badges, Screen 3
  trajectory diff (the hero, polish this most), Screen 4 suite grid
- Dense, dark, monospace-forward. A profiler, not a marketing dashboard.
- **Done when:** all four screens work against real local data and the diff
  screen is good enough to be the README gif.

### Phase 8 — Distribution and demo
- `cassette export --static`, Cloudflare Pages deploy
- GitHub Action + PR comment reporter
- README with the real before/after numbers, gif of the diff screen
- **Done when:** a public URL shows real recorded runs, and the Action fails
  a PR that changes agent behavior.

## 8. Git discipline (non-negotiable)

The commit history is part of the deliverable. A recruiter who opens the repo
sees the history before they see the code, and one giant "initial commit" of
40 files reads as generated. So:

- **One logical unit per commit, one file at a time where it makes sense.**
  Never `git add .`. Stage explicitly by path.
- **Commit as we go, not at the end of a phase.** The format spec, the writer,
  the reader, and the benchmark are four commits, not one.
- **Conventional commit messages** with a real body explaining the why, not
  just the what. The why is what makes the history worth reading.
  Example: `feat(cassette): store blobs pre-framed to avoid replay allocation`
- **Each phase ends with its own tag** (`v0.1-proxy`, `v0.2-record`, ...) so
  the progression is visible at a glance.
- **Commits must build.** Never commit a broken intermediate state.
- Every commit message ends with:
  ```
  Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01WYHJ214mmd9ezJZSL9rH9K
  ```

Rough target: 60 to 100 commits across the eight phases, spread over real
working sessions. That history is itself evidence of how the thing was built.

## 9. Interview talking points this project generates

Keep these current, they are half the reason to build it.

- Why the replay hot path is a local mmap file and the analytics layer is
  ClickHouse. Opposite access patterns, one sentence each.
- Why embeddings are precomputed at record time (moving work off the hot path).
- Why brute-force vector search beats an ANN index at bucket sizes under ~30.
- The read/write miss policy and default-deny. Where you draw that line is the
  actual engineering.
- Why local-first is a product requirement, not a cost dodge.
- The HTTP-testing analogy: agent testing in 2026 is where HTTP testing was
  before vcr/betamax/nock made cassettes normal.

## 10. STATUS (update every session)

### Build environment (read this first if the build does not work)

Work happens on Kshitij's Mac in `/Users/kshitijmishra/Cassette`, reached
through the desktop Linux VM at `$HOME/mnt/Cassette`.

Two environment facts that cost time to discover:

1. **Go is not preinstalled in that VM, and go.dev is blocked** by the egress
   proxy, as are `proxy.golang.org`, `sum.golang.org` and vanity import hosts
   like `go.yaml.in`. Only `github.com` and `registry.npmjs.org` resolve.
   Go 1.27.1 was installed from the `actions/go-versions` GitHub release into
   `$HOME/go-toolchain`. Every shell needs `source $HOME/goenv.sh` first,
   which sets GOROOT, GOPATH, GOPROXY=direct and GOSUMDB=off.
2. **Git needs delete permission** on the connected folder or every commit
   fails on its own lock files. Granted per session via
   `device_request_delete_permission`. If commits start failing with
   "Operation not permitted", that is what to re-request.

Consequence: **the core is stdlib only.** Partly forced by the network, but
kept on purpose. A binary that sits on the wire between an agent and its tools
sees every credential that passes through, so a zero-dependency build keeps
that supply chain surface at zero. CI enforces it by failing if `go.sum` ever
becomes non-empty. chDB (phase 6, cgo) and the React UI (phase 7, npm) are the
two known exceptions and both are isolated behind build tags or a separate
toolchain.

### Current phase

**Phase 0 complete** (tag `v0.1-scaffold`). Phase 1 is next.

### Completed

- **Phase 0 — scaffold.** 14 commits, tagged `v0.1-scaffold`.
  - dependency-free CLI router with fixed exit codes; `ExitFailure` (behavior
    changed) is distinct from `ExitError` (the tool broke), because a CI job
    that cannot tell them apart is useless
  - argv split at `--` happens *before* flag parsing, so a child flag that
    collides with one of ours reaches the child intact; the capped slice
    preventing child-argv corruption has a test pinning it
  - `CASSETTE_MODE` / `CASSETTE_TAPE` env contract, with the reasoning: the
    agent spawns the shim from its own static MCP config, so mode cannot be a
    flag. Unset means off and invalid fails closed to off, so a shim left in a
    config is invisible during ordinary work
  - full command surface registered with phase-gated stubs that error rather
    than exit zero
  - build stamping, Makefile with `-trimpath` and ldflags, CI running fmt,
    vet, test, race, cross-compile, plus a guard that fails if `go.sum`
    becomes non-empty
  - **Verified:** `make check` passes, `cassette --help` and `cassette version`
    work from a stamped release build, usage errors and phase gates return the
    right exit codes.

### Decisions made during implementation

- **Config file is `cassette.json`, not YAML.** No stdlib YAML parser, and
  writing one is yak-shaving. JSON also matches what the agent ecosystem
  already uses (`.mcp.json`, `claude_desktop_config.json`).
- **CLI shape:** `wrap` is the shim installed once in the agent's MCP config;
  `record` and `replay` are outer commands that run the agent with the right
  mode in its environment. The user never edits their MCP config again.

### Measured numbers to fill in

- [ ] Phase 2: lookup latency, CAS1 vs JSONL memory and speed
- [ ] Phase 3: confirmed zero-network replay
- [ ] Phase 5: serial vs parallel suite wall clock
- [ ] README headline metric

### Next action

**Phase 1, the transparent proxy.** In order:

1. `internal/jsonrpc`: newline-delimited framing reader/writer, envelope-only
   parsing (`id`, `method`, `params.name`, `params.arguments`), everything
   else passed through as opaque bytes
2. `internal/proxy`: spawn the child MCP server, pump both directions,
   forward stderr, propagate exit codes and signals
3. wire `wrap` to it
4. **Exit criterion:** Claude Code configured with
   `cassette wrap -- <server>` behaves identically to using the server
   directly, across a full real task. Zero observable difference is the bar.
