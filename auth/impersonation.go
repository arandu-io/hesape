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
	kept.Acting = false
	kept.PasswordConfirmedAt = time.Time{}
	viewed := target
	viewed.Remembered = false
	viewed.PasswordConfirmedAt = time.Time{}
	viewed.Impersonator = &kept
	viewed.Acting = false
	return viewed, nil
}

// ImpersonateActing answers target as actor acts in their account: what
// Impersonate answers, refused in the same cases, and able to write. The
// actor is kept as its Impersonator, so every record of what is written can
// name who really wrote it.
//
// It is a separate name and not a flag of Impersonate, because looking at an
// account and acting in it are two decisions: an application asks its Policy
// about each one -- "user.inspect", say -- and a support screen that only
// looks keeps calling Impersonate and keeps reading only.
func ImpersonateActing(actor, target Subject) (Subject, error) {
	viewed, err := Impersonate(actor, target)
	if err != nil {
		return Subject{}, err
	}
	viewed.Acting = true
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

// ReadOnly reports whether g was issued to a subject somebody else is only
// viewing as. A screen asks it to leave out what could not be done.
func (g Grant) ReadOnly() bool { return g.subject.Impersonator != nil && !g.subject.Acting }

// Impersonator is who really acts with g when it was issued to a subject
// somebody else is viewing or acting as, and nil otherwise. The record of an
// act reads it to name the person who did it, beside the account it was done
// in.
func (g Grant) Impersonator() *Subject { return g.subject.Impersonator }

// Writable answers nil when g may be used to write, and ErrReadOnly when it
// was issued to a subject somebody else is only viewing as. A subject
// ImpersonateActing answered writes.
//
// Every statement of the database that writes asks it -- an insert, an
// update, an upsert, a delete, a truncate, through the query builder or a
// model -- so viewing as somebody cannot change their data whatever the
// application forgot to hide. A store that keeps the account's own records
// elsewhere asks it the same way: the filesystem Disk, the cache, a broadcast,
// a webhook dispatch and a queued job all refuse a read-only Grant. SystemGrant
// has no subject and is always writable: a job is nobody's impersonation.
func Writable(g Grant) error {
	if g.subject.Impersonator == nil || g.subject.Acting {
		return nil
	}
	return fmt.Errorf("%w: %s viewing as %s", ErrReadOnly, g.subject.Impersonator.ID, g.subject.ID)
}

// SystemGrantActing is SystemGrant for work somebody set in motion while
// acting in an account through ImpersonateActing: the same system subject,
// action and tenant, writable, with impersonator kept as its Impersonator so
// the record of what the work writes can name the person who caused it.
//
// It is how a queue worker rebuilds the Grant of a job pushed during an
// impersonation. A system subject is nobody's session, so the impersonator is
// carried by identifier alone, with no roles or actions: it is a name for the
// record, never something a Policy decides on.
//
// An empty impersonator answers exactly what SystemGrant answers, and so does
// a tenant SystemGrant refuses.
func SystemGrantActing(a Action, tenant, impersonator string) Grant {
	g := SystemGrant(a, tenant)
	if !g.valid || impersonator == "" {
		return g
	}
	g.subject.Impersonator = &Subject{ID: impersonator}
	g.subject.Acting = true
	return g
}
