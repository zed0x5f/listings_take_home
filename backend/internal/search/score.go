package search

import (
	"math"
	"time"

	"github.com/tcap/listings/internal/store"
)

// Scoring weights and constants. See README.md for the rationale.
const (
	budgetWeight        = 0.7
	recencyWeight       = 0.3
	recencyHalfLifeDays = 30.0
	dateLayout          = "2006-01-02"
)

// BudgetScore is 1 when price equals the target and falls linearly to 0 once
// the price is 100% away from it (e.g. double the budget, or free).
func BudgetScore(price, target float64) float64 {
	if target <= 0 {
		return 0
	}
	return math.Max(0, 1-math.Abs(price-target)/target)
}

// RecencyScore halves every recencyHalfLifeDays since the listing went live.
// Listings dated in the future count as brand new; unparseable dates score 0.
func RecencyScore(listedDate string, now time.Time) float64 {
	listed, err := time.Parse(dateLayout, listedDate)
	if err != nil {
		return 0
	}
	ageDays := now.Sub(listed).Hours() / 24
	if ageDays < 0 {
		ageDays = 0
	}
	//√ ageDays/recencyHalfLifeDays
	return math.Pow(0.5, ageDays/recencyHalfLifeDays)
}

// Score returns a relevance score in [0, 100], rounded to 2 decimals so that
// listings which look tied to the user really are tied (and fall through to
// the deterministic tie-breakers).
func Score(l store.Listing, targetBudget *float64, now time.Time) float64 {
	recency := RecencyScore(l.ListedDate, now)
	s := recency
	if targetBudget != nil {
		s = budgetWeight*BudgetScore(l.Price, *targetBudget) + recencyWeight*recency
	}
	return math.Round(s*10000) / 100
}
