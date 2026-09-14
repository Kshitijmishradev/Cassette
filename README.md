# Cassette

**Record and replay MCP tool calls, so a change to your agent can be measured instead of guessed at.**

[![ci](https://github.com/Kshitijmishradev/Cassette/actions/workflows/ci.yml/badge.svg)](https://github.com/Kshitijmishradev/Cassette/actions/workflows/ci.yml)
[![go](https://img.shields.io/badge/go-1.27-00ADD8)](https://go.dev)
[![deps](https://img.shields.io/badge/dependencies-0-brightgreen)](./go.mod)

```
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

The agent stopped calling a tool it used to call. That is a behavioral change,
caught in 74 milliseconds, without touching anything real.

---

## The problem

An agent run varies for two independent reasons. The **model** samples tokens,
so it can act differently on identical input. The **world** drifts, because
data changes and APIs move.

Today both vary at once, so when you tweak a prompt and the run comes out
different, you cannot tell which caused it. Testing an agent currently means
rerunning it against production for real money, triggering real side effects,
against a world that no longer looks the way it did when the interesting run
happened.

Cassette pins the world. That leaves the model as the only variable, which
turns "did my change help?" into a controlled comparison. And because replay
is free, you can run the same tape ten times and get a distribution rather
than one noisy sample.

The analogy: agent testing in 2026 is roughly where HTTP testing was before
`vcr` and `nock` made cassettes ordinary.

---

## How it works

Cassette sits on the MCP wire between an agent and its tools.

```
record:   agent ──► cassette ──► real MCP servers
replay:   agent ──► cassette ──► the tape          (nothing else runs)
```

In replay there is no server process at all. Nothing leaves the machine,
nothing costs money, and no side effect can occur.

---

## Quick start

Install the shim in your agent's MCP config **once**:

```json
{
  "mcpServers": {
    "github": {
      "command": "cassette",
      "args": ["wrap", "--", "npx", "-y", "@modelcontextprotocol/server-github"]
    }
  }
}
```

With nothing else set it forwards every frame untouched and records nothing,
so leaving it installed costs you exactly nothing during ordinary work.

Then drive everything from the outside:

```sh
# capture a real session
cassette record fix-auth -- claude -p "fix the failing auth test"

# see what it did
cassette inspect fix-auth

# change your prompt, then re-run it against the recording
cassette replay fix-auth -- claude -p "fix the failing auth test, be careful"

# run the whole suite in CI
cassette test --suite ./cassettes --hermetic
```

`cassette inspect` prints the merged trajectory across every server involved:

```
      at        message                                      took
      0s     0  initialize                              600.439ms
   152ms     1  notifications/initialized                       -  notify
   302ms     2  tools/list                              298.689ms
   452ms     3  tools/call(echo)                        148.908ms
   597ms     4  notifications/tools/list_changed                -  unprompted
   602ms     5  tools/call(add)                             425µs
```

---

## Three verdicts, not two

Every replayed case gets one of:

| | meaning |
|---|---|
| **identical** | the same calls in the same order |
| **drift** | the same write calls, reached by a different path |
| **CHANGED** | the write calls differ; the agent did something else |

**Drift is the one nobody has.** It is where a change made the agent take nine
extra steps to reach the same place: a real regression in cost and latency
that looks like a pass. Folding it into "different" buries the most useful
signal there is.

The outcome is defined as the sequence of *write-class* calls, because those
are the agent's actual effect on the world. Reads are path, not outcome.

---

## Measured

Every number here is reproducible with a make target.

**A suite of 12 cassettes, 4 cores** (`make bench-suite`):

| | wall clock | vs live |
|---|---|---|
| live sessions | 11.20 s | 1x |
| replay, serial | 0.25 s | 45x |
| replay, parallel | 0.08 s | **147x** |

*Stated honestly: that is the cost of the **environment**, not of a whole
agent. A real agent replaying a tape still pays for model inference every
turn. What replay removes is the environment's cost and every side effect.*

**The tape format**, against the obvious alternative of JSON lines:

| | CAS1 | JSONL |
|---|---|---|
| load | 6.9 µs | 3.58 ms |
| load allocations | 5.5 KB | 4.9 MB |
| lookup | 6.8 ns | 8.6 ns |

Lookup is a wash. The win is load time and shared memory, which is what suite
scale is made of.

**The wire layer**, zero allocations per message in steady state:

```
ReadMessage/256      21.5 ns/op    0 allocs/op
WriteMessage/256     10.1 ns/op    0 allocs/op
ParseEnvelope/512KB   425 µs/op   72 B/op      (flat across a 512x range)
ReplaceID/8KB         820 ns/op    1 alloc/op
```

---

## Safety

When the tape cannot answer a call, what happens depends on the tool, not on a
global setting.

A **read-only** tool may be run for real, which keeps a replay moving when the
agent greps for a slightly different string. **Anything else is refused**,
because letting an unmatched `create_pull_request` through opens a real pull
request, and a free experiment silently becomes a production action.

Tools are read-only if listed in `cassette.json` or if their name follows a
conventional read pattern. Everything else is a write. The name heuristic can
only ever prove a tool is safe, never that one is dangerous, and an explicit
listing always wins.

`--hermetic` disables fall-through entirely, so nothing can leave the process.
That is the setting for CI: a replay that quietly talked to the network is not
the experiment anyone thought they were running.

---

## Analytics

```sh
cassette export --clickhouse ./analytics
cd analytics && ./load.sh
clickhouse local --path ./db --queries-file queries.sql
```

The query the schema exists for:

```
┌─tool─────┬─in_failing─┬─in_passing─┐
│ printEnv │          3 │          0 │
└──────────┴────────────┴────────────┘
```

Every run that changed called `printEnv`; no passing run ever did. Not
"something broke" but "runs that broke all did this, and runs that passed
never did".

Cassette exports to ClickHouse rather than embedding it, so the binary stays
pure Go and the same export works against `clickhouse-local`, a self-hosted
cluster, or ClickHouse Cloud. See
[ARCHITECTURE.md](./ARCHITECTURE.md#12-analytics) for why the ordering key,
the out-of-line payloads and the quantile-state rollup are shaped the way they
are.

---

## Design notes

- **No dependencies.** This binary sits on the wire where every credential and
  payload passes through. CI fails if `go.sum` ever becomes non-empty.
- **Local-first, not a service.** Cassettes contain production payloads. There
  is no version of this that involves uploading them somewhere.
- **Cassettes belong in git.** They are test fixtures. They show up in PR
  diffs, and CI needs no infrastructure to run them.
- **Two storage systems on purpose.** Replay is a point lookup against a
  memory-mapped file measured in nanoseconds; cross-run analytics is
  ClickHouse. Opposite access patterns, one engine each.
- **The proxy must be undetectable.** Verified against a real MCP server, not
  a fake, because a fake only proves our own test double round-trips.

Full reasoning in [ARCHITECTURE.md](./ARCHITECTURE.md).

---

## Status

**7 of 9 phases complete.** Recording, replay, diffing, the parallel suite
runner and ClickHouse export all work and are verified end to end. The web UI
(`cassette serve`) and the public demo (`export --static`) are not built yet.

See [PROGRESS.md](./PROGRESS.md) for the detail, including known limitations
and the bugs worth keeping.

---

## Build

```sh
make build     # ./bin/cassette
make check     # fmt, vet, tests, race
make verify    # every end-to-end verification
make dist      # cross-compiled release binaries
```

Requires Go 1.27. Nothing else.

---

## Documentation

| | |
|---|---|
| [ARCHITECTURE.md](./ARCHITECTURE.md) | How it works and why it is built this way |
| [GOALS.md](./GOALS.md) | What it is for, what success means, explicit non-goals |
| [PROGRESS.md](./PROGRESS.md) | What is built, what is left, known limitations |
| [CASSETTE_PLAN.md](./CASSETTE_PLAN.md) | The working plan and session context |

---

## License

MIT
