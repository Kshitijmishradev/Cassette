// Package chexport writes a recorded suite into a form ClickHouse can query.
//
// # Why export rather than embed
//
// The plan originally called for embedding ClickHouse in the binary via
// chDB. That was reconsidered on the merits, not only because chdb-go could
// not be fetched in this build environment.
//
// Embedding means cgo, which costs the single static binary, the clean
// cross-compile matrix, and reproducible builds, all to serve a query path
// that runs weekly and is not latency-sensitive. The replay hot path is
// where the engineering budget belongs, and it needs none of this.
//
// Exporting keeps the binary pure Go and dependency-free, and the part that
// actually carries the ClickHouse thinking, the schema, is unchanged: the
// ordering key, the dictionary encoding, the decision to keep payloads out
// of the scanned table, and the pre-aggregated rollup are all still design
// decisions made here. They just execute in a real ClickHouse rather than a
// linked copy of one, which also means they work against ClickHouse Cloud,
// a self-hosted cluster, or clickhouse-local with no change.
package chexport

// Schema is the DDL written alongside the data.
//
// Every non-obvious choice below is one worth being able to defend, so each
// carries its reason inline. A schema whose ordering key was picked by habit
// is the difference between a query that reads a few granules and one that
// reads the table.
const Schema = `-- Cassette: recorded agent runs, shaped for ClickHouse.
--
-- Load with clickhouse-local:
--     clickhouse local --path ./db --queries-file schema.sql
--     clickhouse local --path ./db --query "INSERT INTO spans FORMAT JSONEachRow" < spans.jsonl
--
-- or against a server:
--     clickhouse-client --queries-file schema.sql
--     clickhouse-client --query "INSERT INTO spans FORMAT JSONEachRow" < spans.jsonl

-- ---------------------------------------------------------------------------
-- spans: one row per message. Deliberately narrow.
-- ---------------------------------------------------------------------------
--
-- This is the table that gets scanned, so nothing large is allowed in it. The
-- request and response bodies live in payloads, keyed by span_id, because
-- the analytical queries touch a handful of numeric and low-cardinality
-- columns and a 40 KB body sitting in the same row would be dragged off disk
-- by every one of them.
--
-- ORDER BY (cassette, tool, started_at) is the primary index, and ClickHouse's
-- primary index is sparse: it marks granules, not rows. The ordering is
-- therefore chosen to match how the data is actually interrogated. Every
-- question below starts by narrowing to a cassette, then usually to a tool,
-- then ranges over time. With this ordering those become granule skips
-- instead of a full scan. Ordering by started_at first, which looks natural
-- for time-series data, would force every per-tool question to read
-- everything.
--
-- LowCardinality on method, tool, cassette and tape is not decoration. A suite
-- of a thousand runs has millions of rows drawn from perhaps thirty distinct
-- tool names; dictionary-encoding them turns a repeated string into a small
-- integer per row and makes the GROUP BYs below cheap.
CREATE TABLE IF NOT EXISTS spans
(
    span_id           UInt64,

    cassette          LowCardinality(String),
    tape              LowCardinality(String),
    run_id            LowCardinality(String),

    seq               UInt32,
    started_at        DateTime64(6),
    duration_ms       Float64,

    method            LowCardinality(String),
    tool              LowCardinality(String),

    key_hash          UInt64,
    norm_hash         UInt64,

    is_tool_call      UInt8,
    is_write          UInt8,
    is_error          UInt8,
    truncated         UInt8,
    server_initiated  UInt8,

    req_bytes         UInt32,
    resp_bytes        UInt32
)
ENGINE = MergeTree
ORDER BY (cassette, tool, started_at);

-- ---------------------------------------------------------------------------
-- payloads: the bodies, out of line.
-- ---------------------------------------------------------------------------
--
-- Point-looked-up by span_id when a human opens one trace, never scanned. The
-- split exists so that keeping full payloads costs nothing on the queries that
-- matter, which is what makes 100% retention affordable rather than a
-- sampling decision.
CREATE TABLE IF NOT EXISTS payloads
(
    span_id  UInt64,
    kind     Enum8('request' = 1, 'response' = 2),
    body     String
)
ENGINE = MergeTree
ORDER BY span_id;

-- ---------------------------------------------------------------------------
-- runs: one row per cassette, carrying the replay verdict when there is one.
-- ---------------------------------------------------------------------------
--
-- This is what lets a query ask about failing versus passing runs rather than
-- only about individual calls.
CREATE TABLE IF NOT EXISTS runs
(
    cassette      LowCardinality(String),
    run_id        LowCardinality(String),
    recorded_at   DateTime64(6),
    agent         String,
    build         LowCardinality(String),

    verdict       LowCardinality(String),  -- identical | drift | changed | unknown
    served        UInt32,
    exact         UInt32,
    normalized    UInt32,
    by_method     UInt32,
    fell_through  UInt32,
    refused       UInt32,
    unused        UInt32,

    messages      UInt32,
    tool_calls    UInt32,
    errors        UInt32
)
ENGINE = MergeTree
ORDER BY (cassette, recorded_at);

-- ---------------------------------------------------------------------------
-- tool_daily: pre-aggregated rollup.
-- ---------------------------------------------------------------------------
--
-- AggregatingMergeTree with quantile *states* rather than finished numbers.
-- Quantiles do not sum, so storing a computed p95 per day would make it
-- impossible to ask for the p95 over a week. Keeping the intermediate state
-- lets the same rollup answer any time range by merging states, which is the
-- whole reason ClickHouse has this machinery.
--
-- calls and errors are SimpleAggregateFunction(sum, ...), not plain UInt64,
-- and that distinction is load-bearing rather than stylistic. In an
-- AggregatingMergeTree, rows sharing a sorting key are collapsed during
-- background merges, and a plain column keeps an arbitrary one of the
-- collapsed values. The table would look correct immediately after insert and
-- start returning wrong counts once a merge happened, which is the worst
-- possible failure shape: silent, delayed, and unreproducible on small data.
-- ClickHouse refuses the DDL outright for exactly this reason.
CREATE TABLE IF NOT EXISTS tool_daily
(
    day             Date,
    cassette        LowCardinality(String),
    tool            LowCardinality(String),
    calls           SimpleAggregateFunction(sum, UInt64),
    errors          SimpleAggregateFunction(sum, UInt64),
    duration_state  AggregateFunction(quantiles(0.5, 0.95, 0.99), Float64)
)
ENGINE = AggregatingMergeTree
ORDER BY (cassette, tool, day);

CREATE MATERIALIZED VIEW IF NOT EXISTS tool_daily_mv TO tool_daily AS
SELECT
    toDate(started_at)                              AS day,
    cassette,
    tool,
    count()                                         AS calls,
    sum(is_error)                                   AS errors,
    quantilesState(0.5, 0.95, 0.99)(duration_ms)    AS duration_state
FROM spans
WHERE is_tool_call = 1
GROUP BY day, cassette, tool;
`

// Queries are the analytical questions the schema exists to answer.
//
// They are shipped with the export rather than left as an exercise, because
// a schema with no queries is a guess. These are the three that justified the
// design, and each one exercises a different part of it.
const Queries = `-- Cassette: the questions this schema was shaped to answer.

-- ---------------------------------------------------------------------------
-- 1. Which tools are slow?
-- ---------------------------------------------------------------------------
-- Exercises the ordering key: filtering by cassette and grouping by tool reads
-- contiguous granules rather than the whole table.
SELECT
    tool,
    count()                                  AS calls,
    round(quantile(0.50)(duration_ms), 1)    AS p50_ms,
    round(quantile(0.95)(duration_ms), 1)    AS p95_ms,
    round(max(duration_ms), 1)               AS max_ms,
    sum(is_error)                            AS errors
FROM spans
WHERE is_tool_call = 1
GROUP BY tool
ORDER BY p95_ms DESC;

-- Same question from the rollup, merging the stored states. This is what
-- makes an arbitrary time range cheap: no raw rows are read at all.
SELECT
    tool,
    sum(calls)                                                   AS calls,
    round(quantilesMerge(0.5, 0.95, 0.99)(duration_state)[2], 1) AS p95_ms
FROM tool_daily
GROUP BY tool
ORDER BY p95_ms DESC;

-- ---------------------------------------------------------------------------
-- 2. How loosely is the suite being matched?
-- ---------------------------------------------------------------------------
-- A suite served mostly by exact matches is a stronger result than one leaning
-- on looser tiers. This is the number that says whether the tapes still
-- describe the agent, or whether they have drifted far enough to need
-- re-recording.
SELECT
    countIf(verdict = 'identical')                            AS identical,
    countIf(verdict = 'drift')                                AS drift,
    countIf(verdict = 'changed')                              AS changed,
    sum(served)                                               AS calls_served,
    round(100 * sum(exact) / nullIf(sum(served), 0), 1)       AS pct_exact,
    round(100 * sum(normalized) / nullIf(sum(served), 0), 1)  AS pct_normalized,
    round(100 * sum(by_method) / nullIf(sum(served), 0), 1)   AS pct_by_method,
    sum(fell_through)                                         AS fell_through,
    sum(refused)                                              AS refused
FROM runs;

-- ---------------------------------------------------------------------------
-- 3. What do failing runs do that passing ones never do?
-- ---------------------------------------------------------------------------
-- The query the whole schema exists for. It finds tools that appear only in
-- cassettes whose behavior changed, which is the closest thing to an automatic
-- explanation of a regression: not "something broke" but "runs that broke all
-- called this, and runs that passed never did".
WITH
    (SELECT groupUniqArray(cassette) FROM runs WHERE verdict = 'changed')   AS failing,
    (SELECT groupUniqArray(cassette) FROM runs WHERE verdict = 'identical') AS passing
SELECT
    tool,
    countIf(has(failing, cassette))   AS in_failing,
    countIf(has(passing, cassette))   AS in_passing,
    uniqIf(cassette, has(failing, cassette)) AS failing_cassettes
FROM spans
WHERE is_tool_call = 1 AND (has(failing, cassette) OR has(passing, cassette))
GROUP BY tool
HAVING in_failing > 0 AND in_passing = 0
ORDER BY in_failing DESC;

-- ---------------------------------------------------------------------------
-- 4. Open one trace. The only query that touches payloads.
-- ---------------------------------------------------------------------------
-- Commented out because it takes a parameter and this file is meant to run
-- end to end. Run it on its own:
--
--     clickhouse local --path ./db --param_cassette=case-04 --query "
--       SELECT s.seq, s.method, s.tool, s.duration_ms, p.kind,
--              substring(p.body, 1, 200) AS body
--       FROM spans AS s
--       LEFT JOIN payloads AS p ON p.span_id = s.span_id
--       WHERE s.cassette = {cassette:String}
--       ORDER BY s.started_at, p.kind"
--
-- This is the only query in the file that reads payloads, which is the point
-- of keeping them in their own table: every aggregate above runs without
-- touching a single body.
`
