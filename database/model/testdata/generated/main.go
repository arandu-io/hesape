package main

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/pagination"
)

// Blog is the service an application writes over its models: every call is
// on the generated types, and nothing here names a type parameter.
type Blog struct{ db model.DB }

func (b *Blog) Listing(ctx context.Context, g auth.Grant, page int) (UserCollection, *pagination.LengthAwarePage, error) {
	return Users(b.db).
		Where(func(q *UserQuery) { q.Where("name", "!=", "").OrWhere("email", "!=", "") }).
		WhereHas("posts", func(sub *query.Builder) { sub.Where("published", "=", true) }).
		WithCount("posts").
		With("posts").
		Latest().
		Paginate(ctx, g, 15, page, pagination.Options{})
}

func (b *Blog) Publish(ctx context.Context, g auth.Grant, id string) (*Post, error) {
	post, err := Posts(b.db).Published().FindOrFail(ctx, g, id)
	if err != nil {
		return nil, err
	}
	post.Title = "published"
	if _, err := post.Save(ctx, g); err != nil {
		return nil, err
	}
	return post.Fresh(ctx, g, "user")
}

func (b *Blog) Walk(ctx context.Context, g auth.Grant) (int, error) {
	seen := 0
	for post, err := range Posts(b.db).Latest().Cursor(ctx, g) {
		if err != nil {
			return seen, err
		}
		if post.Trashed() {
			continue
		}
		seen++
	}
	err := Users(b.db).Chunk(ctx, g, 100, func(users UserCollection, _ int) (bool, error) {
		if err := users.Load(ctx, g, "posts"); err != nil {
			return false, err
		}
		for _, u := range users {
			seen += len(u.Posts())
		}
		return true, nil
	})
	return seen, err
}

func (b *Blog) Seed(ctx context.Context, g auth.Grant) (UserCollection, error) {
	return UserFactories(b.db).Count(3).State(func(u *User) { u.Name = "Ada" }).Create(ctx, g)
}

func main() {
	blog := &Blog{}
	_, _, _ = blog.Listing(context.Background(), auth.Grant{}, 1)
	_, _ = blog.Publish(context.Background(), auth.Grant{}, "p-1")
	_, _ = blog.Walk(context.Background(), auth.Grant{})
	_, _ = blog.Seed(context.Background(), auth.Grant{})
	_ = NewUser(nil)
	_ = NewPost(nil)
}
