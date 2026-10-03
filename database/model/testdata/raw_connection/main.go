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
	model.Model

	ID    int64  `db:"id"`
	Email string `db:"email"`
}

var table = model.NewTable(model.TableSpec{Name: "users", New: func() model.Entity { return new(User) }})

func main() {
	var db model.DB
	users := table.New(db).(*User)

	// The field is unexported: neither the row nor the model inside it hands
	// the connection out.
	_ = users.Connection
}
