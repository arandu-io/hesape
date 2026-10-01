package http_test

import (
	"context"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
)

// TestCSRFTokenFromAnswersWhatWithCSRFTokenStored: the middleware that
// protects forms stores the token here and the page builder reads it back,
// across a context that other middleware may have wrapped in between.
func TestCSRFTokenFromAnswersWhatWithCSRFTokenStored(t *testing.T) {
	ctx := hhttp.WithCSRFToken(context.Background(), "issued-token")
	ctx = context.WithValue(ctx, struct{ name string }{"other"}, "value")

	token, ok := hhttp.CSRFTokenFrom(ctx)
	if !ok || token != "issued-token" {
		t.Fatalf("CSRFTokenFrom = %q, %v, want issued-token, true", token, ok)
	}
}

// TestCSRFTokenFromSaysSoWhenNoneWasIssued: a request no protecting middleware
// ran on has no token, and says so rather than answering an empty one as if it
// were issued.
func TestCSRFTokenFromSaysSoWhenNoneWasIssued(t *testing.T) {
	if token, ok := hhttp.CSRFTokenFrom(context.Background()); ok || token != "" {
		t.Fatalf("CSRFTokenFrom = %q, %v, want empty, false", token, ok)
	}
}

// TestAnEmptyCSRFTokenIsNotStored: storing nothing must not shadow a token an
// outer middleware already stored.
func TestAnEmptyCSRFTokenIsNotStored(t *testing.T) {
	ctx := hhttp.WithCSRFToken(context.Background(), "outer")
	ctx = hhttp.WithCSRFToken(ctx, "")

	if token, ok := hhttp.CSRFTokenFrom(ctx); !ok || token != "outer" {
		t.Fatalf("CSRFTokenFrom = %q, %v, want outer, true", token, ok)
	}
}

// TestTheCSRFTokenAndTheStateAreIndependent: the flash middleware writes a
// fresh State, and that write must not erase the token the form middleware
// stored before it -- nor the other way round.
func TestTheCSRFTokenAndTheStateAreIndependent(t *testing.T) {
	ctx := hhttp.WithCSRFToken(context.Background(), "issued-token")
	ctx = hhttp.WithState(ctx, hhttp.State{})

	if token, ok := hhttp.CSRFTokenFrom(ctx); !ok || token != "issued-token" {
		t.Fatalf("CSRFTokenFrom after WithState = %q, %v", token, ok)
	}
}
