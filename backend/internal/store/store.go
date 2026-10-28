// Package store persists listings in SQLite and runs the filter portion of a
// search. Ranking and pagination live in the search package so the scoring
// logic stays independent of the database.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // pure-Go driver registered as "sqlite"
)

// Listing is a single property listing as ingested from an MLS feed.
type Listing struct {
	ID          string  `db:"id" json:"id"`
	Source      string  `db:"source" json:"source"`
	Address     string  `db:"address" json:"address"`
	City        string  `db:"city" json:"city"`
	State       string  `db:"state" json:"state"`
	Zip         string  `db:"zip" json:"zip"`
	Price       float64 `db:"price" json:"price"`
	Bedrooms    int     `db:"bedrooms" json:"bedrooms"`
	Bathrooms   float64 `db:"bathrooms" json:"bathrooms"`
	Sqft        int     `db:"sqft" json:"sqft"`
	Latitude    float64 `db:"latitude" json:"latitude"`
	Longitude   float64 `db:"longitude" json:"longitude"`
	ListedDate  string  `db:"listed_date" json:"listedDate"`
	Status      string  `db:"status" json:"status"`
	Description string  `db:"description" json:"description"`
}

// Filter holds the optional SQL-side constraints. Nil / empty means "no constraint".
type Filter struct {
	MinPrice    *float64
	MaxPrice    *float64
	MinBedrooms *int
	City        string
	Keyword     string
}

const schema = `
CREATE TABLE IF NOT EXISTS listings (
	id          TEXT    NOT NULL,
	source      TEXT    NOT NULL,
	address     TEXT    NOT NULL,
	city        TEXT    NOT NULL,
	state       TEXT    NOT NULL,
	zip         TEXT    NOT NULL,
	price       REAL    NOT NULL,
	bedrooms    INTEGER NOT NULL,
	bathrooms   REAL    NOT NULL,
	sqft        INTEGER NOT NULL,
	latitude    REAL    NOT NULL,
	longitude   REAL    NOT NULL,
	listed_date TEXT    NOT NULL,
	status      TEXT    NOT NULL,
	description TEXT    NOT NULL,
	-- ids are only unique within a feed
	PRIMARY KEY (source, id)
);
CREATE INDEX IF NOT EXISTS idx_listings_price ON listings (price);
`

// Store wraps an sqlx handle.
type Store struct {
	db *sqlx.DB
}

// Open connects to the SQLite database at dsn and ensures the schema exists.
// Use ":memory:" for an ephemeral database.
func Open(dsn string) (*Store, error) {
	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Every new connection to ":memory:" is a separate database, and SQLite
	// serialises writes anyway, so a single connection keeps things simple.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Upsert inserts listings, replacing any existing row with the same (source, id).
func (s *Store) Upsert(ctx context.Context, listings []Listing) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	const q = `INSERT OR REPLACE INTO listings
		(id, source, address, city, state, zip, price, bedrooms, bathrooms, sqft,
		 latitude, longitude, listed_date, status, description)
		VALUES
		(:id, :source, :address, :city, :state, :zip, :price, :bedrooms, :bathrooms, :sqft,
		 :latitude, :longitude, :listed_date, :status, :description)`
	for _, l := range listings {
		if _, err := tx.NamedExecContext(ctx, q, l); err != nil {
			return fmt.Errorf("insert %s/%s: %w", l.Source, l.ID, err)
		}
	}
	return tx.Commit()
}

// SeedFromFile loads a JSON array of listings and upserts them.
func (s *Store) SeedFromFile(ctx context.Context, path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var listings []Listing
	if err := json.Unmarshal(raw, &listings); err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	return len(listings), s.Upsert(ctx, listings)
}

// Find returns every listing matching the filter, in no particular order.
func (s *Store) Find(ctx context.Context, f Filter) ([]Listing, error) {
	var (
		where []string
		args  []any
	)
	if f.MinPrice != nil {
		where = append(where, "price >= ?")
		args = append(args, *f.MinPrice)
	}
	if f.MaxPrice != nil {
		where = append(where, "price <= ?")
		args = append(args, *f.MaxPrice)
	}
	if f.MinBedrooms != nil {
		where = append(where, "bedrooms >= ?")
		args = append(args, *f.MinBedrooms)
	}
	if f.City != "" {
		where = append(where, cityMatch)
		args = append(args, cityPattern(f.City))
	}
	if terms := keywordTerms(f.Keyword); len(terms) > 0 {
		// Match any term: "garage pool" -> (description LIKE ? OR description LIKE ?).
		// The parentheses keep the ORs from escaping the surrounding ANDs.
		// SQLite's LIKE is case-insensitive for ASCII; wildcards are escaped so
		// user input is matched literally.
		ors := make([]string, len(terms))
		for i, t := range terms {
			ors[i] = `description LIKE ? ESCAPE '\'`
			args = append(args, "%"+escapeLike(t)+"%")
		}
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}

	q := "SELECT * FROM listings"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}

	listings := []Listing{}
	if err := s.db.SelectContext(ctx, &listings, q, args...); err != nil {
		return nil, fmt.Errorf("find listings: %w", err)
	}
	return listings, nil
}

// CityExists reports whether any listing's city matches, using the same
// partial match as Find so the two never disagree.
func (s *Store) CityExists(ctx context.Context, city string) (bool, error) {
	var exists bool
	err := s.db.GetContext(ctx, &exists,
		"SELECT EXISTS (SELECT 1 FROM listings WHERE "+cityMatch+")", cityPattern(city))
	return exists, err
}

// cityMatch is a case-insensitive partial match on city (SQLite's LIKE folds
// ASCII case), so "fair" finds "Fairfax" and feed inconsistencies like
// " fairfax " still match.
const cityMatch = `city LIKE ? ESCAPE '\'`

func cityPattern(city string) string { return "%" + escapeLike(strings.TrimSpace(city)) + "%" }

// keywordTerms splits on whitespace and drops case-insensitive duplicates, so
// "pets PETS" doesn't produce a redundant condition.
func keywordTerms(keyword string) []string {
	var terms []string
	seen := map[string]bool{}
	for _, t := range strings.Fields(keyword) {
		if k := strings.ToLower(t); !seen[k] {
			seen[k] = true
			terms = append(terms, t)
		}
	}
	return terms
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(s string) string { return likeEscaper.Replace(s) }
