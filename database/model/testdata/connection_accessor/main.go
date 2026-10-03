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
	model.Model

	ID    int64  `db:"id"`
	Email string `db:"email"`
}

var table = model.NewTable(model.TableSpec{Name: "users", New: func() model.Entity { return new(User) }})

func main() {
	var db model.DB
	users := table.New(db).(*User)

	// There is no accessor for the connection.
	_ = users.GetConnection()
}
