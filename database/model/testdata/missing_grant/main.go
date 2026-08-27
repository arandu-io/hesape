// This program must NOT compile.
//
// It is the negative fixture of TestTheModelCannotBeReachedWithoutAGrant:
// reading through the model without a Grant has to be a compile error, not a
// convention. The directory is under testdata, so the go tool never builds it
// as part of the module.
package main

import (
	"context"

	"github.com/arandu-io/hesape/database/model"
)

// User stands in for any entity an application owns. What is under test is the
// shape of the contract, which does not depend on the row.
type User struct {
	ID    int64  `db:"id"`
	Email string `db:"email"`
}

func main() {
	users := model.NewModel[User]("users", nil, nil, nil)

	// The Grant argument is missing. There is no overload without it, so a read
	// that nobody authorized cannot be written by accident and shipped.
	_, _ = users.NewQuery().Where("email", "ada@example.test").First(context.Background())
}
