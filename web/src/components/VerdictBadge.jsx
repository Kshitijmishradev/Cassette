import { VERDICTS } from "../lib/format";

export function VerdictBadge({ verdict, compact = false }) {
  const value = VERDICTS.includes(verdict) ? verdict : "unknown";
  return (
    <span
      className={`verdict verdict--${value}${compact ? " verdict--compact" : ""}`}
    >
      <span className="verdict__dot" aria-hidden="true" />
      {value}
    </span>
  );
}
