import { useEffect, useMemo, useState } from 'react'

const VERDICTS = ['identical', 'drift', 'changed', 'unknown']

function useHashRoute() {
  const read = () => window.location.hash.slice(1) || '/runs'
  const [route, setRoute] = useState(read)

  useEffect(() => {
    const onHash = () => setRoute(read())
    window.addEventListener('hashchange', onHash)
    if (!window.location.hash) window.location.hash = '/runs'
    return () => window.removeEventListener('hashchange', onHash)
  }, [])

  const parts = route.split('/').filter(Boolean).map(decodeURIComponent)
  return { route, parts }
}

function useJson(path) {
  const [state, setState] = useState({ data: null, error: null, loading: true })

  useEffect(() => {
    if (!path) {
      setState({ data: null, error: null, loading: false })
      return undefined
    }
    const controller = new AbortController()
    setState({ data: null, error: null, loading: true })
    fetch(path, { signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) {
          const error = new Error(`Request failed (${response.status})`)
          error.status = response.status
          throw error
        }
        return response.json()
      })
      .then((data) => setState({ data, error: null, loading: false }))
      .catch((error) => {
        if (error.name !== 'AbortError') setState({ data: null, error, loading: false })
      })
    return () => controller.abort()
  }, [path])

  return state
}

function href(path) {
  return `#${path}`
}

function VerdictBadge({ verdict, compact = false }) {
  const value = VERDICTS.includes(verdict) ? verdict : 'unknown'
  return (
    <span className={`verdict verdict--${value}${compact ? ' verdict--compact' : ''}`}>
      <span className="verdict__dot" aria-hidden="true" />
      {value}
    </span>
  )
}

function ThemeToggle() {
  const initial = document.documentElement.dataset.theme || 'dark'
  const [theme, setTheme] = useState(initial)

  const toggle = () => {
    const next = theme === 'dark' ? 'light' : 'dark'
    document.documentElement.dataset.theme = next
    localStorage.setItem('cassette-theme', next)
    setTheme(next)
  }

  return (
    <button className="icon-button" type="button" onClick={toggle} title={`Use ${theme === 'dark' ? 'light' : 'dark'} theme`}>
      <span aria-hidden="true">{theme === 'dark' ? '☼' : '◐'}</span>
      <span className="sr-only">Use {theme === 'dark' ? 'light' : 'dark'} theme</span>
    </button>
  )
}

function Shell({ suite, route, children }) {
  const isGrid = route.startsWith('/grid')
  return (
    <div className="app-shell">
      <header className="topbar">
        <a className="brand" href={href('/runs')} aria-label="Cassette runs">
          <span className="brand__reel" aria-hidden="true"><i /><i /></span>
          <span>Cassette</span>
        </a>
        <nav className="topnav" aria-label="Primary navigation">
          <a className={!isGrid ? 'is-active' : ''} href={href('/runs')}>runs</a>
          <a className={isGrid ? 'is-active' : ''} href={href('/grid')}>suite grid</a>
        </nav>
        <div className="topbar__meta">
          {suite && <span className={`connection ${suite.live ? 'connection--live' : ''}`}><i />{suite.live ? 'live' : 'snapshot'}</span>}
          <ThemeToggle />
        </div>
      </header>
      {children}
      {suite && (
        <footer className="footer">
          <span>cassette <b>{suite.cassette}</b></span>
          <span>{suite.live ? 'live session' : `generated ${formatDate(suite.generated, true)}`}</span>
        </footer>
      )}
    </div>
  )
}

function Loading({ label = 'Loading trace data' }) {
  return <div className="state-panel"><span className="loader" aria-hidden="true" />{label}…</div>
}

function ErrorState({ title = 'Could not load data', error }) {
  return (
    <div className="state-panel state-panel--error">
      <span className="state-panel__glyph" aria-hidden="true">!</span>
      <div><strong>{title}</strong><p>{error?.message || 'An unexpected error occurred.'}</p></div>
    </div>
  )
}

function PageHeader({ eyebrow, title, description, actions }) {
  return (
    <div className="page-heading">
      <div>
        <div className="eyebrow">{eyebrow}</div>
        <h1>{title}</h1>
        {description && <p>{description}</p>}
      </div>
      {actions && <div className="page-heading__actions">{actions}</div>}
    </div>
  )
}

function TotalsStrip({ totals }) {
  return (
    <section className="totals-strip" aria-label="Suite totals">
      <div className="total total--primary"><span>runs</span><strong>{totals.runs}</strong></div>
      <div className="total"><span className="total__key total__key--identical">identical</span><strong>{totals.identical}</strong></div>
      <div className="total"><span className="total__key total__key--drift">drift</span><strong>{totals.drift}</strong></div>
      <div className="total"><span className="total__key total__key--changed">changed</span><strong>{totals.changed}</strong></div>
      <div className="total"><span className="total__key total__key--unknown">unknown</span><strong>{totals.unknown}</strong></div>
      <div className="total total--secondary"><span>messages</span><strong>{totals.messages}</strong></div>
      <div className="total total--secondary"><span>tool calls</span><strong>{totals.toolCalls}</strong></div>
      <div className="total total--secondary"><span>errors</span><strong>{totals.errors}</strong></div>
    </section>
  )
}

const columns = [
  { key: 'name', label: 'Run' },
  { key: 'verdict', label: 'Verdict' },
  { key: 'messages', label: 'Steps', numeric: true },
  { key: 'toolCalls', label: 'Tools', numeric: true },
  { key: 'durationMs', label: 'Duration', numeric: true },
  { key: 'bytes', label: 'Size', numeric: true },
  { key: 'recordedAt', label: 'Recorded', numeric: true },
]

function RunsTable({ runs }) {
  const [sort, setSort] = useState({ key: 'recordedAt', direction: 'desc' })
  const sorted = useMemo(() => [...runs].sort((a, b) => {
    const aValue = a[sort.key]
    const bValue = b[sort.key]
    const order = typeof aValue === 'string' ? aValue.localeCompare(bValue) : aValue - bValue
    return sort.direction === 'asc' ? order : -order
  }), [runs, sort])

  const setSortKey = (key) => setSort((current) => ({
    key,
    direction: current.key === key && current.direction === 'asc' ? 'desc' : 'asc',
  }))

  return (
    <div className="table-frame">
      <table className="runs-table">
        <thead><tr>{columns.map((column) => (
          <th key={column.key} className={column.numeric ? 'cell--number' : ''} aria-sort={sort.key === column.key ? `${sort.direction}ending` : 'none'}>
            <button type="button" onClick={() => setSortKey(column.key)}>
              {column.label}<span className={sort.key === column.key ? 'sort sort--active' : 'sort'} aria-hidden="true">{sort.key === column.key && sort.direction === 'desc' ? '↓' : '↑'}</span>
            </button>
          </th>
        ))}</tr></thead>
        <tbody>{sorted.map((run) => (
          <tr key={run.name}>
            <td><a className="run-link" href={href(`/runs/${encodeURIComponent(run.name)}/waterfall`)}><span>{run.name}</span><small>{shortAgent(run.agent)}</small></a></td>
            <td><a className="plain-link" href={href(`/runs/${encodeURIComponent(run.name)}/diff`)}><VerdictBadge verdict={run.verdict} compact /></a></td>
            <td className="cell--number">{run.messages}</td>
            <td className="cell--number">{run.toolCalls}</td>
            <td className="cell--number">{formatDuration(run.durationMs)}</td>
            <td className="cell--number">{formatBytes(run.bytes)}</td>
            <td className="cell--number timestamp">{formatDate(run.recordedAt)}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function RunsPage({ suite }) {
  return (
    <main className="page">
      <PageHeader eyebrow="Replay index" title="Recorded runs" description={`${suite.runs.length} trajectories in the current cassette build`} />
      <TotalsStrip totals={suite.totals} />
      <RunsTable runs={suite.runs} />
    </main>
  )
}

function GridPage({ suite }) {
  const replayed = suite.totals.runs - suite.totals.unknown
  return (
    <main className="page">
      <PageHeader
        eyebrow="Suite overview"
        title="Replay verdict grid"
        description={`${replayed} of ${suite.totals.runs} runs replayed · select a cell to inspect its trajectory diff`}
        actions={<div className="legend">{VERDICTS.map((verdict) => <span key={verdict}><i className={`legend__dot legend__dot--${verdict}`} />{verdict}</span>)}</div>}
      />
      <TotalsStrip totals={suite.totals} />
      <section className="suite-grid" aria-label="Runs by verdict">
        {suite.runs.map((run, index) => (
          <a key={run.name} className={`grid-cell grid-cell--${run.verdict}`} href={href(`/runs/${encodeURIComponent(run.name)}/diff`)}>
            <span className="grid-cell__index">{String(index + 1).padStart(2, '0')}</span>
            <span className="grid-cell__name">{run.name}</span>
            <VerdictBadge verdict={run.verdict} compact />
            <span className="grid-cell__meta">{run.messages} steps · {formatDuration(run.durationMs)}</span>
            <span className="grid-cell__arrow" aria-hidden="true">↗</span>
          </a>
        ))}
      </section>
    </main>
  )
}

function RunTabs({ name, active }) {
  const encoded = encodeURIComponent(name)
  return (
    <nav className="run-tabs" aria-label={`${name} views`}>
      <a className={active === 'waterfall' ? 'is-active' : ''} href={href(`/runs/${encoded}/waterfall`)}>waterfall</a>
      <a className={active === 'diff' ? 'is-active' : ''} href={href(`/runs/${encoded}/diff`)}>trajectory diff</a>
    </nav>
  )
}

function TierBadge({ tier }) {
  const label = tier || 'not replayed'
  return <span className={`tier tier--${tier || 'empty'}`} title={tier ? `Matched at ${tier} tier` : 'No replay match data'}><i />{label}</span>
}

function WaterfallPage({ name }) {
  const runState = useJson(`/api/runs/${encodeURIComponent(name)}/run.json`)
  const [selected, setSelected] = useState(null)

  useEffect(() => setSelected(null), [name])

  if (runState.loading) return <main className="page"><Loading label={`Loading ${name}`} /></main>
  if (runState.error) return <main className="page"><ErrorState title={`Could not load ${name}`} error={runState.error} /></main>

  const run = runState.data
  const endMs = Math.max(1, ...run.steps.map((step) => step.offsetMs + step.durationMs))
  const ticks = [0, 0.25, 0.5, 0.75, 1]
  return (
    <main className="page page--run">
      <PageHeader
        eyebrow="Run trajectory"
        title={run.name}
        description={`${run.steps.length} steps across ${run.tapes.length} ${run.tapes.length === 1 ? 'tape' : 'tapes'} · ${formatDate(run.recordedAt, true)}`}
        actions={<VerdictBadge verdict={run.verdict} />}
      />
      <RunTabs name={name} active="waterfall" />
      <section className="run-facts" aria-label="Run details">
        <div><span>duration</span><b>{formatDuration(endMs)}</b></div>
        <div><span>agent</span><b title={run.agent.join(' ')}>{shortAgent(run.agent)}</b></div>
        <div><span>cassette</span><b>{run.cassette}</b></div>
        <div><span>tape state</span><b>{run.tapes.every((tape) => tape.complete) ? 'complete' : 'incomplete'}</b></div>
      </section>
      <div className={`waterfall-layout${selected !== null ? ' has-inspector' : ''}`}>
        <section className="waterfall" aria-label="Request timeline">
          <div className="waterfall__head">
            <span>call / match tier</span>
            <div className="time-axis">{ticks.map((tick) => <span key={tick} style={{ left: `${tick * 100}%` }}>{formatDuration(endMs * tick)}</span>)}</div>
          </div>
          <div className="waterfall__rows">
            {run.steps.map((step) => {
              const left = (step.offsetMs / endMs) * 100
              const width = Math.max((step.durationMs / endMs) * 100, 0.8)
              const isNotification = step.durationMs === 0
              return (
                <button
                  className={`waterfall-row${selected === step.index ? ' is-selected' : ''}${step.isError ? ' is-error' : ''}`}
                  key={step.index}
                  type="button"
                  onClick={() => setSelected(step.index)}
                >
                  <span className="step-label">
                    <span className="step-label__index">{String(step.index).padStart(2, '0')}</span>
                    <span className="step-label__call">
                      <b>{step.tool || step.method}</b>
                      <small>{step.tool ? step.method : step.args || (step.serverInitiated ? 'server initiated' : 'protocol')}</small>
                    </span>
                    <TierBadge tier={step.tier} />
                  </span>
                  <span className="step-track">
                    {ticks.map((tick) => <i className="track-line" key={tick} style={{ left: `${tick * 100}%` }} />)}
                    <span
                      className={`step-bar step-bar--${step.class}${isNotification ? ' step-bar--notification' : ''}`}
                      style={{ left: `${left}%`, width: isNotification ? undefined : `${width}%` }}
                    >
                      <span className="sr-only">starts at {formatDuration(step.offsetMs)}, lasts {formatDuration(step.durationMs)}</span>
                    </span>
                    <span className="step-duration">{isNotification ? 'notification' : formatDuration(step.durationMs)}</span>
                  </span>
                </button>
              )
            })}
          </div>
        </section>
        {selected !== null && (
          <CallInspector name={name} step={run.steps.find((item) => item.index === selected)} onClose={() => setSelected(null)} />
        )}
      </div>
    </main>
  )
}

function CallInspector({ name, step, onClose }) {
  const callState = useJson(`/api/runs/${encodeURIComponent(name)}/calls/${step.index}.json`)
  return (
    <aside className="inspector" aria-label={`Call ${step.index} details`}>
      <div className="inspector__head">
        <div><span>call {String(step.index).padStart(2, '0')}</span><strong>{step.tool || step.method}</strong></div>
        <button type="button" onClick={onClose} aria-label="Close call details">×</button>
      </div>
      <div className="inspector__badges"><TierBadge tier={step.tier} /><span className={`class-badge class-badge--${step.class}`}>{step.class}</span>{step.isError && <span className="error-badge">error</span>}</div>
      <dl className="inspector__meta">
        <div><dt>offset</dt><dd>{formatDuration(step.offsetMs)}</dd></div>
        <div><dt>duration</dt><dd>{step.durationMs === 0 ? 'notification' : formatDuration(step.durationMs)}</dd></div>
        <div><dt>request</dt><dd>{formatBytes(step.reqBytes)}</dd></div>
        <div><dt>response</dt><dd>{step.hasResponse ? formatBytes(step.respBytes) : 'none'}</dd></div>
      </dl>
      {callState.loading && <Loading label="Loading bodies" />}
      {callState.error && <ErrorState title="Could not load call bodies" error={callState.error} />}
      {callState.data && (
        <div className="payloads">
          <Payload label="request" value={callState.data.request} />
          <Payload label="response" value={callState.data.response} emptyLabel="No response — notification" />
        </div>
      )}
    </aside>
  )
}

function Payload({ label, value, emptyLabel = 'Empty body' }) {
  let display = value
  if (value) {
    try { display = JSON.stringify(JSON.parse(value), null, 2) } catch { display = value.trim() }
  }
  return (
    <section className="payload">
      <div className="payload__label">{label}</div>
      {display ? <pre>{display}</pre> : <div className="payload__empty">{emptyLabel}</div>}
    </section>
  )
}

function groupDiffPairs(pairs) {
  const rows = []
  let index = 0
  while (index < pairs.length) {
    if (pairs[index].op !== 'match') {
      rows.push({ type: 'pair', pair: pairs[index], index })
      index += 1
      continue
    }
    const start = index
    while (index < pairs.length && pairs[index].op === 'match') index += 1
    const matched = pairs.slice(start, index)
    if (matched.length > 1) rows.push({ type: 'group', pairs: matched, start })
    else rows.push({ type: 'pair', pair: matched[0], index: start })
  }
  return rows
}

function DiffPage({ name, summary }) {
  const diffState = useJson(`/api/runs/${encodeURIComponent(name)}/diff.json`)
  const [expanded, setExpanded] = useState({})

  useEffect(() => setExpanded({}), [name])

  if (diffState.loading) return <main className="page page--run"><Loading label={`Aligning ${name}`} /></main>
  if (diffState.error?.status === 404 || summary?.verdict === 'unknown') {
    return <MissingDiff name={name} summary={summary} />
  }
  if (diffState.error) return <main className="page page--run"><ErrorState title={`Could not load the ${name} diff`} error={diffState.error} /></main>

  const diff = diffState.data
  const rows = groupDiffPairs(diff.pairs)
  return (
    <main className="page page--run page--diff">
      <PageHeader
        eyebrow="Trajectory comparison"
        title={diff.name}
        description={`${diff.baselineName} aligned against ${diff.candidateName}`}
        actions={<VerdictBadge verdict={diff.verdict} />}
      />
      <RunTabs name={name} active="diff" />
      <DiffSummary diff={diff} />
      <section className="diff-frame" aria-label={`${diff.name} trajectory diff`}>
        <div className="diff-head">
          <div><span>baseline</span><strong>{diff.baselineName}</strong></div>
          <div className="diff-head__axis"><span>alignment</span></div>
          <div><span>candidate</span><strong>{diff.candidateName}</strong></div>
        </div>
        <div className="diff-body">
          {rows.map((row) => {
            if (row.type === 'pair') return <DiffPairRow key={`pair-${row.index}`} pair={row.pair} index={row.index} />
            const isOpen = Boolean(expanded[row.start])
            return (
              <div className="match-group" key={`group-${row.start}`}>
                <button type="button" aria-expanded={isOpen} onClick={() => setExpanded((current) => ({ ...current, [row.start]: !isOpen }))}>
                  <span className="match-group__line" />
                  <span className="match-group__label"><i aria-hidden="true">{isOpen ? '−' : '+'}</i>{row.pairs.length} identical calls</span>
                  <span className="match-group__line" />
                </button>
                {isOpen && row.pairs.map((pair, offset) => <DiffPairRow key={`pair-${row.start + offset}`} pair={pair} index={row.start + offset} />)}
              </div>
            )
          })}
        </div>
      </section>
    </main>
  )
}

function MissingDiff({ name, summary }) {
  return (
    <main className="page page--run">
      <PageHeader eyebrow="Trajectory comparison" title={name} actions={<VerdictBadge verdict="unknown" />} />
      <RunTabs name={name} active="diff" />
      <section className="missing-diff">
        <div className="missing-diff__mark" aria-hidden="true">∅</div>
        <div>
          <span>comparison unavailable</span>
          <h2>This run has not been replayed.</h2>
          <p>No <code>diff.json</code> exists yet. Unknown is intentionally separate from a passing result.</p>
          {summary && <a href={href(`/runs/${encodeURIComponent(name)}/waterfall`)}>inspect the recorded trajectory →</a>}
        </div>
      </section>
    </main>
  )
}

function DiffSummary({ diff }) {
  const delta = diff.stepDelta > 0 ? `+${diff.stepDelta}` : String(diff.stepDelta)
  return (
    <section className="diff-summary" aria-label="Diff summary">
      <div className="diff-summary__trajectory">
        <div><span>{diff.baselineName}</span><strong>{diff.baselineSteps}</strong><small>steps</small></div>
        <div className={`delta${diff.stepDelta === 0 ? ' delta--zero' : ''}`}><span>Δ</span><b>{delta}</b></div>
        <div><span>{diff.candidateName}</span><strong>{diff.candidateSteps}</strong><small>steps</small></div>
      </div>
      <div className="diff-counts">
        <div><span>matched</span><strong>{diff.matched}</strong></div>
        <div><span>substituted</span><strong>{diff.substituted}</strong></div>
        <div><span>inserted</span><strong>{diff.inserted}</strong></div>
        <div><span>deleted</span><strong>{diff.deleted}</strong></div>
        <div><span>missed</span><strong>{diff.missed}</strong></div>
      </div>
    </section>
  )
}

const OP_LABELS = {
  match: { glyph: '=', label: 'match' },
  substitute: { glyph: '≠', label: 'changed' },
  insert: { glyph: '→', label: 'inserted' },
  delete: { glyph: '←', label: 'deleted' },
}

function DiffPairRow({ pair, index }) {
  const operation = OP_LABELS[pair.op] || OP_LABELS.substitute
  const isDifference = pair.op !== 'match'
  return (
    <div className={`diff-row diff-row--${pair.op}${isDifference && pair.write ? ' diff-row--write' : ''}`}>
      <DiffCallCell call={pair.baseline} side="baseline" index={index} />
      <div className="diff-op">
        <span aria-hidden="true">{operation.glyph}</span>
        <small>{pair.write && isDifference ? 'write' : operation.label}</small>
      </div>
      <DiffCallCell call={pair.candidate} side="candidate" index={index} />
    </div>
  )
}

function DiffCallCell({ call, side, index }) {
  if (!call) {
    return <div className="diff-cell diff-cell--empty"><span>{side === 'baseline' ? 'not in baseline' : 'not in candidate'}</span></div>
  }
  return (
    <div className="diff-cell">
      <span className="diff-cell__index">{String(index + 1).padStart(2, '0')}</span>
      <div className="diff-cell__content">
        <div className="diff-cell__title">
          <strong>{call.tool || call.method}</strong>
          {call.tool && <span>{call.method}</span>}
        </div>
        {call.args && <code>{prettyArgs(call.args)}</code>}
        <div className="diff-cell__meta">
          <span className={`class-token class-token--${call.class}`}>{call.class === 'write' ? 'W' : 'R'} · {call.class}</span>
          {call.tier && <TierBadge tier={call.tier} />}
          {call.missed && <span className="missed-token">missed</span>}
        </div>
      </div>
    </div>
  )
}

function prettyArgs(value) {
  try {
    const parsed = JSON.parse(value)
    return JSON.stringify(parsed).replaceAll(',', ', ').replaceAll(':', ': ')
  } catch {
    return value
  }
}

function formatDuration(value) {
  if (value < 1) return `${Math.round(value * 1000)}µs`
  if (value < 1000) return `${value.toFixed(value < 10 ? 2 : 1)}ms`
  return `${(value / 1000).toFixed(2)}s`
}

function formatBytes(value) {
  if (value < 1024) return `${value}B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)}KB`
  return `${(value / (1024 * 1024)).toFixed(1)}MB`
}

function formatDate(value, withYear = false) {
  if (!value) return '—'
  const date = new Date(value)
  const options = { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }
  if (withYear) options.year = 'numeric'
  return new Intl.DateTimeFormat(undefined, options).format(date)
}

function shortAgent(value) {
  if (!value) return 'unknown agent'
  const parts = Array.isArray(value) ? value : value.split(' ')
  const npxIndex = parts.indexOf('npx')
  return npxIndex >= 0 ? parts.slice(npxIndex).join(' ') : parts.slice(-4).join(' ')
}

export default function App() {
  const { route, parts } = useHashRoute()
  const suite = useJson('/api/suite.json')

  useEffect(() => {
    const saved = localStorage.getItem('cassette-theme')
    document.documentElement.dataset.theme = saved || 'dark'
  }, [])

  if (suite.loading) return <Loading label="Loading cassette index" />
  if (suite.error) return <ErrorState title="Could not load the cassette index" error={suite.error} />

  let page
  if (parts[0] === 'grid') page = <GridPage suite={suite.data} />
  else if (parts[0] === 'runs' && parts[1] && parts[2] === 'diff') page = <DiffPage name={parts[1]} summary={suite.data.runs.find((run) => run.name === parts[1])} />
  else if (parts[0] === 'runs' && parts[1]) page = <WaterfallPage name={parts[1]} />
  else page = <RunsPage suite={suite.data} />

  return <Shell suite={suite.data} route={route}>{page}</Shell>
}
