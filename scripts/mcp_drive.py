#!/usr/bin/env python3
"""Drive a real MCP server over stdio and capture its exact stdout bytes.

Used to verify that running a server through `cassette wrap` produces a
byte-identical stream to running it directly.
"""
import subprocess, sys, time, os

MSGS = [
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"cassette-verify","version":"0"}}}',
    '{"jsonrpc":"2.0","method":"notifications/initialized"}',
    '{"jsonrpc":"2.0","id":2,"method":"tools/list"}',
    '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hello from cassette"}}}',
    '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"add","arguments":{"a":2,"b":40}}}',
]
EXPECTED_RESPONSES = 4

out_path = sys.argv[1]
cmd = sys.argv[2:]

p = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                     stderr=subprocess.DEVNULL, env=os.environ)

for m in MSGS:
    p.stdin.write((m + "\n").encode())
    p.stdin.flush()
    time.sleep(0.15)

lines = []
deadline = time.time() + 20
while len(lines) < EXPECTED_RESPONSES and time.time() < deadline:
    line = p.stdout.readline()
    if not line:
        break
    lines.append(line)

p.stdin.close()
try:
    p.wait(timeout=10)
except subprocess.TimeoutExpired:
    p.kill()

with open(out_path, "wb") as f:
    f.write(b"".join(lines))

print(f"captured {len(lines)} messages, {sum(len(l) for l in lines)} bytes, exit={p.returncode}")
