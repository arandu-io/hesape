package view_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/view"
)

// TestAWrongHashIsNeverServedWithABodyOrStored is the defect of a deploy that
// leaves two builds live: the new page asks for the new stylesheet, the old
// build answers that address with its own bytes, and a shared cache that stores
// static files keeps them under the new name for hours. The handler used to
// answer 200, the current stylesheet and no-cache for any hash at all.
//
// It goes through the router, as an application wires it, and over a real
// connection so the client follows the redirect the way a browser does.
func TestAWrongHashIsNeverServedWithABodyOrStored(t *testing.T) {
	r := routing.NewRouter()
	view.NewModule().Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	good := view.Asset("app.css")
	wrong := strings.Replace(good, "/"+hashOf(good)+"/", "/000000000000/", 1)
	if wrong == good {
		t.Fatalf("could not build a wrong address from %q", good)
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := client.Get(server.URL + wrong)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatalf("a wrong hash answered 200 with Cache-Control %q: the bytes sit under an address they do not belong to", resp.Header.Get("Cache-Control"))
	}
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("a wrong hash answered %d, want a 307 to the current address", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != good {
		t.Errorf("Location = %q, want %q", got, good)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store: a shared cache keeps anything else", got)
	}

	// What the browser ends up with is the file at its own address, cached for
	// good.
	final, err := http.Get(server.URL + wrong)
	if err != nil {
		t.Fatal(err)
	}
	_ = final.Body.Close()
	if final.StatusCode != http.StatusOK || final.Request.URL.Path != good {
		t.Fatalf("following the redirect ended at %s with %d, want %s with 200", final.Request.URL.Path, final.StatusCode, good)
	}
	if got := final.Header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("the right address answered Cache-Control %q, want it immutable", got)
	}
}

// TestAnUnknownAssetIsA404NoCacheMayKeep: a 404 without a Cache-Control is
// stored by a CDN that caches by extension and handed to browsers as fresh for
// hours, so a stylesheet that did not exist for one request is missing from
// every reload until the lifetime ends -- the page unstyled, the network tab
// showing nothing to blame.
func TestAnUnknownAssetIsA404NoCacheMayKeep(t *testing.T) {
	r := routing.NewRouter()
	view.NewModule().Routes(r)
	server := httptest.NewServer(r)
	defer server.Close()

	good := view.Asset("app.css")
	for _, url := range []string{
		strings.Replace(good, "app.css", "nothing.css", 1),
		strings.Replace(good, "app.css", "app.css.map", 1),
	} {
		resp, err := http.Get(server.URL + url)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", url, resp.StatusCode)
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", url, got)
		}
	}
}

// hashOf returns the hash segment of an asset address.
func hashOf(url string) string {
	parts := strings.Split(strings.Trim(url, "/"), "/")
	return parts[len(parts)-2]
}
