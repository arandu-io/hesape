package http_test

import (
	"context"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
)

// TestAppNameFromAnswersWhatWithAppNameStored: the framework stores the name
// once and the page builder reads it back, across a context that other
// middleware may have wrapped in between.
func TestAppNameFromAnswersWhatWithAppNameStored(t *testing.T) {
	ctx := hhttp.WithAppName(context.Background(), "Billing")
	ctx = context.WithValue(ctx, struct{ name string }{"other"}, "value")

	if got := hhttp.AppNameFrom(ctx); got != "Billing" {
		t.Fatalf("AppNameFrom = %q, want Billing", got)
	}
}

// TestAppNameFromIsEmptyWhenNoneWasStored: a request the framework did not put
// a name on answers empty, and asking does not panic.
func TestAppNameFromIsEmptyWhenNoneWasStored(t *testing.T) {
	if got := hhttp.AppNameFrom(context.Background()); got != "" {
		t.Fatalf("AppNameFrom = %q, want empty", got)
	}
}

// TestAnEmptyAppNameIsNotStored: storing nothing must not shadow a name an
// outer middleware already stored.
func TestAnEmptyAppNameIsNotStored(t *testing.T) {
	ctx := hhttp.WithAppName(context.Background(), "outer")
	ctx = hhttp.WithAppName(ctx, "")

	if got := hhttp.AppNameFrom(ctx); got != "outer" {
		t.Fatalf("AppNameFrom = %q, want outer", got)
	}
}

// TestTheAppNameIsIndependentOfTheOtherRequestState: the flash middleware
// writes a fresh State and the form middleware a token, and neither write may
// erase the name -- nor the name either of them.
func TestTheAppNameIsIndependentOfTheOtherRequestState(t *testing.T) {
	ctx := hhttp.WithAppName(context.Background(), "Billing")
	ctx = hhttp.WithState(ctx, hhttp.State{})
	ctx = hhttp.WithCSRFToken(ctx, "issued-token")

	if got := hhttp.AppNameFrom(ctx); got != "Billing" {
		t.Fatalf("AppNameFrom after WithState and WithCSRFToken = %q, want Billing", got)
	}
	if token, ok := hhttp.CSRFTokenFrom(ctx); !ok || token != "issued-token" {
		t.Fatalf("CSRFTokenFrom after WithAppName = %q, %v", token, ok)
	}
}
