// This program must NOT compile.
//
// It is the negative fixture of TestRepositoryWithoutGrantDoesNotCompile in the
// database package: reaching a repository without a Grant has to be a compile
// error, not a lint warning. The directory is under testdata, so the go tool
// never builds it as part of the module.
package main

import (
	"context"

	"github.com/arandu-io/hesape/database"
)

// Invoice stands in for any entity a module owns. What is under test is the
// shape of the contract, which does not depend on the row.
type Invoice struct {
	ID string
}

func main() {
	var repo database.Repository[Invoice, string]

	// The Grant argument is missing. There is no overload without it, so this
	// line cannot be written by accident and shipped.
	_, _ = repo.Find(context.Background(), "some-id")
}
