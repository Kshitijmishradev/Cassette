# Cassette

**Record and replay MCP tool calls, so a change to your agent can be measured instead of guessed at.**

> Status: early. The command surface is in place; the proxy itself lands in
> phase 1. See [CASSETTE_PLAN.md](./CASSETTE_PLAN.md) for the full design and
> the phase plan.

## The problem

An agent run varies for two independent reasons:

- **the model** samples tokens, so it can act differently each time
- **the world** drifts, because data changes and APIs move

Today both vary at once. So when you tweak a prompt and the run comes out
different, you cannot tell whether your change did that or whether it was
noise. Testing an agent currently means rerunning it against production for
real money, triggering real side effects, against a world that no longer looks
the way it did when the interesting run happened.

## The idea

Cassette sits on the MCP wire between an agent and its tools, records every
call and response, and can serve them back later.

```
agent ──► cassette ──► real MCP servers     (record)
agent ──► cassette ──► tape                 (replay, nothing external is touched)
```

Freezing the world leaves the model as the only thing that varies. That turns
"did my prompt change help?" from a vibe into a controlled comparison. And
because replay touches nothing external, it is fast and free, so you can run
the same tape ten times and get a distribution rather than one noisy sample.

The analogy: agent testing in 2026 is roughly where HTTP testing was before
vcr and nock made cassettes ordinary.

## How it will be used

Install the shim in your agent's MCP config once:

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

Then drive everything from the outside:

```sh
cassette record fix-auth -- claude -p "fix the failing auth test"
cassette replay fix-auth -- claude -p "fix the failing auth test"
cassette test --suite ./cassettes
```

With nothing set, the shim forwards every frame untouched and records nothing,
so leaving it installed costs you exactly nothing during ordinary work.

## Design notes

- **No dependencies.** This binary sits on the wire where every credential and
  payload passes through. The supply chain surface stays at zero.
- **Local-first, not a service.** Cassettes contain production payloads. There
  is no version of this that involves uploading them somewhere.
- **Cassettes belong in git.** They are test fixtures. They show up in PR
  diffs, and CI needs no infrastructure to run them.
- **Two storage systems on purpose.** Replay is a point lookup against a
  memory-mapped file measured in nanoseconds. Cross-run analytics is embedded
  ClickHouse. Opposite access patterns, so one engine for each.

## Build

```sh
make build     # ./bin/cassette
make check     # fmt, vet, tests
make dist      # cross-compiled release binaries
```

Requires Go 1.27. Nothing else.

## License

MIT
