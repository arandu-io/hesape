// This program must NOT compile.
//
// The connection under a model is not a field. A statement issued straight
// through it carries no Grant and no tenant filter, so the shortest path to it
// has to be a method with a name -- GetConnection -- rather than a field that a
// reader finds by autocomplete.
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
