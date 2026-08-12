package access

import "net/http"

// HandlesAuthorization is the Illuminate\Auth\Access\HandlesAuthorization trait.
//
// A PHP trait is a set of methods copied into a class; the Go form of that is an
// embedded struct, so a policy embeds this one and gains the same two methods:
//
//	type PostPolicy struct {
//		access.HandlesAuthorization
//	}
//
//	func (p PostPolicy) Update(ctx context.Context, user auth.Subject, arguments ...any) any {
//		return p.DenyAsNotFound("no post with that id", nil)
//	}
//
// Gate embeds it too, exactly as the PHP class does with `use HandlesAuthorization`.
//
// The trait's other two methods, allow() and deny(), are protected. An
// unexported method promoted from an embedded struct is not callable by the
// package that embeds it, so they are the package functions Allow and Deny.
type HandlesAuthorization struct{}

// DenyWithStatus is HandlesAuthorization::denyWithStatus.
func (HandlesAuthorization) DenyWithStatus(status int, message string, code any) *Response {
	return DenyWithStatus(status, message, code)
}

// DenyAsNotFound is HandlesAuthorization::denyAsNotFound.
func (HandlesAuthorization) DenyAsNotFound(message string, code any) *Response {
	return DenyWithStatus(http.StatusNotFound, message, code)
}
