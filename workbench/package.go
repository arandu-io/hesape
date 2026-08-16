package workbench

import (
	"errors"
	"strings"

	"github.com/arandu-io/hesape/str"
)

// ErrInvalidPackage is what NewPackage returns when the vendor or the name is
// empty. Without the check the failure surfaces later, as a directory called
// "/".
var ErrInvalidPackage = errors.New("workbench: vendor and name are both required")

// Package is the identity of the package being generated, and the source of
// every placeholder in the stubs.
//
// [Package.Host], [Package.PackageName] and what [Package.ModulePath] builds
// from them exist because a Go module is identified by a URL rather than by a
// vendor and a name.
type Package struct {
	// Vendor is the vendor in StudlyCase: "Acme".
	Vendor string

	// LowerVendor is Vendor as a hyphenated slug: "acme".
	LowerVendor string

	// Name is the package name in StudlyCase: "InvoiceManager".
	Name string

	// LowerName is Name as a hyphenated slug: "invoice-manager". It is the last
	// element of the module path, the directory the package is written to, and
	// what Module.Name returns.
	LowerName string

	// PackageName is LowerName as a Go identifier: "invoicemanager".
	//
	// It exists because the package clause is an identifier: a hyphen that is
	// fine in a module path is a compile error one line down.
	PackageName string

	// Author is the copyright holder written into LICENSE.
	Author string

	// Email is the author's address.
	Email string

	// Host is the module host: "github.com" unless said otherwise.
	//
	// Go has no central package index, so the host is part of the identifier.
	Host string
}

// DefaultHost is the module host [NewPackage] uses when none is given.
const DefaultHost = "github.com"

// NewPackage builds the identity of a package to generate.
//
// It derives LowerVendor, LowerName and PackageName with str.Snake. host may be
// empty, and then it is [DefaultHost].
//
// It returns an error because an empty segment produces a module path that go(1)
// rejects with a message about a path element, several steps after the mistake
// was made.
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

// GetFullName is "acme/invoice-manager".
//
// It is the directory below the workbench path, so a package lands in
// `workbench/acme/invoice-manager`. The module path a Go tool needs is
// [Package.ModulePath].
func (p *Package) GetFullName() string {
	return p.LowerVendor + "/" + p.LowerName
}

// ModulePath is the module path the generated go.mod declares:
// "github.com/acme/invoice-manager". See [Package.Host] for why it carries a
// host.
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
