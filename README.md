# Listing Search Service

Go + SQLite (sqlx) API in [backend/](backend/), React + TypeScript UI in [my-app/](my-app/).

## Running

Prerequisites: **Go 1.22+** (developed on 1.27) and **Node 20+**. No C compiler is needed.

Run the backend and frontend in two terminals, both from the repo root.

**Terminal 1: backend API** (http://localhost:8080)

```sh
cd backend
go run ./cmd/server
```

On startup it loads `../sample_listings.json` into `listings.db`. Re-running is safe, because rows are
upserted by `(source, id)`. Optional flags:

| Flag    | Default                   | Purpose                                                  |
| ------- | ------------------------- | -------------------------------------------------------- |
| `-addr` | `:8080`                   | Listen address                                           |
| `-db`   | `listings.db`             | SQLite file. Use `:memory:` for a throwaway database     |
| `-seed` | `../sample_listings.json` | JSON file to load on startup. Pass `-seed ""` to skip it |

Check it's up:

```sh
curl http://localhost:8080/api/health
curl "http://localhost:8080/api/listings/search?city=spring&targetBudget=500000&pageSize=3"
```

**Terminal 2: frontend** (http://localhost:5173)

```sh
cd my-app
npm install      # first time only
npm run dev
```

Open http://localhost:5173. Vite proxies `/api/*` to `localhost:8080` ([vite.config.ts](my-app/vite.config.ts)),
so the backend must be running, and no CORS setup is needed.

**Tests**

```sh
cd backend
go test ./...            # add -v to list each case, -count=1 to bypass the test cache
```

```sh
cd my-app
npm run lint
npm run build            # type-checks (tsc) and produces dist/
```

**Troubleshooting**

- *The UI shows "Could not reach the server" or "Search failed (HTTP 500)".* The backend isn't running on :8080.
- *API calls return the HTML page or a 404 from Vite.* The dev server started before `vite.config.ts` had the
  proxy. Restart `npm run dev`.
- *"Port 5173 is in use".* Vite falls back to the next free port; use the URL it prints.
- *`go: command not found`.* Add Go's `bin` folder to `PATH` (on Windows the default is `C:\Program Files\Go\bin`).

SQLite driver: `modernc.org/sqlite` (pure Go) behind `sqlx`. I chose it over `mattn/go-sqlite3`
because it needs no cgo/C toolchain, so `go run` works on a fresh machine, including Windows.

## API

`GET /api/listings/search` – all params optional

| Param          | Rule                                                         |
| -------------- | ------------------------------------------------------------ |
| `minPrice`     | number ≥ 0, must be ≤ `maxPrice` if both set                 |
| `maxPrice`     | number ≥ 0                                                   |
| `minBedrooms`  | integer ≥ 0                                                  |
| `city`         | case-insensitive partial match (`LIKE %city%`, wildcards escaped); must match at least one listing |
| `keyword`      | split on whitespace; matches if `description` contains **any** term (case-insensitive, `%`/`_` escaped) |
| `targetBudget` | number > 0; enables budget scoring                           |
| `page`         | integer ≥ 1, default 1; must be ≤ `totalPages` (page 1 of an empty result is allowed) |
| `pageSize`     | integer 1–100, default 10                                    |

Blank values (`?minPrice=`) count as "not set" so an empty form field isn't an error.

200 response:

```json
{ "results": [{ "id": "A1", "source": "MLS_A", "...": "...", "score": 87.5 }],
  "page": 1, "pageSize": 10, "total": 12, "totalPages": 2 }
```

400 response. Every bad field is reported at once:

```json
{ "error": "invalid_parameters",
  "details": [{ "field": "minPrice", "message": "must be less than or equal to maxPrice" }] }
```

A valid filter combination that matches nothing returns 200 with `results: []`. That is a real answer, not an error.
An **unknown city** returns 400, because it is almost always a typo, and a silent empty list would hide that.
Unexpected failures return 500 with `{"error":"internal_error"}`. Details are logged on the server, not sent to the client.

## Scoring

```
recency = 0.5 ^ (daysSinceListed / 30)                 // 1.0 today, 0.5 at 30 days, 0.25 at 60…
budget  = max(0, 1 − |price − targetBudget| / targetBudget)   // 1.0 on target, 0 at ±100%

score = 100 × (0.7·budget + 0.3·recency)   if targetBudget is given
score = 100 × recency                       otherwise
```

Rounded to 2 decimals.

- **Budget is weighted heavily (70%)**: a buyer's budget is a near-hard constraint, while freshness is a nice-to-have.
- **Budget is relative, not absolute**: $20k off matters more on a $300k budget than on a $2M one.
  Symmetric: being under budget is penalized as much as being over. That's a simplification. An
  asymmetric penalty (over budget hurts more) is a one-line change if product wants it.
- **Recency uses exponential decay** with a 30-day half-life, measured against the server clock
  (the clock is injected, so tests are deterministic). This keeps a listing's score stable no matter
  what else is in the result set. Normalizing within the result set would make scores shift whenever the filters change.
  Future-dated listings count as "new". Unparseable dates score 0.
- **Ties** (scores are compared after rounding, so "tied" means tied as displayed) are broken by
  newest `listedDate`, then lowest price, then `source`, then `id`. That gives a total order, so
  pagination is stable and the same request always returns the same page.

## Design / trade-offs

- **Filtering happens in SQL, ranking and pagination in Go.** Filters use the indexes. The score depends
  on request-time inputs (budget, now), and keeping it in Go makes it unit-testable and easy to change.
  The cost: every matching row is loaded per request. That's fine for thousands of listings. At scale I'd
  push the score into SQL (`ORDER BY … LIMIT/OFFSET`) or use keyset pagination over a precomputed ranking.
- **Declarative request validation.** The `api.SearchRequest` DTO declares its rules in struct tags.
  `go-playground/form` decodes the query string and `go-playground/validator` checks the rules. The DTO is
  then mapped to the domain `search.Params`, so the service never sees unvalidated input. Optional fields are
  pointers, so `pageSize=0` gets validated instead of being skipped by `omitempty`. The min ≤ max price check
  is a struct-level rule, because cross-field tags fail when either side is unset.
- **Identity is `(source, id)`**. ids are only unique within a feed.
- **Cross-feed duplicates are not merged.** The sample has the same home in MLS_A and MLS_B
  (`123 Main St, Apt 4B` vs `123 Main Street, Unit 4B`). Both are shown, each with its own source. Deduping
  would need address normalisation plus a lat/long/sqft match, and a rule for which source wins.
  See [Open questions](#open-questions-for-the-interviewer).
- **City matching** is a case-insensitive substring `LIKE` (`fair` finds Fairfax). This also absorbs small cross-feed inconsistencies in casing and spacing. The trade-off: a short term can match several cities (`s` → Springfield and Reston), and a leading-wildcard `LIKE` can't use an index, so I dropped the city index. Fine at this size; at scale use FTS5 or a normalised city column with prefix matching.
- **Keyword** is split into terms, giving one parameterized `description LIKE ?` per term, ORed together inside parentheses so they stay under the other AND filters. OR favours recall. AND would be stricter, but returns nothing more often on short descriptions. Matching is still substring-based, so `pet` also matches "pets" and "no pets". Terms do not affect the score. Full-text search (SQLite FTS5) would
  be the next step for relevance-aware text search.
- `status` is returned but not filtered on. Pending and sold listings are included.

## Open questions for the interviewer

### How should duplicate listings across feeds be handled?

The sample data has several properties listed by both MLS_A and MLS_B, with the same coordinates and sqft:

| Property         | MLS_A                                 | MLS_B                                       | Differences                            |
| ---------------- | ------------------------------------- | ------------------------------------------- | -------------------------------------- |
| 123 Main St, 4B  | A1: `123 Main St, Apt 4B`, $450,000   | B7: `123 Main Street, Unit 4B`, $452,000    | address format, price, date, wording   |
| 456 Oak Ave      | A2: $525,000, zip 22150               | B8: `456 Oak Avenue`, $527,500, zip 22151   | address format, **zip**, price, date   |
| 789 Pine Rd      | A3: $399,000                          | B9: $399,500                                | price, date, wording                   |
| 55 Elm Ct        | A5: $470,000                          | B11: `55 Elm Court`, $465,000               | address format, price, date, wording   |

This looks intentional: one property can legitimately be listed in more than one feed. So for now
I keep every listing and show each one with its source, rather than guessing at merge rules. How
results should be displayed is a business decision, though. Questions to confirm:

1. **Show one result or all of them?** Should the user see one result per property, all listings
   with a "also listed on MLS_B" indicator, or every listing separately (current behaviour)?
2. **What counts as the same property?** Coordinates, a normalised address (`St` = `Street`,
   `Apt` = `Unit`), sqft, or a combination? The A2/B8 zip mismatch suggests no single field can be trusted.
3. **If merged, which listing wins?** A preferred source, the most recent `listedDate`, or the lowest
   price? Or merge field by field?
4. **Price and score.** Duplicates can have different prices and therefore different scores. Should a
   merged result use the best score, show a price range, or use the price from the winning source?
5. **Counting and pagination.** Should `total` and page counts be based on properties or on listings?
   If properties, deduplication has to happen before pagination, not per page.
6. **Status conflicts.** If one feed says `active` and another says `pending` or `sold`, which is correct?

Whatever the answers, the change fits the current design. A dedupe/grouping step goes between `Find`
and `Rank` in [service.go](backend/internal/search/service.go), so pagination stays correct, and the
API can return a `duplicates` array (or a group key) on each result.

## Layout

```
backend/
  cmd/server/          entry point: open DB, seed, serve
  internal/store/      sqlx + SQLite: schema, upsert, filtered query
  internal/search/     param validation, scoring, ranking, pagination
  internal/api/        HTTP handlers + endpoint test suite (httptest + in-memory SQLite)
my-app/src/
  api.ts               typed client for the search endpoint
  App.tsx              search form, results table, pager, loading/empty/error states
```
