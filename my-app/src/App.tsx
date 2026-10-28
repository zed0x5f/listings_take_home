import { useEffect, useState, type SubmitEvent } from 'react'
import { ApiError, searchListings, type FieldError, type SearchFilters, type SearchResult } from './api'
import './App.css'

const emptyFilters: SearchFilters = {
  minPrice: '',
  maxPrice: '',
  minBedrooms: '',
  city: '',
  keyword: '',
  targetBudget: '',
  pageSize: '5',
}

const currency = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 })

type Status =
  | { kind: 'loading' }
  | { kind: 'error'; message: string; fields: FieldError[] }
  | { kind: 'done'; data: SearchResult }

function App() {
  // `draft` is what's in the form; `query` is what was last submitted. Paging
  // reuses `query` so editing the form doesn't silently change the results.
  const [draft, setDraft] = useState<SearchFilters>(emptyFilters)
  const [query, setQuery] = useState<SearchFilters>(emptyFilters)
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<Status>({ kind: 'loading' })

  useEffect(() => {
    const controller = new AbortController()
    setStatus({ kind: 'loading' })
    searchListings(query, page, controller.signal)
      .then((data) => setStatus({ kind: 'done', data }))
      .catch((err: unknown) => {
        if (controller.signal.aborted) return
        if (err instanceof ApiError) {
          setStatus({ kind: 'error', message: err.message, fields: err.fields })
        } else {
          setStatus({ kind: 'error', message: 'Could not reach the server.', fields: [] })
        }
      })
    return () => controller.abort()
  }, [query, page])

  function onSubmit(e: SubmitEvent<HTMLFormElement>) {
    e.preventDefault()
    setQuery({ ...draft })
    setPage(1)
  }

  function onReset() {
    setDraft(emptyFilters)
    setQuery(emptyFilters)
    setPage(1)
  }

  const fieldErrors = status.kind === 'error' ? status.fields : []
  const errorFor = (field: keyof SearchFilters | 'page') =>
    fieldErrors.find((f) => f.field === field)?.message

  const input = (field: keyof SearchFilters, label: string, type: 'text' | 'number' = 'number') => (
    <label className="field">
      <span>{label}</span>
      <input
        type={type}
        value={draft[field]}
        onChange={(e) => setDraft({ ...draft, [field]: e.target.value })}
        aria-invalid={errorFor(field) ? true : undefined}
      />
      {errorFor(field) && <small className="field-error">{errorFor(field)}</small>}
    </label>
  )

  return (
    <main>
      <h1>Listing Search</h1>

      <form onSubmit={onSubmit} className="filters">
        {input('minPrice', 'Min price')}
        {input('maxPrice', 'Max price')}
        {input('minBedrooms', 'Min bedrooms')}
        {input('city', 'City', 'text')}
        {input('keyword', 'Keyword', 'text')}
        {input('targetBudget', 'Target budget')}
        {input('pageSize', 'Page size')}
        <div className="actions">
          <button type="submit">Search</button>
          <button type="button" onClick={onReset}>
            Reset
          </button>
        </div>
      </form>

      {status.kind === 'loading' && <p role="status">Loading…</p>}

      {status.kind === 'error' && (
        <div role="alert" className="error">
          <p>{status.message}</p>
          {errorFor('page') && <p>{errorFor('page')}</p>}
        </div>
      )}

      {status.kind === 'done' && status.data.total === 0 && <p>No listings match your search.</p>}

      {status.kind === 'done' && status.data.total > 0 && (
        <Results data={status.data} onPage={setPage} />
      )}
    </main>
  )
}

function Results({ data, onPage }: { data: SearchResult; onPage: (page: number) => void }) {
  const first = (data.page - 1) * data.pageSize + 1
  const last = first + data.results.length - 1
  return (
    <section>
      <p>
        Showing {first}–{last} of {data.total}
      </p>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>#</th>
              <th>Address</th>
              <th>City</th>
              <th>Price</th>
              <th>Beds</th>
              <th>Baths</th>
              <th>Listed</th>
              <th>Source</th>
              <th>Status</th>
              <th>Description</th>
              <th>Score</th>
            </tr>
          </thead>
          <tbody>
            {data.results.map((l, i) => (
              <tr key={`${l.source}/${l.id}`}>
                <td>{first + i}</td>
                <td>{l.address}</td>
                <td>{l.city}</td>
                <td>{currency.format(l.price)}</td>
                <td>{l.bedrooms}</td>
                <td>{l.bathrooms}</td>
                <td>{l.listedDate}</td>
                <td>{l.source}</td>
                <td>
                  <span className={`status status-${l.status}`}>{l.status}</span>
                </td>
                <td className="description">{l.description}</td>
                <td>{l.score.toFixed(2)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <nav className="pager" aria-label="Pagination">
        <button disabled={data.page <= 1} onClick={() => onPage(data.page - 1)}>
          ‹ Prev
        </button>
        {Array.from({ length: data.totalPages }, (_, i) => i + 1).map((p) => (
          <button key={p} onClick={() => onPage(p)} aria-current={p === data.page ? 'page' : undefined}>
            {p}
          </button>
        ))}
        <button disabled={data.page >= data.totalPages} onClick={() => onPage(data.page + 1)}>
          Next ›
        </button>
        <span>
          Page {data.page} of {data.totalPages}
        </span>
      </nav>
    </section>
  )
}

export default App
