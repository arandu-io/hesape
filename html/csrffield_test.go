package html_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// csrfFieldSite is one function that spells the name of the hidden CSRF form
// field, together with the expression that lifts the spelling back out of the
// string literals in its body.
//
// Each site carries its own expression because the text around the name is
// different in each: one writes a whole input tag, one writes the leading half
// of an encoded form body, one passes the name as a bare argument. A single
// expression loose enough to cover all three would also match literals that are
// not field names at all.
type csrfFieldSite struct {
	// file is relative to this package's directory, and fn is the function or
	// method declared in it whose body carries the name.
	file string
	fn   string
	// role says what that function does with the name, so a failure names the
	// two ends that stopped agreeing rather than two line numbers.
	role string
	// find is applied to each string literal in the body, and capture group one
	// is the field name.
	find *regexp.Regexp
}

// csrfFieldSites are the places that write or read the hidden CSRF field by
// name. Two of them put it into HTML, and two take it back out.
var csrfFieldSites = []csrfFieldSite{
	{
		file: filepath.Join("..", "view", "runtime.go"),
		fn:   "CSRF",
		role: "the hidden input a rendered template writes",
		find: regexp.MustCompile(`name="([^"]+)"`),
	},
	{
		file: "form.go",
		fn:   "Token",
		role: "the hidden input the form builder writes",
		find: regexp.MustCompile(`^(_[A-Za-z0-9_]+)$`),
	},
	{
		file: filepath.Join("..", "arandutest", "http.go"),
		fn:   "Post",
		role: "the field the test client sends with a submission",
		find: regexp.MustCompile(`^(_[A-Za-z0-9_]+)=$`),
	},
	{
		file: filepath.Join("..", "arandutest", "http.go"),
		fn:   "token",
		role: "the field the test client scrapes off the last page it loaded",
		find: regexp.MustCompile(`name="([^"]+)" value="`),
	},
}

// TestEverySiteSpellsTheCSRFFieldTheSameWay reads the sources that write and
// read the hidden CSRF field and fails when two of them disagree about its
// name.
//
// Nothing in the type system holds this together. The name is a string on the
// wire: the template runtime writes one, the form builder writes another, and
// whoever validates a submission looks the third one up. All of them compile,
// all of them pass their own tests, and the only symptom is a form refused on
// submit -- which a reader takes for an expired session rather than for two
// spellings of one name. That is exactly what happened: a form built here
// carried one name and the request was checked for the other.
//
// A shared constant is the usual answer and it is not taken. The packages that
// would have to import it drag the database, the session and the encryption
// keys in behind them, and this package imports nothing from the module today.
// That edge costs more than the divergence it would close.
//
// It reads sources instead of calling the four functions because one of them
// never returns the name: the scraper holds it inside a search marker, and the
// only way a call exposes a mismatch there is an empty token, which is also
// what a page carrying no form returns. Reading the text asks all four the same
// question.
//
// It fails in three directions. A site whose function was renamed, a site whose
// literals match nothing, and two sites that disagree. The middle one is not
// leniency: a guard that matches nothing finds nothing and passes on anything,
// so reading nothing is an error here.
func TestEverySiteSpellsTheCSRFFieldTheSameWay(t *testing.T) {
	byName := map[string][]string{}
	for _, site := range csrfFieldSites {
		name := spellingAt(t, site)
		byName[name] = append(byName[name], fmt.Sprintf("%s: %s, %s", site.file, site.fn, site.role))
	}

	if len(byName) == 1 {
		return
	}

	var report strings.Builder
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		report.WriteString("\n  " + name)
		for _, where := range byName[name] {
			report.WriteString("\n    " + where)
		}
	}
	t.Errorf("the hidden CSRF field is spelled %d different ways:%s\n"+
		"A form built under one spelling and checked against another is refused on submit. "+
		"Change every site or none of them.",
		len(byName), report.String())
}

// spellingAt returns the field name the site's function spells.
//
// It fails the test when the function is no longer there and when its literals
// carry no name, because either of those turns the check into one that reads
// nothing and reports nothing.
func spellingAt(t *testing.T, site csrfFieldSite) string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, site.file, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", site.file, err)
	}

	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == site.fn && fn.Body != nil {
			body = fn.Body
			break
		}
	}
	if body == nil {
		t.Fatalf("%s declares no %s, and that function was %s. "+
			"Point this site at whatever spells the field now.",
			site.file, site.fn, site.role)
	}

	var found []string
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		text, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatalf("%s: %s holds a string literal that will not unquote: %s", site.file, site.fn, lit.Value)
		}
		for _, match := range site.find.FindAllStringSubmatch(text, -1) {
			if !slices.Contains(found, match[1]) {
				found = append(found, match[1])
			}
		}
		return true
	})

	if len(found) == 0 {
		t.Fatalf("%s: %s is %s, and %s matched none of its string literals. "+
			"This check read nothing, so it would pass on anything.",
			site.file, site.fn, site.role, site.find)
	}
	if len(found) > 1 {
		t.Fatalf("%s: %s spells the field %d ways on its own: %v",
			site.file, site.fn, len(found), found)
	}
	return found[0]
}
