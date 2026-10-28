// Command server runs the listing search API.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/tcap/listings/internal/api"
	"github.com/tcap/listings/internal/search"
	"github.com/tcap/listings/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dsn := flag.String("db", "listings.db", `SQLite database path (":memory:" for ephemeral)`)
	seed := flag.String("seed", "../sample_listings.json", "JSON file to load on startup (empty to skip)")
	flag.Parse()

	st, err := store.Open(*dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	if *seed != "" {
		n, err := st.SeedFromFile(context.Background(), *seed)
		if err != nil {
			log.Fatalf("seed: %v", err)
		}
		log.Printf("loaded %d listings from %s", n, *seed)
	}

	h := api.NewHandler(search.NewService(st, nil))
	srv := &http.Server{
		Addr:              *addr,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("listening on %s", *addr)
	log.Fatal(srv.ListenAndServe())
}
