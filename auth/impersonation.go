package auth

import (
	"errors"
	"fmt"
	"time"
)

// ErrReadOnly is the refusal of a write made with a Grant issued to a subject
// somebody else is viewing as. It wraps ErrForbidden, so a handler that turns
// ErrForbidden into 403 answers it without a line changing, and one that
// wants to say why asks errors.Is(err, ErrReadOnly).
var ErrReadOnly = fmt.Errorf("%w: the subject is being viewed by somebody else, and only reads", ErrForbidden)

// ErrImpersonation is the refusal of an impersonation Impersonate or Leave
// cannot perform: nobody to act as, somebody already acting as another, or
// leaving a session that is nobody else's.
var ErrImpersonation = errors.New("auth: impersonation refused")

// Impersonate answers target as actor sees it when viewing their account: the
// target's identity, roles and actions, read-only, with actor kept as its
// Impersonator so Leave can answer them back.
//
// It decides nothing about who may view whom. That is a Policy's question,
// asked before this is called -- Authorize(ctx, policy, actor,
// "user.impersonate", target) -- because an operator console and a support
// tool draw that line in different places.
//
// It refuses a guest or an anonymous actor, a target with no identifier, an
// actor who is already viewing as somebody, a target who is, and actor viewing
// as themselves. The answer is never remembered and carries no confirmed
// password, and neither does the actor kept inside it: back from viewing,
// a sensitive action asks for the password again.
func Impersonate(actor, target Subject) (Subject, error) {
	switch {
	case actor.ID == "" || actor.guest:
		return Subject{}, fmt.Errorf("%w: an anonymous subject cannot view as anybody", ErrImpersonation)
	case target.ID == "" || target.guest:
		return Subject{}, fmt.Errorf("%w: there is nobody to view as", ErrImpersonation)
	case actor.Impersonator != nil:
		return Subject{}, fmt.Errorf("%w: %s is already viewing as %s", ErrImpersonation, actor.Impersonator.ID, actor.ID)
	case target.Impersonator != nil:
		return Subject{}, fmt.Errorf("%w: %s is itself being viewed by somebody", ErrImpersonation, target.ID)
	case actor.ID == target.ID && actor.Tenant == target.Tenant:
		return Subject{}, fmt.Errorf("%w: %s cannot view as themselves", ErrImpersonation, actor.ID)
	}
	kept := actor
	kept.Impersonator = nil
	kept.PasswordConfirmedAt = time.Time{}
	viewed := target
	viewed.Remembered = false
	viewed.PasswordConfirmedAt = time.Time{}
	viewed.Impersonator = &kept
	return viewed, nil
}

// Leave answers the subject who was viewing as s: the actor Impersonate kept.
// It refuses a subject nobody is viewing as, so a session that was never an
// impersonation is never turned into another one.
func Leave(s Subject) (Subject, error) {
	if s.Impersonator == nil {
		return Subject{}, fmt.Errorf("%w: %s is not being viewed by anybody", ErrImpersonation, s.ID)
	}
	return *s.Impersonator, nil
}

// Impersonated reports whether somebody else is viewing as this subject, and
// therefore whether every Grant it is issued only reads.
func (s Subject) Impersonated() bool { return s.Impersonator != nil }

// ReadOnly reports whether g was issued to a subject somebody else is viewing
// as. A screen asks it to leave out what could not be done.
func (g Grant) ReadOnly() bool { return g.subject.Impersonator != nil }

// Writable answers nil when g may be used to write, and ErrReadOnly when it
// was issued to a subject somebody else is viewing as.
//
// Every statement of the database that writes asks it -- an insert, an
// update, an upsert, a delete, a truncate, through the query builder or a
// model -- so viewing as somebody cannot change their data whatever the
// application forgot to hide. A store that keeps the account's own records
// elsewhere asks it the same way. SystemGrant has no subject and is always
// writable: a job is nobody's impersonation.
func Writable(g Grant) error {
	if g.subject.Impersonator == nil {
		return nil
	}
	return fmt.Errorf("%w: %s viewing as %s", ErrReadOnly, g.subject.Impersonator.ID, g.subject.ID)
}
