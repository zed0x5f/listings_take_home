package api

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"sort"
	"strings"

	"github.com/go-playground/form/v4"
	"github.com/go-playground/validator/v10"

	"github.com/tcap/listings/internal/search"
	"github.com/tcap/listings/internal/store"
)

// SearchRequest is the query-string DTO for GET /api/listings/search.
//
// Optional fields are pointers so "not sent" (nil, skipped by omitempty) is
// distinguishable from "sent as 0" (validated) — otherwise pageSize=0 would
// slip through omitempty. Blank values decode to nil, so an empty form field
// is treated as absent.
type SearchRequest struct {
	MinPrice     *float64 `form:"minPrice"     validate:"omitempty,finite,gte=0"`
	MaxPrice     *float64 `form:"maxPrice"     validate:"omitempty,finite,gte=0"`
	MinBedrooms  *int     `form:"minBedrooms"  validate:"omitempty,gte=0"`
	City         string   `form:"city"`
	Keyword      string   `form:"keyword"`
	TargetBudget *float64 `form:"targetBudget" validate:"omitempty,finite,gt=0"`
	Page         *int     `form:"page"         validate:"omitempty,gte=1"`
	PageSize     *int     `form:"pageSize"     validate:"omitempty,gte=1,lte=100"` // keep in sync with search.MaxPageSize
}

// ToParams maps the validated DTO onto the domain type, applying defaults.
func (r SearchRequest) ToParams() search.Params {
	p := search.Params{
		Filter: store.Filter{
			MinPrice:    r.MinPrice,
			MaxPrice:    r.MaxPrice,
			MinBedrooms: r.MinBedrooms,
			City:        r.City,
			Keyword:     r.Keyword,
		},
		TargetBudget: r.TargetBudget,
		Page:         1,
		PageSize:     search.DefaultPageSize,
	}
	if r.Page != nil {
		p.Page = *r.Page
	}
	if r.PageSize != nil {
		p.PageSize = *r.PageSize
	}
	return p
}

var (
	decoder  = form.NewDecoder()
	validate = newValidator()
)

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	// Report errors using the query-param name, not the Go field name.
	v.RegisterTagNameFunc(func(f reflect.StructField) string { return f.Tag.Get("form") })
	// NaN fails gte, but +Inf would pass it, so reject non-finite numbers explicitly.
	v.RegisterValidation("finite", func(fl validator.FieldLevel) bool {
		f := fl.Field().Float()
		return !math.IsNaN(f) && !math.IsInf(f, 0)
	})
	// Cross-field tags (ltefield) fail when either side is nil, so the range
	// check is a struct-level rule that only applies when both are present.
	v.RegisterStructValidation(func(sl validator.StructLevel) {
		r := sl.Current().Interface().(SearchRequest)
		if r.MinPrice != nil && r.MaxPrice != nil && *r.MinPrice > *r.MaxPrice {
			sl.ReportError(r.MinPrice, "minPrice", "MinPrice", "ltefield_maxPrice", "")
		}
	}, SearchRequest{})
	return v
}

// BindSearchRequest decodes and validates query params, returning a
// *search.ValidationError listing every bad field.
func BindSearchRequest(q url.Values) (SearchRequest, error) {
	var req SearchRequest
	var fields []search.FieldError

	// Decoding is best-effort: fields that fail to parse stay nil and are
	// reported, the rest are still validated so all problems surface at once.
	var decodeErrs form.DecodeErrors
	if err := decoder.Decode(&req, trimSpace(q)); errors.As(err, &decodeErrs) {
		for name := range decodeErrs {
			fields = append(fields, search.FieldError{Field: name, Message: parseMessage(name)})
		}
	} else if err != nil {
		return SearchRequest{}, err
	}

	var validationErrs validator.ValidationErrors
	if err := validate.Struct(req); errors.As(err, &validationErrs) {
		for _, fe := range validationErrs {
			fields = append(fields, search.FieldError{Field: fe.Field(), Message: ruleMessage(fe)})
		}
	} else if err != nil {
		return SearchRequest{}, err
	}

	if len(fields) > 0 {
		sort.Slice(fields, func(i, j int) bool { return fields[i].Field < fields[j].Field })
		return SearchRequest{}, &search.ValidationError{Fields: fields}
	}
	return req, nil
}

func trimSpace(q url.Values) url.Values {
	out := make(url.Values, len(q))
	for k, vs := range q {
		for _, v := range vs {
			out[k] = append(out[k], strings.TrimSpace(v))
		}
	}
	return out
}

// intFields records which params are integers, for decode error messages.
var intFields = func() map[string]bool {
	m := map[string]bool{}
	t := reflect.TypeFor[SearchRequest]()
	for i := range t.NumField() {
		f := t.Field(i)
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		m[f.Tag.Get("form")] = ft.Kind() == reflect.Int
	}
	return m
}()

func parseMessage(field string) string {
	if intFields[field] {
		return "must be a whole number"
	}
	return "must be a number"
}

func ruleMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "finite":
		return "must be a finite number"
	case "gte":
		return fmt.Sprintf("must be %s or greater", fe.Param())
	case "gt":
		return fmt.Sprintf("must be greater than %s", fe.Param())
	case "lte":
		return fmt.Sprintf("must be %s or less", fe.Param())
	case "ltefield_maxPrice":
		return "must be less than or equal to maxPrice"
	default:
		return "is invalid"
	}
}
