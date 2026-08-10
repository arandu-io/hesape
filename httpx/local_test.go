package httpx_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/httpx"
)

// LocalPath is the whole open-redirect defence of the collection, and the value
// of stating its refusals one per line is that each refusal stays attached to
// the shape it refuses.
func TestNothingThatLeavesThisApplicationIsAcceptedAsADestination(t *testing.T) {
	for name, raw := range map[string]string{
		"nowhere at all":                "",
		"another origin":                "https://evil.example/login",
		"a scheme a browser will run":   "javascript:alert(document.cookie)",
		"a protocol-relative address":   "//evil.example/takeover",
		"a backslash in the authority":  "/\\evil.example/takeover",
		"a relative address":            "invoices/42",
		"a newline in a header":         "/invoices\r\nSet-Cookie: a=b",
		"a tab a browser would strip":   "/\t/evil.example",
		"a space a browser would strip": "/ /evil.example",
	} {
		t.Run(name, func(t *testing.T) {
			if to, ok := httpx.LocalPath(raw); ok {
				t.Fatalf("%q was accepted as %q, and a browser told to go there after a sign-in does not come back", raw, to)
			}
		})
	}

	if _, ok := httpx.LocalPath("/" + strings.Repeat("a", 1024)); ok {
		t.Error("an address longer than a cookie can hold was accepted, so the browser drops the cookie instead of us deciding not to write it")
	}
}

func TestAnOrdinaryPageAddressIsAccepted(t *testing.T) {
	for _, raw := range []string{
		"/",
		"/invoices",
		"/invoices/42?tab=lines&sort=date",
		"/invoices/42#total",
		"/faturas/n%C3%BAmero",
	} {
		if to, ok := httpx.LocalPath(raw); !ok || to != raw {
			t.Errorf("%q is a page of this application and was refused, so somebody who followed a link to it signs in and lands on the front page", raw)
		}
	}
}
