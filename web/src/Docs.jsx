import { useEffect, useState } from "react";
import { CodeBlock, SiteHeader } from "./Story.jsx";

const REPO = "https://github.com/Kshitijmishradev/Cassette";
const pages = [
  {
    id: "",
    title: "Your first cassette",
    subtitle: "From a live session to a repeatable world.",
    intro:
      "Cassette records the MCP conversation between your agent and its tools. After a prompt or model change, replay the recorded responses and compare what the agent does.",
    sections: [
      {
        title: "01. Build the binary",
        body: "You’ll need Go 1.27 and an agent that uses MCP servers. The frontend bundle is included in the checkout; Node.js is only needed to develop the web UI. The Go core has zero third-party dependencies.",
        code: "git clone https://github.com/Kshitijmishradev/Cassette.git\ncd Cassette\nmake build",
      },
      {
        title: "02. Put Cassette in the conversation",
        body: "In your agent’s MCP configuration, wrap a server command with Cassette. Replace the binary path with your own absolute path. The example below wraps the GitHub MCP server; use the server appropriate for your agent.",
        label: "MCP CONFIGURATION",
        code: '{\n  "mcpServers": {\n    "github": {\n      "command": "/path/to/Cassette/bin/cassette",\n      "args": ["wrap", "--", "npx", "-y",\n        "@modelcontextprotocol/server-github"]\n    }\n  }\n}',
      },
      {
        title: "03. Record. Change. Replay.",
        body: "Without a recording or replay mode, the shim forwards messages untouched and records nothing. Use the outer command to select a mode, then run your agent normally.",
        code: './bin/cassette record fix-auth -- claude -p "fix the failing auth test"\n./bin/cassette inspect fix-auth\n\n# Change the prompt, keeping the recorded world\n./bin/cassette replay fix-auth --hermetic -- \\\n  claude -p "fix the auth test; avoid risky writes"',
      },
      {
        title: "04. See the story of the run",
        body: "Open the trace viewer to explore recorded calls, replay match tiers, and side-by-side trajectory differences.",
        code: "./bin/cassette serve --suite ./cassettes --open",
      },
    ],
  },
  {
    id: "record",
    title: "Capture the real thing",
    subtitle: "A recording of reality, not a guess about it.",
    intro:
      "During recording, Cassette forwards requests to the real MCP server and saves the conversation. The tools really run, including writes. Choose a session you intend to execute.",
    sections: [
      {
        title: "Record a session",
        body: "First configure the cassette wrap shim as described in the quick start. Give your recording a useful name, then pass your agent command after --.",
        code: 'cassette record refund-session -- \\\n  claude -p "Investigate the customer’s refund request"\ncassette inspect refund-session',
      },
      {
        title: "What goes on the tape?",
        body: "Cassette records MCP requests and responses in its binary CAS1 format. A session can include multiple wrapped servers. Inspect presents the merged tool trajectory so you can see the sequence across them.",
      },
      {
        title: "Treat recordings like real data",
        body: "Tapes may contain sensitive payloads. Cassette is local-first and does not upload them to a hosted service. Review or sanitize recordings before committing or sharing them. A tape is also a dated snapshot: refresh it when the world you want to test has changed.",
      },
    ],
  },
  {
    id: "replay",
    title: "Keep the world still",
    subtitle: "Let the agent change. Hold its environment steady.",
    intro:
      "Replay serves recorded tool responses to your agent. The model still runs and may behave differently; Cassette freezes the external tool environment, not the model’s reasoning.",
    sections: [
      {
        title: "Replay a changed agent",
        body: "Use the same recording name with your new prompt or model command. Hermetic mode refuses any call the recording cannot serve.",
        code: 'cassette replay refund-session --hermetic -- \\\n  claude -p "Check the evidence before issuing a refund"',
      },
      {
        title: "Know how a call matched",
        body: "Exact matching is strongest. Normalized matching tolerates normalized argument differences, and method-only matching is a looser fallback. The viewer shows match tiers per call so you can inspect when your new trajectory moves away from the recording.",
      },
      {
        title: "Understand the safety boundary",
        body: "Live MCP fall-through and tool-name heuristics are disabled by default. Exploratory live reads require replay.fallThrough: true and reviewed, case-sensitive tool names in tools.read. Use --hermetic to override any fall-through permission. This controls wrapped MCP traffic; it does not sandbox the agent’s shell, native tools, or network access. Only run trusted agents and suites.",
      },
      {
        title: "The model is still live",
        body: "Replay removes external-service latency and side effects for calls served from tape. Your agent still pays for model inference. A reproducible environment does not imply a deterministic model or prove that its decisions are correct.",
      },
    ],
  },
  {
    id: "verdicts",
    title: "Read what changed",
    subtitle: "The path matters. The writes decide.",
    intro:
      "Cassette aligns the recorded and candidate tool trajectories. Reads describe the route the agent took. Writes describe its observable effect on the world.",
    sections: [
      {
        title: "IDENTICAL · same calls, same order",
        body: "The compared trajectories match. Nothing changed in the observed tool-call sequence.",
      },
      {
        title: "DRIFT · same writes, different path",
        body: "The agent reached the same write actions by a different route. Extra reads or reordered steps can reveal a behavioral regression that a binary pass/fail result hides.",
      },
      {
        title: "CHANGED · different writes",
        body: "A write was added, removed, or changed. In the refund example, removing create_refund changes the outcome. Cassette exposes the difference; your policy and domain-specific evals decide whether it is good.",
      },
      {
        title: "UNKNOWN · no replay yet",
        body: "An unreplayed recording has no comparison. Unknown is not a passing result.",
      },
      {
        title: "Explore the trace viewer",
        body: "The runs list helps you scan a suite. The waterfall shows calls, timing, payloads and match tiers. The trajectory diff aligns both runs and highlights write changes. The suite grid gives each recording a verdict.",
        link: "#/runs/case-04/diff",
        linkText: "Open the interactive trace viewer",
      },
    ],
  },
  {
    id: "ci",
    title: "Bring a tape to CI",
    subtitle: "A repeatable world for every change.",
    intro:
      "Commit reviewed recordings and replay them as a suite. Use hermetic mode so missing calls cannot fall through to real services.",
    sections: [
      {
        title: "Run a hermetic suite",
        body: "The suite runner compares agent behavior across your recordings. Inspect any changed writes and unexpected drift before accepting a new baseline.",
        code: "cassette test --suite ./cassettes --hermetic",
      },
      {
        title: "Use the GitHub Action",
        body: "The composite action replays a committed suite and preserves Cassette’s exit codes. This example fails on outcome changes and uses read-only permissions. PR comments are optional: never give write credentials to a workflow that executes untrusted pull-request code.",
        label: "GITHUB ACTIONS",
        code: "permissions:\n  contents: read\n\nsteps:\n  - uses: actions/checkout@v4\n    with:\n      persist-credentials: false\n  - uses: Kshitijmishradev/Cassette@main\n    with:\n      suite: ./cassettes\n      fail-on: outcome",
      },
      {
        title: "Inspect the action contract",
        body: "Read the action definition for the available inputs and defaults before adapting it to your workflow.",
        link: `${REPO}/blob/main/action.yml`,
        linkText: "Read action.yml on GitHub",
      },
    ],
  },
  {
    id: "architecture",
    title: "Inside the machine",
    subtitle: "A small tool with a deliberate boundary.",
    intro:
      "Cassette is a transparent MCP proxy, a binary recording format, a replay engine, and a write-aware trajectory differ. The React viewer is embedded in the Go binary and can also be exported as a static site.",
    sections: [
      {
        title: "The architecture",
        body: "Explore the protocol, CAS1 storage, matching strategy, and design decisions in the project’s technical reference.",
        link: `${REPO}/blob/main/ARCHITECTURE.md`,
        linkText: "Read the architecture",
      },
      {
        title: "Evidence from a real agent",
        body: "A verified Codex replay served all six recorded calls with exact matches while configured with a nonexistent server binary. The full run documents the setup and evidence.",
        link: `${REPO}/blob/main/docs/REAL_AGENT_RUN.md`,
        linkText: "Read the real-agent run",
      },
      {
        title: "Viewer API",
        body: "The live viewer and static demo use the same JSON contract for suites, runs, calls, and diffs.",
        link: `${REPO}/blob/main/API.md`,
        linkText: "Read the API contract",
      },
      {
        title: "Status and limitations",
        body: "See what is implemented, known limitations, and the remaining distribution work in the project’s progress log.",
        link: `${REPO}/blob/main/PROGRESS.md`,
        linkText: "Read the progress log",
      },
    ],
  },
];

export default function Docs({ slug = "" }) {
  const [query, setQuery] = useState("");
  const index = pages.findIndex((page) => page.id === slug);
  const page = pages[index];
  useEffect(() => {
    document.title = `${page?.title || "Page not found"} — Cassette docs`;
    window.scrollTo(0, 0);
  }, [slug, page]);
  const matches = pages.filter((p) =>
    `${p.title} ${p.intro} ${p.sections.map((s) => `${s.title} ${s.body} ${s.code || ""}`).join(" ")}`
      .toLowerCase()
      .includes(query.toLowerCase()),
  );
  return (
    <div className="story-site docs-site">
      <SiteHeader docs />
      <div className="docs-layout">
        <aside className="docs-sidebar">
          <a className="eyebrow" href="#/docs">
            THE FIELD GUIDE
          </a>
          <label className="docs-search">
            <span>Search documentation</span>
            <input
              type="search"
              placeholder="Find a chapter…"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </label>
          <nav aria-label="Documentation chapters">
            {matches.map((p) => (
              <a
                key={p.id}
                href={`#/docs${p.id ? `/${p.id}` : ""}`}
                aria-current={p.id === slug ? "page" : undefined}
              >
                <span>0{pages.indexOf(p) + 1}</span>
                {p.title}
              </a>
            ))}
            {matches.length === 0 && (
              <p className="docs-empty">
                No chapters found. Try “replay” or “MCP”.
              </p>
            )}
          </nav>
          <div className="docs-side-note">
            <span className="mini-tape">
              <i />
              <i />
            </span>
            <p>Learn by looking.</p>
            <a href="#/runs/case-04/diff">Open the live demo ↗</a>
          </div>
        </aside>
        <main className="docs-content">
          {page ? (
            <>
              <div className="eyebrow">DOCUMENTATION / 0{index + 1}</div>
              <h1>
                {page.title}
                <span>.</span>
              </h1>
              <p className="docs-subtitle">{page.subtitle}</p>
              <p className="docs-intro">{page.intro}</p>
              {page.sections.map((section, i) => (
                <section
                  className="docs-section"
                  id={`section-${i}`}
                  key={section.title}
                >
                  <h2>{section.title}</h2>
                  <p>{section.body}</p>
                  {section.code && (
                    <CodeBlock label={section.label}>{section.code}</CodeBlock>
                  )}
                  {section.link && (
                    <a className="text-link" href={section.link}>
                      {section.linkText} ↗
                    </a>
                  )}
                </section>
              ))}
              <div className="docs-pagination">
                {index > 0 ? (
                  <a href={`#/docs/${pages[index - 1].id}`}>
                    <small>← PREVIOUS CHAPTER</small>
                    {pages[index - 1].title}
                  </a>
                ) : (
                  <a href="#/story">
                    <small>← BACK TO</small>The story
                  </a>
                )}
                {index < pages.length - 1 && (
                  <a href={`#/docs/${pages[index + 1].id}`}>
                    <small>NEXT CHAPTER →</small>
                    {pages[index + 1].title}
                  </a>
                )}
              </div>
            </>
          ) : (
            <>
              <h1>Tape not found.</h1>
              <p>This documentation chapter doesn’t exist.</p>
              <a className="story-button" href="#/docs">
                Back to the field guide ↗
              </a>
            </>
          )}
        </main>
        {page && (
          <aside className="docs-toc">
            <div className="eyebrow">ON THIS PAGE</div>
            {page.sections.map((s, i) => (
              <button
                key={s.title}
                onClick={() =>
                  document
                    .getElementById(`section-${i}`)
                    .scrollIntoView({
                      behavior: matchMedia("(prefers-reduced-motion: reduce)")
                        .matches
                        ? "instant"
                        : "smooth",
                    })
                }
              >
                {s.title.replace(/^\d+\. /, "")}
              </button>
            ))}
          </aside>
        )}
      </div>
    </div>
  );
}
