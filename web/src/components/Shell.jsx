import { href, formatDate } from "../lib/format";
import { ThemeToggle } from "./ThemeToggle";

export function Shell({ suite, route, children }) {
  const isGrid = route.startsWith("/grid");
  return (
    <div className="app-shell">
      <header className="topbar">
        <a className="brand" href={href("/runs")} aria-label="Cassette runs">
          <span className="brand__reel" aria-hidden="true">
            <i />
            <i />
          </span>
          <span>Cassette</span>
        </a>
        <nav className="topnav" aria-label="Primary navigation">
          <a href={import.meta.env.MODE === "site" ? "#/story" : "https://cassette-agent-replay.pages.dev/#/story"}>story</a>
          <a href={import.meta.env.MODE === "site" ? "#/docs" : "https://cassette-agent-replay.pages.dev/#/docs"}>docs</a>
          <a className={!isGrid ? "is-active" : ""} href={href("/runs")}>
            runs
          </a>
          <a className={isGrid ? "is-active" : ""} href={href("/grid")}>
            suite grid
          </a>
        </nav>
        <div className="topbar__meta">
          {suite && (
            <span
              className={`connection ${suite.live ? "connection--live" : ""}`}
            >
              <i />
              {suite.live ? "live" : "snapshot"}
            </span>
          )}
          <ThemeToggle />
        </div>
      </header>
      {children}
      {suite && (
        <footer className="footer">
          <span>
            cassette <b>{suite.cassette}</b>
          </span>
          <span>
            {suite.live
              ? "live session"
              : `generated ${formatDate(suite.generated, true)}`}
          </span>
        </footer>
      )}
    </div>
  );
}
