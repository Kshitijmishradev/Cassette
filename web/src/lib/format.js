export const VERDICTS = ["identical", "drift", "changed", "unknown"];

export function href(path) {
  return `#${path}`;
}

export function formatDuration(value) {
  if (value < 1) return `${Math.round(value * 1000)}µs`;
  if (value < 1000) return `${value.toFixed(value < 10 ? 2 : 1)}ms`;
  return `${(value / 1000).toFixed(2)}s`;
}

export function formatBytes(value) {
  if (value < 1024) return `${value}B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)}KB`;
  return `${(value / (1024 * 1024)).toFixed(1)}MB`;
}

export function formatDate(value, withYear = false) {
  if (!value) return "—";
  const date = new Date(value);
  const options = {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  };
  if (withYear) options.year = "numeric";
  return new Intl.DateTimeFormat(undefined, options).format(date);
}

export function shortAgent(value) {
  if (!value) return "unknown agent";
  const parts = Array.isArray(value) ? value : value.split(" ");
  const npxIndex = parts.indexOf("npx");
  return npxIndex >= 0
    ? parts.slice(npxIndex).join(" ")
    : parts.slice(-4).join(" ");
}

export function prettyArgs(value) {
  try {
    const parsed = JSON.parse(value);
    return JSON.stringify(parsed).replaceAll(",", ", ").replaceAll(":", ": ");
  } catch {
    return value;
  }
}
