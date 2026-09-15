import { useMemo, useState } from "react";
import {
  href,
  formatDuration,
  formatBytes,
  formatDate,
  shortAgent,
} from "../lib/format";
import { VerdictBadge } from "../components/VerdictBadge";
import { PageHeader } from "../components/PageHeader";
import { TotalsStrip } from "../components/TotalsStrip";

const columns = [
  { key: "name", label: "Run" },
  { key: "verdict", label: "Verdict" },
  { key: "messages", label: "Steps", numeric: true },
  { key: "toolCalls", label: "Tools", numeric: true },
  { key: "durationMs", label: "Duration", numeric: true },
  { key: "bytes", label: "Size", numeric: true },
  { key: "recordedAt", label: "Recorded", numeric: true },
];

function RunsTable({ runs }) {
  const [sort, setSort] = useState({ key: "recordedAt", direction: "desc" });
  const sorted = useMemo(
    () =>
      [...runs].sort((a, b) => {
        const aValue = a[sort.key];
        const bValue = b[sort.key];
        const order =
          typeof aValue === "string"
            ? aValue.localeCompare(bValue)
            : aValue - bValue;
        return sort.direction === "asc" ? order : -order;
      }),
    [runs, sort],
  );

  const setSortKey = (key) =>
    setSort((current) => ({
      key,
      direction:
        current.key === key && current.direction === "asc" ? "desc" : "asc",
    }));

  return (
    <div className="table-frame">
      <table className="runs-table">
        <thead>
          <tr>
            {columns.map((column) => (
              <th
                key={column.key}
                className={column.numeric ? "cell--number" : ""}
                aria-sort={
                  sort.key === column.key ? `${sort.direction}ending` : "none"
                }
              >
                <button type="button" onClick={() => setSortKey(column.key)}>
                  {column.label}
                  <span
                    className={
                      sort.key === column.key ? "sort sort--active" : "sort"
                    }
                    aria-hidden="true"
                  >
                    {sort.key === column.key && sort.direction === "desc"
                      ? "↓"
                      : "↑"}
                  </span>
                </button>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sorted.map((run) => (
            <tr key={run.name}>
              <td>
                <a
                  className="run-link"
                  href={href(`/runs/${encodeURIComponent(run.name)}/waterfall`)}
                >
                  <span>{run.name}</span>
                  <small>{shortAgent(run.agent)}</small>
                </a>
              </td>
              <td>
                <a
                  className="plain-link"
                  href={href(`/runs/${encodeURIComponent(run.name)}/diff`)}
                >
                  <VerdictBadge verdict={run.verdict} compact />
                </a>
              </td>
              <td className="cell--number">{run.messages}</td>
              <td className="cell--number">{run.toolCalls}</td>
              <td className="cell--number">{formatDuration(run.durationMs)}</td>
              <td className="cell--number">{formatBytes(run.bytes)}</td>
              <td className="cell--number timestamp">
                {formatDate(run.recordedAt)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function RunsPage({ suite }) {
  return (
    <main className="page">
      <PageHeader
        eyebrow="Replay index"
        title="Recorded runs"
        description={`${suite.runs.length} trajectories in the current cassette build`}
      />
      <TotalsStrip totals={suite.totals} />
      <RunsTable runs={suite.runs} />
    </main>
  );
}
