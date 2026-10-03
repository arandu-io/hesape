package main

import (
	"time"

	"github.com/arandu-io/hesape/database/model"
)

// Post is the second entity, soft deleting, owned by a user.
type Post struct {
	model.Model

	ID        string     `db:"id"`
	TenantID  string     `db:"tenant_id"`
	UserID    string     `db:"user_id"`
	Title     string     `db:"title"`
	Published bool       `db:"published"`
	CreatedAt time.Time  `db:"created_at"`
	UpdatedAt time.Time  `db:"updated_at"`
	DeletedAt *time.Time `db:"deleted_at"`
}

var postTable = model.NewTable(model.TableSpec{
	Name:        "posts",
	New:         func() model.Entity { return new(Post) },
	UniqueIDs:   true,
	SoftDeletes: true,
})

func init() {
	postTable.Relate("user", func(p *model.Model) model.Relation {
		return model.BelongsTo(p, userTable, "user_id", "id", "user")
	})
}

// Published is a local scope, written as a method in the entity's custom block.
func (q *PostQuery) Published() *PostQuery {
	q.b.Where("published", "=", true)
	return q
}
