#!/usr/bin/env bash
# Verify that running a real MCP server through `cassette wrap` produces a
# byte-identical stream to running it directly.
#
# This is the exit criterion for phase 1 and it is checked against a real
# server rather than a fake one, because a fake proves only that our own test
# double round-trips. The claim being made is about servers we did not write.
#
#   ./scripts/verify-transparency.sh
#
# Requires node/npx and network access to the npm registry.
set -euo pipefail

cd "$(dirname "$0")/.."

BIN="${CASSETTE_BIN:-./bin/cassette}"
SERVER=${CASSETTE_TEST_SERVER_CMD:-"npx -y @modelcontextprotocol/server-everything stdio"}
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

if [ ! -x "$BIN" ]; then
  echo "building $BIN"
  make build >/dev/null
fi

echo "server: $SERVER"
echo
echo "direct:"
python3 scripts/mcp_drive.py "$WORK/direct.ndjson" $SERVER

echo "wrapped:"
python3 scripts/mcp_drive.py "$WORK/wrapped.ndjson" "$BIN" wrap -- $SERVER

echo
# Only the response stream is compared. Unprompted notifications from this
# server arrive on a timer, so two live runs do not agree with each other and
# comparing them would test the server's jitter rather than the proxy.
if cmp -s "$WORK/direct.ndjson" "$WORK/wrapped.ndjson"; then
  echo "PASS  response streams are byte-identical"
  echo "      $(wc -c < "$WORK/direct.ndjson") bytes, $(wc -l < "$WORK/direct.ndjson") responses"
  echo "      sha256 $(sha256sum "$WORK/direct.ndjson" | cut -d' ' -f1)"
  echo "      unprompted notifications: direct $(wc -l < "$WORK/direct.ndjson.notify"), wrapped $(wc -l < "$WORK/wrapped.ndjson.notify") (not compared, timer-driven)"
  exit 0
fi

echo "FAIL  the proxy altered the stream"
cmp "$WORK/direct.ndjson" "$WORK/wrapped.ndjson" || true
diff <(fold -w200 "$WORK/direct.ndjson") <(fold -w200 "$WORK/wrapped.ndjson") | head -40 || true
exit 1
