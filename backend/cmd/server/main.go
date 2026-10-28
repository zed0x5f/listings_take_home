// Command server runs the listing search API.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/tcap/listings/internal/api"
	"github.com/tcap/listings/internal/search"
	"github.com/tcap/listings/internal/store"
)

func main() {
	defaultAddr := ":8080"
	if port := os.Getenv("PORT"); port != "" { // set by hosts like Render
		defaultAddr = ":" + port
	}
	addr := flag.String("addr", defaultAddr, "listen address ($PORT overrides the default)")
	dsn := flag.String("db", "listings.db", `SQLite database path (":memory:" for ephemeral)`)
	seed := flag.String("seed", "../sample_listings.json", "JSON file to load on startup (empty to skip)")
	static := flag.String("static", "", "directory of built frontend files to serve at / (empty to serve the API only)")
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
	handler := h.Routes()
	if *static != "" {
		// Serve the UI and the API from one origin, so the frontend's relative /api calls just work.
		mux := http.NewServeMux()
		mux.Handle("/api/", handler)
		mux.Handle("/", http.FileServer(http.Dir(*static)))
		handler = mux
		log.Printf("serving frontend from %s", *static)
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("listening on %s", *addr)
	log.Fatal(srv.ListenAndServe())
}
