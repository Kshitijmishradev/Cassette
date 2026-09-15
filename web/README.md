# Cassette website and web UI

`cassette serve` starts a local UI for exploring recorded runs and reading
trajectory diffs. This directory is the frontend. It is built with Vite and
embedded into the Go binary with `go:embed`, so the shipped tool stays one
file with no node runtime.

**Read [../API.md](../API.md) first.** It is the contract, and it explains the
parts of the data that are easy to render wrongly.

---

## Public website

The root route is a tape-inspired project introduction with three story chapters:
record, replay, and understand. `#/docs` contains the searchable field guide.
Existing `#/runs`, `#/runs/<name>/diff`, waterfall, and `#/grid` links still open
the trace viewer. `cassette serve --open` opens `#/runs` directly.

- `src/Story.jsx` — story chapters, CSS cassette artwork, interactive verdict example.
- `src/Docs.jsx` — documentation chapters, search, and copyable commands.
- `src/story.css` — website styles, responsive layouts, and reduced-motion support.
- `src/Root.jsx` — website/viewer hash routing.

The illustrative refund experiment is separate from the real fixture demo.
No extra runtime dependencies or external art/font requests are required.

### Cloudflare Pages

The `.github/workflows/demo.yml` builds `web/site-dist` with `npm run build:site`
and adds the reviewed fixture API to the deployment artifact. The Go binary
embeds only the viewer in `web/dist`, with no fixture API or storytelling pages.
After viewer edits, run `npm run build --prefix web` and commit the regenerated bundle. A push to
`main` (or a manual workflow run) deploys to `cassette-agent-replay` when the
Cloudflare secrets are configured. Local edits alone do not publish the site.

## Setup

```sh
cd web
npm install
npm run dev        # dev server, serving ../web/fixtures as the API
npm run build      # viewer -> web/dist, which the Go binary embeds
npm run build:site # website -> web/site-dist, for Cloudflare
npm run dev:viewer # viewer-only development
```

Fixtures are real data from a reviewed demonstration suite, committed to the repo.
Export into a fresh directory, review the data, then replace the fixtures you
intend to publish. Exports refuse nonempty API destinations to avoid stale data.
Nothing needs to be running. Generate a fresh export with:

```sh
cassette export --fixtures /tmp/cassette-fixtures-new --suite ./cassettes
```

---

## Screens

Four. In priority order, because if only one is polished it should be the
third.

### 1. Runs list

Sortable table over `suite.json`: name, verdict, steps, tool calls, duration,
size, recorded-at. Navigation, deliberately plain.

### 2. Run waterfall

`run.json` as a timeline. Each step a bar positioned at `offsetMs` and sized
by `durationMs`. Click a step to fetch `calls/<i>.json` and show the bodies.

**The badge on each call showing its match tier is the differentiator made
visible.** No other tool can tell you a call was served by a loose match. Do
not bury it.

Notifications have `durationMs: 0` and need a tick or marker rather than a
zero-width bar.

### 3. Trajectory diff — the hero

`diff.json` as two aligned columns, baseline left, candidate right. This is
the screen that goes in the README gif and the one a stranger will judge the
project by.

- `match` — both cells, quiet
- `substitute` — both cells, marked as changed
- `insert` — empty left cell
- `delete` — empty right cell
- `write: true` on any of the above — **visually distinct**, because write
  differences are what decide the verdict

Collapse runs of consecutive matches with an expandable "N identical calls"
marker.

The summary line matters as much as the rows: steps vs steps, the delta, and
the verdict.

### 4. Suite grid

Every run as a cell, colored by verdict, click through to its diff. This is
where the aggregate number lives.

---

## What not to build

**No metrics dashboard.** No time-series charts of latency or token counts.
That space is saturated (Dynatrace, SigNoz and Dash0 all shipped Claude Code
dashboards in 2026) and building one would make this look like a clone of
better-resourced tools. The differentiator is replay and diffing.

If a chart genuinely helps a screen above, it is allowed. A charts *section*
is not.

---

## Design direction

Dense, dark, monospace-forward. Closer to a profiler or a trace viewer than to
a marketing dashboard. The people using this are reading tool call sequences,
not glancing at KPIs.

- Define colors as CSS custom properties in one place, not scattered literals
- Support light and dark; dark is the primary
- Monospace for anything that is data: tool names, arguments, ids, durations
- Restraint with color. Verdict and write-difference are the two things that
  earn it; everything else is grayscale
- No animation that delays reading

---

## Constraints

- **The API is fixed.** If a screen needs data the contract does not carry,
  say so rather than inventing an endpoint; the Go side has to provide it and
  the static export has to be able to dump it.
- **Static-compatible.** Only `fetch` of the documented relative paths. No
  query parameters, no POST, no websockets. The public demo is a static host
  with no backend.
- **`suite.live === false`** means no backend: hide anything that would need
  one.
- **Handle a missing `diff.json`.** A run that was never replayed has none.
- Keep the dependency list short and justified. The Go side has zero
  dependencies; the frontend does not have to match that, but every addition
  should earn its place.
