// This program must NOT compile.
//
// It is the second negative fixture of the database package: even knowing the
// shape of auth.Grant, code outside that package cannot build a valid one,
// because every field is unexported. The zero value is the only Grant available,
// and it fails Check at runtime.
package main

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
)

// Invoice stands in for any entity a module owns.
type Invoice struct {
	ID string
}

func main() {
	var repo database.Repository[Invoice, string]

	// Forging a valid Grant: the fields are unexported, so this does not compile.
	forged := auth.Grant{valid: true, action: auth.Action("invoice.view")}

	_, _ = repo.Find(context.Background(), forged, "some-id")
}
