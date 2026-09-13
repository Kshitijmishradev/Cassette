#!/usr/bin/env bash
# Load a suite export into a real ClickHouse and run every shipped query.
#
# This is the exit criterion for the analytics phase. A schema that has never
# been executed is a guess, and this one was wrong the first time it ran: the
# rollup used plain columns in an AggregatingMergeTree, which ClickHouse
# refuses precisely because background merges would silently return wrong
# counts.
#
#   ./scripts/verify-clickhouse.sh
#
# Requires a clickhouse binary on PATH. Skips cleanly if there is none.
set -euo pipefail
cd "$(dirname "$0")/.."

BIN="${CASSETTE_BIN:-./bin/cassette}"
SUITE=${1:-/tmp/cassette-ch-suite}
OUT=$(mktemp -d)
trap 'rm -rf "$OUT"' EXIT

if ! command -v clickhouse >/dev/null 2>&1; then
  echo "SKIP  no clickhouse binary on PATH"
  echo "      get one: https://clickhouse.com/docs/install"
  exit 0
fi

[ -x "$BIN" ] || make build >/dev/null

if [ ! -d "$SUITE" ]; then
  echo "recording a suite (every 4th case exercises an extra tool)"
  EXTRA_EVERY=4 ./scripts/build-demo-suite.sh 12 "$SUITE" >/dev/null
  echo "replaying it so the cassettes carry verdicts"
  env -u HTTPS_PROXY -u https_proxy -u HTTP_PROXY -u http_proxy \
      -u ALL_PROXY -u all_proxy \
    "$BIN" test --suite "$SUITE" --jobs 8 >/dev/null 2>&1 || true
fi

echo "exporting"
"$BIN" export --clickhouse "$OUT" --suite "$SUITE"

echo
echo "loading into clickhouse $(clickhouse local --query 'SELECT version()')"
( cd "$OUT" && ./load.sh >/dev/null )

for t in spans payloads runs tool_daily; do
  n=$(clickhouse local --path "$OUT/db" --query "SELECT count() FROM $t")
  printf "  %-12s %s rows\n" "$t" "$n"
done

echo
echo "running every shipped query"
clickhouse local --path "$OUT/db" --queries-file "$OUT/queries.sql" --format PrettyCompact

# The rollup must agree with the raw scan. If a materialized view silently
# diverges from the table it summarizes, every dashboard built on it lies.
echo
raw=$(clickhouse local --path "$OUT/db" --query \
  "SELECT round(quantile(0.95)(duration_ms),1) FROM spans WHERE is_tool_call = 1 AND tool = 'echo'")
rollup=$(clickhouse local --path "$OUT/db" --query \
  "SELECT round(quantilesMerge(0.5,0.95,0.99)(duration_state)[2],1) FROM tool_daily WHERE tool = 'echo'")

if [ "$raw" = "$rollup" ]; then
  echo "PASS  rollup agrees with the raw scan (echo p95 = ${raw}ms both ways)"
else
  echo "FAIL  rollup says ${rollup}ms, raw scan says ${raw}ms"
  exit 1
fi
