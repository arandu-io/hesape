package pipeline_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/pipeline"
)

// TestMiddlewareOrder pins the contract: the first middleware in the list is the
// outermost, so the pipeline order is the order of execution. This is the test
// that travelled with Chain from http, and the assertion is unchanged -- the
// composition became generic, the order did not.
func TestMiddlewareOrder(t *testing.T) {
	var order []string
	mark := func(name string) pipeline.Middleware[http.Handler] {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, "in:"+name)
				next.ServeHTTP(w, r)
				order = append(order, "out:"+name)
			})
		}
	}

	h := pipeline.Chain(http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
	})), mark("first"), mark("second"))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	got := strings.Join(order, ",")
	want := "in:first,in:second,handler,out:second,out:first"
	if got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}
}

// TestChainWithoutMiddlewareReturnsTheHandler: a group with nothing on it must
// not change what it wraps.
func TestChainWithoutMiddlewareReturnsTheHandler(t *testing.T) {
	called := false
	h := pipeline.Chain(http.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !called {
		t.Fatal("handler was not reached: Chain with no middleware must return it untouched")
	}
}

// TestChainIsGenericOverTheHandler is why the type moved out of http: the same
// composition has to serve a handler that is not an http.Handler.
func TestChainIsGenericOverTheHandler(t *testing.T) {
	type command func(string) string

	shout := pipeline.Middleware[command](func(next command) command {
		return func(s string) string { return next(s) + "!" }
	})
	quote := pipeline.Middleware[command](func(next command) command {
		return func(s string) string { return `"` + next(s) + `"` }
	})

	c := pipeline.Chain(command(func(s string) string { return s }), shout, quote)

	if got, want := c("ok"), `"ok"!`; got != want {
		t.Fatalf("composed = %q, want %q: the first middleware is the outermost", got, want)
	}
}

// TestIdentityLeavesTheHandlerAlone: the switched-off middleware has to be a
// value that composes, not a hole in the slice.
func TestIdentityLeavesTheHandlerAlone(t *testing.T) {
	var order []string
	record := pipeline.Middleware[http.Handler](func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "record")
			next.ServeHTTP(w, r)
		})
	})

	h := pipeline.Chain(http.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		order = append(order, "handler")
	})), pipeline.Identity, record, pipeline.Identity)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got, want := strings.Join(order, ","), "record,handler"; got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}
}

// TestHandlerComposesWithChain: the curried form of a job middleware is a
// Middleware[Handler[T]], and Chain takes a slice of them with no second
// composition function to keep in step.
func TestHandlerComposesWithChain(t *testing.T) {
	type job struct{ name string }

	var order []string
	stage := func(name string) pipeline.Middleware[pipeline.Handler[job]] {
		return func(next pipeline.Handler[job]) pipeline.Handler[job] {
			return func(ctx context.Context, j job) error {
				order = append(order, "in:"+name)
				err := next(ctx, j)
				order = append(order, "out:"+name)
				return err
			}
		}
	}

	stages := []pipeline.Middleware[pipeline.Handler[job]]{stage("first"), stage("second")}
	h := pipeline.Chain(pipeline.Handler[job](func(_ context.Context, j job) error {
		order = append(order, "handle:"+j.name)
		return nil
	}), stages...)

	if err := h(context.Background(), job{name: "invoice.send"}); err != nil {
		t.Fatalf("handler returned %v, want nil", err)
	}

	got := strings.Join(order, ",")
	want := "in:first,in:second,handle:invoice.send,out:second,out:first"
	if got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}
}

// TestHandlerStageCanStopTheHandler: a stage that refuses the work must be able
// to return without calling the next one, which is what WithoutOverlapping and
// RateLimited are.
func TestHandlerStageCanStopTheHandler(t *testing.T) {
	errRefused := errors.New("pipeline_test: refused")

	refuse := pipeline.Middleware[pipeline.Handler[int]](func(pipeline.Handler[int]) pipeline.Handler[int] {
		return func(context.Context, int) error { return errRefused }
	})

	reached := false
	h := pipeline.Chain(pipeline.Handler[int](func(context.Context, int) error {
		reached = true
		return nil
	}), refuse)

	if err := h(context.Background(), 1); !errors.Is(err, errRefused) {
		t.Fatalf("error = %v, want %v", err, errRefused)
	}
	if reached {
		t.Fatal("handler ran: a stage that does not call next must stop the pipeline")
	}
}
