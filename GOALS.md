# Goals

What this project is for, what "done" means, and how we would know it worked.

---

## The thesis

**Agent testing in 2026 is where HTTP testing was before VCR.**

Before `vcr`, `betamax` and `nock`, testing code that talked to an HTTP API
meant either hitting the real API (slow, flaky, rate-limited, occasionally
destructive) or hand-writing mocks that drifted from reality until they tested
nothing. Cassettes made a third option normal: record the real interaction
once, replay it forever, commit it next to the code.

Agents are in the first world today. Every team shipping one has the same
three problems and no standard answer:

1. **You cannot re-run a real failure.** Doing so would re-trigger its side
   effects. Nobody replays the ticket that issued a refund.
2. **You cannot afford to iterate.** 200 real sessions is an hour and a few
   hundred dollars per experiment, so people test on two or three fake cases
   they invented.
3. **The world moved.** Even if you could re-run it, the data changed, so the
   agent sees different facts and you learn nothing about your change.

Cassette's bet is that the same answer works: put a recorder on the
protocol chokepoint, commit the tapes as fixtures, replay them in CI.

---

## What success looks like

### Primary outcome

**A change to an agent produces an answer instead of a guess.**

Concretely: someone edits a system prompt, runs `cassette test`, and gets

```
200 cassettes · 163 identical · 31 drift · 6 CHANGED · 4.2s
```

with a side-by-side diff of the six. That is a decision they can make. Today
the same person ships on Friday and finds out Monday.

### The three verdicts are the product

Most tools would report pass or fail. The three-valued verdict is the thing
that is actually new here:

- **identical** — nothing to look at
- **drift** — same outcome, different path. *This is the one nobody has.* It
  is where a change made the agent take nine extra steps to reach the same
  place: a real regression in cost and latency that looks like a pass
- **CHANGED** — the agent did something else to the world

If this project contributes one idea, it is that "the agent still got it
right" and "the agent got it right the same way" are different questions, and
the second one is measurable.

---

## Measurable targets

| target | status | evidence |
|---|---|---|
| Proxy is undetectable to a real agent | **met** | Response streams byte-identical direct vs wrapped, against a real MCP server |
| Replay reproduces a session exactly | **met** | Every response byte-identical to the live session, hermetically |
| Replay costs near-zero | **met** | 888 ms live → 19 ms replayed, single session |
| A suite replays fast enough for CI | **met** | 12 cassettes: 11.20 s live → 0.08 s parallel |
| No unmatched write ever reaches the world | **met** | Refused even outside hermetic mode; default-deny classification |
| A behavioral change is caught and explained | **met** | 3 altered cassettes detected in a 12-case suite; ClickHouse query names the tool responsible |
| Someone other than the author can run it | **open** | Needs the web UI and a public demo (phases 7 and 8) |
| Someone other than the author adopts it | **open** | Requires all of the above plus real distribution |

---

## Non-goals

Being explicit about these, because each one is a plausible direction that
would make the project worse.

**Not a metrics dashboard.** Coding-agent monitoring is saturated: Dynatrace,
SigNoz and Dash0 all shipped Claude Code OTel dashboards in 2026. Building
time-series charts would make this look like a clone of things that already
exist and are better resourced. The differentiator is replay and diffing, not
observability.

**Not a hosted service.** Cassettes contain production payloads and
credentials. No engineering team is uploading their agent's Stripe responses
to a stranger's cloud. Local-first is not a cost compromise, it is the only
architecture that could ever be adopted. The public demo is a static export
with no backend for exactly this reason.

**Not a mocking framework.** Cassette never invents a response. Everything it
serves was actually returned by a real server at a real moment. A hand-written
mock drifts from reality; a recording cannot, because it *is* reality, dated.

**Not a model evaluation harness.** Cassette says nothing about whether an
answer was good. It says whether the trajectory changed. Those are different
problems, and conflating them is how eval tools become unfalsifiable.

**Not trying to eliminate model nondeterminism.** We pin the world and leave
the model free, on purpose. The model's variance is real and worth measuring;
what made it unmeasurable was the world varying at the same time. Freezing the
environment is what turns the model's randomness into a distribution you can
observe.

---

## Who it is for

**The engineer who owns an agent in production.** They have real sessions,
real side effects, and no way to test a change. They are the reason write-class
calls are refused by default rather than by configuration.

**The CI job.** Which is why exit codes distinguish "behavior changed" from
"the tool broke", why `--hermetic` exists, and why tapes are committed to git
so CI needs no infrastructure at all.

**The person debugging one weird run.** Which is why `inspect` prints a merged
trajectory, why every match reports its tier, and why 100% of payloads are
retained instead of sampled.

---

## Design commitments

These are the rules that shaped the architecture. They are listed here so that
a future change can be checked against them rather than quietly violating one.

1. **The proxy must be undetectable.** If it perturbs behavior, every
   recording describes an agent that does not exist, and everything built on
   those recordings measures the wrong thing.

2. **A recording must never silently lose data.** Backpressure over dropping,
   truncation marked explicitly, tapes finalized atomically. A tape with holes
   makes every replay built on it wrong in a way nothing downstream detects.

3. **An unmatched write never reaches the world.** Default-deny, heuristics
   that can only prove safety, explicit listings that always win.

4. **Every claim is measured.** Every number in the docs is reproducible with
   a make target, and the end-to-end checks run against real MCP servers and a
   real ClickHouse, because a fake only proves our own double round-trips.

5. **Zero dependencies in the core.** This binary sees every credential that
   passes between an agent and its tools. CI fails if `go.sum` becomes
   non-empty.

---

## What would falsify the thesis

Worth writing down, because a project with no failure condition is not a bet.

- **If tapes go stale faster than they are useful.** If agents drift so fast
  that recordings stop matching within days, the maintenance cost swamps the
  benefit. The match-tier percentages are the early warning: a suite sliding
  from 95% exact toward 50% is telling you the tapes no longer describe the
  agent.

- **If drift turns out not to matter.** The three-valued verdict is a bet that
  teams care about path changes and not only outcomes. If everyone runs
  `--fail-on outcome` and ignores drift forever, the interesting third of the
  product is dead weight.

- **If the protocol chokepoint stops being one.** The whole design rests on
  agents reaching tools through MCP. If tool calling fragments across
  incompatible protocols, the single interception point that makes this cheap
  disappears.
