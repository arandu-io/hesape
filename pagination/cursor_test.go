package pagination_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/pagination"
)

// parametersOf reads the ordering values back out of a cursor.
//
// Cursor::toArray() merges the direction in under _pointsToNextItems, so the
// flag is dropped here to leave the columns the caller passed in. The fields
// themselves are unexported, as they are read through parameter() and
// pointsToNextItems() in Illuminate.
func parametersOf(t *testing.T, c pagination.Cursor) map[string]string {
	t.Helper()
	out := make(map[string]string)
	for name, value := range c.ToArray() {
		if name == "_pointsToNextItems" {
			continue
		}
		text, ok := value.(string)
		if !ok {
			t.Fatalf("parameter %q is %T, want string", name, value)
		}
		out[name] = text
	}
	return out
}

// cursorPtr builds a cursor a paginator can be handed.
//
// NewCursor answers Cursor::__construct and returns a value, because a cursor
// is one; the constructors take a pointer so that "no cursor" is expressible,
// which is what PHP's null argument means.
func cursorPtr(parameters map[string]string, pointsToNextItems bool) *pagination.Cursor {
	c := pagination.NewCursor(parameters, pointsToNextItems)
	return &c
}

// parameterOf reads one ordering value, discarding the error a test has already
// asserted elsewhere. An absent name reads as the empty string here, and the
// comparison that follows fails on it.
func parameterOf(c *pagination.Cursor, name string) string {
	value, _ := c.Parameter(name)
	return value
}

func TestCursorRoundTrip(t *testing.T) {
	parameters := map[string]string{"created_at": "2026-08-10T16:35:00.123456Z", "id": "9f1c"}
	want := pagination.NewCursor(parameters, true)

	got, err := pagination.FromEncoded(want.Encode())
	if err != nil {
		t.Fatalf("FromEncoded = %v", err)
	}
	for name, value := range parameters {
		read, err := got.Parameter(name)
		if err != nil {
			t.Fatalf("Parameter(%q) = %v", name, err)
		}
		if read != value {
			t.Errorf("Parameter(%q) = %q, want %q", name, read, value)
		}
	}
	if !got.PointsToNextItems() {
		t.Error("PointsToNextItems = false, want true")
	}
}

func TestCursorRoundTripBackwards(t *testing.T) {
	want := pagination.NewCursor(map[string]string{"id": "7"}, false)
	got, err := pagination.FromEncoded(want.Encode())
	if err != nil {
		t.Fatalf("FromEncoded = %v", err)
	}
	if got.PointsToNextItems() {
		t.Error("PointsToNextItems = true, want false")
	}
	if !got.PointsToPreviousItems() {
		t.Error("PointsToPreviousItems = false, want true")
	}
}

// A parameter the caller does not carry is an error rather than an empty
// string, because an empty string is a legitimate value for a nullable column:
// telling the two apart is the difference between reading the next page and
// reading the first one again. Illuminate throws UnexpectedValueException here.
func TestCursorParameterThatIsNotThere(t *testing.T) {
	c := pagination.NewCursor(map[string]string{"id": "7"}, true)
	if _, err := c.Parameter("created_at"); err == nil {
		t.Error("Parameter of an absent name = nil error, want a failure")
	}
}

// Parameters reads several at once and fails on the first name it does not
// carry, rather than returning a short list.
func TestCursorParametersReadsInOrder(t *testing.T) {
	c := pagination.NewCursor(map[string]string{"created_at": "2026", "id": "7"}, true)
	got, err := c.Parameters([]string{"created_at", "id"})
	if err != nil {
		t.Fatalf("Parameters = %v", err)
	}
	if len(got) != 2 || got[0] != "2026" || got[1] != "7" {
		t.Errorf("Parameters = %v, want [2026 7]", got)
	}
	if _, err := c.Parameters([]string{"id", "missing"}); err == nil {
		t.Error("Parameters with an absent name = nil error, want a failure")
	}
}

// The map a caller keeps must not be a way to change a cursor after the fact.
func TestCursorCopiesTheParametersItWasGiven(t *testing.T) {
	parameters := map[string]string{"id": "7"}
	c := pagination.NewCursor(parameters, true)
	parameters["id"] = "8"

	got, err := c.Parameter("id")
	if err != nil {
		t.Fatalf("Parameter = %v", err)
	}
	if got != "7" {
		t.Errorf("Parameter(id) = %q, want %q: the cursor kept a reference to its caller's map", got, "7")
	}
}

// The token travels in a URL and in an e-mail, so it must survive both without
// escaping.
func TestCursorEncodeIsURLSafe(t *testing.T) {
	c := pagination.NewCursor(map[string]string{"name": "a?b&c=d/e+f"}, true)
	encoded := c.Encode()
	if strings.ContainsAny(encoded, "+/=?&#") {
		t.Errorf("Encode = %q, want only URL-safe characters", encoded)
	}
}

func TestCursorEncodeEmptyParameters(t *testing.T) {
	got, err := pagination.FromEncoded(pagination.NewCursor(nil, true).Encode())
	if err != nil {
		t.Fatalf("FromEncoded = %v", err)
	}
	if parameters := parametersOf(t, got); len(parameters) != 0 {
		t.Errorf("parameters = %v, want empty", parameters)
	}
	if !got.PointsToNextItems() {
		t.Error("PointsToNextItems = false, want true")
	}
}

func TestFromEncodedToleratesPadding(t *testing.T) {
	encoded := pagination.NewCursor(map[string]string{"id": "1"}, true).Encode()
	if _, err := pagination.FromEncoded(encoded + "=="); err != nil {
		t.Fatalf("FromEncoded with padding = %v", err)
	}
}

func TestFromEncodedRejectsRubbish(t *testing.T) {
	for _, raw := range []string{"", "not base64 at all!!", "aGVsbG8"} {
		if _, err := pagination.FromEncoded(raw); !errors.Is(err, pagination.ErrCursor) {
			t.Errorf("FromEncoded(%q) error = %v, want ErrCursor", raw, err)
		}
	}
}

// A cursor over a 64-bit key has to survive the round trip with every digit it
// was written with. Decoding through a float would round it, and a rounded
// boundary walks past rows.
func TestCursorKeepsTheDigitsOfALargeKey(t *testing.T) {
	const key = "9007199254740993" // 2^53 + 1: the first integer a float64 cannot hold
	encoded := pagination.NewCursor(map[string]string{"id": key}, true).Encode()

	got, err := pagination.FromEncoded(encoded)
	if err != nil {
		t.Fatalf("FromEncoded = %v", err)
	}
	read, err := got.Parameter("id")
	if err != nil {
		t.Fatalf("Parameter = %v", err)
	}
	if read != key {
		t.Errorf("Parameter(id) = %q, want %q", read, key)
	}
}

func TestResolveCurrentCursor(t *testing.T) {
	encoded := pagination.NewCursor(map[string]string{"id": "42"}, true).Encode()
	got := pagination.ResolveCurrentCursor(mustParse(t, "/users?cursor="+encoded), "")
	if got == nil {
		t.Fatal("ResolveCurrentCursor = nil, want a cursor")
	}
	read, err := got.Parameter("id")
	if err != nil {
		t.Fatalf("Parameter = %v", err)
	}
	if read != "42" {
		t.Errorf("Parameter(id) = %q, want %q", read, "42")
	}
}

// A mangled cursor is a truncated link in an e-mail client, not an attack worth
// a 400, so it reads as "start from the beginning". Illuminate returns null.
func TestResolveCurrentCursorAbsentOrBroken(t *testing.T) {
	for _, raw := range []string{"/users", "/users?cursor=", "/users?cursor=%21%21%21"} {
		if got := pagination.ResolveCurrentCursor(mustParse(t, raw), ""); got != nil {
			t.Errorf("ResolveCurrentCursor(%q) = %v, want nil", raw, got)
		}
	}
	if got := pagination.ResolveCurrentCursor(nil, ""); got != nil {
		t.Errorf("ResolveCurrentCursor(nil) = %v, want nil", got)
	}
}

func TestResolveCurrentCursorCustomName(t *testing.T) {
	encoded := pagination.NewCursor(map[string]string{"id": "8"}, false).Encode()
	got := pagination.ResolveCurrentCursor(mustParse(t, "/users?after="+encoded), "after")
	if got == nil {
		t.Fatal("ResolveCurrentCursor = nil, want a cursor")
	}
	if got.PointsToNextItems() {
		t.Error("PointsToNextItems = true, want false")
	}
}
