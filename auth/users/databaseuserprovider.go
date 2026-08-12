package users

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// Connection is the part of Illuminate\Database\ConnectionInterface that a user
// provider asks for: a query builder against one table.
//
// It is declared here, with its consumer, rather than imported from the database
// package -- the same choice query.Connection makes, and for the same reason. A
// provider needs one method of a connection, and depending on the whole
// interface to get it is a dependency the wiring cannot substitute in a test.
//
// *database.Connection satisfies it as written.
type Connection interface {
	// Table answers ConnectionInterface::table. The context is the fifth
	// mechanical change of ADR 0044: a hesape builder binds its connection at the
	// moment it is made, so the context enters here.
	Table(ctx context.Context, table any, as ...string) *query.Builder
}

// DatabaseUserProvider answers Illuminate\Auth\DatabaseUserProvider: the
// UserProvider that reads a table directly and wraps each row in a GenericUser.
//
// It is EloquentUserProvider with the model taken out. There is no user type to
// construct and nothing to hydrate, which is what it is for: an application that
// has not declared a user struct, or a second table of users that does not
// deserve one.
//
// Everything the Eloquent provider's doc says about the Grant is true here.
// Every statement is taken under auth.SystemGrant, because a provider runs
// before there is a subject to authorize; the action is RetrieveUser or
// UpdateUser; and the tenant comes from configuration, never from the request
// (RULE 14). Reads are scoped exactly as writes are (RULE 17).
type DatabaseUserProvider struct {
	// connection answers $connection.
	connection Connection

	// hasher answers $hasher.
	hasher auth.Hasher

	// table answers $table.
	table string

	// tenant is the tenant every statement this provider issues is scoped by.
	tenant string
}

// Verify at compile time that the provider is the contract a guard consumes.
var _ auth.UserProvider = (*DatabaseUserProvider)(nil)

// NewDatabaseUserProvider answers DatabaseUserProvider::__construct.
//
// The fourth argument is the tenant every statement is filtered by. It has no
// counterpart in the PHP, which has no tenants; see the type's doc for where it
// must come from.
func NewDatabaseUserProvider(connection Connection, hasher auth.Hasher, table, tenant string) *DatabaseUserProvider {
	return &DatabaseUserProvider{connection: connection, hasher: hasher, table: table, tenant: tenant}
}

// RetrieveByID answers DatabaseUserProvider::retrieveById. The PHP spells the
// last word Id.
//
// A nil user and a nil error is the PHP's null: nobody has that identifier.
func (p *DatabaseUserProvider) RetrieveByID(ctx context.Context, identifier any) (auth.Authenticatable, error) {
	record, err := p.getTable(ctx).Find(ctx, p.grant(RetrieveUser), identifier)
	if err != nil {
		return nil, err
	}
	return p.getGenericUser(record), nil
}

// RetrieveByToken answers DatabaseUserProvider::retrieveByToken.
//
// The comparison is hash_equals: constant time, and an empty stored token never
// matches.
func (p *DatabaseUserProvider) RetrieveByToken(ctx context.Context, identifier any, token string) (auth.Authenticatable, error) {
	retrieved, err := p.RetrieveByID(ctx, identifier)
	if err != nil || retrieved == nil {
		return nil, err
	}
	if !tokensMatch(retrieved.GetRememberToken(), token) {
		return nil, nil
	}
	return retrieved, nil
}

// UpdateRememberToken answers DatabaseUserProvider::updateRememberToken.
//
// The instance the caller holds is updated as well as the row. The PHP does not
// -- it writes the column and leaves the object it was handed carrying the old
// token -- and the difference shows the moment a guard sets a cookie from the
// user it just updated. Doing both is what the Eloquent provider does there, so
// this is the two providers agreeing rather than a new behaviour.
func (p *DatabaseUserProvider) UpdateRememberToken(ctx context.Context, user auth.Authenticatable, token string) error {
	user.SetRememberToken(token)

	_, err := p.getTable(ctx).
		Where(user.GetAuthIdentifierName(), "=", user.GetAuthIdentifier()).
		Update(ctx, p.grant(UpdateUser), map[string]any{user.GetRememberTokenName(): token})
	return err
}

// RetrieveByCredentials answers DatabaseUserProvider::retrieveByCredentials.
//
// Every key holding the word "password" is dropped before a clause is built, and
// credentials that are nothing but password keys match nobody rather than
// matching the first row in the table.
func (p *DatabaseUserProvider) RetrieveByCredentials(ctx context.Context, credentials map[string]any) (auth.Authenticatable, error) {
	filtered := filterCredentials(credentials)
	if len(filtered) == 0 {
		return nil, nil
	}

	q := p.getTable(ctx)
	applyCredentials(q, filtered)

	record, err := q.First(ctx, p.grant(RetrieveUser))
	if err != nil {
		return nil, err
	}
	return p.getGenericUser(record), nil
}

// ValidateCredentials answers DatabaseUserProvider::validateCredentials. It is
// the Eloquent provider's method, unchanged, as it is in the PHP.
func (p *DatabaseUserProvider) ValidateCredentials(_ context.Context, user auth.Authenticatable, credentials map[string]any) bool {
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
// DatabaseUserProvider::rehashPasswordIfRequired.
//
// The PHP writes the column with an update statement here rather than through a
// model, because there is no model; that is what this does too, and it updates
// the GenericUser it was handed so the rest of the request reads the new hash.
func (p *DatabaseUserProvider) RehashPasswordIfRequired(ctx context.Context, user auth.Authenticatable, credentials map[string]any, force bool) error {
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
	if _, err := p.getTable(ctx).
		Where(user.GetAuthIdentifierName(), "=", user.GetAuthIdentifier()).
		Update(ctx, p.grant(UpdateUser), map[string]any{column: hashed}); err != nil {
		return err
	}

	if generic, ok := user.(*GenericUser); ok {
		generic.Attributes[column] = hashed
	}
	return nil
}

// getGenericUser answers the protected DatabaseUserProvider::getGenericUser: a
// row becomes a user, and no row becomes no user.
//
// The nil is returned as an untyped nil rather than as a (*GenericUser)(nil)
// inside an interface, because the second compares unequal to nil at every call
// site and would make "no such user" read as a user with no columns.
func (p *DatabaseUserProvider) getGenericUser(record query.Record) auth.Authenticatable {
	if record == nil {
		return nil
	}
	return NewGenericUser(record)
}

// getTable answers the connection->table($this->table) that opens every method
// of the PHP class.
func (p *DatabaseUserProvider) getTable(ctx context.Context) *query.Builder {
	return p.connection.Table(ctx, p.table)
}

// grant is the authorization every statement in this file goes through. See the
// type's doc for why it is a system grant and where its tenant comes from.
func (p *DatabaseUserProvider) grant(action auth.Action) auth.Grant {
	return auth.SystemGrant(action, p.tenant)
}
