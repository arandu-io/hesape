// This program must compile.
//
// It is the positive fixture of TestAModuleCanReachTheRelationSurface, and it is
// the only fixture here that is not a negative one. What it proves is that the
// relation surface can be reached from outside the package: it imports the model
// the way an application does, holds what a module constructor is handed, and
// touches every entry point that goes through a registered relation.
//
// It exists because that surface was once unreachable and everything inside the
// package still compiled. The relation interface the builder asked for had one
// implementation, a stand-in in a test file, and no relation in model/relations
// satisfied it -- so RelationResolvers could not be populated at all and With,
// Load, Has, WhereHas and WithCount had no way to be called. Nothing in the
// package's own tests could say so, because the stand-in satisfied them.
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

// User and Post are entities as an application writes them: exported fields,
// db tags, and the model embedded so that a row hands back what was loaded onto
// it.
type User struct {
	model.Model[User]

	ID       int64  `db:"id"`
	Name     string `db:"name"`
	TenantID string `db:"tenant_id"`
}

type Post struct {
	model.Model[Post]

	ID        int64  `db:"id"`
	UserID    int64  `db:"user_id"`
	Title     string `db:"title"`
	Published bool   `db:"published"`
	TenantID  string `db:"tenant_id"`
}

// Blog is a module: the models it owns, built once and kept.
type Blog struct {
	users *model.Model[User]
	posts *model.Model[Post]
}

// NewBlog is the module constructor, and its arguments are the whole of what a
// module receives.
func NewBlog(db query.Connection, grammar query.Grammar, processor query.Processor) *Blog {
	users := model.NewModel[User]("users", db, grammar, processor)
	posts := model.NewModel[Post]("posts", db, grammar, processor)

	// The relation, registered by name. The Unconstrained constructor is what a
	// resolver takes: the builder resolves it from the model it queries through,
	// which carries no key, and narrows it to the batch afterwards.
	users.RelationResolvers = map[string]func(*model.Model[User]) model.Relation{
		"posts": func(u *model.Model[User]) model.Relation {
			return model.HasManyOfUnconstrained(u, posts, "", "")
		},
	}

	return &Blog{users: users, posts: posts}
}

// WithTheirPosts is the eager load, the existence filter and the aggregate in
// one sentence, read back typed.
func (b *Blog) WithTheirPosts(ctx context.Context, g auth.Grant) ([]model.Collection[Post], error) {
	rows, err := b.users.NewQuery().
		With("posts").
		WhereHas("posts", func(sub *query.Builder) {
			sub.Where("published", "=", true)
		}).
		WithCount("posts").
		Get(ctx, g)
	if err != nil {
		return nil, err
	}

	out := make([]model.Collection[Post], 0, len(rows))
	for _, row := range rows {
		posts, ok := model.Related[User, Post](row, "posts")
		if !ok {
			continue
		}
		out = append(out, posts)
	}
	return out, nil
}

// LoadOnto is the lazy half: the relation loaded onto rows already in hand.
func (b *Blog) LoadOnto(ctx context.Context, g auth.Grant, rows model.Collection[User]) error {
	return rows.Load(ctx, g, "posts")
}

// CountThem is the aggregate loaded onto rows already in hand.
func (b *Blog) CountThem(ctx context.Context, g auth.Grant, rows model.Collection[User]) error {
	return rows.LoadCount(ctx, g, "posts")
}

// Silent is the other side of the existence filter.
func (b *Blog) Silent(ctx context.Context, g auth.Grant) (model.Collection[User], error) {
	return b.users.NewQuery().
		WhereDoesntHave("posts", nil).
		Has("posts", "<", 1, "and", nil).
		Get(ctx, g)
}

// OneUser reads a relation onto a single model.
func (b *Blog) OneUser(ctx context.Context, g auth.Grant, id int64) (*User, error) {
	found, err := b.users.NewQuery().Find(ctx, g, id)
	if err != nil || found == nil {
		return nil, err
	}
	if err := found.Load(ctx, g, "posts"); err != nil {
		return nil, err
	}
	return found.Entity, nil
}

func main() {
	blog := NewBlog(nil, nil, nil)
	_, _ = blog.WithTheirPosts(context.Background(), auth.Grant{})
}
