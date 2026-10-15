package account

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/auth"
	authusers "github.com/arandu-io/hesape/auth/users"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/hashing"
)

// credentialUser is the narrow adapter the user provider hydrates.
//
// The provider reads the row as a record and hands it to SetRawAttributes,
// where it becomes an account the way every other read makes one: a row of the
// application's users query. The rehash that follows a sign-in puts the new
// hash on it through ForceFill. The remember-token methods have no storage
// behind them on purpose: a session is created by the application after every
// factor, never by a provider from a cookie.
type credentialUser struct {
	user   Account
	filled bool
	query  func() *model.Builder
}

// SetRawAttributes fills the adapter from the row the provider read, onto an
// account wired to the connection the row came from.
func (c *credentialUser) SetRawAttributes(attributes map[string]any, sync bool) error {
	user, err := newAccount(c.query())
	if err != nil {
		return err
	}
	if err := user.SetRawAttributes(attributes, sync); err != nil {
		return err
	}
	c.user, c.filled = user, true
	return nil
}

// ForceFill puts what the provider wrote back on the account it read.
func (c *credentialUser) ForceFill(attributes map[string]any) error {
	if !c.filled {
		return model.ErrUnwired
	}
	return c.user.ForceFill(attributes)
}

func (*credentialUser) GetAuthIdentifierName() string { return columnID }
func (*credentialUser) GetAuthPasswordName() string   { return columnPassword }
func (*credentialUser) GetRememberToken() string      { return "" }
func (*credentialUser) SetRememberToken(string)       {}
func (*credentialUser) GetRememberTokenName() string  { return "" }

func (c *credentialUser) GetAuthIdentifier() any {
	if !c.filled {
		return nil
	}
	return c.user.AccountID()
}

func (c *credentialUser) GetAuthPassword() string {
	if !c.filled {
		return ""
	}
	return c.user.PasswordHash()
}

// credentialProvider keeps the broad provider contract from opening a
// remember-token persistence path nothing here owns.
type credentialProvider struct{ *authusers.ModelUserProvider }

func (*credentialProvider) RetrieveByToken(context.Context, any, string) (auth.Authenticatable, error) {
	return nil, nil
}

func (*credentialProvider) UpdateRememberToken(_ context.Context, user auth.Authenticatable, token string) error {
	user.SetRememberToken(token)
	return nil
}

// credentials is the verifier of one tenant's passwords. It rehashes a hash
// made with weaker parameters on the sign-in that proved it, through the
// application's users query.
func (s *Users) credentials(tenant string) *auth.CredentialVerifier {
	provider := authusers.NewModelUserProvider(
		hashing.ForAuth(nil),
		func() auth.Authenticatable { return &credentialUser{query: s.query} },
		func(context.Context) *query.Builder { return s.query().GetQuery() },
		tenant,
	)
	return auth.NewCredentialVerifier(&credentialProvider{ModelUserProvider: provider}, nil, true, 0)
}

// newAccount is an empty, unsaved account on the users query's connection.
func newAccount(b *model.Builder) (Account, error) {
	entity, err := b.NewModelInstance(nil)
	if err != nil {
		return nil, err
	}
	user, ok := entity.(Account)
	if !ok {
		return nil, fmt.Errorf("account: the users query builds %T, which does not implement account.Account", entity)
	}
	return user, nil
}

var (
	_ authusers.Hydratable = (*credentialUser)(nil)
	_ authusers.Fillable   = (*credentialUser)(nil)
)
