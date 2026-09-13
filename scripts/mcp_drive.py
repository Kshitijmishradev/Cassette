#!/usr/bin/env python3
"""Drive an MCP server over stdio and capture what it sent back.

Output is split into two files on purpose.

  <out>          responses, keyed and ordered by request id
  <out>.notify   messages the server sent unprompted

The split exists because of something measured rather than assumed: running
this against @modelcontextprotocol/server-everything three times produced
three different byte streams, because that server emits
notifications/tools/list_changed on a timer. Two *live* runs do not agree
with each other, so comparing whole streams byte for byte tests the server's
jitter rather than anything about cassette.

Responses are the deterministic part: each is the server's answer to a
specific request, correlated by id. That is what a replay must reproduce
exactly, and that is what the verify scripts compare.
"""
import json
import os
import subprocess
import sys
import time

REQUESTS = [
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"cassette-verify","version":"0"}}}',
    '{"jsonrpc":"2.0","id":2,"method":"tools/list"}',
    '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hello from cassette"}}}',
    '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"add","arguments":{"a":2,"b":40}}}',
    '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"echo","arguments":{"message":"second echo"}}}',
]
NOTIFICATION = '{"jsonrpc":"2.0","method":"notifications/initialized"}'

# CASSETTE_DRIVE_EXTRA adds a further tool call, so a suite can contain
# cassettes that genuinely exercise different tools. Without that, every
# analytical query that compares tool usage across runs has nothing to find.
_extra = os.environ.get("CASSETTE_DRIVE_EXTRA", "")
if _extra:
    REQUESTS.append(
        '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"%s","arguments":{}}}' % _extra
    )

WANT_IDS = set(range(1, len(REQUESTS) + 1))

out_path = sys.argv[1]
cmd = sys.argv[2:]

proc = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                        stderr=subprocess.DEVNULL, env=os.environ)


def send(line):
    proc.stdin.write((line + "\n").encode())
    proc.stdin.flush()


send(REQUESTS[0])
send(NOTIFICATION)
for r in REQUESTS[1:]:
    send(r)

responses = {}
notifications = []
deadline = time.time() + 30

while len(responses) < len(WANT_IDS) and time.time() < deadline:
    line = proc.stdout.readline()
    if not line:
        break
    try:
        msg = json.loads(line)
    except json.JSONDecodeError:
        notifications.append(line)
        continue
    if isinstance(msg, dict) and msg.get("id") is not None:
        responses[msg["id"]] = line
    else:
        notifications.append(line)

proc.stdin.close()
try:
    proc.wait(timeout=10)
except subprocess.TimeoutExpired:
    proc.kill()

with open(out_path, "wb") as f:
    for rid in sorted(responses, key=lambda k: (str(type(k)), k)):
        f.write(responses[rid])

with open(out_path + ".notify", "wb") as f:
    f.writelines(notifications)

missing = sorted(WANT_IDS - set(responses))
print(f"responses {len(responses)}/{len(WANT_IDS)}"
      f"  bytes {sum(len(v) for v in responses.values())}"
      f"  unprompted {len(notifications)}"
      + (f"  MISSING {missing}" if missing else ""))
sys.exit(1 if missing else 0)
