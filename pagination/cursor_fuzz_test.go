package pagination

// A cursor is the one value in this package that arrives from outside: it comes
// back in a query string, and reading it runs an HMAC check and a JSON decode
// over whatever was sent. These targets are internal rather than in
// pagination_test because signing an arbitrary payload takes cursorPurpose,
// which is not exported.
//
// What they do not cover: the signature comparison is hmac.Equal, so it does
// not return early on the first wrong byte. That is a property of the code, not
// of any input, and no number of executions is evidence of it.

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/arandu-io/hesape/encryption"
)

// fuzzKey is the application key these targets sign with. It is not the key the
// rest of the package's tests use, so a token that leaks from one into the
// other is refused rather than quietly accepted.
var fuzzKey = encryption.NewSigner([]byte("a key for the fuzz targets, long enough"))

var fuzzCursors = NewCursorSigner(fuzzKey, time.Hour)

// FuzzCursorFromEncoded feeds arbitrary strings to the reader a query string
// reaches. Nothing may panic, nothing may run forever, and a cursor that was
// refused may not carry a parameter out with it.
func FuzzCursorFromEncoded(f *testing.F) {
	f.Add("")
	f.Add("=")
	f.Add("====")
	f.Add(".")
	f.Add("..")
	f.Add("a.b.c")
	f.Add("a.b.c.d")
	f.Add(strings.Repeat(".", 4096))
	f.Add(fuzzCursors.Encode(NewCursor(map[string]string{"id": "42"}, true)))
	f.Add(fuzzCursors.Encode(NewCursor(map[string]string{"created_at": "2026-08-20T11:04:05.123456Z", "id": "42"}, false)))

	f.Fuzz(func(t *testing.T, token string) {
		cursor, err := fuzzCursors.FromEncoded(token)
		if err != nil {
			if !errors.Is(err, ErrCursor) {
				t.Fatalf("FromEncoded(%q) = %v, which is not an ErrCursor", token, err)
			}
			if fields := cursor.ToArray(); len(fields) != 1 {
				t.Fatalf("a refused cursor carried %v out", fields)
			}
			if cursor.PointsToNextItems() {
				t.Fatal("a refused cursor points forward")
			}
			return
		}

		// Accepted means this application wrote it, so the other door onto the
		// same token has to agree. A difference between the two is a page that
		// reads one boundary from a link and another from the same link.
		u := &url.URL{Path: "/items", RawQuery: url.Values{DefaultCursorName: {token}}.Encode()}
		resolved := ResolveCurrentCursor(fuzzCursors, u, "")
		if resolved == nil {
			t.Fatalf("FromEncoded read %q and ResolveCurrentCursor read nothing", token)
		}
		if resolved.PointsToNextItems() != cursor.PointsToNextItems() {
			t.Fatalf("the two readings of %q disagree on the direction", token)
		}
		for name, value := range cursor.ToArray() {
			if name == pointsToNextItemsKey {
				continue
			}
			got, err := resolved.Parameter(name)
			if err != nil || got != value {
				t.Fatalf("the two readings of %q disagree on [%s]: %q and %v", token, name, value, got)
			}
		}
	})
}

// FuzzCursorTamperedTokenIsRefused changes one byte of a token this application
// wrote and requires the result to be refused. It is the invariant the key is
// there for: a client that edits the boundary moves it onto a row that was
// never shown.
func FuzzCursorTamperedTokenIsRefused(f *testing.F) {
	f.Add("id", "42", uint(0), byte(1))
	f.Add("id", "42", uint(7), byte(128))
	f.Add("created_at", "2026-08-20T11:04:05Z", uint(31), byte(255))

	f.Fuzz(func(t *testing.T, name, value string, position uint, delta byte) {
		token := fuzzCursors.Encode(NewCursor(map[string]string{name: value}, true))
		if token == "" || delta == 0 {
			return
		}

		bytes := []byte(token)
		bytes[position%uint(len(bytes))] += delta
		tampered := string(bytes)

		// Trailing padding is tolerated on purpose, so a change that only adds
		// or removes some is not a change to the token.
		if strings.TrimRight(tampered, "=") == strings.TrimRight(token, "=") {
			return
		}
		if _, err := fuzzCursors.FromEncoded(tampered); err == nil {
			t.Fatalf("%q was accepted after one byte of %q was changed", tampered, token)
		}
	})
}

// FuzzCursorSignedPayload signs arbitrary bytes with the key and reads them
// back, which is the only way to reach the JSON decoder with a shape Encode
// never writes: nested to any depth, holding any number, holding a value that
// is not a scalar at all.
//
// It is not a threat model -- signing takes the key -- it is the decoder's own
// bounds. What it asserts is that the work stays in proportion to the input: a
// payload cannot produce more parameters than it has bytes.
func FuzzCursorSignedPayload(f *testing.F) {
	f.Add(`{}`)
	f.Add(`null`)
	f.Add(`[]`)
	f.Add(`{"id":"42","_pointsToNextItems":true}`)
	f.Add(`{"id":9223372036854775807}`)
	f.Add(`{"id":1e1000}`)
	f.Add(`{"id":{"nested":"object"}}`)
	f.Add(`{"id":"1"} {"id":"2"}`)
	f.Add(`{"id":"1","id":"2"}`)
	f.Add(strings.Repeat(`{"a":`, 20000))

	f.Fuzz(func(t *testing.T, payload string) {
		token := fuzzKey.Sign(cursorPurpose, payload, time.Hour)

		cursor, err := fuzzCursors.FromEncoded(token)
		if err != nil {
			if !errors.Is(err, ErrCursor) {
				t.Fatalf("a signed payload failed with %v, which is not an ErrCursor", err)
			}
			return
		}
		if parameters := len(cursor.ToArray()) - 1; parameters > len(payload) {
			t.Fatalf("a payload of %d bytes produced %d parameters", len(payload), parameters)
		}
	})
}

// FuzzCursorRoundTrip requires a cursor that has a token to come back out of it
// unchanged. A boundary that shifts in transit walks the next query past rows
// and back over rows, and nothing reports it.
func FuzzCursorRoundTrip(f *testing.F) {
	f.Add("id", "42", "created_at", "2026-08-20T11:04:05.123456Z", true)
	f.Add("", "", "", "", false)
	f.Add("id", "\xff", "name", "café", true)
	f.Add("id", "\x00", "name", "日本語", false)
	f.Add("id", `{"quoted":"="}`, "name", "a\tb", true)

	f.Fuzz(func(t *testing.T, firstName, firstValue, secondName, secondValue string, next bool) {
		parameters := map[string]string{firstName: firstValue, secondName: secondValue}
		if _, taken := parameters[pointsToNextItemsKey]; taken {
			// An ordering column of this name collides with the direction, and
			// ToArray says why that is not defended against.
			return
		}

		token := fuzzCursors.Encode(NewCursor(parameters, next))
		if token == "" {
			// The one reason a cursor has no token.
			for name, value := range parameters {
				if !utf8.ValidString(name) || !utf8.ValidString(value) {
					return
				}
			}
			t.Fatalf("no token for %v, and every name and value is text", parameters)
		}

		back, err := fuzzCursors.FromEncoded(token)
		if err != nil {
			t.Fatalf("the token for %v does not read back: %v", parameters, err)
		}
		if back.PointsToNextItems() != next {
			t.Fatalf("the direction came back as %v, want %v", back.PointsToNextItems(), next)
		}
		if fields := len(back.ToArray()) - 1; fields != len(parameters) {
			t.Fatalf("%d parameters went in and %d came back", len(parameters), fields)
		}
		for name, want := range parameters {
			got, err := back.Parameter(name)
			if err != nil {
				t.Fatalf("[%s] did not come back: %v", name, err)
			}
			if got != want {
				t.Fatalf("[%s] went in as %q and came back as %q", name, want, got)
			}
		}
	})
}
