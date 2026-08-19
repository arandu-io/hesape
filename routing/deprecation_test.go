package routing_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/routing"
)

var (
	// deprecatedAt and sunsetAt are the two instants every test in this file
	// marks a route with. They are fixed, so the headers they produce are a
	// value a test can compare against rather than describe.
	deprecatedAt = time.Date(2026, time.June, 30, 23, 59, 59, 0, time.UTC)
	sunsetAt     = time.Date(2027, time.June, 30, 23, 59, 59, 0, time.UTC)
)

// TestADeprecatedRouteAnswersWithBothHeaders. The dates are on the response,
// so a client learns the route is going away from the route itself.
func TestADeprecatedRouteAnswersWithBothHeaders(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/v1/invoices", ok).Name("v1.invoices").Deprecated(deprecatedAt, sunsetAt)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/invoices", nil))

	// 1782863999 is 2026-06-30T23:59:59Z, checked against date(1) rather than
	// against the code that produces it.
	if got, want := rec.Header().Get("Deprecation"), "@1782863999"; got != want {
		t.Errorf("Deprecation = %q, want %q", got, want)
	}
	if got, want := rec.Header().Get("Sunset"), "Wed, 30 Jun 2027 23:59:59 GMT"; got != want {
		t.Errorf("Sunset = %q, want %q", got, want)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("a deprecated route stopped answering: %d", rec.Code)
	}
}

// TestARouteThatIsNotDeprecatedSendsNeitherHeader, because a header on every
// response is a header nobody reads.
func TestARouteThatIsNotDeprecatedSendsNeitherHeader(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/invoices", ok)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/invoices", nil))

	for _, name := range []string{"Deprecation", "Sunset"} {
		if got := rec.Header().Get(name); got != "" {
			t.Errorf("a route that is not deprecated sent %s: %q", name, got)
		}
	}
}

// TestTheHeadersSurviveAHandlerThatWritesAStatus. A handler that writes a
// status freezes the header block, so a marker set after it never leaves the
// process.
//
// It goes over a real connection rather than through a recorder on purpose: a
// httptest.ResponseRecorder keeps handing out its live header map after
// WriteHeader, so a marker written too late still shows up in it and the test
// would pass on an order that fails in production.
func TestTheHeadersSurviveAHandlerThatWritesAStatus(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/v1/invoices", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("gone soon"))
	})).Deprecated(deprecatedAt, sunsetAt)

	server := httptest.NewServer(r)
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/v1/invoices")
	if err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("the handler did not run: %d", resp.StatusCode)
	}
	if resp.Header.Get("Sunset") == "" {
		t.Error("the handler's write dropped the Sunset field")
	}
	if resp.Header.Get("Deprecation") == "" {
		t.Error("the handler's write dropped the Deprecation field")
	}
}

// TestRunSendsTheHeadersToo. Run skips the middleware, which is the router's
// work; the dates are the route's, and skipping them would make the marker
// depend on which path dispatched the route.
func TestRunSendsTheHeadersToo(t *testing.T) {
	r := routing.NewRouter()
	route := r.Get("/v1/invoices", ok).Deprecated(deprecatedAt, sunsetAt)

	rec := httptest.NewRecorder()
	route.Run(rec, httptest.NewRequest(http.MethodGet, "/v1/invoices", nil))

	if rec.Header().Get("Sunset") == "" {
		t.Error("Run sent no Sunset field")
	}
	if rec.Header().Get("Deprecation") == "" {
		t.Error("Run sent no Deprecation field")
	}
}

// TestASunsetBeforeTheDeprecationDatePanics, at registration, because the two
// fields would contradict each other on the wire.
func TestASunsetBeforeTheDeprecationDatePanics(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("a sunset before the deprecation date was accepted")
		}
		if msg, _ := recovered.(string); !strings.Contains(msg, "/v1/invoices") {
			t.Errorf("the panic does not name the route: %v", recovered)
		}
	}()

	r := routing.NewRouter()
	r.Get("/v1/invoices", ok).Deprecated(sunsetAt, deprecatedAt)
}

// TestAMissingDatePanics. One date without the other cannot produce the pair
// the response is supposed to carry.
func TestAMissingDatePanics(t *testing.T) {
	cases := map[string]struct{ since, sunset time.Time }{
		"no deprecation date": {time.Time{}, sunsetAt},
		"no sunset date":      {deprecatedAt, time.Time{}},
		"neither":             {time.Time{}, time.Time{}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("the incomplete pair was accepted")
				}
			}()
			r := routing.NewRouter()
			r.Get("/v1/invoices", ok).Deprecated(c.since, c.sunset)
		})
	}
}

// TestIsDeprecatedAndGetDeprecationReadBackWhatWasSet, which is what route
// introspection outside this package goes through.
func TestIsDeprecatedAndGetDeprecationReadBackWhatWasSet(t *testing.T) {
	r := routing.NewRouter()
	plain := r.Get("/invoices", ok)
	marked := r.Get("/v1/invoices", ok).Deprecated(deprecatedAt, sunsetAt)

	if plain.IsDeprecated() {
		t.Error("a route nobody marked reports itself deprecated")
	}
	if since, sunset := plain.GetDeprecation(); !since.IsZero() || !sunset.IsZero() {
		t.Errorf("an unmarked route carries dates: %v, %v", since, sunset)
	}
	if !marked.IsDeprecated() {
		t.Error("a marked route does not report itself deprecated")
	}
	since, sunset := marked.GetDeprecation()
	if !since.Equal(deprecatedAt) || !sunset.Equal(sunsetAt) {
		t.Errorf("GetDeprecation = %v, %v, want %v, %v", since, sunset, deprecatedAt, sunsetAt)
	}
}

// TestTheMarkerReachesEverySiblingOfAMatchRegistration. Match produces one row
// per verb, and a marker on the first alone would leave the other verbs of the
// same route answering without the dates.
func TestTheMarkerReachesEverySiblingOfAMatchRegistration(t *testing.T) {
	r := routing.NewRouter()
	r.Match([]string{http.MethodGet, http.MethodPost}, "/v1/search", ok).
		Deprecated(deprecatedAt, sunsetAt)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, "/v1/search", nil))
		if rec.Header().Get("Sunset") == "" {
			t.Errorf("%s /v1/search sent no Sunset field", method)
		}
	}
}

// TestFormatRoutesPrintsTheSunsetDate, so the route table says which routes are
// going away without anybody having to read the registration.
func TestFormatRoutesPrintsTheSunsetDate(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/v1/invoices", ok).Name("v1.invoices").Deprecated(deprecatedAt, sunsetAt)
	r.Get("/v1/health", ok).Deprecated(deprecatedAt, sunsetAt)
	r.Get("/invoices", ok).Name("invoices")

	out := routing.FormatRoutes(r.Routes())

	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "/v1/invoices"):
			if !strings.HasSuffix(line, "sunset 2027-06-30") {
				t.Errorf("a named deprecated route does not end in its sunset date: %q", line)
			}
			if !strings.Contains(line, "v1.invoices") {
				t.Errorf("the sunset date replaced the name: %q", line)
			}
		case strings.Contains(line, "/v1/health"):
			if !strings.HasSuffix(line, "sunset 2027-06-30") {
				t.Errorf("an unnamed deprecated route does not end in its sunset date: %q", line)
			}
		case strings.Contains(line, "/invoices"):
			if strings.Contains(line, "sunset") {
				t.Errorf("a route nobody deprecated prints a sunset date: %q", line)
			}
		}
	}
}

// TestFormatRoutesIsUnchangedForRoutesNobodyDeprecated. The table is pinned by
// value elsewhere, so the column may not move rows that have nothing in it.
func TestFormatRoutesIsUnchangedForRoutesNobodyDeprecated(t *testing.T) {
	r := routing.NewRouter()
	r.Get("/invoices/{id}", ok).Name("invoices.show")
	r.Get("/health", ok)

	// The table sorts by pattern, so /health leads.
	want := "  GET     /health\n" +
		"  GET     /invoices/{id}                     invoices.show\n"
	if got := routing.FormatRoutes(r.Routes()); got != want {
		t.Errorf("FormatRoutes =\n%q\nwant\n%q", got, want)
	}
}
