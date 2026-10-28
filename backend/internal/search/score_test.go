package search

import (
	"math"
	"testing"
	"time"

	"github.com/tcap/listings/internal/store"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestBudgetScore(t *testing.T) {
	tests := []struct {
		price, target, want float64
	}{
		{500, 500, 1},
		{450, 500, 0.9},
		{550, 500, 0.9}, // symmetric
		{1000, 500, 0},
		{2000, 500, 0}, // clamped
		{0, 500, 0},
		{500, 0, 0}, // guarded
	}
	for _, tt := range tests {
		if got := BudgetScore(tt.price, tt.target); !approx(got, tt.want) {
			t.Errorf("BudgetScore(%v, %v) = %v, want %v", tt.price, tt.target, got, tt.want)
		}
	}
}

func TestRecencyScore(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		date string
		want float64
	}{
		{"2026-09-05", 1},
		{"2026-08-06", 0.5},  // one half-life
		{"2026-07-07", 0.25}, // two half-lives
		{"2026-12-01", 1},    // future dates aren't boosted above "new"
		{"not-a-date", 0},
		{"", 0},
	}
	for _, tt := range tests {
		if got := RecencyScore(tt.date, now); !approx(got, tt.want) {
			t.Errorf("RecencyScore(%q) = %v, want %v", tt.date, got, tt.want)
		}
	}
}

func TestScore(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	l := store.Listing{Price: 450000, ListedDate: "2026-08-06"}
	budget := 500000.0

	if got := Score(l, nil, now); got != 50 {
		t.Errorf("without budget = %v, want 50 (recency only)", got)
	}
	// 0.7*0.9 + 0.3*0.5 = 0.78
	if got := Score(l, &budget, now); got != 78 {
		t.Errorf("with budget = %v, want 78", got)
	}
}

func TestScoreIsRoundedToTwoDecimals(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	got := Score(store.Listing{ListedDate: "2026-09-01"}, nil, now)
	if got != math.Round(got*100)/100 {
		t.Errorf("score %v not rounded to 2 decimals", got)
	}
}
