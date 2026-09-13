#!/usr/bin/env bash
# Verify that a replayed session is byte-identical to the live one it came
# from, and that replay touches nothing external.
#
# This is the exit criterion for phase 3. The claim is not "replay works",
# it is "the agent cannot tell", so the check compares the exact bytes the
# agent received in both cases.
#
#   ./scripts/verify-replay.sh
set -euo pipefail
cd "$(dirname "$0")/.."

BIN="${CASSETTE_BIN:-./bin/cassette}"
SERVER=${CASSETTE_TEST_SERVER_CMD:-"npx -y @modelcontextprotocol/server-everything stdio"}
WORK=$(mktemp -d)
SUITE="$WORK/cassettes"
trap 'rm -rf "$WORK"' EXIT

[ -x "$BIN" ] || make build >/dev/null

echo "1. record a live session"
"$BIN" record verify --suite "$SUITE" -- \
  python3 scripts/mcp_drive.py "$WORK/live.ndjson" \
  "$BIN" wrap -- $SERVER

echo
echo "2. replay it hermetically, with every proxy variable stripped"
env -u HTTPS_PROXY -u https_proxy -u HTTP_PROXY -u http_proxy \
    -u ALL_PROXY -u all_proxy -u npm_config_cache \
  "$BIN" replay verify --suite "$SUITE" --hermetic -- \
  python3 scripts/mcp_drive.py "$WORK/replayed.ndjson" \
  "$BIN" wrap -- $SERVER

echo
# Only the response stream is compared, and that is a measured decision, not
# a convenience. Three live runs of this server produced three different byte
# streams because it emits tools/list_changed on a timer. Responses are the
# deterministic part: each answers a specific request, correlated by id.
if cmp -s "$WORK/live.ndjson" "$WORK/replayed.ndjson"; then
  echo "PASS  every response replayed byte-identically to the live session"
  echo "      $(wc -c < "$WORK/live.ndjson") bytes, $(wc -l < "$WORK/live.ndjson") responses"
  echo "      sha256 $(sha256sum "$WORK/live.ndjson" | cut -d' ' -f1)"
  echo "      unprompted notifications: live $(wc -l < "$WORK/live.ndjson.notify"), replay $(wc -l < "$WORK/replayed.ndjson.notify") (not compared, timer-driven)"
  exit 0
fi

echo "FAIL  replay diverged from the live session"
cmp "$WORK/live.ndjson" "$WORK/replayed.ndjson" || true
diff <(fold -w160 "$WORK/live.ndjson") <(fold -w160 "$WORK/replayed.ndjson") | head -40 || true
exit 1
