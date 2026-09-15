export function Loading({ label = "Loading trace data" }) {
  return (
    <div className="state-panel">
      <span className="loader" aria-hidden="true" />
      {label}…
    </div>
  );
}

export function ErrorState({ title = "Could not load data", error }) {
  return (
    <div className="state-panel state-panel--error">
      <span className="state-panel__glyph" aria-hidden="true">
        !
      </span>
      <div>
        <strong>{title}</strong>
        <p>{error?.message || "An unexpected error occurred."}</p>
      </div>
    </div>
  );
}
