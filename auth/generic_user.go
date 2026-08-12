package auth

// GenericUser is Illuminate\Auth\GenericUser: an Authenticatable that is
// nothing but the row it was built from.
//
// It is what a user provider hands back when there is no model to hydrate --
// the database provider's every result is one of these. An application with its
// own user type does not use it: that type satisfies Authenticatable by having
// the seven methods.
type GenericUser struct {
	// Attributes is the PHP's $attributes: the row, column for column.
	//
	// The PHP reaches the columns that are not one of the seven contract
	// methods through __get, __set, __isset and __unset, which Go has no
	// equivalent of. The map is those four: read a column with [GenericUser.Get]
	// or by indexing, write one by assigning, ask with the two-value index, drop
	// one with delete. It is exported for exactly that reason.
	Attributes map[string]any
}

var _ Authenticatable = (*GenericUser)(nil)

// NewGenericUser is GenericUser::__construct.
func NewGenericUser(attributes map[string]any) *GenericUser {
	if attributes == nil {
		attributes = map[string]any{}
	}
	return &GenericUser{Attributes: attributes}
}

// GetAuthIdentifierName is GenericUser::getAuthIdentifierName.
func (u *GenericUser) GetAuthIdentifierName() string {
	return "id"
}

// GetAuthIdentifier is GenericUser::getAuthIdentifier.
func (u *GenericUser) GetAuthIdentifier() any {
	return u.Attributes[u.GetAuthIdentifierName()]
}

// GetAuthPasswordName is GenericUser::getAuthPasswordName.
func (u *GenericUser) GetAuthPasswordName() string {
	return "password"
}

// GetAuthPassword is GenericUser::getAuthPassword: the hash, never the plain
// password.
//
// The contract types it as a string and the map holds any, so a column that is
// not a string reads as "". The PHP would hand the other type on and fail
// further in, at the hasher.
func (u *GenericUser) GetAuthPassword() string {
	return u.stringAttribute(u.GetAuthPasswordName())
}

// GetRememberToken is GenericUser::getRememberToken.
func (u *GenericUser) GetRememberToken() string {
	return u.stringAttribute(u.GetRememberTokenName())
}

// SetRememberToken is GenericUser::setRememberToken.
func (u *GenericUser) SetRememberToken(value string) {
	u.Attributes[u.GetRememberTokenName()] = value
}

// GetRememberTokenName is GenericUser::getRememberTokenName.
func (u *GenericUser) GetRememberTokenName() string {
	return "remember_token"
}

// Get is GenericUser::__get: one column, whatever type it came back as.
//
// PHP invokes __get on any property that is not declared; Go has no such hook,
// so reading a column is this call. A column that is not there is nil, which is
// the null the PHP's undefined index warns about and then returns.
func (u *GenericUser) Get(key string) any {
	return u.Attributes[key]
}

// stringAttribute reads a column the contract types as a string.
func (u *GenericUser) stringAttribute(key string) string {
	value, _ := u.Attributes[key].(string)
	return value
}
