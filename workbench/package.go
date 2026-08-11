package workbench

import (
	"errors"
	"strings"

	"github.com/arandu-io/hesape/str"
)

// ErrInvalidPackage is what NewPackage answers when the vendor or the name is
// empty. PHP has no such check: Package::__construct takes whatever it is given
// and the failure surfaces later, as a directory called "/" .
var ErrInvalidPackage = errors.New("workbench: vendor and name are both required")

// Package answers to Illuminate\Workbench\Package: the identity of the package
// being generated, and the source of every placeholder in the stubs.
//
// PHP has six public properties and derives two of them in the constructor.
// This has those six with the same names, plus three that only Go needs --
// [Package.Host], [Package.PackageName] and what [Package.ModulePath] builds
// from them -- because a PHP package is identified by "vendor/name" and a Go
// module is identified by a URL.
type Package struct {
	// Vendor is the vendor in StudlyCase: "Acme".
	Vendor string

	// LowerVendor is Vendor as a slug: "acme". PHP's snake_case($vendor, '-').
	LowerVendor string

	// Name is the package name in StudlyCase: "InvoiceManager".
	Name string

	// LowerName is Name as a slug: "invoice-manager". PHP's
	// snake_case($name, '-'). It is the last element of the module path, the
	// directory the package is written to, and what Module.Name returns.
	LowerName string

	// PackageName is LowerName as a Go identifier: "invoicemanager".
	//
	// It has no PHP counterpart. There the namespace is StudlyCase and the
	// directory is the slug, and the two never have to agree. In Go the package
	// clause is an identifier, so a hyphen that is fine in a module path is a
	// compile error one line down.
	PackageName string

	// Author is the copyright holder written into LICENSE.
	Author string

	// Email is the author's address.
	Email string

	// Host is the module host: "github.com" unless said otherwise.
	//
	// PHP does not need it -- Packagist resolves "acme/invoice-manager" from a
	// central index -- and Go has no central index, so the host is part of the
	// identifier.
	Host string
}

// DefaultHost is the module host [NewPackage] uses when none is given.
const DefaultHost = "github.com"

// NewPackage answers to Package::__construct.
//
// It derives LowerVendor, LowerName and PackageName the way PHP derives the
// first two, with str.Snake standing in for snake_case. host may be empty, and
// then it is [DefaultHost].
//
// PHP returns void and cannot fail. This returns (*Package, error) because an
// empty segment produces a module path that go(1) rejects with a message about
// a path element, several steps after the mistake was made.
func NewPackage(vendor, name, author, email, host string) (*Package, error) {
	if strings.TrimSpace(vendor) == "" || strings.TrimSpace(name) == "" {
		return nil, ErrInvalidPackage
	}
	if host == "" {
		host = DefaultHost
	}

	lowerName := str.Snake(name, "-")
	return &Package{
		Vendor:      vendor,
		LowerVendor: str.Snake(vendor, "-"),
		Name:        name,
		LowerName:   lowerName,
		PackageName: goIdentifier(lowerName),
		Author:      author,
		Email:       email,
		Host:        strings.TrimSuffix(host, "/"),
	}, nil
}

// GetFullName answers to Package::getFullName: "acme/invoice-manager".
//
// It is what PHP writes as the composer name and what this uses as the
// directory below the workbench path, so `workbench/acme/invoice-manager` holds
// the same thing in both. The module path a Go tool needs is [Package.ModulePath].
func (p *Package) GetFullName() string {
	return p.LowerVendor + "/" + p.LowerName
}

// ModulePath is the module path the generated go.mod declares:
// "github.com/acme/invoice-manager".
//
// It has no PHP counterpart, for the reason given on [Package.Host].
func (p *Package) ModulePath() string {
	return p.Host + "/" + p.GetFullName()
}

// goIdentifier reduces a slug to something legal in a package clause.
//
// Go allows letters, digits and underscore, and forbids a leading digit. The
// convention for a hyphenated module is to drop the hyphens rather than turn
// them into underscores, which is what the standard library does with
// "go.mod" style names and what everybody reading the import expects.
func goIdentifier(slug string) string {
	var b strings.Builder
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		}
	}
	identifier := b.String()
	if identifier == "" || (identifier[0] >= '0' && identifier[0] <= '9') {
		identifier = "pkg" + identifier
	}
	return identifier
}
