// Package search validates search requests, ranks matching listings and
// paginates the ranked results.
package search

import (
	"context"
	"sort"
	"time"

	"github.com/tcap/listings/internal/store"
)

// Repository is the subset of the store the search service needs.
type Repository interface {
	Find(ctx context.Context, f store.Filter) ([]store.Listing, error)
	CityExists(ctx context.Context, city string) (bool, error)
}

// ScoredListing is a listing plus its relevance score.
type ScoredListing struct {
	store.Listing
	Score float64 `json:"score"`
}

// Result is one page of ranked results.
type Result struct {
	Results    []ScoredListing `json:"results"`
	Page       int             `json:"page"`
	PageSize   int             `json:"pageSize"`
	Total      int             `json:"total"`
	TotalPages int             `json:"totalPages"`
}

type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService builds a Service. now is injectable so recency scoring is
// deterministic in tests; pass nil to use time.Now.
func NewService(repo Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, now: now}
}

// Search filters, ranks and paginates. It returns a *ValidationError for
// requests that are well-formed but can't be satisfied (unknown city, page
// past the end).
func (s *Service) Search(ctx context.Context, p Params) (Result, error) {
	if p.Filter.City != "" {
		ok, err := s.repo.CityExists(ctx, p.Filter.City)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			verr := &ValidationError{}
			verr.add("city", "no listings found in city %q", p.Filter.City)
			return Result{}, verr
		}
	}

	listings, err := s.repo.Find(ctx, p.Filter)
	if err != nil {
		return Result{}, err
	}

	ranked := Rank(listings, p.TargetBudget, s.now())

	total := len(ranked)
	totalPages := (total + p.PageSize - 1) / p.PageSize
	// Page 1 of an empty result set is a valid "no results" answer; any other
	// page past the end is a client error rather than a silent empty page.
	if p.Page > 1 && p.Page > totalPages {
		verr := &ValidationError{}
		verr.add("page", "page %d is out of range (total pages: %d)", p.Page, totalPages)
		return Result{}, verr
	}

	start := (p.Page - 1) * p.PageSize
	end := min(start+p.PageSize, total)
	return Result{
		Results:    ranked[start:end],
		Page:       p.Page,
		PageSize:   p.PageSize,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}

// Rank scores listings and sorts them best-first. Ties are broken by newest
// listing, then lowest price, then source and id, so the order is total and
// pages never shuffle between requests.
func Rank(listings []store.Listing, targetBudget *float64, now time.Time) []ScoredListing {
	ranked := make([]ScoredListing, len(listings))
	for i, l := range listings {
		ranked[i] = ScoredListing{Listing: l, Score: Score(l, targetBudget, now)}
	}
	sort.Slice(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		switch {
		case a.Score != b.Score:
			return a.Score > b.Score
		case a.ListedDate != b.ListedDate:
			return a.ListedDate > b.ListedDate // ISO dates sort lexically
		case a.Price != b.Price:
			return a.Price < b.Price
		case a.Source != b.Source:
			return a.Source < b.Source
		default:
			return a.ID < b.ID
		}
	})
	return ranked
}
