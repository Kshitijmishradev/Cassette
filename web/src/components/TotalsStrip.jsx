export function TotalsStrip({ totals }) {
  return (
    <section className="totals-strip" aria-label="Suite totals">
      <div className="total total--primary">
        <span>runs</span>
        <strong>{totals.runs}</strong>
      </div>
      <div className="total">
        <span className="total__key total__key--identical">identical</span>
        <strong>{totals.identical}</strong>
      </div>
      <div className="total">
        <span className="total__key total__key--drift">drift</span>
        <strong>{totals.drift}</strong>
      </div>
      <div className="total">
        <span className="total__key total__key--changed">changed</span>
        <strong>{totals.changed}</strong>
      </div>
      <div className="total">
        <span className="total__key total__key--unknown">unknown</span>
        <strong>{totals.unknown}</strong>
      </div>
      <div className="total total--secondary">
        <span>messages</span>
        <strong>{totals.messages}</strong>
      </div>
      <div className="total total--secondary">
        <span>tool calls</span>
        <strong>{totals.toolCalls}</strong>
      </div>
      <div className="total total--secondary">
        <span>errors</span>
        <strong>{totals.errors}</strong>
      </div>
    </section>
  );
}
