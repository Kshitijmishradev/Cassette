# API contract

One contract, two servers. `cassette serve` answers these over HTTP;
`cassette export --static` writes identical JSON to identical paths. The
frontend fetches the same relative URLs either way and cannot tell which it is
talking to.

That constraint shapes the design: **every endpoint ends in `.json` and takes
no query parameters**, because a static host serves files, not routes. An
endpoint needing `?filter=` would work live and silently break on the public
demo, which is the deployment a stranger actually sees.

Go types: [`internal/api/types.go`](./internal/api/types.go). That file is the
source of truth; this page is the summary.

---

## Endpoints

| path | returns |
|---|---|
| `GET /api/suite.json` | `Suite` — every run, verdicts, totals |
| `GET /api/runs/<name>/run.json` | `Run` — one run's merged trajectory |
| `GET /api/runs/<name>/diff.json` | `Diff` — trajectory comparison |
| `GET /api/runs/<name>/calls/<i>.json` | `CallBodies` — full request and response |

`<name>` is the cassette directory name. `<i>` is a step index from
`Run.steps[].index`.

`diff.json` is **absent** (404 live, missing file statically) for a run that
has never been replayed. That is deliberate: writing an empty diff would make
the UI show a comparison that never happened. Handle the absence.

---

## Fixtures

Real data, generated from a real suite, committed so the frontend needs
nothing running:

```sh
cassette export --fixtures web/fixtures --suite ./cassettes
```

`web/fixtures/api/...` mirrors the live paths exactly. Point the dev server's
static root at `web/fixtures` and every `fetch('/api/...')` resolves.

The committed fixture set covers the cases that matter: 9 identical runs,
3 changed, one of which has a deleted write call.

---

## Types, and the parts worth knowing

### `Suite`

`live` is `true` from `cassette serve`, `false` from a static export. Use it to
hide controls that cannot work without a backend.

`generated` lets a static demo say how old it is rather than pretending to be
live.

### Verdicts

`identical` | `drift` | `changed` | `unknown`

**`unknown` is not a pass.** It means the cassette has never been replayed.
Rendering it as green would inflate the apparent pass rate, which is the exact
mistake the ClickHouse export made once already.

`drift` means the same write-class calls reached by a different path. It is
the most interesting verdict and should not be styled as a minor variant of
`identical`.

### `Run.steps`

Ordered by **when the request was sent**, merged across every server. Not by
tape position: the recorder writes an entry when the *response* arrives, so a
fast call started second can sit ahead of a slow one started first.

`offsetMs` is milliseconds from the run's first message, which is what a
waterfall lays out against. `durationMs` is `0` for notifications, which never
get a response.

`class` is `read` or `write`. Write calls are what decide a verdict.

`tier` is how replay matched that call: `exact`, `norm`, `method`, `fuzzy`, or
`miss`. **Empty on a run that has only been recorded**, never replayed. A run
served mostly by loose tiers is a weaker result than one served exactly, so the
tier belongs on screen rather than buried.

`args` is a truncated preview. Full bodies come from the calls endpoint, so a
run with a thousand large payloads still loads as one small document.

### `Diff.pairs`

One entry per aligned position.

- `op: "match"` — both sides, same call
- `op: "substitute"` — both sides, different call
- `op: "insert"` — `baseline` is `null`; only the candidate made this call
- `op: "delete"` — `candidate` is `null`; only the baseline made it

`write: true` means either side is a write-class call. **Write differences
decide the verdict and must be visually distinct from read differences.** In
the terminal renderer they are `!` where reads are `~`, `>` or `<`.

Runs of consecutive `match` pairs should be collapsible. A hundred identical
reads between two differences is noise, and a diff nobody reads to the end is
not doing its job.

---

## Errors

Non-2xx responses carry `{"error": "..."}`. Static exports have no error
shape; a missing file is the signal.
