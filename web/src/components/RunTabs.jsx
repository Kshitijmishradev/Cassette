import { href } from "../lib/format";

export function RunTabs({ name, active }) {
  const encoded = encodeURIComponent(name);
  return (
    <nav className="run-tabs" aria-label={`${name} views`}>
      <a
        className={active === "waterfall" ? "is-active" : ""}
        href={href(`/runs/${encoded}/waterfall`)}
      >
        waterfall
      </a>
      <a
        className={active === "diff" ? "is-active" : ""}
        href={href(`/runs/${encoded}/diff`)}
      >
        trajectory diff
      </a>
    </nav>
  );
}
