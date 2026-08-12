package users

import "github.com/arandu-io/hesape/auth"

// GenericUser answers Illuminate\Auth\GenericUser: a user that is nothing but
// the row it came from.
//
// It is what DatabaseUserProvider hands back. There is no user type to
// construct, because the point of that provider is not having one -- so the
// columns stay a map and the four Authenticatable names are read out of it.
//
// It lives in this package rather than beside the guards because the provider
// below is the only thing that makes one, and a type with a single producer
// belongs with it.
type GenericUser struct {
	// Attributes answers $attributes: every column of the row.
	//
	// It is exported, and that is what answers the PHP's __get, __set, __isset
	// and __unset. Those four exist because a property named at run time is not
	// otherwise reachable in PHP; indexing a map is the same operation with no
	// magic behind it, so the four methods have nothing left to do. That is
	// reason (1) of ADR 0044.
	Attributes map[string]any
}

// Verify at compile time that a generic user is what a guard consumes.
var _ auth.Authenticatable = (*GenericUser)(nil)

// NewGenericUser answers GenericUser::__construct.
func NewGenericUser(attributes map[string]any) *GenericUser {
	return &GenericUser{Attributes: attributes}
}

// GetAuthIdentifierName answers GenericUser::getAuthIdentifierName.
func (u *GenericUser) GetAuthIdentifierName() string { return "id" }

// GetAuthIdentifier answers GenericUser::getAuthIdentifier.
func (u *GenericUser) GetAuthIdentifier() any { return u.Attributes[u.GetAuthIdentifierName()] }

// GetAuthPasswordName answers GenericUser::getAuthPasswordName.
func (u *GenericUser) GetAuthPasswordName() string { return "password" }

// GetAuthPassword answers GenericUser::getAuthPassword.
//
// The PHP returns whatever is in the column, of whatever type the driver made
// of it. The contract says string, so a column that is not one reads as empty --
// which every caller already treats as "this account has no password", and which
// is the safe reading of a column nobody can compare a hash against.
func (u *GenericUser) GetAuthPassword() string {
	return stringOf(u.Attributes[u.GetAuthPasswordName()])
}

// GetRememberToken answers GenericUser::getRememberToken.
func (u *GenericUser) GetRememberToken() string {
	return stringOf(u.Attributes[u.GetRememberTokenName()])
}

// SetRememberToken answers GenericUser::setRememberToken.
func (u *GenericUser) SetRememberToken(token string) {
	if u.Attributes == nil {
		u.Attributes = map[string]any{}
	}
	u.Attributes[u.GetRememberTokenName()] = token
}

// GetRememberTokenName answers GenericUser::getRememberTokenName.
func (u *GenericUser) GetRememberTokenName() string { return "remember_token" }

// stringOf reads a column that ought to be text.
//
// A driver may hand a text column back as []byte -- MySQL does -- and PHP would
// have coerced it on the way into a string comparison. Anything else is not
// text, and reads as empty rather than as its Go rendering: a password column
// holding an integer must not become the string "42" and be compared against.
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
