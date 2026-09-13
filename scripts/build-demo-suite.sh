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
# EXTRA_EVERY makes every Nth cassette exercise an additional tool, so the
# suite is not uniform. A suite where every case is identical cannot
# demonstrate any query that compares tool usage across runs.
EXTRA_TOOL=${EXTRA_TOOL:-printEnv}
EXTRA_EVERY=${EXTRA_EVERY:-0}

for i in $(seq 1 "$COUNT"); do
  name=$(printf "case-%02d" "$i")
  extra=""
  if [ "$EXTRA_EVERY" -gt 0 ] && [ $((i % EXTRA_EVERY)) -eq 0 ]; then
    extra="$EXTRA_TOOL"
  fi
  CASSETTE_DRIVE_EXTRA="$extra" "$BIN" record "$name" --suite "$SUITE" -- \
    python3 scripts/mcp_drive.py /dev/null \
    "$BIN" wrap -- $SERVER >/dev/null 2>&1
  printf "."
done
echo
echo "done: $(ls "$SUITE" | wc -l) cassettes, $(du -sh "$SUITE" | cut -f1)"
