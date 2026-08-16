package access

import "net/http"

// HandlesAuthorization is the two denial shorthands a policy embeds to reach
// without importing them:
//
//	type PostPolicy struct {
//		access.HandlesAuthorization
//	}
//
//	func (p PostPolicy) Update(ctx context.Context, user auth.Subject, arguments ...any) any {
//		return p.DenyAsNotFound("no post with that id", nil)
//	}
//
// [Gate] embeds it too.
//
// The allow and deny shorthands are the package functions [Allow] and [Deny]
// instead: an unexported method promoted from an embedded struct is not
// callable by the package that embeds it.
type HandlesAuthorization struct{}

// DenyWithStatus is a denial that answers with the given HTTP status.
func (HandlesAuthorization) DenyWithStatus(status int, message string, code any) *Response {
	return DenyWithStatus(status, message, code)
}

// DenyAsNotFound is a denial that answers 404.
func (HandlesAuthorization) DenyAsNotFound(message string, code any) *Response {
	return DenyWithStatus(http.StatusNotFound, message, code)
}
