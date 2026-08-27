// This program must NOT compile.
//
// It is the negative fixture for the context: a statement that cannot be
// cancelled outlives the request that asked for it, so the context is a
// parameter and not something a caller may leave out.
package main

import (
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
)

type User struct {
	ID    int64  `db:"id"`
	Email string `db:"email"`
}

func main() {
	users := model.NewModel[User]("users", nil, nil, nil)

	// The context argument is missing.
	_, _ = users.NewQuery().Get(auth.SystemGrant("users.read", "acme"))
}
