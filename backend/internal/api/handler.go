// Package api exposes the search service over HTTP.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/tcap/listings/internal/search"
)

// ErrorResponse is the body returned for every non-2xx response.
type ErrorResponse struct {
	Error   string              `json:"error"`
	Details []search.FieldError `json:"details,omitempty"`
}

type Handler struct {
	svc *search.Service
}

func NewHandler(svc *search.Service) *Handler { return &Handler{svc: svc} }

// Routes registers the API endpoints.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", h.health)
	mux.HandleFunc("GET /api/listings/search", h.searchListings)
	return mux
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// searchListings handles GET /api/listings/search.
//
// Query params (all optional): minPrice, maxPrice, minBedrooms, city, keyword,
// targetBudget, page (default 1), pageSize (default 10, max 100).
func (h *Handler) searchListings(w http.ResponseWriter, r *http.Request) {
	req, err := BindSearchRequest(r.URL.Query())
	if err != nil {
		writeError(w, err)
		return
	}
	result, err := h.svc.Search(r.Context(), req.ToParams())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeError(w http.ResponseWriter, err error) {
	var verr *search.ValidationError
	if errors.As(err, &verr) {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid_parameters", Details: verr.Fields})
		return
	}
	log.Printf("internal error: %v", err)
	writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal_error"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("encode response: %v", err)
	}
}
