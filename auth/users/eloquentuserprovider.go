package users

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// EloquentUserProvider answers Illuminate\Auth\EloquentUserProvider: the
// UserProvider that finds users through the application's own user type.
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
// credentials map (RULE 14). A tenant taken off the request would let a caller
// name whose users the sign-in form searches, which is every account in the
// system reachable from an unauthenticated endpoint.
//
// The read paths are authorized exactly as the writes are (RULE 17): retrieveById
// and retrieveByCredentials hold a Grant and are filtered by its tenant, because
// a query without one reads every customer's users.
//
// An application that serves several tenants builds one provider per tenant. A
// provider is four fields and holds no connection state of its own.
//
// # What replaces the Eloquent model, and why
//
// The PHP holds $model, a class-string, and calls `new $class` on it. Go has no
// class string and no way to construct a type named at run time, so the model is
// a constructor function the wiring supplies -- func() auth.Authenticatable.
// That is reason (1) of ADR 0044: a PHP language feature Go does not have.
//
// The query the model would have opened comes in the same way. In PHP
// $model->newQuery() reaches the model's connection, table and global scopes; an
// auth.Authenticatable is an interface value and has none of that behind it, so
// the wiring hands over the model's query factory next to its constructor.
type EloquentUserProvider struct {
	// hasher answers $hasher.
	hasher auth.Hasher

	// model answers $model, the class-string, as the constructor described above.
	model func() auth.Authenticatable

	// newQuery answers $model->newQuery(), hoisted out of the model for the
	// reason given above. It takes a context because the connection a hesape
	// builder runs on is bound at the moment the builder is made.
	newQuery func(ctx context.Context) *query.Builder

	// tenant is the tenant every statement this provider issues is scoped by. It
	// comes from configuration and from nowhere else (RULE 14).
	tenant string

	// queryCallback answers $queryCallback.
	queryCallback func(*query.Builder)
}

// Verify at compile time that the provider is the contract a guard consumes.
var _ auth.UserProvider = (*EloquentUserProvider)(nil)

// NewEloquentUserProvider answers EloquentUserProvider::__construct.
//
// The PHP takes a hasher and the name of a model class. The four arguments here
// are those two with the class-string unpacked: model constructs one, newQuery
// opens a statement against the table it lives in, and tenant is the one every
// statement is filtered by. See the type's doc for both.
func NewEloquentUserProvider(
	hasher auth.Hasher,
	model func() auth.Authenticatable,
	newQuery func(ctx context.Context) *query.Builder,
	tenant string,
) *EloquentUserProvider {
	return &EloquentUserProvider{hasher: hasher, model: model, newQuery: newQuery, tenant: tenant}
}

// RetrieveByID answers EloquentUserProvider::retrieveById. The PHP spells the
// last word Id.
//
// A nil user and a nil error is the PHP's null return: nobody has that
// identifier. An error means the statement failed, which is a different outcome
// and reads differently at the call site.
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

// RetrieveByToken answers EloquentUserProvider::retrieveByToken: the user
// behind a remember-me cookie.
//
// The row is found by identifier and only then is the token compared, in
// constant time, as the PHP's hash_equals does. A row whose remember_token is
// empty never matches -- the PHP's `$rememberToken &&` -- so a user who has
// never ticked the box cannot be signed in with an empty cookie.
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

// UpdateRememberToken answers EloquentUserProvider::updateRememberToken.
//
// The PHP sets the token on the model, turns $timestamps off, saves, and turns
// them back on -- so that being remembered does not look like the account was
// edited. Here the statement writes one column and never mentions updated_at, so
// there is nothing to turn off and nothing to restore.
//
// The instance the caller holds is updated too, which is what setRememberToken
// does there.
func (p *EloquentUserProvider) UpdateRememberToken(ctx context.Context, user auth.Authenticatable, token string) error {
	user.SetRememberToken(token)

	_, err := p.newQuery(ctx).
		Where(user.GetAuthIdentifierName(), "=", user.GetAuthIdentifier()).
		Update(ctx, p.grant(UpdateUser), map[string]any{user.GetRememberTokenName(): token})
	return err
}

// RetrieveByCredentials answers EloquentUserProvider::retrieveByCredentials.
//
// Every key holding the word "password" is dropped before a clause is built, so
// no statement this method issues ever compares a password. Credentials that are
// nothing but password keys match nobody, and the PHP returns null there rather
// than running a query with no where clause -- which would answer with the first
// user in the table.
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

// ValidateCredentials answers EloquentUserProvider::validateCredentials.
//
// A missing password, and a user whose password column is empty, are both false
// -- the PHP's two is_null guards. The empty column matters: a row created by an
// invite flow that has never had a password set must not be signed in to by
// offering an empty one.
//
// A password that is not a string is coerced rather than refused, and an empty
// one is handed to the hasher rather than refused early. Both are what the PHP
// does, and neither used to be -- see [passwordOf] for the login body that
// proved it.
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

// RehashPasswordIfRequired answers
// EloquentUserProvider::rehashPasswordIfRequired: it upgrades a hash that was
// made with weaker parameters than the ones in force.
//
// It runs on a sign-in that has already proved the plain password, which is the
// only moment the plain password exists and the whole reason this is possible at
// all. A hash that already meets the parameters is left alone unless force says
// otherwise, and that is the common case -- so the statement below is not issued
// on an ordinary sign-in.
//
// The PHP writes the new hash with forceFill(...)->save(). Here the column is
// written by the statement, and the instance in memory is updated when the user
// type can be filled -- see Fillable. A user type that cannot keeps the old hash
// in memory for the rest of the request, and the row is correct either way.
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

// newModelQuery answers the protected EloquentUserProvider::newModelQuery: a
// fresh query with the retrieval callback applied.
//
// The PHP takes an optional model to open the query on, so that retrieveById can
// reuse the instance it made for getAuthIdentifierName. There is nothing to
// reuse here -- the query comes from the factory rather than from the instance
// -- so the parameter has nothing to carry and is gone.
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

// CreateModel answers EloquentUserProvider::createModel: a fresh instance of the
// user type, with nothing filled in.
//
// The PHP builds it from the class-string with `new $class`; here it calls the
// constructor the provider was given, for the reason in the type's doc.
func (p *EloquentUserProvider) CreateModel() auth.Authenticatable { return p.model() }

// GetHasher answers EloquentUserProvider::getHasher.
func (p *EloquentUserProvider) GetHasher() auth.Hasher { return p.hasher }

// SetHasher answers EloquentUserProvider::setHasher, and returns the provider
// as the PHP returns $this.
func (p *EloquentUserProvider) SetHasher(hasher auth.Hasher) *EloquentUserProvider {
	p.hasher = hasher
	return p
}

// GetModel answers EloquentUserProvider::getModel.
//
// The PHP answers with the class-string; this answers with the constructor that
// replaced it.
func (p *EloquentUserProvider) GetModel() func() auth.Authenticatable { return p.model }

// SetModel answers EloquentUserProvider::setModel, and returns the provider as
// the PHP returns $this.
func (p *EloquentUserProvider) SetModel(model func() auth.Authenticatable) *EloquentUserProvider {
	p.model = model
	return p
}

// GetQueryCallback answers EloquentUserProvider::getQueryCallback.
func (p *EloquentUserProvider) GetQueryCallback() func(*query.Builder) { return p.queryCallback }

// WithQuery answers EloquentUserProvider::withQuery: it sets the callback that
// modifies every retrieval query -- a soft-delete filter, an "active only"
// clause -- and returns the provider as the PHP returns $this.
//
// It does not reach the two writes, exactly as it does not in PHP: those go
// through $user->save(), which no query callback sees. A nil callback clears it,
// which is the PHP's default argument of null.
func (p *EloquentUserProvider) WithQuery(queryCallback func(*query.Builder)) *EloquentUserProvider {
	p.queryCallback = queryCallback
	return p
}
