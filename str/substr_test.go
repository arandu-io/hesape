package str_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/str"
)

// TestLimitCountsRunes: cutting UTF-8 at a byte boundary is how a name with an
// accent in it ends in a replacement character halfway down a table.
func TestLimitCountsRunes(t *testing.T) {
	for _, c := range []struct {
		in    string
		limit int
		end   string
		want  string
	}{
		{"the quick brown fox", 9, "...", "the quick..."},
		{"the quick brown fox", 10, "...", "the quick..."},
		{"short", 20, "...", "short"},
		{"ação entre sócios", 4, "…", "ação…"},
		// A limit of zero still gets the ellipsis: mb_strwidth is above it, so
		// the PHP falls past the early return and appends $end to nothing.
		{"anything", 0, "...", "..."},
		{"", 5, "...", ""},
	} {
		if got := str.Limit(c.in, c.limit, c.end, false); got != c.want {
			t.Errorf("Limit(%q, %d, %q) = %q, want %q", c.in, c.limit, c.end, got, c.want)
		}
	}
}

func TestWords(t *testing.T) {
	for _, c := range []struct {
		in    string
		words int
		want  string
	}{
		{"the quick brown fox", 2, "the quick..."},
		{"the quick brown fox", 4, "the quick brown fox"},
		{"the quick brown fox", 9, "the quick brown fox"},
		// A count of zero returns the subject: the pattern Illuminate builds
		// carries the quantifier {1,0}, which does not compile, and preg_match
		// failing leaves $value untouched.
		{"one", 0, "one"},
		{"", 3, ""},
	} {
		if got := str.Words(c.in, c.words, "..."); got != c.want {
			t.Errorf("Words(%q, %d) = %q, want %q", c.in, c.words, got, c.want)
		}
	}
}

func TestSquishCollapsesEveryRunOfWhitespace(t *testing.T) {
	for in, want := range map[string]string{
		"  too   many\tspaces\n": "too many spaces",
		"already squished":       "already squished",
		"   ":                    "",
	} {
		if got := str.Squish(in); got != want {
			t.Errorf("Squish(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAfterBeforeBetweenReturnTheWholeStringWhenTheyDoNotMatch(t *testing.T) {
	const address = "paulo@example.com"

	for _, c := range []struct{ got, want string }{
		{str.After(address, "@"), "example.com"},
		{str.After(address, "!"), address},
		{str.After(address, ""), address},
		{str.Before(address, "@"), "paulo"},
		{str.Before(address, "!"), address},
		{str.Before(address, ""), address},
		{str.Between("[error] boom", "[", "]"), "error"},
		{str.Between("a[b][c]d", "[", "]"), "b][c"},
		{str.Between("plain", "[", "]"), "plain"},
		{str.Between("plain", "", "]"), "plain"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestStartAndFinishCollapseRepeats(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{str.Start("api/users", "/"), "/api/users"},
		{str.Start("/api/users", "/"), "/api/users"},
		{str.Start("//api/users", "/"), "/api/users"},
		{str.Start("api", ""), "api"},
		{str.Finish("path", "/"), "path/"},
		{str.Finish("path//", "/"), "path/"},
		{str.Finish("path", ""), "path"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// The PHP's $length defaults to null, which mb_substr reads as "to the end";
// the variadic spells that as no argument at all. A length of zero is a length
// of zero, and masks nothing -- mb_substr returns the empty segment and the
// method returns the subject untouched.
func TestMask(t *testing.T) {
	for _, c := range []struct {
		in     string
		index  int
		length []int
		want   string
	}{
		{"paulo@example.com", 3, []int{8}, "pau********le.com"},
		{"paulo@example.com", 3, nil, "pau" + strings.Repeat("*", 14)},
		{"paulo@example.com", 3, []int{0}, "paulo@example.com"},
		{"4242424242424242", -12, nil, "4242" + strings.Repeat("*", 12)},
		{"4242", 10, nil, "4242"},
		{"", 0, nil, ""},
	} {
		if got := str.Mask(c.in, "*", c.index, c.length...); got != c.want {
			t.Errorf("Mask(%q, %q, %d, %v) = %q, want %q", c.in, "*", c.index, c.length, got, c.want)
		}
	}
}
