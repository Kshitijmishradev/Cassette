export function TierBadge({ tier }) {
  const label = tier || "not replayed";
  return (
    <span
      className={`tier tier--${tier || "empty"}`}
      title={tier ? `Matched at ${tier} tier` : "No replay match data"}
    >
      <i />
      {label}
    </span>
  );
}
