// This program must NOT compile.
//
// raw_connection proves the connection under a model is not a field. This proves
// there is no method handing it back either, which is the same hole with a name
// on it: query.Connection has five verbs and not one takes a Grant, so anything
// that returns it turns an authorized model into a statement with no tenant
// filter.
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

	// There is no accessor for the connection.
	_ = users.GetConnection()
}
