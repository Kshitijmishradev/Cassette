# Architecture

How Cassette works, and why it is built this way.

For the phase-by-phase build log see [PROGRESS.md](./PROGRESS.md). For what the
project is trying to achieve see [GOALS.md](./GOALS.md).

---

## 1. The problem this shape solves

An agent run varies for two independent reasons:

- **the model** samples tokens, so it can act differently on identical input
- **the world** drifts, because data changes, APIs move, and clocks advance

Today both vary at once. When you change a prompt and the run comes out
different, the difference is uninterpretable: you cannot attribute it.

Cassette pins the world. That leaves the model as the only thing that varies,
which turns "did my change help?" from a vibe into a controlled comparison.
And because a pinned world is free to re-enter, you can run the same tape ten
times and get a distribution instead of one noisy sample.

This is not a hypothetical problem. While building the verification harness,
three identical live runs against the same MCP server produced three
different byte streams, because that server emits a notification on a timer.
The premise turned up inside our own test suite.

---

## 2. Where it sits

MCP is the chokepoint. An agent does not call Stripe or the filesystem
directly; it emits a structured request and something hands back a structured
response. Every tool, every server, one uniform protocol.

```
                    ┌──────────────────────────────┐
   agent process    │  cassette wrap               │    real MCP servers
   (Claude Code) ──►│  stdio JSON-RPC proxy        │──► (filesystem, github,
                    │                              │     postgres, ...)
                    │  off:     forward untouched  │
                    │  record:  forward and tee    │
                    └──────────────────────────────┘
```

In **replay** there is no server at all:

```
   agent process    ┌──────────────────────────────┐
   (Claude Code) ──►│  cassette wrap (replay)      │──► the tape
                    │                              │
                    │  match → patch id → write    │    (a live server is
                    └──────────────────────────────┘     spawned only for a
                                                         read-class miss)
```

That absence is the whole point. Nothing leaves the machine, nothing costs
money, and no side effect can occur.

**Consequence worth stating:** because replay has no server, everything the
agent needs to open a session must be on the tape. A tape holding only
`tools/call` cannot boot, because an agent starts with `initialize`, then
`notifications/initialized`, then `tools/list`. So everything is recorded.

---

## 3. Control flow

The shim is never launched by us. It is launched by the agent, out of the
agent's own MCP configuration, which is static and edited once:

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

We cannot rewrite that file on every run to flip record into replay, and we
should not want to. So mode travels by environment instead:

```
cassette record foo -- claude ...
        │
        │  sets CASSETTE_MODE=record, CASSETTE_TAPE=<dir>, CASSETTE_RUN_ID
        ▼
   agent process
        │  inherits the environment
        ▼
   cassette wrap ──► reads its mode from the environment it woke up in
```

One static config line supports recording, replay, and passthrough forever.

**Unset means off**, and that default is load-bearing. A shim left sitting in
someone's config has to behave exactly like no shim at all during ordinary
work, or nobody will leave it installed. An invalid value fails closed to
`off` for the same reason: a typo degrades to passthrough, not to something
else.

---

## 4. Packages

| package | owns |
|---|---|
| `internal/jsonrpc` | Newline-delimited framing, envelope-only parsing, exact id splicing |
| `internal/proxy` | Bidirectional pump, stderr passthrough, shutdown ladder |
| `internal/tape` | The CAS1 on-disk format: writer, mmap reader, content hashes |
| `internal/record` | Recorder observer, per-server tape naming, run manifest, trajectory merge |
| `internal/match` | The matching ladder and its tier reporting |
| `internal/safety` | Read/write classification, failing closed |
| `internal/replay` | Serving an agent from a tape, lazy live fall-through |
| `internal/diff` | Trajectory alignment, verdict, side-by-side rendering |
| `internal/suite` | Discovery and the parallel case runner |
| `internal/chexport` | ClickHouse schema, queries, JSONEachRow export |
| `internal/cli` | Dependency-free command router |
| `internal/config` | `cassette.json` |

No third-party Go dependencies. CI fails if `go.sum` ever becomes non-empty.
That is not minimalism for its own sake: this binary sits on the wire where
every credential and payload passes through, so the supply-chain surface
stays at zero, and cross-compilation and reproducible builds stay trivial.

---

## 5. The wire layer

MCP stdio is **newline-delimited JSON**, one message per line, with embedded
newlines forbidden by the spec. (The plan originally said Content-Length
framing. That is LSP's framing, and checking the spec before building on it
saved a whole layer from being wrong.)

Splitting on `\n` is therefore correct rather than merely convenient.

### Borrowed bytes

`ReadMessage` returns a slice valid only until the next call. This is the
central decision in the package. The proxy's common case is read, inspect,
forward, and tool results are routinely hundreds of kilobytes, so copying on
every hop would be the dominant cost of the whole tool. Callers that need to
retain a message copy it themselves, which puts that cost at the call site
instead of hiding it.

```
ReadMessage/256      21.5 ns/op    0 allocs/op
ReadMessage/524288  27.9 µs/op     0 allocs/op
WriteMessage/256     10.1 ns/op    0 allocs/op
```

### Envelope-only parsing

The `Envelope` struct has no `Result` and no `Params` field, and that absence
is the design. Cassette never needs to understand a tool response in order to
move it, since it goes to the agent unchanged.

Stage one parses a struct that omits `params` and `result` entirely.
`encoding/json` walks fields with no counterpart in the target struct and
discards them without building anything, so a 512 KB result is validated but
never materialized:

```
ParseEnvelopeLargeResult/1024     72 B/op   2 allocs/op
ParseEnvelopeLargeResult/524288   72 B/op   2 allocs/op
```

Flat across a 512x payload range. Stage two runs only for `tools/call`, which
are small requests. Paying twice on the small half avoids paying once on the
large half.

### Transparency invariants

An agent running through the proxy must be unable to tell. That is not a
quality goal, it is the precondition for everything else: if the proxy
perturbs behavior, every recording describes an agent that does not exist.

- Messages forward as the **exact bytes** that arrived, never reserialized.
  Key order, spacing and duplicate keys are all observable to a peer.
- **stderr is handed to the child directly**, so the runtime passes the file
  descriptor through. The spec says stderr is free-form and clients must not
  read it as an error channel, so it is not ours to interpret.
- **Unparseable messages are forwarded anyway**, classified as unknown. A
  proxy that drops what it does not understand breaks agents on protocol
  revisions it has never seen.
- **Observers are wrapped in a recover.** A recording bug must never take
  down the agent's tooling.
- **Forwarding happens before observation**, always, so a tap can never delay
  or suppress a message.

Verified against a real MCP server, not a fake: `make verify-transparency`
drives a full lifecycle direct and wrapped and compares the response streams
byte for byte.

### Shutdown

The spec's ladder is: close the child's stdin, wait, SIGTERM, wait, SIGKILL.
Having interposed ourselves, we owe the child the same discipline the agent
would have applied.

Escalation runs on its **own clock**, deliberately not gated on the output
drain. An earlier version reaped the child only after both pumps finished,
which deadlocked against exactly the case escalation exists for: a server
ignoring stdin close never closes its stdout, so the drain never completes
and the SIGTERM never fires. A rescue timer cannot depend on the thing it is
rescuing.

The drain still completes before `cmd.Wait`, because Wait closes the stdout
pipe and would truncate the server's final messages.

---

## 6. The CAS1 tape format

Laid out like a small SSTable: a fixed-width index castable straight out of a
memory mapping, and a blob section that is never parsed.

```
┌────────────────────────────────────────────────┐
│ header    64 B, fixed, carries section offsets │
├────────────────────────────────────────────────┤
│ index     n × 72 B, fixed width                │
├────────────────────────────────────────────────┤
│ strings   method and tool names, interned      │
├────────────────────────────────────────────────┤
│ vectors   n × dim × float32, optional          │
├────────────────────────────────────────────────┤
│ blobs     pre-framed message bytes             │
└────────────────────────────────────────────────┘
```

**Blobs are stored pre-framed**, meaning the exact wire bytes including the
trailing newline. Replay patches the id and writes the slice, with no
serialization step at all.

**Entry field order is neither alphabetical nor logical.** Every 8-byte field
sits at an 8-byte offset, which is what makes the reader's unsafe cast legal.
Reordering for readability would fault on strict-alignment platforms.
`EntrySize` is a constant with a test asserting it matches the struct, because
if a field is added and the constant is not bumped, every tape written
afterwards is silently unreadable.

**Offsets are uint32**, capping a tape at 4 GiB. A single run producing that
much tool traffic is a bug to fail on, not a case to spend eight more bytes
per entry supporting.

**Names are interned.** A run makes thousands of calls across a handful of
distinct names. Measured: 5,000 entries across 7 distinct tool names produce a
61-byte string table.

**All offsets are validated once at load**, against the real file size, before
anything is reached through the unsafe cast. A truncated or corrupt tape must
fail to open rather than hand out slices past the end of the mapping.

### Borrowed versus owned, learned the hard way

`Request` and `Response` return slices into the mapping. `Method` and
`ToolName` return **copies**.

The distinction was learned by segfaulting. An earlier version returned
borrowed strings for names too; building a trajectory from a tape and closing
the reader before rendering crashed in `strings.TrimSpace`, six frames from
anything that looked related. The borrowed contract is right for blobs, where
it saves megabytes per replay. For an interned name it saved roughly twenty
nanoseconds and bought a use-after-free.

### Why a custom format at all

Measured against the obvious alternative, a file of JSON lines, on 200 calls
with 8 KB responses:

| | CAS1 | JSONL |
|---|---|---|
| load time | 6.9 µs | 3.58 ms |
| load allocations | 5.5 KB, 9 | 4.9 MB, 1,013 |
| lookup | 6.8 ns, 0 allocs | 8.6 ns, 0 allocs |
| heap after load | ~0 (page cache) | 1.96 MB |
| at 50 parallel workers | one shared 1.6 MB mapping | ~93 MB heap, nothing shared |

**Lookup is a wash.** Once a map is built, a map is a map. CAS1 wins on load,
520x in time and 900x in memory, because it decodes nothing to become usable.
That is what suite scale is made of: replays are independent processes, so
only the mapped form is shared by the kernel.

Which is where the design started. Reads were never the bottleneck, so the
format only had to avoid allocating.

---

## 7. Recording

The recorder is a `proxy.Observer`, so it sits inline on the forwarding path
of every message. That placement dictates its shape: whatever it does per
message is added to the latency of every tool call the agent makes.

So it does almost nothing inline. Copy the borrowed bytes, correlate a
response to its request by raw id bytes, hand the result to a writer
goroutine. All file I/O happens off the forwarding path.

When the queue fills, `OnMessage` **blocks rather than dropping**. A tape with
silent holes would make every replay built on it wrong in a way nothing
downstream could detect, which is far worse than a few microseconds of
backpressure.

### Several servers, one run

Each server writes its own tape. They are separate processes, and making them
share one file would mean a lock on the recording path. The manifest is
assembled afterwards by scanning the directory, which needs no coordination at
all.

Tape names are derived from the server command: a readable slug plus a short
hash of the full argv. The slug is for whoever reads the directory listing;
the hash keeps two servers differing only in arguments off the same file.

The slug scan goes **forward**, not backward. Taking the last non-flag
argument named a real recording `stdio`, because arguments trail the thing
they configure: `npx -y @modelcontextprotocol/server-everything stdio`.

### Ordering across tapes

Separate processes share no sequence number, and introducing one means
cross-process coordination on exactly the path that must stay fast. So
ordering comes from wall-clock timestamps taken as each message is seen: same
machine, same clock, nanosecond resolution. Ties break deterministically,
because this ordering feeds a diff that CI compares.

**The tape is in completion order, not request order.** The recorder writes an
entry when the *response* arrives, because that is the first moment the
exchange is complete, so a fast call started second lands ahead of a slow call
started first. Anything comparing a tape against a live trajectory must sort
by `StartedNanos`. Not doing so made an unchanged re-run report "outcome
changed".

---

## 8. Matching

Agents do not ask the same questions twice. A prompt change makes the agent
grep for "authentication token" where it once grepped "auth token". Exact
matching misses all of those, and every miss is either a fall-through to the
real world or a dead replay.

So matching is a ladder, and **every answer reports its tier**. A run served
80% exact and 20% loose is a weaker result than one served exactly, and the
user deserves to see which they got.

```
exact       method + tool + exact argument bytes
normalized  method + tool + sorted-key arguments (numeric precision preserved)
method      same method, arguments ignored (3 protocol methods only)
fuzzy       reserved, not yet live
miss        policy depends on the tool's class
```

Hashes are **FNV-1a, not `hash/maphash`**, because they go on disk.
`maphash` is seeded per process, so a tape written today would not match
itself when read tomorrow.

### The method-only tier

This exists because of a failure a real replay caught, not because it seemed
like a good idea. An agent's `initialize` params carry its own name, version
and capabilities, which differ between clients and across versions of one
client. Hashing them meant replay refused the very first message and the agent
could not start at all.

It is restricted to an explicit three-method set (`initialize`,
`server/discover`, `ping`) rather than used as a general fallback, because it
is a decision to call two different requests the same. It holds where the
response describes the server rather than the request. It does **not** hold
for `tools/call`, and it does not hold for `tools/list` either, which takes a
pagination cursor. Both have tests saying so.

### Repeats

A hash maps to a list, not one entry, because the same call can return
different things. Reading a file before and after editing it is the obvious
case, and collapsing those would serve pre-edit contents to a post-edit read.

When an agent loops one more time than the recording did, the last recording
is reused and flagged as a repeat rather than failing. A small behavioral
difference should not become a dead replay.

---

## 9. Safety: the boundary between a test and an accident

When the tape cannot answer a call, there are two options: refuse, or let it
through to a live server.

Letting it through is often right. The agent grepped for a slightly different
string; running that for real costs nothing and keeps the replay moving.

Letting it through is sometimes catastrophic. The same fall-through applied to
`create_pull_request` opens a real pull request. Applied to a refund tool it
refunds a real customer. A free experiment silently becomes a production
action.

**So anything not known to be read-only is a write.** Being wrong in that
direction stops a replay and asks a human. Being wrong the other way has no
upper bound on the damage.

```
explicit write listing   →  write     (always wins)
explicit read listing    →  read
protocol method          →  read      (describes the server, changes nothing)
name heuristic           →  read      (only if it recognizes the name)
anything else            →  write
```

Tool-name heuristics are disabled by default. A name cannot prove that a tool
is safe. Explicit read allowlists are case-sensitive because MCP tool names
are case-sensitive; explicit write overrides remain conservative.

Live MCP fall-through is disabled by default. To opt into exploratory live
reads, configure `replay.fallThrough: true` and list reviewed `tools.read`
names. `--hermetic` passes `CASSETTE_HERMETIC=1` to every shim and overrides
configured fall-through permission without writing a policy file into a suite.

This boundary covers wrapped MCP traffic, not the agent process. Native tools,
shell commands, and direct network calls remain outside Cassette. Manifests
contain executable commands and must be trusted. See [SECURITY.md](./SECURITY.md).

### Id splicing

Agents do not reuse JSON-RPC ids across runs. The recorded response says id 7;
the live request it answers might be id 42. So the top-level id is spliced,
leaving every other byte untouched:

```
ReplaceID/8KB   820 ns/op   1 alloc/op
```

Two alternatives were rejected. Normalizing ids at record time would allow an
in-place patch with no allocation, but the tape would stop holding the literal
bytes the server sent, which is the strongest claim this project makes.
Storing an id offset per entry avoids most copies but adds a field to every
index entry to save ~200 ns against a model turn measured in seconds.

The scan is hand-written rather than decode-and-re-encode, because
`encoding/json` would reorder keys. **Only depth 1 is considered**: tool
payloads routinely carry their own `id` fields, and rewriting one would
corrupt the data the agent receives.

---

## 10. The trajectory diff

Two trajectories, aligned with Needleman-Wunsch, the same dynamic program used
for sequence alignment in bioinformatics, and for the same reasons: the
sequences differ in length, order matters, and the question is which elements
correspond rather than whether they differ. A set comparison could say "run B
grepped three more times" without saying where, and where is the point.

### Costs

```
match (same tool, same args)        0
same tool, different args           1
insert or delete                    2
different tool                      3
```

Ordinal, and **the ordering carries the meaning**. Same tool with different
arguments is the cheapest difference, because an agent grepping a slightly
different string is doing the same thing. If cross-tool substitution were
cheaper than an indel, the alignment would pair unrelated calls rather than
admit one run did something the other did not, and the diff would read as
garbled swaps instead of a clear insertion. There is a test pinning the
ordering.

O(n·m) in time and space, which for agent runs is nothing. Hirschberg gives
the same alignment in linear space, but that is complexity spent on a
dimension nothing is near.

### The verdict

Three-valued, not two:

- **identical** — the same calls in the same order
- **drift** — the same write calls, reached by a different path
- **CHANGED** — the write calls differ; the agent did something else

Two values would fold drift into "different" and bury the most useful signal
there is: a change that cost nine extra steps without changing the result.
That is a regression nobody currently catches, because the result looks fine.

**The outcome is the sequence of write-class calls**, reusing the safety
classifier. The alternative was the agent's final prose answer, which is
closer to what a user cares about but needs the agent's own output rather than
its tool traffic, and is itself model-generated and therefore noisy. Write
calls are the agent's actual effect on the world.

Writes are aligned **separately**, not filtered out of the full alignment. In
the full alignment a write can end up paired against a read that happened to
sit at the same index, which says nothing about whether the two runs did the
same thing.

---

## 11. Suite parallelism

Replay touches nothing external: no network, no shared database, no
rate-limited API, no file the cases contend over. Cases are embarrassingly
parallel in the strict sense.

12 cassettes on 4 cores:

| | wall clock | vs live |
|---|---|---|
| live sessions | 11.20 s | 1x |
| replay, serial | 0.25 s | 45x |
| replay, parallel | 0.08 s | **147x** |

Parallel over serial is 3.3x on 4 cores, close to the ceiling.

**State this carefully.** The 147x is the cost of the *environment*, not of a
whole agent. A real agent replaying a tape still pays for model inference
every turn. The narrow, true claim: replay removes the environment's cost
almost entirely and removes every side effect. What remains is model time,
which is also where parallelism helps most.

A directory counts as a case only if it holds a manifest. A directory with
tapes and no manifest is a recording that never finished, and running it would
compare against a partial baseline while looking like a normal pass.

---

## 12. Analytics

Two storage systems on purpose, because the access patterns are opposite:

| | tape (CAS1) | ClickHouse |
|---|---|---|
| access pattern | point lookup by key | scan over millions of rows |
| scope | one run | every run ever |
| latency budget | microseconds | ~1 second |
| location | local file, page cache | wherever you put it |
| answers | "what did this exact call return" | "what do failing runs do that passing ones do not" |

Cassette **exports** to ClickHouse rather than embedding it. Embedding costs
cgo, and with it the single static binary, the clean cross-compile matrix and
reproducible builds, to serve a query path that runs weekly and is not
latency-sensitive. The export works unchanged against `clickhouse-local`, a
self-hosted cluster, or ClickHouse Cloud.

The schema is where the thinking lives:

**`ORDER BY (cassette, tool, started_at)`** is the primary index, and
ClickHouse's primary index is sparse: it marks granules, not rows. Every
question narrows to a cassette, then a tool, then ranges over time, so this
ordering turns them into granule skips. Ordering by `started_at` first, which
looks natural for time-series data, would force every per-tool question to
read everything.

**Payloads live in their own table** keyed by `span_id`. The aggregate queries
touch a handful of numeric and low-cardinality columns, and a 40 KB body in
the same row would be dragged off disk by all of them. That split is what
makes 100% retention affordable rather than a sampling decision.

**`LowCardinality`** on the name columns is not decoration: a thousand runs
produce millions of rows drawn from perhaps thirty distinct tool names.

**The rollup stores quantile states**, not finished numbers, because quantiles
do not sum. A stored daily p95 could never answer a weekly p95; merging states
can. `calls` and `errors` are `SimpleAggregateFunction(sum, UInt64)`, not
plain `UInt64` — in an `AggregatingMergeTree`, rows sharing a sorting key are
collapsed during background merges and a plain column keeps an arbitrary one
of the collapsed values. The table would look correct after insert and start
returning wrong counts after a merge. ClickHouse refuses the DDL outright,
which is how this was found.

Verified against ClickHouse 26.8.3, including that the rollup's merged p95
matches the raw scan exactly. A materialized view that silently diverges from
its source makes every dashboard built on it lie.

---

## 13. Cross-cutting invariants

Things that hold everywhere, and the reasoning behind each.

**Fail open on the wire, fail closed on safety.** The proxy forwards what it
cannot parse, because breaking an agent is worse than failing to classify a
message. The classifier refuses what it cannot vouch for, because performing
an unintended write is worse than stopping. Opposite defaults, same principle:
be wrong in the direction that costs less.

**Observation never blocks the wire.** Forwarding precedes observation, and
observers are panic-contained.

**Borrowed where the megabytes are, owned everywhere else.** Payload slices
are borrowed from the mapping; names are copied. The rule is not "zero copy
everywhere", it is "zero copy where copying would dominate".

**Determinism wherever output feeds a comparison.** Tie-breaks in alignment,
trajectory ordering, suite row order, span ids. Anything a human diffs or CI
compares must not vary between runs on identical input.

**Every claim is measured, not asserted.** Every performance number in this
document is reproducible with a make target. The end-to-end verifications run
against real MCP servers and a real ClickHouse, because a fake only proves our
own double round-trips.
