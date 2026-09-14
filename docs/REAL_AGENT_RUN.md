# Real agent verification

Verified 2026-09-14 with Codex 0.153.0 using `gpt-6-astra` and the MCP
Everything reference server.

## Recorded trajectory

The agent opened one MCP session and made these tool calls in order:

1. `echo` — `cassette real agent checkpoint`
2. `get-sum` — `17 + 25`
3. `get-annotated-message` — success message without an image
4. `echo` — `replay ready`

The tape contains 10 MCP messages, including initialization and one
notification. It is 16,617 bytes and took 12.475 seconds to record.

## Hermetic replay result

The replay configuration named a deliberately nonexistent MCP server binary.
All responses still succeeded because Cassette answered from the tape:

```text
tape                          served   exact    norm  method    live refused
server-40a45c                      6       6       0       0       0       0

6 served (100% exact) · 14.995s
server-40a45c: 6 steps vs 6 · identical
```

Codex completed all four calls and returned the addition result `42`. No call
fell through to a real server and no call was refused.

The reference server's annotated-content response still produces an
`Unexpected response type` warning in Codex 0.153.0. That is a client/server
compatibility issue rather than replay drift: the same bytes were recorded
and served exactly, and the agent continued as instructed.

## Bug found and fixed

Codex terminates MCP children after its task rather than reliably closing
their stdin first. Cassette previously wrote `.replay.json` only after a clean
EOF, so a valid trajectory could disappear and the outer command reported
`no shim reported in`.

Replay reports are now atomically checkpointed after every MCP message. A
regression test keeps stdin open after a served call and verifies that the
result is already durable before shutdown.
