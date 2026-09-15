import { href, formatDuration, VERDICTS } from "../lib/format";
import { VerdictBadge } from "../components/VerdictBadge";
import { PageHeader } from "../components/PageHeader";
import { TotalsStrip } from "../components/TotalsStrip";

export function GridPage({ suite }) {
  const replayed = suite.totals.runs - suite.totals.unknown;
  return (
    <main className="page">
      <PageHeader
        eyebrow="Suite overview"
        title="Replay verdict grid"
        description={`${replayed} of ${suite.totals.runs} runs replayed · select a cell to inspect its trajectory diff`}
        actions={
          <div className="legend">
            {VERDICTS.map((verdict) => (
              <span key={verdict}>
                <i className={`legend__dot legend__dot--${verdict}`} />
                {verdict}
              </span>
            ))}
          </div>
        }
      />
      <TotalsStrip totals={suite.totals} />
      <section className="suite-grid" aria-label="Runs by verdict">
        {suite.runs.map((run, index) => (
          <a
            key={run.name}
            className={`grid-cell grid-cell--${run.verdict}`}
            href={href(`/runs/${encodeURIComponent(run.name)}/diff`)}
          >
            <span className="grid-cell__index">
              {String(index + 1).padStart(2, "0")}
            </span>
            <span className="grid-cell__name">{run.name}</span>
            <VerdictBadge verdict={run.verdict} compact />
            <span className="grid-cell__meta">
              {run.messages} steps · {formatDuration(run.durationMs)}
            </span>
            <span className="grid-cell__arrow" aria-hidden="true">
              ↗
            </span>
          </a>
        ))}
      </section>
    </main>
  );
}
