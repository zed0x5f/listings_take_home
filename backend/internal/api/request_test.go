package api

import (
	"errors"
	"net/url"
	"testing"

	"github.com/tcap/listings/internal/search"
)

func bind(t *testing.T, raw string) (SearchRequest, []search.FieldError) {
	t.Helper()
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	req, err := BindSearchRequest(q)
	var verr *search.ValidationError
	if errors.As(err, &verr) {
		return req, verr.Fields
	}
	if err != nil {
		t.Fatal(err)
	}
	return req, nil
}

func TestBind_DefaultsWhenAbsent(t *testing.T) {
	req, fields := bind(t, "")
	if fields != nil {
		t.Fatalf("unexpected errors %+v", fields)
	}
	p := req.ToParams()
	if p.Page != 1 || p.PageSize != search.DefaultPageSize || p.TargetBudget != nil || p.Filter.MinPrice != nil {
		t.Fatalf("params = %+v", p)
	}
}

func TestBind_TrimsWhitespace(t *testing.T) {
	req, fields := bind(t, "minPrice=%20100%20&city=%20Reston%20&pageSize=%205")
	if fields != nil {
		t.Fatalf("unexpected errors %+v", fields)
	}
	if *req.MinPrice != 100 || req.City != "Reston" || *req.PageSize != 5 {
		t.Fatalf("req = %+v", req)
	}
}

func TestBind_ZeroIsValidatedNotSkipped(t *testing.T) {
	// Guards the pointer fields: with a plain int, omitempty would treat 0 as absent.
	_, fields := bind(t, "pageSize=0")
	if len(fields) != 1 || fields[0].Field != "pageSize" {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestBind_PriceRangeOnlyCheckedWhenBothSet(t *testing.T) {
	for _, q := range []string{"minPrice=500", "maxPrice=500", "minPrice=500&maxPrice=500"} {
		if _, fields := bind(t, q); fields != nil {
			t.Errorf("%s: unexpected errors %+v", q, fields)
		}
	}
}

func TestBind_Messages(t *testing.T) {
	_, fields := bind(t, "minPrice=900&maxPrice=100&minBedrooms=1.5&page=0&pageSize=101&targetBudget=Inf&keyword=x")
	want := []search.FieldError{
		{Field: "minBedrooms", Message: "must be a whole number"},
		{Field: "minPrice", Message: "must be less than or equal to maxPrice"},
		{Field: "page", Message: "must be 1 or greater"},
		{Field: "pageSize", Message: "must be 100 or less"},
		{Field: "targetBudget", Message: "must be a finite number"},
	}
	if len(fields) != len(want) {
		t.Fatalf("fields = %+v", fields)
	}
	for i := range want {
		if fields[i] != want[i] {
			t.Errorf("fields[%d] = %+v, want %+v", i, fields[i], want[i])
		}
	}
}
