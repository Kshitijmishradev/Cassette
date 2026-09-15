# Cassette

**Record an agent's real tool calls once. Replay them safely after every prompt or model change.**

[![CI](https://github.com/Kshitijmishradev/Cassette/actions/workflows/ci.yml/badge.svg)](https://github.com/Kshitijmishradev/Cassette/actions/workflows/ci.yml)
[![Go 1.27](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Go dependencies: 0](https://img.shields.io/badge/Go_dependencies-0-2ea44f)](./go.mod)
[![Live demo](https://img.shields.io/badge/Live_demo-open-8b5cf6)](https://cassette-agent-replay.pages.dev/#/runs/case-04/diff)

> Change the agent. Keep the world still. See exactly what changed.

[Try the interactive demo](https://cassette-agent-replay.pages.dev/#/runs/case-04/diff) · [Read the architecture](./ARCHITECTURE.md) · [See a real-agent replay](./docs/REAL_AGENT_RUN.md)

---

## The 30-second explanation

Imagine a customer-support agent that can:

1. read a customer profile;
2. fetch an order;
3. check a delivery partner;
4. issue a Stripe refund.

You make its system prompt more cautious about refunds. How do you know the
change helped without rerunning old cases against live customer data and
possibly issuing another real refund?

Cassette records the MCP conversation during the original run. On replay, it
serves the recorded tool responses itself. The databases, delivery API and
Stripe never run.

```text
                         RECORD

  agent  ─────►  Cassette  ─────►  customer DB
                   │        ─────►  order DB
                   │        ─────►  delivery API
                   │        ─────►  Stripe
                   │
                   ▼
              recording.cas1


                         REPLAY

  changed agent  ─────►  Cassette  ─────►  recording.cas1
                                      ✕    real services
```

Cassette then aligns the old and new tool trajectories and answers the useful
question: **did the agent merely take a different path, or did it do something
different to the world?**

---

## What a refund test looks like

Suppose the recorded agent did this:

```text
get_customer → get_order → get_delivery_status → create_refund
                                                        WRITE
```

After changing the prompt, replay produces:

```text
get_customer → get_order → get_delivery_status → ask_for_more_evidence
```

Cassette reports `CHANGED` because the write-class action—creating the
refund—disappeared. It does not claim that the new decision is morally or
business-wise correct. It gives you a deterministic behavioral diff that your
tests, reviewers and domain-specific evals can judge.

That separation is deliberate:

- Cassette freezes the external world.
- Your agent and model remain free to behave differently.
- Your policy or eval decides whether that difference is good.

---

## Three verdicts, not just pass and fail

| Verdict | Meaning | Why you care |
|---|---|---|
| **IDENTICAL** | Same calls, same order | Nothing changed |
| **DRIFT** | Same writes, different path | Outcome is stable, but cost, latency or reasoning changed |
| **CHANGED** | Write calls differ | The agent did something different to the world |
| **UNKNOWN** | Never replayed | Not a pass; there is no comparison yet |

**Drift is the unusual one.** An agent may still reach the same result after
nine extra calls. A binary pass/fail check hides that regression; Cassette
makes it visible.

Writes decide the verdict because they are the observable effect: refunding a
payment, creating a ticket or changing a record. Reads describe the path.

---

## See it

The trace viewer is part of the Cassette binary and also works as a completely
static export.

### [Open the live trajectory diff →](https://cassette-agent-replay.pages.dev/#/runs/case-04/diff)

The public demo contains intentionally safe fixture data, not a live customer
agent or production credentials. It demonstrates the same UI used for real
recordings:

- **Runs list** — sort and scan an entire cassette suite.
- **Waterfall** — inspect timing and the match tier for every call.
- **Trajectory diff** — compare recorded and replayed calls side by side.
- **Suite grid** — spot identical, drifted, changed and untested runs.

Match tiers are visible per call. Exact matches are strongest; normalized and
method-only matches tell you when the new agent is moving away from the tape.

---

## Quick start

For a ready-to-run binary, choose your platform archive from
[Releases](https://github.com/Kshitijmishradev/Cassette/releases) and follow
[QUICKSTART.md](QUICKSTART.md). It includes the local viewer and needs no Go or
Node installation. Release archives contain only the binary, license, quick-start
guide, and security guidance; the website and demo recordings stay separate.

Build the dependency-free Go binary:

```sh
git clone https://github.com/Kshitijmishradev/Cassette.git
cd Cassette
make build
```

Put the shim in front of an MCP server in your agent configuration:

```json
{
  "mcpServers": {
    "github": {
      "command": "/path/to/Cassette/bin/cassette",
      "args": ["wrap", "--", "npx", "-y", "@modelcontextprotocol/server-github"]
    }
  }
}
```

With no Cassette mode selected, the shim forwards messages untouched and
records nothing. It can remain in the configuration during normal work.

Then run the workflow from the outside:

```sh
# 1. Capture a real session
./bin/cassette record fix-auth -- claude -p "fix the failing auth test"

# 2. Inspect its merged tool trajectory
./bin/cassette inspect fix-auth

# 3. Change the prompt or model, then replay the same world
./bin/cassette replay fix-auth -- claude -p "fix the auth test; avoid risky writes"

# 4. Replay a committed suite with no live fall-through
./bin/cassette test --suite ./cassettes --hermetic

# 5. Explore it visually
./bin/cassette serve --suite ./cassettes --open
```

Use `--hermetic` in CI. Wrapped MCP misses cannot reach a live server.
Cassette does not sandbox the agent’s own network, shell, or native tools.

---

## A diff in the terminal

```text
$ cassette test --suite ./cassettes

  case-01                  ok          5 steps         51ms
  case-04                  CHANGED     5 steps -1      34ms
  case-08                  CHANGED     5 steps -1      55ms
  ...

  recorded                                 this run
  ──────────────────────────────────────   ──────────────────────────────────────
                             … 4 identical calls …
  tools/call(echo) {"message":"second…   · tools/call(echo) {"message":"second…
  tools/call(printEnv) {}                !

  5 steps vs 6 (-1) · outcome changed
  5 matched · 0 changed · 0 added · 1 dropped

  12 cassettes · 9 identical · 0 drift · 3 CHANGED · 74ms
```

The agent stopped calling a tool it used to call. Cassette caught the change
in milliseconds, without invoking the real tool again.

---

## Why replay instead of mocks?

| | Live rerun | Handwritten mocks | Cassette |
|---|---:|---:|---:|
| Uses responses a real server returned | Yes | No | **Yes** |
| Repeats the same external world | No | Usually | **Yes** |
| Prevents repeated side effects | No | Yes | **Yes** |
| Shows the agent's trajectory | Limited | Limited | **Yes** |
| Works hermetically in CI | No | Yes | **Yes** |

A mock is somebody's guess about reality. A cassette is a dated recording of
reality. It can become stale, but it cannot pretend a response existed when it
never did.

---

## Safety model

Cassette classifies tools as reads or writes.

- Live MCP fall-through is disabled by default. Exploratory replay requires
  explicit `replay.fallThrough: true` and reviewed tool names in `tools.read`.
- Tool-name heuristics are off by default; names do not prove read-only behavior.
- A write miss is refused by default.
- `--hermetic` refuses all misses and is the recommended CI mode.
- Replay can run without starting the original MCP server at all.

**Run only trusted agents and suites.** A manifest contains an executable agent
command, and hermetic replay does not sandbox that process. See [SECURITY.md](./SECURITY.md)
for deployment guidance, trust boundaries, and the security audit.

Recorded payloads may contain sensitive data. Cassette is local-first and does
not upload tapes to a hosted service. Review or sanitize any recording before
committing or sharing it.

---

## Measured, not guessed

Every number below is reproducible from a Make target.

### Twelve-cassette suite on four cores

| Mode | Wall time | Relative to live |
|---|---:|---:|
| Live sessions | 11.20 s | 1× |
| Replay, serial | 0.25 s | 45× |
| Replay, parallel | 0.08 s | **147×** |

This measures the environment, not model inference. A real agent still pays
for its model calls; Cassette removes external-service latency and side
effects.

### Tape format versus JSONL

| Operation | CAS1 | JSONL |
|---|---:|---:|
| Load | 6.9 µs | 3.58 ms |
| Load allocations | 5.5 KB | 4.9 MB |
| Lookup | 6.8 ns | 8.6 ns |

The MCP wire path runs with zero steady-state allocations per message. The Go
core has zero third-party dependencies, enforced by CI.

---

## Real-agent proof

Cassette was verified with Codex and the MCP Everything reference server. The
replay deliberately pointed at a nonexistent server binary:

```text
tape                          served   exact    norm  method    live refused
server-40a45c                      6       6       0       0       0       0

6 served (100% exact) · 14.995s
server-40a45c: 6 steps vs 6 · identical
```

The agent completed all calls—including `17 + 25`—and returned `42`. Every
response came from the tape. Read the full [real-agent run](./docs/REAL_AGENT_RUN.md).

---

## Use it in a pull request

The repository is also a composite GitHub Action. It replays a committed
suite and preserves Cassette's exit codes. A PR comment is optional; give write
credentials only to workflows that execute exclusively trusted code.

```yaml
permissions:
  contents: read

steps:
  - uses: actions/checkout@v4
    with:
      persist-credentials: false
  - uses: Kshitijmishradev/Cassette@main
    with:
      suite: ./cassettes
      fail-on: outcome
```

---

## Analytics without turning into a dashboard

```sh
cassette export --clickhouse ./analytics
cd analytics && ./load.sh
clickhouse local --path ./db --queries-file queries.sql
```

Cassette exports to ClickHouse for questions across many runs, such as “which
tool appears only in changed cases?” It does not embed a metrics database or
ship a generic observability dashboard. Replay and behavioral diffing are the
product.

---

## Project status

The core workflow is implemented and verified end to end:

- transparent MCP proxy;
- binary CAS1 recording format;
- exact, normalized and method-only replay matching;
- write-aware trajectory diff;
- parallel suite runner;
- ClickHouse export;
- embedded React trace viewer and static export;
- release packaging and composite GitHub Action;
- public Cloudflare Pages demo.

The remaining distribution task is activating the separate Homebrew tap. See
[PROGRESS.md](./PROGRESS.md) for detailed phase history, limitations and the
bugs found through real end-to-end testing.

---

## Build and verify

```sh
make build     # build ./bin/cassette
make check     # format check, vet and tests
make verify    # end-to-end proxy, replay and ClickHouse checks
make dist      # macOS/Linux × ARM64/AMD64
```

Requires Go 1.27. The Go core has no third-party dependencies.

---

## Documentation

| Document | What it contains |
|---|---|
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Protocol, storage and design decisions |
| [GOALS.md](./GOALS.md) | Thesis, users, success criteria and non-goals |
| [PROGRESS.md](./PROGRESS.md) | Completed phases, remaining work and known limitations |
| [docs/REAL_AGENT_RUN.md](./docs/REAL_AGENT_RUN.md) | Evidence from a real Codex record/replay run |
| [CASSETTE_PLAN.md](./CASSETTE_PLAN.md) | Original implementation plan and session context |

## License

[MIT](./LICENSE)
