package users

import "github.com/arandu-io/hesape/auth"

// GenericUser is a user that is nothing but the row it came from.
//
// It is what [DatabaseUserProvider] hands back. There is no user type to
// construct, because the point of that provider is not having one -- so the
// columns stay a map and the four auth.Authenticatable names are read out of
// it.
//
// It lives in this package rather than beside the guards because the provider
// below is the only thing that makes one, and a type with a single producer
// belongs with it.
type GenericUser struct {
	// Attributes is every column of the row.
	//
	// It is exported so that the columns beyond the ones the contract names are
	// reachable at all: index the map to read one, assign to write one.
	Attributes map[string]any
}

// Verify at compile time that a generic user is what a guard consumes.
var _ auth.Authenticatable = (*GenericUser)(nil)

// NewGenericUser returns a user over the row.
func NewGenericUser(attributes map[string]any) *GenericUser {
	return &GenericUser{Attributes: attributes}
}

// GetAuthIdentifierName is the column holding the id: "id".
func (u *GenericUser) GetAuthIdentifierName() string { return "id" }

// GetAuthIdentifier is the id itself.
func (u *GenericUser) GetAuthIdentifier() any { return u.Attributes[u.GetAuthIdentifierName()] }

// GetAuthPasswordName is the column holding the hash: "password".
func (u *GenericUser) GetAuthPasswordName() string { return "password" }

// GetAuthPassword is the hash, never the plain password.
//
// The contract says string, so a column that is not one reads as empty -- which
// every caller already treats as "this account has no password", and which is
// the safe reading of a column nobody can compare a hash against.
func (u *GenericUser) GetAuthPassword() string {
	return stringOf(u.Attributes[u.GetAuthPasswordName()])
}

// GetRememberToken is the token the "remember me" cookie is checked against.
func (u *GenericUser) GetRememberToken() string {
	return stringOf(u.Attributes[u.GetRememberTokenName()])
}

// SetRememberToken writes that token onto the row.
func (u *GenericUser) SetRememberToken(token string) {
	if u.Attributes == nil {
		u.Attributes = map[string]any{}
	}
	u.Attributes[u.GetRememberTokenName()] = token
}

// GetRememberTokenName is the column holding it: "remember_token".
func (u *GenericUser) GetRememberTokenName() string { return "remember_token" }

// stringOf reads a column that ought to be text.
//
// A driver may hand a text column back as []byte -- MySQL does -- so both are
// read as text. Anything else is not text, and reads as empty rather than as
// its Go rendering: a password column holding an integer must not become the
// string "42" and be compared against.
func stringOf(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}
