package auth

import "strings"

// Recaller is Illuminate\Auth\Recaller: the "remember me" cookie, read.
//
// The cookie is three fields joined by a pipe -- the user id, the remember
// token, and an HMAC of the password hash -- and this type is the only thing
// that takes it apart. It validates nothing about the user: a valid recaller is
// a well-formed one, and whether it names anybody is the provider's answer to
// RetrieveByToken.
//
// The PHP constructor runs the value through unserialize() first, because a
// Laravel application that upgraded from an older release may still have
// serialized cookies in browsers. PHP's serialization format is a language
// feature Go does not have, and there are no legacy Arandu cookies to read, so
// the value is the string.
type Recaller struct {
	// recaller is the PHP's $recaller: the cookie value.
	recaller string
}

// NewRecaller is Recaller::__construct.
func NewRecaller(recaller string) *Recaller {
	return &Recaller{recaller: recaller}
}

// ID is Recaller::id: the user id, the first segment.
//
// The PHP indexes the result of explode() and warns when the segment is not
// there; this returns "". Ask [Recaller.Valid] before believing any of the
// three, which is what SessionGuard does.
func (r *Recaller) ID() string {
	return r.segment(strings.SplitN(r.recaller, "|", 3), 0)
}

// Token is Recaller::token: the remember token, the second segment.
func (r *Recaller) Token() string {
	return r.segment(strings.SplitN(r.recaller, "|", 3), 1)
}

// Hash is Recaller::hash: the HMAC of the password hash, the third segment.
//
// The PHP splits into four here and into three in the other two methods, so a
// cookie carrying a stray pipe gives the third field alone rather than the rest
// of the string. That is kept, split for split.
func (r *Recaller) Hash() string {
	return r.segment(strings.SplitN(r.recaller, "|", 4), 2)
}

// Valid is Recaller::valid: the cookie is a pipe-joined string with all of its
// segments.
func (r *Recaller) Valid() bool {
	return r.properString() && r.hasAllSegments()
}

// Segments is Recaller::segments: every segment, unlimited.
func (r *Recaller) Segments() []string {
	return strings.Split(r.recaller, "|")
}

// properString is Recaller::properString. The PHP also asks is_string, because
// unserialize() may have handed it something else; here it is always a string.
func (r *Recaller) properString() bool {
	return strings.Contains(r.recaller, "|")
}

// hasAllSegments is Recaller::hasAllSegments: three segments, with an id and a
// token that are not blank. The hash is not checked -- a cookie from before the
// hash existed still names its user.
func (r *Recaller) hasAllSegments() bool {
	segments := r.Segments()

	return len(segments) >= 3 &&
		strings.TrimSpace(segments[0]) != "" &&
		strings.TrimSpace(segments[1]) != ""
}

// segment reads one field of an already split recaller, and answers "" where
// the PHP would read past the end of its array.
func (r *Recaller) segment(segments []string, index int) string {
	if index >= len(segments) {
		return ""
	}
	return segments[index]
}
