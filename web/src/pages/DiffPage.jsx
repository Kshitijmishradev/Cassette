import { useEffect, useState } from "react";
import { href, prettyArgs } from "../lib/format";
import { useJson } from "../hooks/useJson";
import { Loading, ErrorState } from "../components/Loading";
import { PageHeader } from "../components/PageHeader";
import { VerdictBadge } from "../components/VerdictBadge";
import { RunTabs } from "../components/RunTabs";
import { TierBadge } from "../components/TierBadge";

function groupDiffPairs(pairs) {
  const rows = [];
  let index = 0;
  while (index < pairs.length) {
    if (pairs[index].op !== "match") {
      rows.push({ type: "pair", pair: pairs[index], index });
      index += 1;
      continue;
    }
    const start = index;
    while (index < pairs.length && pairs[index].op === "match") index += 1;
    const matched = pairs.slice(start, index);
    if (matched.length > 1) rows.push({ type: "group", pairs: matched, start });
    else rows.push({ type: "pair", pair: matched[0], index: start });
  }
  return rows;
}

function DiffSummary({ diff }) {
  const delta =
    diff.stepDelta > 0 ? `+${diff.stepDelta}` : String(diff.stepDelta);
  return (
    <section className="diff-summary" aria-label="Diff summary">
      <div className="diff-summary__trajectory">
        <div>
          <span>{diff.baselineName}</span>
          <strong>{diff.baselineSteps}</strong>
          <small>steps</small>
        </div>
        <div className={`delta${diff.stepDelta === 0 ? " delta--zero" : ""}`}>
          <span>Δ</span>
          <b>{delta}</b>
        </div>
        <div>
          <span>{diff.candidateName}</span>
          <strong>{diff.candidateSteps}</strong>
          <small>steps</small>
        </div>
      </div>
      <div className="diff-counts">
        <div>
          <span>matched</span>
          <strong>{diff.matched}</strong>
        </div>
        <div>
          <span>substituted</span>
          <strong>{diff.substituted}</strong>
        </div>
        <div>
          <span>inserted</span>
          <strong>{diff.inserted}</strong>
        </div>
        <div>
          <span>deleted</span>
          <strong>{diff.deleted}</strong>
        </div>
        <div>
          <span>missed</span>
          <strong>{diff.missed}</strong>
        </div>
      </div>
    </section>
  );
}

const OP_LABELS = {
  match: { glyph: "=", label: "match" },
  substitute: { glyph: "≠", label: "changed" },
  insert: { glyph: "→", label: "inserted" },
  delete: { glyph: "←", label: "deleted" },
};

function DiffCallCell({ call, side, index }) {
  if (!call) {
    return (
      <div className="diff-cell diff-cell--empty">
        <span>
          {side === "baseline" ? "not in baseline" : "not in candidate"}
        </span>
      </div>
    );
  }
  return (
    <div className="diff-cell">
      <span className="diff-cell__index">
        {String(index + 1).padStart(2, "0")}
      </span>
      <div className="diff-cell__content">
        <div className="diff-cell__title">
          <strong>{call.tool || call.method}</strong>
          {call.tool && <span>{call.method}</span>}
        </div>
        {call.args && <code>{prettyArgs(call.args)}</code>}
        <div className="diff-cell__meta">
          <span className={`class-token class-token--${call.class}`}>
            {call.class === "write" ? "W" : "R"} · {call.class}
          </span>
          {call.tier && <TierBadge tier={call.tier} />}
          {call.missed && <span className="missed-token">missed</span>}
        </div>
      </div>
    </div>
  );
}

function DiffPairRow({ pair, index }) {
  const operation = OP_LABELS[pair.op] || OP_LABELS.substitute;
  const isDifference = pair.op !== "match";
  return (
    <div
      className={`diff-row diff-row--${pair.op}${isDifference && pair.write ? " diff-row--write" : ""}`}
    >
      <DiffCallCell call={pair.baseline} side="baseline" index={index} />
      <div className="diff-op">
        <span aria-hidden="true">{operation.glyph}</span>
        <small>{pair.write && isDifference ? "write" : operation.label}</small>
      </div>
      <DiffCallCell call={pair.candidate} side="candidate" index={index} />
    </div>
  );
}

function MissingDiff({ name, summary }) {
  return (
    <main className="page page--run">
      <PageHeader
        eyebrow="Trajectory comparison"
        title={name}
        actions={<VerdictBadge verdict="unknown" />}
      />
      <RunTabs name={name} active="diff" />
      <section className="missing-diff">
        <div className="missing-diff__mark" aria-hidden="true">
          ∅
        </div>
        <div>
          <span>comparison unavailable</span>
          <h2>This run has not been replayed.</h2>
          <p>
            No <code>diff.json</code> exists yet. Unknown is intentionally
            separate from a passing result.
          </p>
          {summary && (
            <a href={href(`/runs/${encodeURIComponent(name)}/waterfall`)}>
              inspect the recorded trajectory →
            </a>
          )}
        </div>
      </section>
    </main>
  );
}

export function DiffPage({ name, summary }) {
  const diffState = useJson(
    summary?.verdict === "unknown"
      ? null
      : `/api/runs/${encodeURIComponent(name)}/diff.json`,
  );
  const [expanded, setExpanded] = useState({});

  useEffect(() => setExpanded({}), [name]);

  if (diffState.loading)
    return (
      <main className="page page--run">
        <Loading label={`Aligning ${name}`} />
      </main>
    );
  if (diffState.error?.status === 404 || summary?.verdict === "unknown") {
    return <MissingDiff name={name} summary={summary} />;
  }
  if (diffState.error)
    return (
      <main className="page page--run">
        <ErrorState
          title={`Could not load the ${name} diff`}
          error={diffState.error}
        />
      </main>
    );

  const diff = diffState.data;
  const rows = groupDiffPairs(diff.pairs);
  return (
    <main className="page page--run page--diff">
      <PageHeader
        eyebrow="Trajectory comparison"
        title={diff.name}
        description={`${diff.baselineName} aligned against ${diff.candidateName}`}
        actions={<VerdictBadge verdict={diff.verdict} />}
      />
      <RunTabs name={name} active="diff" />
      <DiffSummary diff={diff} />
      <section
        className="diff-frame"
        aria-label={`${diff.name} trajectory diff`}
      >
        <div className="diff-head">
          <div>
            <span>baseline</span>
            <strong>{diff.baselineName}</strong>
          </div>
          <div className="diff-head__axis">
            <span>alignment</span>
          </div>
          <div>
            <span>candidate</span>
            <strong>{diff.candidateName}</strong>
          </div>
        </div>
        <div className="diff-body">
          {rows.map((row) => {
            if (row.type === "pair")
              return (
                <DiffPairRow
                  key={`pair-${row.index}`}
                  pair={row.pair}
                  index={row.index}
                />
              );
            const isOpen = Boolean(expanded[row.start]);
            return (
              <div className="match-group" key={`group-${row.start}`}>
                <button
                  type="button"
                  aria-expanded={isOpen}
                  onClick={() =>
                    setExpanded((current) => ({
                      ...current,
                      [row.start]: !isOpen,
                    }))
                  }
                >
                  <span className="match-group__line" />
                  <span className="match-group__label">
                    <i aria-hidden="true">{isOpen ? "−" : "+"}</i>
                    {row.pairs.length} identical calls
                  </span>
                  <span className="match-group__line" />
                </button>
                {isOpen &&
                  row.pairs.map((pair, offset) => (
                    <DiffPairRow
                      key={`pair-${row.start + offset}`}
                      pair={pair}
                      index={row.start + offset}
                    />
                  ))}
              </div>
            );
          })}
        </div>
      </section>
    </main>
  );
}
