# Progress

What is built, what is left, and what went wrong on the way.

Last updated: 2026-09-14 · 88 commits · tags through `v0.8-web`

For how it works see [ARCHITECTURE.md](./ARCHITECTURE.md). For what it is for
see [GOALS.md](./GOALS.md). The working plan with full session context is
[CASSETTE_PLAN.md](./CASSETTE_PLAN.md).

---

## Status at a glance

**8 of 9 phases complete. Phase 8 implementation is complete; external publication and its README capture are pending.**

| phase | | tag | verified by |
|---|---|---|---|
| 0 | Scaffold | `v0.1-scaffold` | stamped release build, exit codes |
| 1 | Transparent proxy | `v0.2-proxy` | byte-identical against a real MCP server |
| 2 | Recording and CAS1 | `v0.3-record` | real session recorded end to end |
| 3 | Replay and matching | `v0.4-replay` | byte-identical hermetic replay |
| 4 | Trajectory diff | `v0.5-diff` | unchanged run reports identical; changed run renders |
| 5 | Suite and parallelism | `v0.6-suite` | 12 cassettes, 3 altered, correctly detected |
| 6 | ClickHouse analytics | `v0.7-analytics` | every query run against ClickHouse 26.8.3 |
| 7 | Web UI | `v0.8-web` | four screens against live and static data |
| 8 | Distribution and demo | — | code verified; Cloudflare/tap secrets pending |

```
7,816 lines of Go    4,170 lines of tests    137 tests    11 benchmarks
0 dependencies (enforced by CI)
```

Working commands: `wrap`, `record`, `inspect`, `replay`, `test`, `serve`,
`export --clickhouse`, `export --static`, `export --fixtures`, `version`.
There are no remaining command stubs.

---

## What is built

### Phase 0 — Scaffold

Dependency-free CLI router with an exit-code contract that distinguishes
"behavior changed" from "the tool broke", because a CI job that cannot tell
those apart is useless. Argv splits at `--` before flag parsing, so a child
flag colliding with one of ours reaches the child intact. The
`CASSETTE_MODE` environment contract, with unset meaning off so a shim left in
a config is invisible during ordinary work. Build stamping, Makefile with
`-trimpath`, CI running fmt, vet, test, race and cross-compile plus a guard
that fails if `go.sum` becomes non-empty.

### Phase 1 — Transparent proxy

Newline-delimited JSON-RPC framing (the plan said Content-Length; that is
LSP's, and checking the spec first saved a whole layer). Envelope-only parsing
in two stages. A bidirectional pump with stderr passed through by file
descriptor, unparseable messages forwarded anyway, observers panic-contained,
and forwarding always ahead of observation.

Measured: zero allocations per message on both halves in steady state.
Envelope allocation flat at 72 B across a 512x payload range.

**Verified:** `make verify-transparency` runs a real MCP server through a full
lifecycle direct and wrapped and compares the response streams byte for byte.

### Phase 2 — Recording and the CAS1 format

The on-disk format: fixed-width index castable from a memory mapping,
pre-framed blobs, interned names, atomic assembly, all offsets validated at
load. The recorder as an inline observer that does almost nothing inline.
Per-server tapes with a manifest assembled afterwards by scanning, so
concurrent shims never contend.

Measured against JSON lines: 520x faster to load, 900x less memory, lookup a
wash. The finding the format was not designed for, and the more useful one.

### Phase 3 — Replay and the matching ladder

Serving an agent entirely from a tape with **no server process**. The
exact/normalized/method-only ladder with per-call tier reporting. Read/write
classification failing closed. Exact id splicing at depth 1. Lazy live
fall-through that fast-forwards a freshly spawned server through the recorded
handshake first.

**Verified:** every response byte-identical to the live session, with every
proxy variable stripped from the environment. 888 ms live → 19 ms replayed.

### Phase 4 — Trajectory diff

Needleman-Wunsch alignment with tool-aware substitution costs. The
three-valued verdict decided on write-class calls, with writes aligned
separately from the full trajectory. Side-by-side rendering that collapses
runs of identical calls.

### Phase 5 — Suite and parallelism

Discovery that refuses recordings without a manifest, a parallel worker pool,
per-case output capture, and an aggregate report with a CI exit contract.

Measured: 12 cassettes, 11.20 s live → 0.25 s serial → 0.08 s parallel.

### Phase 6 — ClickHouse analytics

Schema, queries and JSONEachRow export. Exported rather than embedded, which
reversed an earlier decision: embedding costs cgo, the static binary and the
cross-compile matrix to serve a weekly query path.

**Verified against ClickHouse 26.8.3**, including that the rollup's merged p95
matches the raw scan exactly.

---

## What is left

### Phase 7 — Web UI — complete

`cassette serve` runs on loopback, with React built by Vite and embedded via
`go:embed` so the binary stays one file with no Node runtime.

Four screens:

1. **Runs list** — sortable table. Navigation, deliberately plain.
2. **Run waterfall** — each call as a timeline bar, with a badge showing which
   tier served it. That badge is the differentiator made visible.
3. **Trajectory diff** — the hero screen and the README gif. If only one
   screen is polished, this is it.
4. **Suite grid** — pass/drift/fail cells, click through to a diff.

Explicitly not building a metrics dashboard (see [GOALS.md](./GOALS.md)).

Everything the UI needs already exists as structured data: `record.OpenRun`
gives the merged trajectory, `replay.Report` gives per-call tiers,
`diff.Compare` gives the alignment and verdict. This is a presentation layer
over APIs that already work.

All four screens work against both real local data and backend-free exports.
The runs list is sortable, waterfall calls expose their match tier, trajectory
diffs collapse identical regions and distinguish verdict-deciding writes, and
the suite grid links each cell to its diff.

**Verified:** production build, embedded asset test, live API handler tests,
static export test, missing-diff 404, dark/light browser inspection, and a
real Codex recording rendered in the waterfall and diff views.

### Phase 8 — Distribution and demo — activation pending

- `cassette export --static` precomputes every view as JSON next to the SPA
- a Cloudflare Pages workflow assembles the offline demo, preserves it as a
  workflow artifact, and deploys when account secrets are configured
- GoReleaser builds Darwin/Linux for ARM64/AMD64, writes SHA-256 checksums,
  and generates a Homebrew cask for `Kshitijmishradev/homebrew-tap`
- the composite GitHub Action preserves exit codes 0/1/2, writes the job
  summary, and creates or updates one PR comment
- a portable committed cassette exercises that Action without Node, network,
  or the original MCP server
- a real Codex agent run now reports 6/6 exact, no fall-through, no refusals,
  and an identical trajectory; see `docs/REAL_AGENT_RUN.md`

GoReleaser 2.18.1 validated the configuration and produced all four snapshot
archives plus the cask. `actionlint` 1.7.12 accepts every workflow.

**External activation still required:** create/configure the Cloudflare Pages
project and its two GitHub secrets, create the separate Homebrew tap and its
write-token secret, publish the public URL, then capture that deployed diff
screen for the final README gif. Those are activation work, not unfinished
application code.

---

## Bugs worth keeping

Every one of these was found by running something, not by reading code. They
are listed because the failure modes are more instructive than the fixes.

**A correct real-agent replay vanished during normal MCP shutdown.** The shim
wrote its report only after stdin reached EOF, but Codex terminates MCP
children as soon as the task is done. Every response had come from tape and
the agent had finished, yet the parent saw `no shim reported in`. Reports are
now atomically checkpointed after each message, so abrupt client teardown
cannot erase the observed trajectory or leave truncated JSON behind.

**Shutdown escalation was gated on the drain it was meant to rescue.** Reaping
the child waited for both pumps to finish. That deadlocks against exactly the
case escalation exists for: a server ignoring stdin close never closes its
stdout, so the drain never completes and the SIGTERM never fires. Caught by a
test that hung for a full minute. A rescue timer cannot depend on the thing it
is rescuing.

**A use-after-free created by our own zero-copy optimization.** Building a
trajectory from a tape and closing the reader before rendering segfaulted in
`strings.TrimSpace`, six frames from anything related. Borrowed slices are
right for blobs, where they save megabytes per replay; for an interned method
name it saved twenty nanoseconds and bought memory corruption.

**The tape is in completion order, the replay is in request order.** The
recorder writes an entry when the *response* arrives, so a fast call started
second lands ahead of a slow one started first. Comparing tape order against
request order made an unchanged re-run report "outcome changed". No unit test
would have found this: both sides were individually correct, and only running
them against each other exposed it.

**`initialize` could never match.** Its params carry the agent's own name,
version and capabilities, so hashing them meant replay refused the very first
message and the agent could not start. Fixed with a narrow method-only tier.

**The verification harness was measuring the server's jitter.** Three identical
live runs produced three different byte streams, because that server emits a
notification on a timer. Comparing whole streams tested nothing about
cassette, and it means the phase 1 check had been passing by luck. The premise
of the project turned up inside its own test suite.

**The rollup would have silently returned wrong counts.** `calls` and `errors`
were plain `UInt64` in an `AggregatingMergeTree`. Background merges collapse
rows sharing a sorting key and keep an arbitrary value for plain columns, so
the table looks correct after insert and goes wrong after a merge: silent,
delayed, unreproducible on small data. ClickHouse refuses the DDL, which is
how it was found.

**Two definitions of "changed" in one codebase.** The export re-derived a
verdict from counters instead of using the diff, disagreed with `cassette
test` on the first real suite, and silently emptied the query that asks what
failing runs do differently.

**Flags after positionals were silently dropped.** Go's `flag` stops at the
first non-flag argument, so `cassette record demo --suite /tmp/x` parsed as
three positionals while `--suite` kept its default. Found by typing the
command.

**Tape naming took the last argument** and named a real recording `stdio`, the
transport selector, because arguments trail the thing they configure.

---

## Known limitations

Stated plainly, because a docs page that lists only strengths is marketing.

**Unprompted server notifications are reproduced by position, not by timing.**
A server that emits notifications on a timer cannot be replayed faithfully in
that respect, because two live runs of it do not agree with each other either.
Responses, which are the deterministic part, replay exactly.

**The fuzzy match tier is designed but not live.** The format reserves a vector
section and entries carry tool ids for bucketing, but nothing populates it yet.
Until then, a call whose arguments differ semantically but not textually is a
miss.

**A tape is a point-in-time snapshot and will go stale.** Nothing here detects
that a recording no longer describes reality. The match-tier percentages are
the early warning, but acting on them is currently a human judgement.

**Fall-through extends the tape's usefulness but not the tape.** A read-class
miss is served live and reported, but the response is not appended to the
recording, so the next replay misses identically.

**Single-machine ordering.** Cross-tape ordering uses wall-clock timestamps,
which is correct for servers on one machine and would not be for a distributed
agent.

**The measured speedups are environment cost, not agent cost.** A real agent
replaying a tape still pays for model inference every turn. See
[GOALS.md](./GOALS.md).

---

## Reproducing every claim

```sh
make check                 # fmt, vet, tests, race
make verify-transparency   # real MCP server: direct vs wrapped, byte for byte
make verify-replay         # live session vs hermetic replay, byte for byte
make verify-clickhouse     # load an export, run every query, check the rollup
make bench-suite           # live vs serial vs parallel wall clock
go test -bench . ./...     # framing, parsing, hashing, format comparison
```
