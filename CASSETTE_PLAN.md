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
| Analytics engine | ~~chDB embedded~~ → **export to ClickHouse** | Reversed in phase 6. chdb-go needs `golang.org/x/sys`, which this network blocks, but the decision stands on merit: embedding costs cgo, the static binary, and the cross-compile matrix, to serve a weekly query path. The schema is still ours and now runs against clickhouse-local, self-hosted, or Cloud unchanged |
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

**Phase 6 complete** (tag `v0.7-analytics`). Phase 7 is next.

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

- **Phase 1 — transparent proxy.** 10 commits, tagged `v0.2-proxy`.
  - `internal/jsonrpc`: newline-delimited framing. `ReadMessage` returns
    borrowed bytes valid until the next call, so a megabyte tool result is not
    copied on every hop; callers that retain must copy, which puts the cost at
    the call site. Measured zero allocations per message in steady state on
    both read and write.
  - `internal/jsonrpc`: two-stage envelope parsing. Stage one omits `params`
    and `result` so encoding/json walks and discards payloads without
    materializing them; stage two runs only for `tools/call`, which are small.
    Allocation is flat at 72 B across a 512x payload range.
  - `internal/proxy`: bidirectional pump, stderr passed through by file
    descriptor, unparseable messages forwarded anyway, observers wrapped in
    recover, forwarding always ahead of observation.
  - Shutdown ladder: close child stdin, wait, SIGTERM, wait, SIGKILL, with
    the drain completing before `cmd.Wait` (which closes the stdout pipe).
  - `wrap` wired up; passthrough uses a nil observer so nothing is parsed.
  - **Verified:** a real MCP server (`@modelcontextprotocol/server-everything`)
    driven through initialize, tools/list and two tools/call requests produces
    a byte-identical stream direct vs wrapped. 9878 bytes, sha256
    `bab38bd4...`. Reproduce with `make verify-transparency`.
  - Also verified by hand: exit status propagates (7 stays 7), child stderr
    passes through, a 2 MB payload survives a round trip, an invalid
    `CASSETTE_MODE` is rejected rather than silently ignored.

- **Phase 2 — recording and CAS1.** 14 commits, tagged `v0.3-record`.
  - `internal/tape`: CAS1 format, writer with atomic assembly, mmap reader
    with all offsets validated up front (the index is reached via an unsafe
    cast, so a corrupt tape must fail to open rather than hand out slices
    into nothing).
  - `internal/record`: recorder as a `proxy.Observer`, per-server tape naming,
    run manifest, cross-tape trajectory merge.
  - `record` and `inspect` commands.
  - **Verified:** a real MCP session recorded end to end. 6 messages, 2 tool
    calls, including an unprompted `notifications/tools/list_changed` from
    the server, which is what justified the `EntryServerInitiated` flag.

- **Phase 3 — replay and the matching ladder.** 8 commits, tagged `v0.4-replay`.
  - `internal/jsonrpc`: exact id splicing (820 ns, 1 alloc for an 8 KB
    message), depth-1 only so nested payload ids are never touched.
  - `internal/safety`: read/write classification, failing closed.
  - `internal/match`: exact → normalized → method-only ladder, every answer
    reporting its tier.
  - `internal/replay`: serves from the tape with **no server process**;
    spawns one lazily only for read-class misses, fast-forwarding it through
    the recorded handshake first.
  - `internal/config`: `cassette.json`.
  - **Verified:** `make verify-replay`. Every response replayed
    byte-identically to the live session, 10032 bytes across 5 responses,
    sha256 `7dcb0ea5...`, with every proxy variable stripped from the
    environment. Record took 888 ms; hermetic replay took **24 ms**.
  - **Verified by hand:** an unmatched read-class call falls through to a
    lazily spawned server (242 ms → 1.017 s, the cost made visible); an
    unmatched `create_pull_request` stays refused even outside hermetic mode.

- **Phase 4 — trajectory diff.** 5 commits, tagged `v0.5-diff`.
  - `internal/diff`: Needleman-Wunsch alignment with tool-aware substitution
    costs, three-valued verdict, side-by-side rendering that collapses
    identical runs.
  - Replay now records its own trajectory; the diff runs automatically after
    every replay and `--fail-on` gates CI on it.
  - **Decided:** the outcome is the sequence of write-class calls. Reads are
    path, not outcome. Reuses the safety classifier, which is a good sign that
    abstraction was the right one.
  - **Verified:** an unchanged re-run reports `identical`; a genuinely
    different agent against the same tape renders a readable side-by-side and
    exits non-zero.

- **Phase 5 — suite runner and parallelism.** 4 commits, tagged `v0.6-suite`.
  - `internal/suite`: discovery, parallel worker pool, per-case capture.
  - `cassette test` with `--jobs`, `--fail-on`, `--hermetic`.
  - **Verified:** a 12-cassette suite with 3 deliberately altered cases
    reports 9 ok / 3 CHANGED and exits 1.

- **Phase 6 — ClickHouse analytics.** 3 commits, tagged `v0.7-analytics`.
  - `internal/chexport`: schema, queries, JSONEachRow export, `load.sh`.
  - `cassette export --clickhouse`.
  - **Verified against ClickHouse 26.8.3**, not asserted. `make verify-clickhouse`.

### Measured: the analytics layer, run for real

12-cassette suite, 87 spans, 150 payloads, 11 KB exported.

```
tool       calls  p50_ms  p95_ms        identical drift changed served pct_exact
echo          24   551.3   643.8                9     0       3     60       100
add           12   551.2   637.8
printEnv       3   545.8   554.5        tool      in_failing in_passing
                                        printEnv           3          0
```

The last table is the query the schema exists for: every run that changed
called `printEnv`, no passing run ever did. Not "something broke" but "runs
that broke all did this, and runs that passed never did".

The rollup's merged p95 matches the raw scan exactly, which the verification
asserts, because a materialized view that silently diverges from its source
makes every dashboard built on it lie.

### Measured: what replay actually buys

12 cassettes, 4 cores. Reproduce with `make bench-suite`.

| | wall clock | vs live |
|---|---|---|
| live sessions | 11.20 s | 1x |
| replay, serial | 0.25 s | 45x |
| replay, parallel | 0.08 s | **147x** |

Parallel over serial is 3.3x on 4 cores, close to the ceiling, which is what
you get when cases share no external resource.

**State this carefully in the README.** The 147x is the cost of the
*environment*, not of a whole agent. The driver issues a fixed sequence of
requests; it is not a model deciding what to do next, and a real agent
replaying a tape still pays for inference every turn. The narrow, true claim:
replay removes the environment's cost almost entirely and removes every side
effect. What remains is model time, which is also where parallelism helps
most, since replayed cases have nothing to contend over.

### Two bugs the end-to-end run caught that unit tests could not

Both are worth keeping; they are the best stories in the project.

**A use-after-free created by our own zero-copy optimization.** `FromTape`
built a trajectory holding strings that pointed into the memory mapping, then
the caller closed the reader before rendering. Segfault in `strings.TrimSpace`,
six frames from anything that looked related. The borrowed-slice contract is
right for blobs, where it saves megabytes per replay; for an interned method
name it saved perhaps twenty nanoseconds and bought memory corruption. Names
are now owned, blobs still are not.

**The tape is in completion order, the replay is in request order.** The
recorder writes an entry when the *response* arrives, because that is the
first moment the exchange is complete, so a fast call started second lands on
the tape ahead of a slow call started first. Comparing tape order against
request order made an unchanged re-run report `outcome changed` with a
spurious reordering. No unit test would have found this: both sides were
individually correct, and only running them against each other exposed it.

### The verification harness was wrong, and finding out was the point

The phase 3 replay check failed. The cause was not in cassette. Running the
same driver against `@modelcontextprotocol/server-everything` three times:

    run 1  9904 bytes  sha aca5cb5c...
    run 2  9878 bytes  sha bab38bd4...
    run 3  9878 bytes  sha bab38bd4...

That server emits `notifications/tools/list_changed` on a timer, so two
**live** runs do not agree with each other. Comparing whole byte streams was
measuring the server's jitter. It also means the phase 1 transparency check
had been passing by luck.

The driver now compares only the response stream, correlated by request id,
which is the deterministic part. Unprompted notifications are counted and not
compared, because nothing could make them reproducible.

Worth stating plainly in the README: the world being nondeterministic is the
premise of this entire project, and it turned up inside the test harness.

### Measured: CAS1 versus JSON lines

Corpus of 200 calls with 8 KB responses, on arm64.

| | CAS1 | JSONL |
|---|---|---|
| load time | 6.9 µs | 3.58 ms |
| load allocs | 5.5 KB, 9 | 4.9 MB, 1013 |
| lookup | 6.8 ns, 0 allocs | 8.6 ns, 0 allocs |
| heap after load | ~0 (page cache) | 1.96 MB |
| projected at 50 workers | one shared 1.6 MB mapping | ~93 MB heap, nothing shared |

The honest finding: **lookup is a wash.** Once a map is built, a map is a
map. CAS1 wins on load, 520x in time and 900x in memory, because it decodes
nothing to become usable. That is what suite scale is made of, since replays
are independent processes and only the mapped form is shared by the kernel.

Which is where the design started: reads were never the bottleneck, so the
format only had to avoid allocating.

### Bugs found by tests rather than by reasoning

Worth keeping, since these are the interview stories.

- **Shutdown deadlock.** Reaping the child was gated on both pumps
  finishing. That deadlocks against exactly the case escalation exists for: a
  server ignoring stdin close never closes stdout, so the drain never
  completes and the SIGTERM never fires. A rescue timer must not depend on
  the thing it is rescuing. Caught by a test that hung for 60 seconds.
- **Benchmark measuring the wrong thing.** The first framing benchmark built
  a new Reader per iteration and charged every run for a 256 KiB bufio
  allocation, hiding the per-message number entirely.
- **Overbroad gitignore.** A bare `cassette` pattern matched the
  `cmd/cassette` package directory, not just the built binary.
- **Flags after positionals were silently dropped.** Go's `flag` stops at the
  first non-flag argument, so `cassette record demo --suite /tmp/x` parsed as
  three positionals and `--suite` kept its default while the command appeared
  to work. Found by typing the command, not by reading the code.
- **Tape naming took the last argument** and named a real recording `stdio`,
  the transport selector. Arguments trail the thing they configure, so the
  scan had to go forward, not backward.
- **Recording only tools/call would produce unbootable tapes.** Replay has no
  server process, so `initialize` and `tools/list` must be on the tape too.
  Caught while writing the format, not after.
- **`initialize` could never match.** Its params carry the agent's own name,
  version and capabilities, which differ between clients and across versions
  of one client. Hashing them meant replay refused the very first message and
  the agent could not start. Fixed with a narrow method-only tier, restricted
  to an explicit set rather than used as a general fallback. Found by running
  a real replay, not by reading the code.
- **Shutdown escalation was gated on the drain it was meant to rescue** (phase
  1, still the best of these).
- **The rollup would have silently returned wrong counts.** `calls` and
  `errors` were plain `UInt64` in an `AggregatingMergeTree`. Background merges
  collapse rows sharing a sorting key and keep an arbitrary value for plain
  columns, so the table looks correct after insert and goes wrong after a
  merge: silent, delayed, unreproducible on small data. ClickHouse refuses the
  DDL outright, which is how it was found. Fixed with
  `SimpleAggregateFunction(sum, UInt64)`.
- **Two definitions of "changed" in one codebase.** The export re-derived a
  verdict from report counters instead of using the diff, disagreed with
  `cassette test` on the first real suite, and silently emptied the query that
  asks what failing runs do differently.
- **A uniform demo suite cannot demonstrate a comparison.** The first run of
  the failing-versus-passing query returned nothing because every cassette
  used the same tools. Fixed the harness, not the query.

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

**Phase 7, the web UI.** In order:

1. React + Vite, built and embedded with `go:embed` so `cassette serve` stays
   one binary with no node runtime.
2. Four screens: runs list, run waterfall with match-tier badges, trajectory
   diff (the hero, polish this most), suite grid.
3. Deliberately no metrics dashboard. That space is saturated and building one
   would make the project look like a clone.
4. **Exit criterion:** all four screens work against real local data and the
   diff screen is good enough to be the README gif.

Everything the UI needs already exists as structured data: `record.OpenRun`
gives the merged trajectory, `replay.Report` gives per-call tiers,
`diff.Compare` gives the alignment and verdict. The UI is a presentation layer
over APIs that already work, which is the right order to have built them in.

Note: node and npm work in this environment (the npm registry is reachable),
so the Vite build is buildable here, unlike chDB.

Superseded plan for phase 6, kept for reference:

1. chDB via `chdb-go` behind a build tag, so the default build stays pure Go
   and dependency-free and only the analytics build pulls cgo.
2. Schema: narrow spans table ordered by `(project, tool_name, started_at)`,
   payloads out of line keyed by span id, materialized view for daily
   rollups.
3. An ingest path that walks a suite's tapes into it.
4. The three queries that justify it: per-tool p95 latency, match-tier hit
   rates across runs, and calls present in failing trajectories but absent
   from passing ones.
5. **Exit criterion:** those three queries run against real recorded data and
   return something a human finds interesting.

Open question to settle while building: chdb-go is cgo, which breaks the
single static binary and the clean cross-compile matrix. A build tag keeps
the default build clean, but then the analytics feature is not in the
released binary at all. The alternative is shipping two binaries. Decide once
the cgo build is actually working, not before.

Superseded plan for phase 5, kept for reference:

1. `internal/suite`: worker pool, one replay process per cassette. Replays
   touch nothing external and share no state, so they are embarrassingly
   parallel; the only real ceiling is the model provider's rate limit.
2. Aggregate reporting: the pass/drift/fail grid across every cassette.
3. Wire `cassette test`, with `--jobs` and the `--fail-on` policy already
   built in phase 4.
4. **Exit criterion:** N cassettes run in parallel, with a measured
   serial-versus-parallel wall clock for the README.

Everything phase 5 needs already exists: the diff, the verdict, the per-tape
report files, and the exit-code contract. This phase is mostly orchestration,
which is why it should go quickly.

Note for the README: the single-run numbers are already good. Recording the
verification session took 888 ms; the hermetic replay took **19 ms**. The
parallel number from this phase is what turns that into the headline.

Superseded plan for phase 4, kept for reference:

1. `internal/diff`: weighted edit-distance alignment over two tool-call
   sequences. Substitution cost from tool equivalence, not name equality.
2. Verdict classification: identical / same-outcome-different-path /
   outcome-changed. The middle one is the interesting verdict.
3. Terminal rendering. The CLI is the primary interface and this is its
   headline output, so it has to be genuinely good.
4. **Exit criterion:** changing a system prompt on a real recorded task
   produces a diff a human agrees with.

What phase 3 already provides for free, from the matcher's own bookkeeping:
`Unused()` gives recorded calls this run skipped, and the tier counts give
how loosely each call was matched. Half the diff exists already.

Open question for phase 4: what counts as "the outcome"? For a coding agent
the honest answer is probably the set of write-class calls made, since those
are what actually changed the world, with read-class calls treated as path
rather than outcome. Decide against a real recording rather than in the
abstract.

Superseded plan for phase 3, kept for reference:

1. `internal/match`: the L0/L1/L2 ladder. L0 and L1 hashes already exist on
   every entry, so build both maps at load and add the tool-bucketed fuzzy
   tier behind them.
2. Tool safety classification (read vs write) with default-deny, plus the
   `cassette.json` config that carries the allowlist.
3. `internal/replay`: serve matched calls from the tape with **no child
   process at all**. Patch the JSON-RPC id in place and write the mmap slice.
4. Emit server-initiated entries unprompted at the right point in the
   trajectory.
5. Wire `replay`; report the match tier per call.
6. **Exit criterion:** a recorded session replays end to end with network
   access blocked entirely, the agent completes, and every call reports which
   tier served it.

Open question for phase 3, decide while building: id rewriting. The agent
will not necessarily reuse the same JSON-RPC ids on a second run, so the
recorded response's id has to be replaced with the live request's. The
format anticipates patching in place, which requires the id field to be at a
fixed width. It is not today, so either the writer normalizes ids at record
time (changing the stored bytes, which weakens the byte-transparency claim)
or replay does one small copy per response. Leaning toward the copy: it is
one allocation against a 1.5 s model turn, and keeping recorded bytes exactly
as they arrived is worth more than the nanoseconds.

Superseded plan for phase 2, kept for reference:

1. `internal/tape`: CAS1 writer. Blobs stored pre-framed (message plus
   newline), fixed-width 32-byte index entries, string table, optional vector
   section.
2. `internal/tape`: reader. mmap, index cast at load, zero-copy blob access.
3. `internal/record`: an `proxy.Observer` that correlates request to response
   by id and appends to the tape. Must copy the borrowed bytes, and must not
   do real work inline on the forwarding path.
4. `record` command: set `CASSETTE_MODE`/`CASSETTE_TAPE` on the agent process
   and run it.
5. Benchmark harness against a naive JSON-lines implementation.
6. **Exit criterion:** a real agent session records to a `.cas` file, and
   `go test -bench` reports lookup latency and the memory delta versus JSONL.
   Put the actual numbers in the Measured section above.

Open questions to settle in phase 2:

- Multiple MCP servers under one agent run share a `CASSETTE_RUN_ID` but are
  separate processes writing concurrently. One tape per server with a
  manifest, or one shared tape with file locking? Leaning toward per-server
  tapes plus a run manifest, since it avoids cross-process write contention
  on the latency path.
- Embedding at record time needs an embedder. Defer the choice until phase 3
  actually needs vectors; leave the section optional in the format so the
  writer does not block on it.
