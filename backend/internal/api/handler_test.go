package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/tcap/listings/internal/api"
	"github.com/tcap/listings/internal/search"
	"github.com/tcap/listings/internal/store"
)

// fixedNow pins recency scoring so expected scores are exact.
var fixedNow = time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

// fixtures are chosen so that, with targetBudget=500000 and fixedNow:
//
//	A/1  budget 1.0, recency 1.0 -> 100
//	B/2  budget 0.9, recency 1.0 -> 93   (ties with B/3, cheaper so first)
//	B/3  budget 0.9, recency 1.0 -> 93
//	B/1  budget 0.8, recency 1.0 -> 86
//	A/2  budget 0.8, recency 0.5 -> 71   (listed exactly one half-life ago)
//
// A/1 and B/1 share an id to prove ids are scoped per source.
var fixtures = []store.Listing{
	{ID: "1", Source: "MLS_A", Address: "1 Main St", City: "Springfield", State: "VA", Zip: "22150",
		Price: 500000, Bedrooms: 3, Bathrooms: 2, Sqft: 1400, ListedDate: "2026-09-05", Status: "active",
		Description: "Pet friendly condo near Metro."},
	{ID: "2", Source: "MLS_A", Address: "2 Oak Ave", City: "Springfield", State: "VA", Zip: "22150",
		Price: 400000, Bedrooms: 2, Bathrooms: 1, Sqft: 900, ListedDate: "2026-08-06", Status: "active",
		Description: "Cozy starter home, no pets."},
	{ID: "1", Source: "MLS_B", Address: "3 Pine Rd", City: "Fairfax", State: "VA", Zip: "22030",
		Price: 600000, Bedrooms: 4, Bathrooms: 3, Sqft: 2200, ListedDate: "2026-09-05", Status: "active",
		Description: "Big yard, 100% renovated."},
	{ID: "2", Source: "MLS_B", Address: "4 Birch Ln", City: " fairfax ", State: "VA", Zip: "22030",
		Price: 450000, Bedrooms: 3, Bathrooms: 2, Sqft: 1500, ListedDate: "2026-09-05", Status: "active",
		Description: "Pets allowed, attached garage."},
	{ID: "3", Source: "MLS_B", Address: "5 Elm Ct", City: "Reston", State: "VA", Zip: "20190",
		Price: 550000, Bedrooms: 3, Bathrooms: 2.5, Sqft: 1700, ListedDate: "2026-09-05", Status: "active",
		Description: "Corner_lot with pool."},
}

func newServer(t *testing.T, listings []store.Listing) *httptest.Server {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Upsert(context.Background(), listings); err != nil {
		t.Fatal(err)
	}
	return newServerWithRepo(t, st)
}

func newServerWithRepo(t *testing.T, repo search.Repository) *httptest.Server {
	t.Helper()
	svc := search.NewService(repo, func() time.Time { return fixedNow })
	srv := httptest.NewServer(api.NewHandler(svc).Routes())
	t.Cleanup(srv.Close)
	return srv
}

// get issues GET /api/listings/search?<query> and decodes the body into out.
func get(t *testing.T, srv *httptest.Server, query string, out any) int {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/listings/search?" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return resp.StatusCode
}

func searchOK(t *testing.T, srv *httptest.Server, query string) search.Result {
	t.Helper()
	var res search.Result
	if code := get(t, srv, query, &res); code != http.StatusOK {
		t.Fatalf("GET ?%s: status %d, want 200", query, code)
	}
	return res
}

func searchErr(t *testing.T, srv *httptest.Server, query string) api.ErrorResponse {
	t.Helper()
	var res api.ErrorResponse
	if code := get(t, srv, query, &res); code != http.StatusBadRequest {
		t.Fatalf("GET ?%s: status %d, want 400", query, code)
	}
	if res.Error != "invalid_parameters" {
		t.Errorf("error = %q, want invalid_parameters", res.Error)
	}
	return res
}

func keys(res search.Result) []string {
	out := make([]string, len(res.Results))
	for i, r := range res.Results {
		out[i] = r.Source + "/" + r.ID
	}
	return out
}

func assertKeys(t *testing.T, res search.Result, want ...string) {
	t.Helper()
	got := keys(res)
	if len(got) != len(want) {
		t.Fatalf("results = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("results = %v, want %v", got, want)
		}
	}
}

func hasFieldError(res api.ErrorResponse, field string) bool {
	for _, d := range res.Details {
		if d.Field == field {
			return true
		}
	}
	return false
}

func TestHealth(t *testing.T) {
	srv := newServer(t, fixtures)
	resp, err := http.Get(srv.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
}

func TestSearch_NoParamsRanksByRecencyThenPrice(t *testing.T) {
	srv := newServer(t, fixtures)
	res := searchOK(t, srv, "")

	if res.Total != 5 || res.Page != 1 || res.PageSize != search.DefaultPageSize || res.TotalPages != 1 {
		t.Fatalf("meta = %+v", res)
	}
	// Four listings are brand new (score 100) and tie; cheapest first. A/2 is
	// one half-life old (score 50).
	assertKeys(t, res, "MLS_B/2", "MLS_A/1", "MLS_B/3", "MLS_B/1", "MLS_A/2")
	if res.Results[0].Score != 100 || res.Results[4].Score != 50 {
		t.Errorf("scores = %v, %v; want 100, 50", res.Results[0].Score, res.Results[4].Score)
	}
}

func TestSearch_TargetBudgetRanking(t *testing.T) {
	srv := newServer(t, fixtures)
	res := searchOK(t, srv, "targetBudget=500000")

	assertKeys(t, res, "MLS_A/1", "MLS_B/2", "MLS_B/3", "MLS_B/1", "MLS_A/2")
	wantScores := []float64{100, 93, 93, 86, 71}
	for i, w := range wantScores {
		if got := res.Results[i].Score; got != w {
			t.Errorf("result %d (%s/%s) score = %v, want %v", i, res.Results[i].Source, res.Results[i].ID, got, w)
		}
	}
}

func TestSearch_TiedScoresAreDeterministic(t *testing.T) {
	// Insert in reverse order to make sure ordering doesn't depend on storage order.
	reversed := make([]store.Listing, len(fixtures))
	for i, l := range fixtures {
		reversed[len(fixtures)-1-i] = l
	}
	srv := newServer(t, reversed)

	for range 5 {
		res := searchOK(t, srv, "targetBudget=500000")
		// B/2 and B/3 both score 93 with the same date; lower price wins.
		if res.Results[1].Score != res.Results[2].Score {
			t.Fatalf("expected a tie, got %v vs %v", res.Results[1].Score, res.Results[2].Score)
		}
		assertKeys(t, res, "MLS_A/1", "MLS_B/2", "MLS_B/3", "MLS_B/1", "MLS_A/2")
	}
}

func TestSearch_TieBreakFallsThroughToSourceAndID(t *testing.T) {
	same := store.Listing{City: "X", Price: 1, ListedDate: "2026-09-05", Description: "d"}
	a, b, c := same, same, same
	a.Source, a.ID = "MLS_B", "1"
	b.Source, b.ID = "MLS_A", "2"
	c.Source, c.ID = "MLS_A", "1"
	srv := newServer(t, []store.Listing{a, b, c})

	assertKeys(t, searchOK(t, srv, ""), "MLS_A/1", "MLS_A/2", "MLS_B/1")
}

func TestSearch_Filters(t *testing.T) {
	srv := newServer(t, fixtures)
	tests := []struct {
		name  string
		query string
		want  []string // in ranked order (no targetBudget)
	}{
		{"minPrice inclusive", "minPrice=550000", []string{"MLS_B/3", "MLS_B/1"}},
		{"maxPrice inclusive", "maxPrice=450000", []string{"MLS_B/2", "MLS_A/2"}},
		{"price range", "minPrice=450000&maxPrice=550000", []string{"MLS_B/2", "MLS_A/1", "MLS_B/3"}},
		{"minPrice equals maxPrice", "minPrice=500000&maxPrice=500000", []string{"MLS_A/1"}},
		{"minBedrooms", "minBedrooms=4", []string{"MLS_B/1"}},
		{"minBedrooms zero", "minBedrooms=0", []string{"MLS_B/2", "MLS_A/1", "MLS_B/3", "MLS_B/1", "MLS_A/2"}},
		{"city is case and whitespace insensitive", "city=FAIRFAX", []string{"MLS_B/2", "MLS_B/1"}},
		{"city with padding", "city=" + url.QueryEscape("  Fairfax "), []string{"MLS_B/2", "MLS_B/1"}},
		{"city partial prefix", "city=fair", []string{"MLS_B/2", "MLS_B/1"}},
		{"city partial infix", "city=ingf", []string{"MLS_A/1", "MLS_A/2"}},
		{"city partial matches several cities", "city=S", []string{"MLS_A/1", "MLS_B/3", "MLS_A/2"}}, // Springfield + Reston
		{"keyword case insensitive", "keyword=PETS", []string{"MLS_B/2", "MLS_A/2"}},
		{"keyword substring", "keyword=pet", []string{"MLS_B/2", "MLS_A/1", "MLS_A/2"}},
		{"keyword percent is literal", "keyword=" + url.QueryEscape("%"), []string{"MLS_B/1"}},
		{"keyword underscore is literal", "keyword=_", []string{"MLS_B/3"}},
		{"keywords match any term", "keyword=" + url.QueryEscape("garage pool"), []string{"MLS_B/2", "MLS_B/3"}},
		{"keywords split on any whitespace", "keyword=" + url.QueryEscape("yard \t  metro"), []string{"MLS_A/1", "MLS_B/1"}},
		{"duplicate keywords match once", "keyword=" + url.QueryEscape("pets PETS"), []string{"MLS_B/2", "MLS_A/2"}},
		{"keyword wildcard stays literal among terms", "keyword=" + url.QueryEscape("% pool"), []string{"MLS_B/3", "MLS_B/1"}},
		{"keyword OR stays inside other filters", "city=Springfield&keyword=" + url.QueryEscape("garage pool"), []string{}},
		{"keyword quote is safe", "keyword=" + url.QueryEscape("'; DROP TABLE listings; --"), []string{}},
		{"blank values are ignored", "minPrice=&maxPrice=&city=&keyword=&minBedrooms=", []string{"MLS_B/2", "MLS_A/1", "MLS_B/3", "MLS_B/1", "MLS_A/2"}},
		{"combined", "city=springfield&minBedrooms=2&maxPrice=450000&keyword=pets", []string{"MLS_A/2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := searchOK(t, srv, tt.query)
			assertKeys(t, res, tt.want...)
			if res.Total != len(tt.want) {
				t.Errorf("total = %d, want %d", res.Total, len(tt.want))
			}
		})
	}
}

func TestSearch_NoMatchesReturnsEmptyArray(t *testing.T) {
	srv := newServer(t, fixtures)

	// Decode raw so we can tell [] from null.
	var raw map[string]json.RawMessage
	if code := get(t, srv, "city=Springfield&minPrice=900000", &raw); code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if string(raw["results"]) != "[]" {
		t.Errorf("results = %s, want []", raw["results"])
	}
	if string(raw["total"]) != "0" || string(raw["totalPages"]) != "0" {
		t.Errorf("total = %s, totalPages = %s; want 0, 0", raw["total"], raw["totalPages"])
	}
}

func TestSearch_EmptyDatabase(t *testing.T) {
	srv := newServer(t, nil)
	res := searchOK(t, srv, "")
	if res.Total != 0 || len(res.Results) != 0 || res.TotalPages != 0 {
		t.Fatalf("got %+v, want empty", res)
	}
}

func TestSearch_UnknownCityIsAnError(t *testing.T) {
	srv := newServer(t, fixtures)
	for _, city := range []string{"Atlantis", "Fairfaxx", "%", "_"} {
		// % and _ are matched literally, so they don't act as wildcards.
		res := searchErr(t, srv, "city="+url.QueryEscape(city))
		if !hasFieldError(res, "city") {
			t.Errorf("%q: details = %+v, want a city error", city, res.Details)
		}
	}
}

func TestSearch_InvalidParams(t *testing.T) {
	srv := newServer(t, fixtures)
	tests := []struct {
		query string
		field string
	}{
		{"minPrice=600000&maxPrice=500000", "minPrice"},
		{"minPrice=abc", "minPrice"},
		{"minPrice=-1", "minPrice"},
		{"minPrice=NaN", "minPrice"},
		{"maxPrice=Inf", "maxPrice"},
		{"maxPrice=-5", "maxPrice"},
		{"minBedrooms=-1", "minBedrooms"},
		{"minBedrooms=1.5", "minBedrooms"},
		{"minBedrooms=two", "minBedrooms"},
		{"targetBudget=0", "targetBudget"},
		{"targetBudget=-100", "targetBudget"},
		{"targetBudget=lots", "targetBudget"},
		{"page=0", "page"},
		{"page=-1", "page"},
		{"page=x", "page"},
		{"pageSize=0", "pageSize"},
		{"pageSize=-5", "pageSize"},
		{"pageSize=101", "pageSize"},
		{"pageSize=2.5", "pageSize"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			res := searchErr(t, srv, tt.query)
			if !hasFieldError(res, tt.field) {
				t.Errorf("details = %+v, want an error for %q", res.Details, tt.field)
			}
		})
	}
}

func TestSearch_ReportsAllInvalidFieldsAtOnce(t *testing.T) {
	srv := newServer(t, fixtures)
	res := searchErr(t, srv, "minPrice=abc&pageSize=0&page=0&minBedrooms=-1")
	for _, f := range []string{"minPrice", "pageSize", "page", "minBedrooms"} {
		if !hasFieldError(res, f) {
			t.Errorf("missing error for %q in %+v", f, res.Details)
		}
	}
}

func TestSearch_Pagination(t *testing.T) {
	srv := newServer(t, fixtures)

	t.Run("first page", func(t *testing.T) {
		res := searchOK(t, srv, "targetBudget=500000&pageSize=2&page=1")
		assertKeys(t, res, "MLS_A/1", "MLS_B/2")
		if res.Total != 5 || res.TotalPages != 3 {
			t.Errorf("total = %d, totalPages = %d; want 5, 3", res.Total, res.TotalPages)
		}
	})
	t.Run("partial last page", func(t *testing.T) {
		res := searchOK(t, srv, "targetBudget=500000&pageSize=2&page=3")
		assertKeys(t, res, "MLS_A/2")
	})
	t.Run("page past the end", func(t *testing.T) {
		if res := searchErr(t, srv, "pageSize=2&page=4"); !hasFieldError(res, "page") {
			t.Errorf("details = %+v", res.Details)
		}
	})
	t.Run("exact fit has one page", func(t *testing.T) {
		res := searchOK(t, srv, "pageSize=5")
		if res.TotalPages != 1 || len(res.Results) != 5 {
			t.Errorf("totalPages = %d, len = %d; want 1, 5", res.TotalPages, len(res.Results))
		}
		searchErr(t, srv, "pageSize=5&page=2")
	})
	t.Run("pageSize of one", func(t *testing.T) {
		res := searchOK(t, srv, "pageSize=1&page=5")
		if res.TotalPages != 5 || len(res.Results) != 1 {
			t.Errorf("totalPages = %d, len = %d; want 5, 1", res.TotalPages, len(res.Results))
		}
	})
	t.Run("max pageSize accepted", func(t *testing.T) {
		searchOK(t, srv, "pageSize=100")
	})
	t.Run("page 2 of empty result is out of range", func(t *testing.T) {
		searchErr(t, srv, "minPrice=900000&page=2")
	})
	t.Run("pages cover every result exactly once in rank order", func(t *testing.T) {
		all := keys(searchOK(t, srv, "targetBudget=500000"))
		var paged []string
		for p := 1; p <= 3; p++ {
			paged = append(paged, keys(searchOK(t, srv, "targetBudget=500000&pageSize=2&page="+strconv.Itoa(p)))...)
		}
		if len(paged) != len(all) {
			t.Fatalf("paged = %v, all = %v", paged, all)
		}
		for i := range all {
			if paged[i] != all[i] {
				t.Fatalf("paged = %v, all = %v", paged, all)
			}
		}
	})
}

func TestSearch_ResponseIncludesListingFields(t *testing.T) {
	srv := newServer(t, fixtures)
	var raw struct {
		Results []map[string]any `json:"results"`
	}
	get(t, srv, "city=Reston", &raw)
	if len(raw.Results) != 1 {
		t.Fatalf("got %d results", len(raw.Results))
	}
	for _, f := range []string{"id", "source", "address", "city", "price", "bedrooms", "bathrooms", "listedDate", "status", "description", "score"} {
		if _, ok := raw.Results[0][f]; !ok {
			t.Errorf("result missing field %q", f)
		}
	}
}

func TestSearch_MethodNotAllowed(t *testing.T) {
	srv := newServer(t, fixtures)
	resp, err := http.Post(srv.URL+"/api/listings/search", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", resp.StatusCode)
	}
}

type failingRepo struct{}

func (failingRepo) Find(context.Context, store.Filter) ([]store.Listing, error) {
	return nil, errors.New("disk on fire")
}
func (failingRepo) CityExists(context.Context, string) (bool, error) { return true, nil }

func TestSearch_RepositoryErrorIs500WithoutLeakingDetails(t *testing.T) {
	srv := newServerWithRepo(t, failingRepo{})
	var res api.ErrorResponse
	if code := get(t, srv, "", &res); code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", code)
	}
	if res.Error != "internal_error" {
		t.Errorf("error = %q, want internal_error", res.Error)
	}
}

func TestSearch_SampleDataset(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	n, err := st.SeedFromFile(context.Background(), "../../../sample_listings.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := newServerWithRepo(t, st)

	res := searchOK(t, srv, "pageSize=100")
	if res.Total != n || n != 12 {
		t.Fatalf("total = %d, loaded = %d; want 12", res.Total, n)
	}
	res = searchOK(t, srv, "city=Springfield&minBedrooms=3&targetBudget=525000")
	assertKeys(t, res, "MLS_A/A2", "MLS_B/B8")
}
