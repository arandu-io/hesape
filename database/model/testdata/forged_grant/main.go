// This program must NOT compile.
//
// A Grant is only issued by the auth package. Every field of it is unexported,
// so a caller that wants one without asking a Policy has to reach for a field
// that is not there -- which is a compile error and not a code review.
package main

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
)

type User struct {
	ID    int64  `db:"id"`
	Email string `db:"email"`
}

func main() {
	users := model.NewModel[User]("users", nil, nil, nil)

	// Forging the Grant rather than being issued one.
	forged := auth.Grant{valid: true}

	_, _ = users.NewQuery().Get(context.Background(), forged)
}
