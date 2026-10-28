package search

import (
	"fmt"
	"strings"

	"github.com/tcap/listings/internal/store"
)

const (
	DefaultPageSize = 10
	MaxPageSize     = 100
)

// Params is a fully validated search request. The HTTP layer builds it from
// api.SearchRequest; the service only ever sees valid values.
type Params struct {
	Filter       store.Filter
	TargetBudget *float64
	Page         int // 1-based
	PageSize     int
}

// FieldError describes one invalid input field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError collects every problem with a request so the client can fix
// them all at once instead of one round-trip per mistake.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + ": " + f.Message
	}
	return "invalid search parameters: " + strings.Join(parts, "; ")
}

func (e *ValidationError) add(field, format string, args ...any) {
	e.Fields = append(e.Fields, FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
}
