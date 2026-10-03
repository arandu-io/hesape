// This program must compile, and what it compiles to is measured.
//
// It is two entities written the way an application's models package holds
// them: the struct and its table in a file the developer owns, and beside it a
// query type, a collection and a constructor in the shape the generator emits --
// forwards of one to three lines over the non-generic core, with no type
// parameter anywhere. TestGeneratedModelsCompileToNoModelLayerInstantiation
// compiles it and reads its symbols, so a change in the core that makes the
// generated code instantiate the model layer per entity fails there.
//
// The directory is under testdata, so the go tool never builds it as part of the
// module.
package main

import (
	"time"

	"github.com/arandu-io/hesape/database/model"
)

// User is the developer's file: the struct, and the table it is a row of.
type User struct {
	model.Model

	ID        string    `db:"id"`
	TenantID  string    `db:"tenant_id"`
	Name      string    `db:"name"`
	Email     string    `db:"email"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

var userTable = model.NewTable(model.TableSpec{
	Name:      "users",
	New:       func() model.Entity { return new(User) },
	UniqueIDs: true,
})

func init() {
	userTable.Relate("posts", func(u *model.Model) model.Relation {
		return model.HasMany(u, postTable, "user_id", "id")
	})
}

// Posts is a local relation accessor, written in the entity's custom block.
func (u *User) Posts() PostCollection {
	rows, _ := u.Related("posts")
	return PostCollectionOf(rows)
}
