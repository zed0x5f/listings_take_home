export interface Listing {
  id: string
  source: string
  address: string
  city: string
  state: string
  zip: string
  price: number
  bedrooms: number
  bathrooms: number
  sqft: number
  listedDate: string
  status: string
  description: string
  score: number
}

export interface SearchResult {
  results: Listing[]
  page: number
  pageSize: number
  total: number
  totalPages: number
}

export interface FieldError {
  field: string
  message: string
}

export interface SearchFilters {
  minPrice: string
  maxPrice: string
  minBedrooms: string
  city: string
  keyword: string
  targetBudget: string
  pageSize: string
}

/** Thrown for any non-2xx response; `fields` is set for 400 validation errors. */
export class ApiError extends Error {
  readonly status: number
  readonly fields: FieldError[]

  constructor(status: number, message: string, fields: FieldError[] = []) {
    super(message)
    this.status = status
    this.fields = fields
  }
}

export async function searchListings(
  filters: SearchFilters,
  page: number,
  signal?: AbortSignal,
): Promise<SearchResult> {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(filters)) {
    // Blank inputs mean "no filter"; the API treats missing and blank the same.
    if (value.trim() !== '') params.set(key, value.trim())
  }
  params.set('page', String(page))

  const res = await fetch(`/api/listings/search?${params}`, { signal })
  // A body that isn't JSON (e.g. an HTML page from a misconfigured proxy, or a
  // truncated response) must not be treated as a result: on a 200 it would be
  // returned as `null` and crash the page.
  let body: unknown
  try {
    body = await res.json()
  } catch (err: unknown) {
    console.error(`Unexpected error ${err}`)
    if (signal?.aborted) throw signal.reason
    throw new ApiError(
      res.status,
      res.ok
        ? 'The server sent an unexpected response. Please try again.'
        : `Search failed (HTTP ${res.status}). Please try again.`,
    )
  }

  if (!res.ok) {
    const details = (body as { details?: FieldError[] } | null)?.details
    if (res.status === 400 && details) {
      throw new ApiError(res.status, 'Please fix the highlighted fields.', details)
    }
    throw new ApiError(res.status, `Search failed (HTTP ${res.status}). Please try again.`)
  }
  return body as SearchResult
}
