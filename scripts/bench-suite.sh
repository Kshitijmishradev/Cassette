#!/usr/bin/env bash
# Measure what replay actually buys: live sessions versus replayed ones,
# serial and parallel.
#
#   ./scripts/bench-suite.sh [count]
#
# What this measures, stated honestly: the cost of the *environment*, not of
# a whole agent. The driver is a script that issues a fixed sequence of MCP
# requests, not a model deciding what to do next. A real agent replaying a
# tape still pays for model inference on every turn.
#
# So the claim this number supports is narrow and real: replay removes the
# environment's cost almost entirely, and it removes every side effect. What
# remains on a real run is model time, which is the part you cannot avoid
# and the part that parallelism helps with most, since replayed cases share
# no external resource to contend over.
set -euo pipefail
cd "$(dirname "$0")/.."

COUNT=${1:-12}
SUITE=/tmp/cassette-bench-suite
BIN="${CASSETTE_BIN:-./bin/cassette}"
NOPROXY=(env -u HTTPS_PROXY -u https_proxy -u HTTP_PROXY -u http_proxy -u ALL_PROXY -u all_proxy)

[ -x "$BIN" ] || make build >/dev/null

secs() { date +%s.%N; }
took() { printf "%.2f" "$(echo "$2 - $1" | bc)"; }

echo "recording $COUNT live sessions"
t0=$(secs)
./scripts/build-demo-suite.sh "$COUNT" "$SUITE" >/dev/null 2>&1
t1=$(secs)
LIVE=$(took "$t0" "$t1")

echo "replaying serially"
t0=$(secs)
"${NOPROXY[@]}" "$BIN" test --suite "$SUITE" --jobs 1 >/dev/null 2>&1
t1=$(secs)
SERIAL=$(took "$t0" "$t1")

echo "replaying in parallel"
t0=$(secs)
"${NOPROXY[@]}" "$BIN" test --suite "$SUITE" --jobs "$COUNT" >/dev/null 2>&1
t1=$(secs)
PARALLEL=$(took "$t0" "$t1")

echo
printf "%d cassettes on %d cores\n\n" "$COUNT" "$(nproc 2>/dev/null || sysctl -n hw.ncpu)"
printf "  %-22s %8ss\n" "live sessions"      "$LIVE"
printf "  %-22s %8ss   %sx\n" "replay, serial"   "$SERIAL"   "$(printf %.0f "$(echo "$LIVE/$SERIAL" | bc -l)")"
printf "  %-22s %8ss   %sx\n" "replay, parallel" "$PARALLEL" "$(printf %.0f "$(echo "$LIVE/$PARALLEL" | bc -l)")"
echo
printf "  parallel speedup over serial: %.1fx\n" "$(echo "$SERIAL/$PARALLEL" | bc -l)"
