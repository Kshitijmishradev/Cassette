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

function PlaceholderPage({ name, mode }) {
  return (
    <main className="page">
      <PageHeader eyebrow={name} title={mode === 'diff' ? 'Trajectory diff' : 'Run waterfall'} />
      <Loading />
    </main>
  )
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
  else if (parts[0] === 'runs' && parts[1]) page = <PlaceholderPage name={parts[1]} mode={parts[2] || 'waterfall'} />
  else page = <RunsPage suite={suite.data} />

  return <Shell suite={suite.data} route={route}>{page}</Shell>
}
