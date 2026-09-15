import { useEffect, useState } from "react";
import "./story.css";

const REPO = "https://github.com/Kshitijmishradev/Cassette";
const chapters = ["Record", "Replay", "Understand"];

export function SiteHeader({ docs = false }) {
  return (
    <header className="story-header">
      <a className="story-brand" href="#/" aria-label="Cassette home">
        <span className="mini-tape" aria-hidden="true">
          <i />
          <i />
        </span>
        cassette
      </a>
      <nav aria-label="Main navigation">
        <a href="#/story" aria-current={!docs ? "page" : undefined}>
          The story
        </a>
        <a href="#/docs" aria-current={docs ? "page" : undefined}>
          Documentation
        </a>
        <a className="github-link" href={REPO}>
          GitHub <span>↗</span>
        </a>
      </nav>
      <a className="header-demo" href="#/runs/case-04/diff">
        Open demo <span>↗</span>
      </a>
    </header>
  );
}

export function CodeBlock({ children, label = "TERMINAL" }) {
  const [status, setStatus] = useState("Copy");
  async function copy() {
    try {
      await navigator.clipboard.writeText(children);
      setStatus("Copied");
    } catch {
      setStatus("Select to copy");
    }
  }
  useEffect(() => {
    if (status === "Copy") return;
    const timer = setTimeout(() => setStatus("Copy"), 2200);
    return () => clearTimeout(timer);
  }, [status]);
  return (
    <div className="story-code">
      <div>
        <span>{label}</span>
        <button onClick={copy} type="button">
          {status}
        </button>
      </div>
      <pre>
        <code>{children}</code>
      </pre>
    </div>
  );
}

function CassetteArt({ playing }) {
  return (
    <div
      className={`cassette-scene ${playing ? "is-playing" : ""}`}
      aria-label="Illustrated orange cassette tape with rotating reels"
      role="img"
    >
      <div className="orbit orbit-one" />
      <div className="orbit orbit-two" />
      <span className="art-cross cross-one">+</span>
      <span className="art-cross cross-two">+</span>
      <span className="art-coordinate">FIG. 001 — A WORLD, CAPTURED.</span>
      <div className="tape-shadow" />
      <div className="tape">
        <span className="screw screw-tl" />
        <span className="screw screw-tr" />
        <span className="screw screw-bl" />
        <span className="screw screw-br" />
        <div className="tape-label">
          <div className="tape-label-top">
            <b>cassette</b>
            <span>
              TYPE I<br />
              MCP / REPLAY
            </span>
          </div>
          <div className="tape-rule" />
          <div className="tape-title">THE WORLD, ON TAPE.</div>
          <div className="tape-window">
            <div className="reel">
              <i />
            </div>
            <div className="tape-window-center">
              <span />
              <span />
              <span />
            </div>
            <div className="reel">
              <i />
            </div>
          </div>
          <div className="tape-label-bottom">
            <b>A</b>
            <span>RECORD ONCE. REPLAY SAFELY.</span>
            <b>∞</b>
          </div>
        </div>
        <div className="tape-bottom">
          <i />
          <span />
          <i />
        </div>
      </div>
      <span className="art-note">
        A little less chaos.
        <br />
        <em>A lot more certainty.</em>
      </span>
    </div>
  );
}

const scenarios = {
  identical: {
    name: "Same behavior",
    verdict: "IDENTICAL",
    text: "Same calls. Same order. The agent followed the recorded path.",
    calls: [
      "get_customer",
      "get_order",
      "get_delivery_status",
      "create_refund",
    ],
  },
  drift: {
    name: "A different path",
    verdict: "DRIFT",
    text: "An extra read, but the same write. The outcome is stable; the path has drifted.",
    calls: [
      "get_customer",
      "get_order",
      "get_delivery_status",
      "get_order",
      "create_refund",
    ],
  },
  changed: {
    name: "A different outcome",
    verdict: "CHANGED",
    text: "The refund disappeared. A write changed, so the agent did something different to the world.",
    calls: ["get_customer", "get_order", "get_delivery_status"],
  },
};

export default function Story() {
  const [playing, setPlaying] = useState(true);
  const [scenario, setScenario] = useState("changed");
  const [active, setActive] = useState(0);
  const result = scenarios[scenario];
  useEffect(() => {
    document.title = "Cassette — Change the agent. Keep the world still.";
    const observer = new IntersectionObserver(
      (entries) =>
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            entry.target.classList.add("revealed");
            if (entry.target.dataset.chapter)
              setActive(Number(entry.target.dataset.chapter));
          }
        }),
      { threshold: 0.15 },
    );
    document
      .querySelectorAll(".reveal, [data-chapter]")
      .forEach((el) => observer.observe(el));
    return () => observer.disconnect();
  }, []);
  const jump = (id) =>
    document
      .getElementById(id)
      ?.scrollIntoView({
        behavior: matchMedia("(prefers-reduced-motion: reduce)").matches
          ? "instant"
          : "smooth",
      });
  return (
    <div className={`story-site ${playing ? "" : "motion-paused"}`}>
      <a
        className="story-skip"
        href="#story-content"
        onClick={(event) => {
          event.preventDefault();
          document.getElementById("story-content").focus();
        }}
      >
        Skip to content
      </a>
      <SiteHeader />
      <main id="story-content" tabIndex={-1}>
        <section className="story-hero">
          <div className="hero-copy">
            <div className="eyebrow">
              <span className="orange-dot" /> AN OPEN-SOURCE REPLAY ENGINE FOR
              AI AGENTS
            </div>
            <h1>
              Change the agent.
              <br />
              Keep the world
              <br />
              <span>still.</span>
              <svg
                className="hero-scribble"
                viewBox="0 0 210 55"
                aria-hidden="true"
              >
                <path d="M6 32 Q96 5 200 22 M17 45 Q122 20 192 34" />
              </svg>
            </h1>
            <p>
              The world moves. Your tests shouldn’t.
              <br />
              Record real tool calls. Replay the same reality.
              <br />
              See exactly what your agent changed.
            </p>
            <div className="hero-actions">
              <button className="story-button" onClick={() => jump("record")}>
                Press play on the story <span>↘</span>
              </button>
              <a className="text-link" href="#/docs">
                Get started <span>↗</span>
              </a>
            </div>
            <div className="hero-footnote">
              <span>LOCAL-FIRST</span>
              <span>ZERO GO DEPENDENCIES</span>
              <span>MIT LICENSE</span>
            </div>
          </div>
          <div className="hero-visual">
            <CassetteArt playing={playing} />
            <button
              className="motion-toggle"
              onClick={() => setPlaying(!playing)}
              aria-pressed={!playing}
            >
              {playing ? "Ⅱ" : "▶"}{" "}
              <span>{playing ? "Pause motion" : "Play motion"}</span>
            </button>
          </div>
        </section>
        <div className="story-divider">
          <span>REALITY IS A MOVING TARGET.</span>
          <span>GIVE YOUR AGENT A REWIND BUTTON.</span>
          <button onClick={() => jump("record")} aria-label="Go to chapter one">
            ↓
          </button>
        </div>
        <div className="chapter-nav" aria-label="Story chapters">
          {chapters.map((chapter, i) => (
            <button
              key={chapter}
              className={active === i ? "active" : ""}
              onClick={() => jump(["record", "replay", "understand"][i])}
            >
              <span>0{i + 1}</span>
              {chapter}
            </button>
          ))}
          <a href="#/docs">The field guide ↗</a>
        </div>

        <section id="record" className="story-chapter" data-chapter="0">
          <div className="chapter-heading reveal">
            <div className="eyebrow">01 / RECORD THE REAL THING</div>
            <h2>
              Every great test
              <br />
              starts with <em>reality.</em>
            </h2>
            <p>
              A customer needs a refund. Your agent checks an order, talks to a
              delivery service, and makes a decision. Cassette sits between the
              agent and its MCP servers, quietly recording the conversation.
            </p>
            <a className="text-link" href="#/docs/record">
              How recording works ↗
            </a>
          </div>
          <div className="record-diagram reveal">
            <div className="diagram-caption">
              <span className="orange-dot" /> REC · LIVE SESSION{" "}
              <span>01:42</span>
            </div>
            <div className="diagram-node">
              Your agent <span>thinking, calling, deciding</span>
            </div>
            <div className="signal-line">
              <i />
            </div>
            <div className="diagram-node orange-node">
              <span className="mini-tape">
                <i />
                <i />
              </span>
              Cassette <small>Every request. Every response.</small>
            </div>
            <div className="signal-line">
              <i />
            </div>
            <div className="service-row">
              <span>Customer DB</span>
              <span>Delivery API</span>
              <span>Stripe</span>
            </div>
            <div className="tape-file">
              <span>↳</span> refund-session.cas1 <b>WORLD SAVED ✓</b>
            </div>
          </div>
        </section>

        <section id="replay" className="replay-section" data-chapter="1">
          <div className="replay-art reveal" aria-hidden="true">
            <div className="rewind-ring ring-outer" />
            <div className="rewind-ring ring-inner" />
            <div className="rewind-symbol">◀◀</div>
            <div className="replay-art-label">
              SAME WORLD.
              <br />
              <span>NEW POSSIBILITIES.</span>
            </div>
            <span className="replay-star">✳</span>
          </div>
          <div className="chapter-heading reveal">
            <div className="eyebrow">02 / REPLAY WITHOUT THE RISK</div>
            <h2>
              A new prompt.
              <br />
              The <em>same world.</em>
            </h2>
            <p>
              Make your agent more cautious. Switch its model. Try a different
              approach. On replay, Cassette serves the responses from your tape.
            </p>
            <p>
              Use hermetic mode to refuse every unmatched call. Your agent can
              change its mind without issuing another real refund.
            </p>
            <CodeBlock>
              {
                'cassette replay refund-session --hermetic -- \\\n  claude -p "Check the evidence before refunding"'
              }
            </CodeBlock>
            <a className="text-link" href="#/docs/replay">
              Inside the replay engine ↗
            </a>
          </div>
        </section>

        <section
          id="understand"
          className="understand-section"
          data-chapter="2"
        >
          <div className="chapter-heading reveal">
            <div className="eyebrow">03 / UNDERSTAND THE DIFFERENCE</div>
            <h2>
              A different path?
              <br />
              Or a different <em>outcome?</em>
            </h2>
            <p>
              A pass/fail check can’t tell the whole story. Cassette compares
              the sequence of tool calls, with special attention to the writes
              that change the world.
            </p>
          </div>
          <div className="interactive-diff reveal">
            <div className="diff-demo-heading">
              <span>THE REFUND EXPERIMENT</span>
              <span>INTERACTIVE EXAMPLE ↙</span>
            </div>
            <div
              className="scenario-tabs"
              aria-label="Choose a replay scenario"
            >
              {Object.entries(scenarios).map(([key, s]) => (
                <button
                  key={key}
                  aria-pressed={key === scenario}
                  className={key === scenario ? "selected" : ""}
                  onClick={() => setScenario(key)}
                >
                  {s.name}
                </button>
              ))}
            </div>
            <div className="story-trajectories">
              <div>
                <h3>
                  <span>●</span> Recorded <small>BASELINE</small>
                </h3>
                {scenarios.identical.calls.map((call, i) => (
                  <div
                    className={`story-call ${call === "create_refund" ? "write-call" : ""}`}
                    key={i}
                  >
                    <span>0{i + 1}</span>
                    <code>{call}</code>
                    <small>{call === "create_refund" ? "WRITE" : "READ"}</small>
                  </div>
                ))}
              </div>
              <div key={scenario} className="candidate">
                <h3>
                  <span>↻</span> Replayed <small>CANDIDATE</small>
                </h3>
                {result.calls.map((call, i) => (
                  <div
                    className={`story-call ${call === "create_refund" ? "write-call" : ""} ${scenario === "drift" && i === 3 ? "added-call" : ""}`}
                    key={i}
                  >
                    <span>0{i + 1}</span>
                    <code>{call}</code>
                    <small>{call === "create_refund" ? "WRITE" : "READ"}</small>
                  </div>
                ))}
                {scenario === "changed" && (
                  <div className="removed-call">
                    − create_refund <small>WRITE REMOVED</small>
                  </div>
                )}
              </div>
            </div>
            <div className={`story-verdict ${scenario}`} aria-live="polite">
              <b>{result.verdict}</b>
              <p>{result.text}</p>
            </div>
            <a className="diff-demo-link" href="#/runs/case-04/diff">
              Explore a real fixture in the trace viewer <span>↗</span>
            </a>
          </div>
        </section>

        <section className="story-start reveal">
          <div>
            <div className="eyebrow">YOUR NEXT CHAPTER</div>
            <h2>
              Make your next change
              <br />
              with a <em>rewind button.</em>
            </h2>
            <p>One binary. Your machine. A little more peace of mind.</p>
            <a className="story-button" href="#/docs">
              Make your first recording <span>↗</span>
            </a>
          </div>
          <div className="field-notes">
            <span>THE FIELD GUIDE</span>
            <a href="#/docs">
              01 <b>Get up and running</b> ↗
            </a>
            <a href="#/docs/record">
              02 <b>Capture a real session</b> ↗
            </a>
            <a href="#/docs/replay">
              03 <b>Replay in a frozen world</b> ↗
            </a>
            <a href="#/docs/verdicts">
              04 <b>Read the differences</b> ↗
            </a>
            <a href="#/docs/ci">
              05 <b>Take it into CI</b> ↗
            </a>
          </div>
        </section>
      </main>
      <footer className="story-footer">
        <a className="story-brand" href="#/">
          cassette
        </a>
        <span>CHANGE THE AGENT. KEEP THE WORLD STILL.</span>
        <a href={REPO}>Open source, on purpose. ↗</a>
      </footer>
    </div>
  );
}
