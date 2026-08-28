// This program must NOT compile.
//
// The connection under a model is not a field. A statement issued straight
// through it carries no Grant and no tenant filter, so the field that would put
// it one autocomplete away is unexported. The accessor that used to stand in for
// it is gone too, which is what connection_accessor proves.
package main

import (
	"github.com/arandu-io/hesape/database/model"
)

type User struct {
	ID    int64  `db:"id"`
	Email string `db:"email"`
}

func main() {
	users := model.NewModel[User]("users", nil, nil, nil)

	// The field is unexported.
	_ = users.Connection
}
