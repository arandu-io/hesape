package pagination_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/pagination"
)

// cursors is the signer these tests page with. A cursor carries the boundary
// row of a page and comes back from the client, so writing one takes the
// application key, here as everywhere.
var cursors = pagination.NewCursorSigner(
	encryption.NewSigner([]byte("an application key long enough to be one")), 0)

// signedOptions is the Options a cursor paginator needs: a path, and the signer
// without which CursorPaginate refuses to write a link.
func signedOptions(path string) pagination.Options {
	return pagination.Options{Path: path, Signer: cursors}
}

// cursorIn reads the cursor out of a generated URL.
//
// A link is compared this way and not against a second encoding of the same
// cursor: the token carries an expiry, so two encodings of one cursor are two
// different strings.
func cursorIn(t *testing.T, rawURL string) *pagination.Cursor {
	t.Helper()
	got := pagination.ResolveCurrentCursor(cursors, mustParse(t, rawURL), "")
	if got == nil {
		t.Fatalf("no cursor in %q", rawURL)
	}
	return got
}

// parametersOf reads the ordering values back out of a cursor.
//
// The encoded form merges the direction in under _pointsToNextItems, so the flag
// is dropped here to leave the columns the caller passed in.
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
// NewCursor returns a value, because a cursor is one; the constructors take a
// pointer so that "no cursor" is expressible.
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

	got, err := cursors.FromEncoded(cursors.Encode(want))
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
	got, err := cursors.FromEncoded(cursors.Encode(want))
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
// reading the first one again.
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
	encoded := cursors.Encode(c)
	if strings.ContainsAny(encoded, "+/=?&#") {
		t.Errorf("Encode = %q, want only URL-safe characters", encoded)
	}
}

func TestCursorEncodeEmptyParameters(t *testing.T) {
	got, err := cursors.FromEncoded(cursors.Encode(pagination.NewCursor(nil, true)))
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
	encoded := cursors.Encode(pagination.NewCursor(map[string]string{"id": "1"}, true))
	if _, err := cursors.FromEncoded(encoded + "=="); err != nil {
		t.Fatalf("FromEncoded with padding = %v", err)
	}
}

func TestFromEncodedRejectsRubbish(t *testing.T) {
	for _, raw := range []string{"", "not base64 at all!!", "aGVsbG8"} {
		if _, err := cursors.FromEncoded(raw); !errors.Is(err, pagination.ErrCursor) {
			t.Errorf("FromEncoded(%q) error = %v, want ErrCursor", raw, err)
		}
	}
}

// A cursor over a 64-bit key has to survive the round trip with every digit it
// was written with. Decoding through a float would round it, and a rounded
// boundary walks past rows.
func TestCursorKeepsTheDigitsOfALargeKey(t *testing.T) {
	const key = "9007199254740993" // 2^53 + 1: the first integer a float64 cannot hold
	encoded := cursors.Encode(pagination.NewCursor(map[string]string{"id": key}, true))

	got, err := cursors.FromEncoded(encoded)
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
	encoded := cursors.Encode(pagination.NewCursor(map[string]string{"id": "42"}, true))
	got := pagination.ResolveCurrentCursor(cursors, mustParse(t, "/users?cursor="+encoded), "")
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
// a 400, so it reads as "start from the beginning".
func TestResolveCurrentCursorAbsentOrBroken(t *testing.T) {
	for _, raw := range []string{"/users", "/users?cursor=", "/users?cursor=%21%21%21"} {
		if got := pagination.ResolveCurrentCursor(cursors, mustParse(t, raw), ""); got != nil {
			t.Errorf("ResolveCurrentCursor(%q) = %v, want nil", raw, got)
		}
	}
	if got := pagination.ResolveCurrentCursor(cursors, nil, ""); got != nil {
		t.Errorf("ResolveCurrentCursor(nil) = %v, want nil", got)
	}
}

func TestResolveCurrentCursorCustomName(t *testing.T) {
	encoded := cursors.Encode(pagination.NewCursor(map[string]string{"id": "8"}, false))
	got := pagination.ResolveCurrentCursor(cursors, mustParse(t, "/users?after="+encoded), "after")
	if got == nil {
		t.Fatal("ResolveCurrentCursor = nil, want a cursor")
	}
	if got.PointsToNextItems() {
		t.Error("PointsToNextItems = true, want false")
	}
}

// The whole point of the signature: a client that reads its own cursor, moves
// the boundary to a row it was never shown and sends it back gets nothing.
func TestCursorRejectsATamperedToken(t *testing.T) {
	token := cursors.Encode(pagination.NewCursor(map[string]string{"id": "7"}, true))
	payload, _, _ := strings.Cut(token, ".")

	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("the payload of a token is not base64: %v", err)
	}
	edited := strings.Replace(string(raw), `"7"`, `"9000"`, 1)
	if edited == string(raw) {
		t.Fatalf("the payload %s does not carry the value to edit", raw)
	}
	forged := base64.RawURLEncoding.EncodeToString([]byte(edited)) + token[len(payload):]

	if _, err := cursors.FromEncoded(forged); !errors.Is(err, pagination.ErrCursor) {
		t.Fatalf("FromEncoded of an edited cursor = %v, want ErrCursor", err)
	}
	if _, err := cursors.FromEncoded(forged); !errors.Is(err, encryption.ErrSignature) {
		t.Errorf("the error does not unwrap to encryption.ErrSignature, so a caller cannot tell forged from expired")
	}
}

// A token signed with another application's key does not read here, which is
// what makes one key per application worth anything.
func TestCursorRejectsAnotherApplicationsKey(t *testing.T) {
	other := pagination.NewCursorSigner(
		encryption.NewSigner([]byte("a different application key entirely")), 0)
	token := other.Encode(pagination.NewCursor(map[string]string{"id": "7"}, true))

	if _, err := cursors.FromEncoded(token); !errors.Is(err, pagination.ErrCursor) {
		t.Fatalf("FromEncoded of another key's cursor = %v, want ErrCursor", err)
	}
}

// A bare base64 payload is what an unsigned cursor looks like, and it is what a
// forged one looks like once the signature is dropped. There is no reading that
// accepts it.
func TestCursorRejectsAnUnsignedToken(t *testing.T) {
	payload, err := json.Marshal(pagination.NewCursor(map[string]string{"id": "7"}, true).ToArray())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(payload)

	if _, err := cursors.FromEncoded(unsigned); !errors.Is(err, pagination.ErrCursor) {
		t.Fatalf("FromEncoded of an unsigned cursor = %v, want ErrCursor", err)
	}
	if got := pagination.ResolveCurrentCursor(cursors, mustParse(t, "/users?cursor="+unsigned), ""); got != nil {
		t.Errorf("ResolveCurrentCursor of an unsigned cursor = %v, want the first page", got)
	}
}

func TestNewCursorSignerWithoutAKeyPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewCursorSigner with a nil signer did not panic")
		}
	}()
	pagination.NewCursorSigner(nil, 0)
}

func TestResolveCurrentCursorWithoutASignerPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ResolveCurrentCursor with a nil signer did not panic")
		}
	}()
	pagination.ResolveCurrentCursor(nil, mustParse(t, "/users?cursor=x"), "")
}

// TestACursorHoldingBytesThatAreNotTextHasNoToken: json.Marshal replaces every
// byte that is not valid UTF-8 with U+FFFD rather than failing, so a cursor over
// a boundary value carrying raw bytes used to encode, verify and decode back to
// a different boundary than the one it was made from -- and a query reading from
// that boundary walks past rows and repeats rows with no error anywhere. Found
// by fuzzing the round trip.
func TestACursorHoldingBytesThatAreNotTextHasNoToken(t *testing.T) {
	for _, value := range []string{"\xff", "a\x80b", "\xed\xa0\x80"} {
		if token := cursors.Encode(pagination.NewCursor(map[string]string{"id": value}, true)); token != "" {
			back, err := cursors.FromEncoded(token)
			if err != nil {
				t.Fatalf("value %q encoded to a token that does not decode: %v", value, err)
			}
			got, _ := back.Parameter("id")
			t.Errorf("value %q encoded and came back as %q", value, got)
		}
	}
}

// TestACursorHoldingTextEncodesEveryByteOfIt guards the other side of the check
// above: a value is refused for not being UTF-8, never for being unusual.
func TestACursorHoldingTextEncodesEveryByteOfIt(t *testing.T) {
	for _, value := range []string{"", "\x00", "a\tb", "café", "日本語", "ÿ", `{"quoted": "="}`} {
		back, err := cursors.FromEncoded(cursors.Encode(pagination.NewCursor(map[string]string{"id": value}, true)))
		if err != nil {
			t.Errorf("value %q: %v", value, err)
			continue
		}
		if got, _ := back.Parameter("id"); got != value {
			t.Errorf("value %q came back as %q", value, got)
		}
	}
}

// TestAPageWhoseCursorHasNoTokenHasNoLinkToIt: URL fell through to
// Options.url, which drops an empty value and so answered with the address of
// the first page -- offered as the next one, which is a loop.
func TestAPageWhoseCursorHasNoTokenHasNoLinkToIt(t *testing.T) {
	p := pagination.CursorPaginate(ascending(1, 3), 10, nil, postKey, signedOptions("/posts"))
	unencodable := pagination.NewCursor(map[string]string{"id": "\xff"}, true)

	if got := p.URL(&unencodable); got != "" {
		t.Errorf("URL = %q, want the empty string rather than a link to the first page", got)
	}
}
