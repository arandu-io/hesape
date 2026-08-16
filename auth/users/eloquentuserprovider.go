package users

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// EloquentUserProvider is the auth.UserProvider that finds users through the
// application's own user type.
//
// # Where the Grant comes from, which is the first question to ask of this file
//
// A user provider runs at sign-in. There is no subject yet -- proving who
// somebody is is what is about to happen -- so there is no Policy to run and no
// Grant to inherit. Every statement here is therefore taken under
// auth.SystemGrant, with RetrieveUser or UpdateUser as the action.
//
// The tenant that grant carries comes from the provider, which got it from
// configuration when the application was wired. It never comes from the request
// -- not from a header, not from a subdomain the handler parsed, not from the
// credentials map. A tenant taken off the request would let a caller name whose
// users the sign-in form searches, which is every account in the system
// reachable from an unauthenticated endpoint.
//
// The read paths are authorized exactly as the writes are: RetrieveByID and
// RetrieveByCredentials hold a Grant and are filtered by its tenant, because a
// query without one reads every customer's users.
//
// An application that serves several tenants builds one provider per tenant. A
// provider is four fields and holds no connection state of its own.
//
// # What stands in for the model, and why
//
// Go cannot construct a type from a name in a string, so the model is a
// constructor function the wiring supplies -- func() auth.Authenticatable.
//
// The query that model would have opened comes in the same way. An
// auth.Authenticatable is an interface value, and an interface value has no
// connection, table or scopes behind it, so the wiring hands over the query
// factory next to the constructor.
type EloquentUserProvider struct {
	// hasher checks and rewrites the stored password hash.
	hasher auth.Hasher

	// model builds a fresh instance of the user type, for the reason given
	// above.
	model func() auth.Authenticatable

	// newQuery opens a statement against the table the user type lives in. It
	// takes a context because the connection a hesape builder runs on is bound
	// at the moment the builder is made.
	newQuery func(ctx context.Context) *query.Builder

	// tenant is what every statement this provider issues is scoped by. It comes
	// from configuration and from nowhere else.
	tenant string

	// queryCallback runs on every retrieval query, when one was set.
	queryCallback func(*query.Builder)
}

// Verify at compile time that the provider is the contract a guard consumes.
var _ auth.UserProvider = (*EloquentUserProvider)(nil)

// NewEloquentUserProvider returns a provider over the user type model builds.
//
// model constructs one, newQuery opens a statement against the table it lives
// in, and tenant is what every statement is filtered by. See the type's doc for
// why the first two are separate arguments.
func NewEloquentUserProvider(
	hasher auth.Hasher,
	model func() auth.Authenticatable,
	newQuery func(ctx context.Context) *query.Builder,
	tenant string,
) *EloquentUserProvider {
	return &EloquentUserProvider{hasher: hasher, model: model, newQuery: newQuery, tenant: tenant}
}

// RetrieveByID finds the account with this identifier.
//
// A nil user and a nil error mean nobody has that identifier. An error means the
// statement failed, which is a different outcome and reads differently at the
// call site.
func (p *EloquentUserProvider) RetrieveByID(ctx context.Context, identifier any) (auth.Authenticatable, error) {
	model := p.CreateModel()

	record, err := p.newModelQuery(ctx).
		Where(model.GetAuthIdentifierName(), "=", identifier).
		First(ctx, p.grant(RetrieveUser))
	if err != nil {
		return nil, err
	}
	return hydrate(p.model, record)
}

// RetrieveByToken is the user behind a "remember me" cookie.
//
// The row is found by identifier and only then is the token compared, in
// constant time. A row whose remember token is empty never matches, so a user
// who has never ticked the box cannot be signed in with an empty cookie.
func (p *EloquentUserProvider) RetrieveByToken(ctx context.Context, identifier any, token string) (auth.Authenticatable, error) {
	retrieved, err := p.RetrieveByID(ctx, identifier)
	if err != nil || retrieved == nil {
		return nil, err
	}
	if !tokensMatch(retrieved.GetRememberToken(), token) {
		return nil, nil
	}
	return retrieved, nil
}

// UpdateRememberToken writes a new remember token for this account.
//
// The statement writes one column and never mentions updated_at, so being
// remembered does not look like the account was edited.
//
// The instance the caller holds is updated too.
func (p *EloquentUserProvider) UpdateRememberToken(ctx context.Context, user auth.Authenticatable, token string) error {
	user.SetRememberToken(token)

	_, err := p.newQuery(ctx).
		Where(user.GetAuthIdentifierName(), "=", user.GetAuthIdentifier()).
		Update(ctx, p.grant(UpdateUser), map[string]any{user.GetRememberTokenName(): token})
	return err
}

// RetrieveByCredentials finds the account these credentials name.
//
// Every key holding the word "password" is dropped before a clause is built, so
// no statement this method issues ever compares a password. Credentials that are
// nothing but password keys match nobody: the alternative is a query with no
// where clause, which would answer with the first user in the table.
func (p *EloquentUserProvider) RetrieveByCredentials(ctx context.Context, credentials map[string]any) (auth.Authenticatable, error) {
	filtered := filterCredentials(credentials)
	if len(filtered) == 0 {
		return nil, nil
	}

	q := p.newModelQuery(ctx)
	applyCredentials(q, filtered)

	record, err := q.First(ctx, p.grant(RetrieveUser))
	if err != nil {
		return nil, err
	}
	return hydrate(p.model, record)
}

// ValidateCredentials reports whether these credentials belong to this account.
//
// A missing password, and a user whose password column is empty, are both
// false. The empty column matters: a row created by an invite flow that has
// never had a password set must not be signed in to by offering an empty one.
//
// A password that is not a string is coerced rather than refused, and an empty
// one is handed to the hasher rather than refused early -- see [passwordOf].
//
// The context is unused because nothing here touches storage; it is on the
// signature because auth.UserProvider declares it, so that a provider which does
// need one can be written without changing the contract.
func (p *EloquentUserProvider) ValidateCredentials(_ context.Context, user auth.Authenticatable, credentials map[string]any) bool {
	plain, ok := passwordOf(credentials)
	if !ok {
		return false
	}
	hashed := user.GetAuthPassword()
	if hashed == "" {
		return false
	}
	return p.hasher.Check(plain, hashed)
}

// RehashPasswordIfRequired upgrades a hash that was made with weaker parameters
// than the ones in force.
//
// It runs on a sign-in that has already proved the plain password, which is the
// only moment the plain password exists and the whole reason this is possible at
// all. A hash that already meets the parameters is left alone unless force says
// otherwise, and that is the common case -- so the statement below is not issued
// on an ordinary sign-in.
//
// The column is written by the statement, and the instance in memory is updated
// when the user type can be filled -- see [Fillable]. A user type that cannot
// keeps the old hash in memory for the rest of the request, and the row is
// correct either way.
func (p *EloquentUserProvider) RehashPasswordIfRequired(ctx context.Context, user auth.Authenticatable, credentials map[string]any, force bool) error {
	if !p.hasher.NeedsRehash(user.GetAuthPassword()) && !force {
		return nil
	}

	plain, ok := passwordOf(credentials)
	if !ok {
		return ErrNoPassword
	}
	hashed, err := p.hasher.Make(plain)
	if err != nil {
		return err
	}

	column := user.GetAuthPasswordName()
	if _, err := p.newQuery(ctx).
		Where(user.GetAuthIdentifierName(), "=", user.GetAuthIdentifier()).
		Update(ctx, p.grant(UpdateUser), map[string]any{column: hashed}); err != nil {
		return err
	}

	if fillable, ok := user.(Fillable); ok {
		return fillable.ForceFill(map[string]any{column: hashed})
	}
	return nil
}

// newModelQuery is a fresh query with the retrieval callback applied.
//
// It takes no model to open the query on: the query comes from the factory
// rather than from an instance, so there is nothing an instance would carry.
func (p *EloquentUserProvider) newModelQuery(ctx context.Context) *query.Builder {
	q := p.newQuery(ctx)
	if p.queryCallback != nil {
		p.queryCallback(q)
	}
	return q
}

// grant is the authorization every statement in this file goes through. See the
// type's doc for why it is a system grant and where its tenant comes from.
func (p *EloquentUserProvider) grant(action auth.Action) auth.Grant {
	return auth.SystemGrant(action, p.tenant)
}

// CreateModel is a fresh instance of the user type, with nothing filled in.
//
// It calls the constructor the provider was given, for the reason in the type's
// doc.
func (p *EloquentUserProvider) CreateModel() auth.Authenticatable { return p.model() }

// GetHasher is the hasher this provider checks passwords with.
func (p *EloquentUserProvider) GetHasher() auth.Hasher { return p.hasher }

// SetHasher replaces it, and returns the provider.
func (p *EloquentUserProvider) SetHasher(hasher auth.Hasher) *EloquentUserProvider {
	p.hasher = hasher
	return p
}

// GetModel is the constructor this provider builds a user type with.
func (p *EloquentUserProvider) GetModel() func() auth.Authenticatable { return p.model }

// SetModel replaces it, and returns the provider.
func (p *EloquentUserProvider) SetModel(model func() auth.Authenticatable) *EloquentUserProvider {
	p.model = model
	return p
}

// GetQueryCallback is the callback every retrieval query runs through, or nil.
func (p *EloquentUserProvider) GetQueryCallback() func(*query.Builder) { return p.queryCallback }

// WithQuery sets the callback that modifies every retrieval query -- a
// soft-delete filter, an "active only" clause -- and returns the provider.
//
// It does not reach the two writes. A nil callback clears it.
func (p *EloquentUserProvider) WithQuery(queryCallback func(*query.Builder)) *EloquentUserProvider {
	p.queryCallback = queryCallback
	return p
}
