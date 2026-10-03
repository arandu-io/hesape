// This program must compile.
//
// It is the positive fixture of TestAModuleCanReachTheRelationSurface, and it is
// the only fixture here that is not a negative one. What it proves is that the
// relation surface can be reached from outside the package: it imports the model
// the way an application does, declares its tables, registers a relation, and
// touches every entry point that goes through a registered relation.
//
// It exists because that surface was once unreachable and everything inside the
// package still compiled. The relation interface the builder asked for had one
// implementation, a stand-in in a test file, and no relation in model/relations
// satisfied it -- so relations could not be registered at all and With, Load,
// Has, WhereHas and WithCount had no way to be called. Nothing in the package's
// own tests could say so, because the stand-in satisfied them.
//
// The directory is under testdata, so the go tool never builds it as part of the
// module.
package main

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/query"
)

// User and Post are entities as an application writes them: the model
// embedded, exported fields and db tags.
type User struct {
	model.Model

	ID       int64  `db:"id"`
	Name     string `db:"name"`
	TenantID string `db:"tenant_id"`
}

type Post struct {
	model.Model

	ID        int64  `db:"id"`
	UserID    int64  `db:"user_id"`
	Title     string `db:"title"`
	Published bool   `db:"published"`
	TenantID  string `db:"tenant_id"`
}

// The tables, declared once each.
var (
	users = model.NewTable(model.TableSpec{Name: "users", New: func() model.Entity { return new(User) }})
	posts = model.NewTable(model.TableSpec{Name: "posts", New: func() model.Entity { return new(Post) }})
)

// The relation, registered by name, after both tables exist. The builder
// resolves it from the model it queries through, which stands for no row, and
// narrows it to the batch afterwards.
func init() {
	users.Relate("posts", func(u *model.Model) model.Relation {
		return model.HasMany(u, posts, "", "")
	})
}

// Blog is a module: the connection it was handed.
type Blog struct{ db model.DB }

// WithTheirPosts is the eager load, the existence filter and the aggregate in
// one sentence, read back.
func (b *Blog) WithTheirPosts(ctx context.Context, g auth.Grant) ([][]*Post, error) {
	rows, err := users.Query(b.db).
		With("posts").
		WhereHas("posts", func(sub *query.Builder) {
			sub.Where("published", "=", true)
		}).
		WithCount("posts").
		Get(ctx, g)
	if err != nil {
		return nil, err
	}

	out := make([][]*Post, 0, len(rows))
	for _, row := range rows {
		related, ok := row.(*User).Related("posts")
		if !ok {
			continue
		}
		page := make([]*Post, 0, len(related))
		for _, e := range related {
			page = append(page, e.(*Post))
		}
		out = append(out, page)
	}
	return out, nil
}

// LoadOnto is the lazy half: the relation loaded onto rows already in hand.
func (b *Blog) LoadOnto(ctx context.Context, g auth.Grant, rows model.Rows) error {
	return rows.Load(ctx, g, "posts")
}

// CountThem is the aggregate loaded onto rows already in hand.
func (b *Blog) CountThem(ctx context.Context, g auth.Grant, rows model.Rows) error {
	return rows.LoadCount(ctx, g, "posts")
}

// Silent is the other side of the existence filter.
func (b *Blog) Silent(ctx context.Context, g auth.Grant) (model.Rows, error) {
	return users.Query(b.db).
		WhereDoesntHave("posts", nil).
		Has("posts", "<", 1, "and", nil).
		Get(ctx, g)
}

// OneUser reads a relation onto a single row.
func (b *Blog) OneUser(ctx context.Context, g auth.Grant, id int64) (*User, error) {
	found, err := users.Query(b.db).Find(ctx, g, id)
	if err != nil || found == nil {
		return nil, err
	}
	user := found.(*User)
	if err := user.Load(ctx, g, "posts"); err != nil {
		return nil, err
	}
	return user, nil
}

func main() {
	blog := &Blog{}
	_, _ = blog.WithTheirPosts(context.Background(), auth.Grant{})
}
