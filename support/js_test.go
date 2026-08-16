package support

import (
	"encoding/json"
	"strings"
	"testing"
)

// unwrap undoes the two layers the expression is built from. The payload inside
// JSON.parse('...') is a JavaScript string literal whose contents are the JSON,
// so the browser reads the literal first and parses what comes out of it.
func unwrap(t *testing.T, expression string) string {
	t.Helper()

	payload, ok := strings.CutPrefix(expression, "JSON.parse('")
	if !ok {
		t.Fatalf("not a JSON.parse expression: %s", expression)
	}
	payload, ok = strings.CutSuffix(payload, "')")
	if !ok {
		t.Fatalf("not a JSON.parse expression: %s", expression)
	}
	return payload
}

// decode undoes both layers and returns the object the browser would end up
// with: the JavaScript string literal first, then the JSON inside it.
func decode(t *testing.T, payload string) map[string]string {
	t.Helper()

	var inner string
	if err := json.Unmarshal([]byte(`"`+payload+`"`), &inner); err != nil {
		t.Fatalf("the literal the browser reads is not decodable: %v\n%s", err, payload)
	}

	var back map[string]string
	if err := json.Unmarshal([]byte(strings.ReplaceAll(inner, `\/`, "/")), &back); err != nil {
		t.Fatalf("what JSON.parse is handed is not valid JSON: %v\n%s", err, inner)
	}
	return back
}

// The five bytes that close something if they reach the page intact. The
// expression is dropped into an HTML attribute, so each one ends a context the
// data is supposed to stay inside.
//
// Four of these five branches used to write the byte back unchanged, so the
// function named for hex escaping escaped nothing.
func TestTheFiveDangerousBytesLeaveEscaped(t *testing.T) {
	for _, c := range []struct {
		name string
		byte string
	}{
		{"apostrophe ends the JavaScript string", "\u0027"},
		{"less-than opens a tag", "\u003c"},
		{"greater-than closes one", "\u003e"},
		{"ampersand starts an entity", "\u0026"},
	} {
		t.Run(c.name, func(t *testing.T) {
			value := "a" + c.byte + "b"

			js, err := NewJs(map[string]string{"v": value})
			if err != nil {
				t.Fatal(err)
			}
			payload := unwrap(t, js.ToHtml())

			// The property: the byte does not reach the page as itself. It is
			// the only thing standing between the data and a closed context.
			if strings.Contains(payload, c.byte) {
				t.Fatalf("%q reached the output unescaped: %s", value, payload)
			}

			// And it still means what it meant: both layers decode back.
			if got := decode(t, payload)["v"]; got != value {
				t.Fatalf("the value did not survive: %q, wanted %q", got, value)
			}
		})
	}
}

// A forward slash is the one that leaves as an escape pair rather than a hex
// escape, so that a literal cannot spell a closing script tag.
func TestASlashCannotSpellAClosingTag(t *testing.T) {
	js, err := NewJs(map[string]string{"v": "</script>"})
	if err != nil {
		t.Fatal(err)
	}
	payload := unwrap(t, js.ToHtml())

	if strings.Contains(payload, "</script>") {
		t.Fatalf("a closing tag reached the output whole: %s", payload)
	}
	if got := decode(t, payload)["v"]; got != "</script>" {
		t.Fatalf("the value did not survive: %q", got)
	}
}

// A double quote inside a value used to come out unescaped: the branch that saw
// \" wrote a bare " and left the JSON literal open. The result was neither valid
// JSON nor a string a value could not escape from.
func TestADoubleQuoteStaysEscaped(t *testing.T) {
	const value = `he said "hi"`

	js, err := NewJs(map[string]string{"v": value})
	if err != nil {
		t.Fatal(err)
	}
	payload := unwrap(t, js.ToHtml())

	// The first layer is the JavaScript string literal. Decoding it the way the
	// browser would leaves the JSON the parser is handed.
	var inner string
	if err := json.Unmarshal([]byte(`"`+payload+`"`), &inner); err != nil {
		t.Fatalf("the literal the browser reads is not decodable: %v\n%s", err, payload)
	}

	// The second layer is the JSON itself. Before the fix this read
	// {"v":"he said "hi""} and JSON.parse threw on it.
	var back map[string]string
	if err := json.Unmarshal([]byte(strings.ReplaceAll(inner, `\/`, "/")), &back); err != nil {
		t.Fatalf("what JSON.parse is handed is not valid JSON: %v\n%s", err, inner)
	}
	if back["v"] != value {
		t.Fatalf("the value did not survive the round trip: %q", back["v"])
	}
}

// Structural characters belong to the JSON and must not be touched: escaping
// them would break the document the browser is meant to parse.
func TestTheStructureIsLeftAlone(t *testing.T) {
	js, err := NewJs(map[string]any{"n": 1, "s": "x"})
	if err != nil {
		t.Fatal(err)
	}
	payload := unwrap(t, js.ToHtml())

	for _, want := range []string{"{", "}", ":", ","} {
		if !strings.Contains(payload, want) {
			t.Fatalf("the %s of the object did not survive: %s", want, payload)
		}
	}
}
