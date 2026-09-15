import { useEffect, useState } from "react";
import {
  formatDuration,
  formatBytes,
  formatDate,
  shortAgent,
} from "../lib/format";
import { useJson } from "../hooks/useJson";
import { Loading, ErrorState } from "../components/Loading";
import { PageHeader } from "../components/PageHeader";
import { VerdictBadge } from "../components/VerdictBadge";
import { RunTabs } from "../components/RunTabs";
import { TierBadge } from "../components/TierBadge";

function Payload({ label, value, emptyLabel = "Empty body" }) {
  let display = value;
  if (value) {
    try {
      display = JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      display = value.trim();
    }
  }
  return (
    <section className="payload">
      <div className="payload__label">{label}</div>
      {display ? (
        <pre>{display}</pre>
      ) : (
        <div className="payload__empty">{emptyLabel}</div>
      )}
    </section>
  );
}

function CallInspector({ name, step, onClose }) {
  const callState = useJson(
    `/api/runs/${encodeURIComponent(name)}/calls/${step.index}.json`,
  );
  return (
    <aside className="inspector" aria-label={`Call ${step.index} details`}>
      <div className="inspector__head">
        <div>
          <span>call {String(step.index).padStart(2, "0")}</span>
          <strong>{step.tool || step.method}</strong>
        </div>
        <button type="button" onClick={onClose} aria-label="Close call details">
          ×
        </button>
      </div>
      <div className="inspector__badges">
        <TierBadge tier={step.tier} />
        <span className={`class-badge class-badge--${step.class}`}>
          {step.class}
        </span>
        {step.isError && <span className="error-badge">error</span>}
      </div>
      <dl className="inspector__meta">
        <div>
          <dt>offset</dt>
          <dd>{formatDuration(step.offsetMs)}</dd>
        </div>
        <div>
          <dt>duration</dt>
          <dd>
            {step.durationMs === 0
              ? "notification"
              : formatDuration(step.durationMs)}
          </dd>
        </div>
        <div>
          <dt>request</dt>
          <dd>{formatBytes(step.reqBytes)}</dd>
        </div>
        <div>
          <dt>response</dt>
          <dd>{step.hasResponse ? formatBytes(step.respBytes) : "none"}</dd>
        </div>
      </dl>
      {callState.loading && <Loading label="Loading bodies" />}
      {callState.error && (
        <ErrorState
          title="Could not load call bodies"
          error={callState.error}
        />
      )}
      {callState.data && (
        <div className="payloads">
          <Payload label="request" value={callState.data.request} />
          <Payload
            label="response"
            value={callState.data.response}
            emptyLabel="No response — notification"
          />
        </div>
      )}
    </aside>
  );
}

export function WaterfallPage({ name }) {
  const runState = useJson(`/api/runs/${encodeURIComponent(name)}/run.json`);
  const [selected, setSelected] = useState(null);

  useEffect(() => setSelected(null), [name]);

  if (runState.loading)
    return (
      <main className="page">
        <Loading label={`Loading ${name}`} />
      </main>
    );
  if (runState.error)
    return (
      <main className="page">
        <ErrorState title={`Could not load ${name}`} error={runState.error} />
      </main>
    );

  const run = runState.data;
  const endMs = Math.max(
    1,
    ...run.steps.map((step) => step.offsetMs + step.durationMs),
  );
  const ticks = [0, 0.25, 0.5, 0.75, 1];
  return (
    <main className="page page--run">
      <PageHeader
        eyebrow="Run trajectory"
        title={run.name}
        description={`${run.steps.length} steps across ${run.tapes.length} ${run.tapes.length === 1 ? "tape" : "tapes"} · ${formatDate(run.recordedAt, true)}`}
        actions={<VerdictBadge verdict={run.verdict} />}
      />
      <RunTabs name={name} active="waterfall" />
      <section className="run-facts" aria-label="Run details">
        <div>
          <span>duration</span>
          <b>{formatDuration(endMs)}</b>
        </div>
        <div>
          <span>agent</span>
          <b title={run.agent.join(" ")}>{shortAgent(run.agent)}</b>
        </div>
        <div>
          <span>cassette</span>
          <b>{run.cassette}</b>
        </div>
        <div>
          <span>tape state</span>
          <b>
            {run.tapes.every((tape) => tape.complete)
              ? "complete"
              : "incomplete"}
          </b>
        </div>
      </section>
      <div
        className={`waterfall-layout${selected !== null ? " has-inspector" : ""}`}
      >
        <section className="waterfall" aria-label="Request timeline">
          <div className="waterfall__head">
            <span>call / match tier</span>
            <div className="time-axis">
              {ticks.map((tick) => (
                <span key={tick} style={{ left: `${tick * 100}%` }}>
                  {formatDuration(endMs * tick)}
                </span>
              ))}
            </div>
          </div>
          <div className="waterfall__rows">
            {run.steps.map((step) => {
              const left = (step.offsetMs / endMs) * 100;
              const width = Math.max((step.durationMs / endMs) * 100, 0.8);
              const isNotification = step.durationMs === 0;
              return (
                <button
                  className={`waterfall-row${selected === step.index ? " is-selected" : ""}${step.isError ? " is-error" : ""}`}
                  key={step.index}
                  type="button"
                  onClick={() => setSelected(step.index)}
                >
                  <span className="step-label">
                    <span className="step-label__index">
                      {String(step.index).padStart(2, "0")}
                    </span>
                    <span className="step-label__call">
                      <b>{step.tool || step.method}</b>
                      <small>
                        {step.tool
                          ? step.method
                          : step.args ||
                            (step.serverInitiated
                              ? "server initiated"
                              : "protocol")}
                      </small>
                    </span>
                    <TierBadge tier={step.tier} />
                  </span>
                  <span className="step-track">
                    {ticks.map((tick) => (
                      <i
                        className="track-line"
                        key={tick}
                        style={{ left: `${tick * 100}%` }}
                      />
                    ))}
                    <span
                      className={`step-bar step-bar--${step.class}${isNotification ? " step-bar--notification" : ""}`}
                      style={{
                        left: `${left}%`,
                        width: isNotification ? undefined : `${width}%`,
                      }}
                    >
                      <span className="sr-only">
                        starts at {formatDuration(step.offsetMs)}, lasts{" "}
                        {formatDuration(step.durationMs)}
                      </span>
                    </span>
                    <span className="step-duration">
                      {isNotification
                        ? "notification"
                        : formatDuration(step.durationMs)}
                    </span>
                  </span>
                </button>
              );
            })}
          </div>
        </section>
        {selected !== null && (
          <CallInspector
            name={name}
            step={run.steps.find((item) => item.index === selected)}
            onClose={() => setSelected(null)}
          />
        )}
      </div>
    </main>
  );
}
