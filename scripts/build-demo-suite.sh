#!/usr/bin/env bash
# Record a suite of cassettes against a real MCP server.
#
# Used to exercise the parallel suite runner and to produce the numbers in
# the README. Each case is an identical session, which is fine for measuring
# throughput: what is being timed is the harness, not the agent.
#
#   ./scripts/build-demo-suite.sh [count] [suite-dir]
set -euo pipefail
cd "$(dirname "$0")/.."

COUNT=${1:-12}
SUITE=${2:-/tmp/cassette-demo-suite}
BIN="${CASSETTE_BIN:-./bin/cassette}"
SERVER=${CASSETTE_TEST_SERVER_CMD:-"npx -y @modelcontextprotocol/server-everything stdio"}

[ -x "$BIN" ] || make build >/dev/null
rm -rf "$SUITE"

echo "recording $COUNT cassettes into $SUITE"
for i in $(seq 1 "$COUNT"); do
  name=$(printf "case-%02d" "$i")
  "$BIN" record "$name" --suite "$SUITE" -- \
    python3 scripts/mcp_drive.py /dev/null \
    "$BIN" wrap -- $SERVER >/dev/null 2>&1
  printf "."
done
echo
echo "done: $(ls "$SUITE" | wc -l) cassettes, $(du -sh "$SUITE" | cut -f1)"
