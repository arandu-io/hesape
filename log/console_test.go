package log_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/log"
)

// consoleWith returns a console holding one request built by fill.
func consoleWith(t *testing.T, fill func(*log.Collector)) (*log.Console, string) {
	t.Helper()

	recorder := log.NewRecorder(10)
	col := log.NewCollector("abc123")
	fill(col)
	recorder.Record(log.Recorded{
		RequestID: "abc123",
		Method:    "GET",
		Path:      "/customers",
		Status:    200,
		Duration:  120 * time.Millisecond,
		At:        time.Now(),
		Collector: col,
	})
	return log.NewConsole(recorder, "vscode", nil), "abc123"
}

func get(t *testing.T, c *log.Console, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c.Handler(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestTheListShowsTheRequest(t *testing.T) {
	console, _ := consoleWith(t, func(c *log.Collector) {
		c.RecordQuery("SELECT * FROM customer", nil, 5*time.Millisecond, 10, nil)
	})

	rec := get(t, console, log.ConsolePath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{"/customers", "GET", "200", "abc123"} {
		if !strings.Contains(body, want) {
			t.Errorf("the list does not show %q", want)
		}
	}
}

// TestAnEmptyConsoleSaysWhatToDo: "no data" with nothing else is the state a
// person hits first, and it should not read like a broken page.
func TestAnEmptyConsoleSaysWhatToDo(t *testing.T) {
	console := log.NewConsole(log.NewRecorder(10), "vscode", nil)

	body := get(t, console, log.ConsolePath).Body.String()
	if !strings.Contains(body, "Nothing recorded yet") {
		t.Errorf("the empty console does not explain itself:\n%s", body)
	}
}

// TestTheListDrawsTheGauges: the numbers a process keeps are only worth keeping
// if somebody can look at them, and this page is where somebody looks.
func TestTheListDrawsTheGauges(t *testing.T) {
	gauges := log.NewGauges()
	gauges.Set(log.GaugeName{Metric: "connections", Tenant: "acme"}, 12)
	gauges.Set(log.GaugeName{Metric: "channels"}, 3)

	console := log.NewConsole(log.NewRecorder(10), "vscode", gauges)

	body := get(t, console, log.ConsolePath).Body.String()
	for _, want := range []string{"Gauges", "connections", "acme", "12", "channels", "3"} {
		if !strings.Contains(body, want) {
			t.Errorf("the gauge section does not show %q:\n%s", want, body)
		}
	}
	// An empty tenant is the process as a whole, and a blank cell reads as a
	// value that failed to arrive rather than as the answer.
	if !strings.Contains(body, "whole process") {
		t.Errorf("a gauge with no tenant renders as a blank cell:\n%s", body)
	}
}

// TestAnEmptyRegistryDrawsNoGaugeSection: a heading over an empty table on a
// diagnostic page is noise, and noise on this page costs more than elsewhere.
func TestAnEmptyRegistryDrawsNoGaugeSection(t *testing.T) {
	console := log.NewConsole(log.NewRecorder(10), "vscode", log.NewGauges())

	if body := get(t, console, log.ConsolePath).Body.String(); strings.Contains(body, "Gauges") {
		t.Errorf("a registry holding nothing still drew a section:\n%s", body)
	}
}

// TestNoGaugeRegistryIsNotAFailure covers the nil registry, which is what a
// process that measures nothing hands over. The page still answers, and it
// answers without the section.
func TestNoGaugeRegistryIsNotAFailure(t *testing.T) {
	console := log.NewConsole(log.NewRecorder(10), "vscode", nil)

	rec := get(t, console, log.ConsolePath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "Gauges") {
		t.Errorf("a console with no registry drew a gauge section:\n%s", body)
	}
}

// TestTheNPlusOneIsNamedAtTheTop is the whole point of the console. A page that
// makes you read the query table to notice the repetition only helps people who
// already knew what to look for.
func TestTheNPlusOneIsNamedAtTheTop(t *testing.T) {
	console, id := consoleWith(t, func(c *log.Collector) {
		c.RecordQuery("SELECT * FROM customer", nil, 2*time.Millisecond, 10, nil)
		for range 10 {
			c.RecordQuery("SELECT * FROM invoice WHERE customer_id = ?", nil, time.Millisecond, 1, nil)
		}
	})

	body := get(t, console, log.ConsolePath+"/"+id).Body.String()

	// "N+1" reaches the page as "N&#43;1": html/template escapes the plus, and
	// the browser renders it back. Matching on the escaped form would tie this
	// test to the escaper, so it matches on the part that is not escaped -- and
	// the JSON test below checks the text a human reads.
	if !strings.Contains(body, "the same statement ran 10 times") {
		t.Fatalf("the console did not diagnose the N+1:\n%s", body)
	}
	if !strings.Contains(body, "FROM invoice") {
		t.Error("the diagnosis does not name the statement")
	}
	// The findings block comes before the query table, or it is not a diagnosis,
	// it is a footnote.
	if strings.Index(body, "the same statement ran") > strings.Index(body, "<h2>Queries") {
		t.Error("the diagnosis is rendered below the query table")
	}
}

func TestASlowQueryIsFlagged(t *testing.T) {
	console, id := consoleWith(t, func(c *log.Collector) {
		c.RecordQuery("SELECT * FROM invoice", nil, 400*time.Millisecond, 5000, nil)
	})

	body := get(t, console, log.ConsolePath+"/"+id).Body.String()
	if !strings.Contains(body, "Slow query") {
		t.Errorf("a 400ms query was not flagged:\n%s", body)
	}
}

// TestTheTimelineIsRendered: "this page takes 800ms" leads to guessing. The
// breakdown is what turns it into an investigation.
func TestTheTimelineIsRendered(t *testing.T) {
	console, id := consoleWith(t, func(c *log.Collector) {
		c.RecordQuery("SELECT 1", nil, 90*time.Millisecond, 1, nil)
		c.RecordRender("customer/list.templ", 20*time.Millisecond)
		c.RecordExternal("GET", "https://api.example/rates", 200, 10*time.Millisecond)
	})

	body := get(t, console, log.ConsolePath+"/"+id).Body.String()
	for _, want := range []string{"sql", "render", "external", "other", "90ms", "20ms", "10ms"} {
		if !strings.Contains(body, want) {
			t.Errorf("the timeline does not show %q", want)
		}
	}
	// 90 of 120ms in the database is the finding that matters more than the bar.
	if !strings.Contains(body, "spent in the database") {
		t.Error("a request that is three quarters database did not say so")
	}
}

// TestTheOriginLinkOpensTheEditor: this is the line that saves the most time,
// and html/template silently rewrites an unknown scheme to #ZgotmplZ -- which
// turns every link on the page into a dead one with no hint why.
func TestTheOriginLinkOpensTheEditor(t *testing.T) {
	console, id := consoleWith(t, func(c *log.Collector) {
		c.RecordQuery("SELECT 1", nil, time.Millisecond, 1, nil)
	})

	body := get(t, console, log.ConsolePath+"/"+id).Body.String()
	if strings.Contains(body, "ZgotmplZ") {
		t.Fatal("the editor link was rewritten by html/template: it needs template.URL")
	}
	if !strings.Contains(body, "vscode://file") {
		t.Errorf("no editor link in the page:\n%s", body)
	}
}

// TestEveryEditorHasAScheme: every name in the table gets its own scheme, so a
// person who configured one is never handed a link into an editor they do not
// have.
func TestEveryEditorHasAScheme(t *testing.T) {
	for editor, want := range map[string]string{
		"vscode":   "vscode://file",
		"cursor":   "cursor://file",
		"goland":   "jetbrains://goland/navigate",
		"zed":      "zed://file",
		"emacs":    "emacs://open",
		"phpstorm": "phpstorm://open",
		"sublime":  "subl://open",
		"idea":     "idea://open",
		"textmate": "txmt://open",
		"xdebug":   "xdebug://",
	} {
		got := log.EditorLink(editor, "/src/app/main.go", 42)
		if !strings.HasPrefix(got, want) {
			t.Errorf("%s = %q, want the %s scheme", editor, got, want)
		}
		if !strings.Contains(got, "42") {
			t.Errorf("%s = %q, want it to carry the line number", editor, got)
		}
		if !strings.Contains(got, "/src/app/main.go") {
			t.Errorf("%s = %q, want it to carry the file", editor, got)
		}
	}
}

// TestTheConsolePageDoesNotRenderAnEmptyHref is the other half of
// TestNoEditorIsNoLink: an anchor with no href is a link back to the page it is
// on, so clicking "open in editor" reloaded the debug console. With no editor
// configured the origin renders as plain text.
func TestTheConsolePageDoesNotRenderAnEmptyHref(t *testing.T) {
	recorder := log.NewRecorder(10)
	col := log.NewCollector("no-editor")
	col.RecordQuery("SELECT 1", nil, time.Millisecond, 1, nil)
	log.Dump(log.WithCollector(context.Background(), col), "value", 1)
	recorder.Record(log.Recorded{
		RequestID: "no-editor",
		Method:    http.MethodGet,
		Path:      "/customers",
		Status:    200,
		Duration:  time.Millisecond,
		At:        time.Now(),
		Collector: col,
	})

	body := get(t, log.NewConsole(recorder, "", nil), log.ConsolePath+"/no-editor").Body.String()
	if strings.Contains(body, `href=""`) {
		t.Errorf("the page carries an anchor with no href:\n%s", body)
	}
}

// TestNoEditorIsNoLink: an unset or unknown editor renders the frame without a
// link. Guessing a scheme there is a link that opens nothing, and opens nothing
// in a way that reads as a broken debug page rather than as an editor nobody
// configured.
func TestNoEditorIsNoLink(t *testing.T) {
	for _, editor := range []string{"", "notepad", "VSCode"} {
		if got := log.EditorLink(editor, "/src/app/main.go", 42); got != "" {
			t.Errorf("EditorLink(%q) = %q, want no link at all", editor, got)
		}
	}
}

// TestTheEditorLinkLeavesTheContainer: the frame was recorded inside the
// container, at /app, and the editor is outside it. Without the rewrite the link
// is built, rendered, clicked, and opens nothing.
func TestTheEditorLinkLeavesTheContainer(t *testing.T) {
	got := log.EditorLink("vscode", "/app/handler.go", 7, log.PathRewrite{
		From: "/app",
		To:   "/Users/ana/project",
	})
	if want := "vscode://file/Users/ana/project/handler.go:7"; got != want {
		t.Errorf("EditorLink = %q, want %q", got, want)
	}

	// Only the root is translated, and only once: a path that repeats the root
	// deeper down keeps it.
	got = log.EditorLink("vscode", "/app/vendor/app/x.go", 1, log.PathRewrite{From: "/app", To: "/src"})
	if want := "vscode://file/src/vendor/app/x.go:1"; got != want {
		t.Errorf("EditorLink = %q, want %q", got, want)
	}
}

func TestDumpsAndEventsAreShown(t *testing.T) {
	console, id := consoleWith(t, func(c *log.Collector) {
		log.Dump(log.WithCollector(context.Background(), c), "the customer", map[string]any{"id": "c-1", "name": "Ana"})
		c.RecordEvent("customer.created", map[string]any{"id": "c-1"})
	})

	body := get(t, console, log.ConsolePath+"/"+id).Body.String()
	for _, want := range []string{"the customer", "Ana", "customer.created"} {
		if !strings.Contains(body, want) {
			t.Errorf("the console does not show %q", want)
		}
	}
}

func TestAFailedQueryShowsItsError(t *testing.T) {
	console, id := consoleWith(t, func(c *log.Collector) {
		c.RecordQuery("SELECT * FROM customer", nil, time.Millisecond, 0, errors.New("no such table: customer"))
	})

	body := get(t, console, log.ConsolePath+"/"+id).Body.String()
	if !strings.Contains(body, "no such table: customer") {
		t.Errorf("a failed query is shown as an ordinary row:\n%s", body)
	}
}

// TestAnUnknownIdExplainsTheBuffer: the id came from a log line, and "not
// found" alone leaves you wondering whether tracing is broken.
func TestAnUnknownIdExplainsTheBuffer(t *testing.T) {
	console, _ := consoleWith(t, func(*log.Collector) {})

	rec := get(t, console, log.ConsolePath+"/nope")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "oldest are dropped") {
		t.Errorf("the page does not explain why the id is missing:\n%s", rec.Body.String())
	}
}

// TestTheJSONMatchesThePage is what `aru trace` depends on: one source, so the
// terminal and the page never disagree about what happened.
func TestTheJSONMatchesThePage(t *testing.T) {
	console, id := consoleWith(t, func(c *log.Collector) {
		for range 6 {
			c.RecordQuery("SELECT * FROM invoice WHERE customer_id = ?", []any{"c-1"}, time.Millisecond, 1, nil)
		}
	})

	rec := get(t, console, log.ConsolePath+"/"+id+"?format=json")
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}

	var payload struct {
		ID       string   `json:"ID"`
		Findings []string `json:"Findings"`
		Queries  []struct {
			SQL      string `json:"SQL"`
			Repeated int    `json:"Repeated"`
		} `json:"Queries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decoding: %v\n%s", err, rec.Body.String())
	}
	if payload.ID != id {
		t.Errorf("id = %q", payload.ID)
	}
	if len(payload.Findings) == 0 || !strings.Contains(payload.Findings[0], "N+1") {
		t.Errorf("findings = %v, want the N+1", payload.Findings)
	}
	if len(payload.Queries) != 6 || payload.Queries[0].Repeated != 6 {
		t.Errorf("queries = %+v", payload.Queries)
	}
}

func TestTheJSONListIsAlsoAvailable(t *testing.T) {
	console, _ := consoleWith(t, func(c *log.Collector) {
		c.RecordQuery("SELECT 1", nil, time.Millisecond, 1, nil)
	})

	rec := get(t, console, log.ConsolePath+"?format=json")
	var payload struct {
		Rows []struct {
			ID   string `json:"ID"`
			Path string `json:"Path"`
		} `json:"Rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(payload.Rows) != 1 || payload.Rows[0].Path != "/customers" {
		t.Fatalf("rows = %+v", payload.Rows)
	}
}

func TestAnUnknownIdInJSONIsNotFound(t *testing.T) {
	console, _ := consoleWith(t, func(*log.Collector) {})

	rec := get(t, console, log.ConsolePath+"/nope?format=json")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 so the CLI can tell the difference", rec.Code)
	}
}

// TestTheConsoleEscapesWhatItRenders: a path, a dump or a statement can carry
// anything, and the console renders it back. Without escaping, one request with
// a script tag in the query string turns the debug page into a way to run code
// in the developer's browser.
func TestTheConsoleEscapesWhatItRenders(t *testing.T) {
	recorder := log.NewRecorder(10)
	col := log.NewCollector("xss")
	log.Dump(log.WithCollector(context.Background(), col), "<script>alert(1)</script>", "<img src=x onerror=alert(1)>")
	recorder.Record(log.Recorded{
		RequestID: "xss",
		Method:    "GET",
		Path:      "/customers?q=<script>alert(1)</script>",
		Status:    200,
		Duration:  time.Millisecond,
		At:        time.Now(),
		Collector: col,
	})
	console := log.NewConsole(recorder, "vscode", nil)

	for _, path := range []string{log.ConsolePath, log.ConsolePath + "/xss"} {
		body := get(t, console, path).Body.String()
		// The angle bracket is what decides it. The words can appear as text --
		// they are, after all, what the request contained.
		if strings.Contains(body, "<script>alert") {
			t.Errorf("%s rendered a script tag unescaped", path)
		}
		if strings.Contains(body, "<img src=x") {
			t.Errorf("%s rendered an img tag unescaped", path)
		}
	}
}
