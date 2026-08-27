package scheduling

import (
	"context"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// User sets which user the command runs as.
//
// CommandBuilder is what turns it into a sudo, and it does nothing on
// Windows.
func (e *Event) User(user string) *Event {
	e.user = user
	return e
}

// Environments limits the environments the event runs in.
//
// Naming none means every one.
func (e *Event) Environments(environments ...string) *Event {
	e.environments = environments
	return e
}

// EvenInMaintenanceMode lets the event run while the application is down.
func (e *Event) EvenInMaintenanceMode() *Event {
	e.evenInMaintenanceMode = true
	return e
}

// WithoutOverlapping stops the event from running while a copy of it is still
// running.
//
// The lock is both the mark and the filter: an event whose previous run is
// still going is not merely refused, it is reported as skipped.
//
// expiresAt is in minutes, and left out it is a day. It is how long the lock
// lives when the process holding it dies, so it is sized above the longest run
// rather than tight against the usual one.
func (e *Event) WithoutOverlapping(expiresAt ...int) *Event {
	e.withoutOverlapping = true
	e.expiresAt = defaultExpiresAt
	if len(expiresAt) > 0 && expiresAt[0] > 0 {
		e.expiresAt = expiresAt[0]
	}

	return e.Skip(func(ctx context.Context) bool {
		if e.Mutex == nil {
			return false
		}
		exists, err := e.Mutex.Exists(ctx, e)
		// A store that could not answer is treated as held: running twice is
		// worse than not running once, and the next window asks again.
		return err != nil || exists
	})
}

// ExpiresAt reports how many minutes the overlap lock lives. Go cannot have
// a field and a method of the same name.
func (e *Event) ExpiresAt() int { return e.expiresAt }

// OnOneServer allows the event to run on exactly one replica per window.
//
// It is the scheduling mutex rather than the event mutex: the first replica
// to claim the window runs it, and the others skip it entirely rather than
// waiting.
func (e *Event) OnOneServer() *Event {
	e.onOneServer = true
	return e
}

// RunsOnOneServer reports whether the event was limited to one replica.
func (e *Event) RunsOnOneServer() bool { return e.onOneServer }

// RunInBackground sends the command to the background.
//
// Run returns as soon as the program has started, so the runner goes on to the
// next event. The command is waited for beside it, and the exit code, the after
// callbacks and the mutex release happen when it ends rather than when Run
// returns -- which is why an event sent to the background wants
// WithoutOverlapping.
func (e *Event) RunInBackground() *Event {
	e.runInBackground = true
	return e
}

// RunsInBackground reports whether the event was sent to the background.
func (e *Event) RunsInBackground() bool { return e.runInBackground }

// When registers a callback that has to return true for the event to run.
//
// WhenTrue is the bool form, because a Go method cannot take both a closure
// and a bool.
func (e *Event) When(filter Filter) *Event {
	if filter != nil {
		e.filters = append(e.filters, filter)
	}
	return e
}

// WhenTrue is When with a value already decided.
func (e *Event) WhenTrue(condition bool) *Event {
	return e.When(func(context.Context) bool { return condition })
}

// Skip registers a callback that stops the event when it returns true.
func (e *Event) Skip(reject Filter) *Event {
	if reject != nil {
		e.rejects = append(e.rejects, reject)
	}
	return e
}

// SkipTrue is Skip with a value already decided.
func (e *Event) SkipTrue(condition bool) *Event {
	return e.Skip(func(context.Context) bool { return condition })
}

// Name sets the readable description of the event.
//
// It is one call to Description. Both exist because WithoutOverlapping on a
// closure requires one of them to have been called first.
func (e *Event) Name(description string) *Event { return e.Description(description) }

// Description sets the readable description of the event.
func (e *Event) Description(description string) *Event {
	e.description = description
	return e
}

// GetDescription reports the description, or empty.
//
// Go cannot have a field and a method of the same name, so the setter is
// Description and the getter is named for what it does instead.
func (e *Event) GetDescription() string { return e.description }

// GetTimezone reports the timezone the expression is evaluated in, or nil
// for the timezone of the process.
func (e *Event) GetTimezone() *time.Location { return e.timezone }

// Action is what the event's Grant is issued for.
//
// A scheduled task reaches a repository the same way a request does, through
// a Grant that a Policy checked. An event with no action gets a Grant that
// passes nothing, which is a task that can print and can call an API and
// cannot read a customer's rows -- and having to say so is the point.
func (e *Event) Action(action auth.Action) *Event {
	e.action = action
	return e
}

// GetAction is what the event is authorized for, and it reads what Action
// set.
func (e *Event) GetAction() auth.Action { return e.action }

// PerTenant expands the event to one run per tenant, each with its own
// Grant and its own lock.
//
// The tenant comes from the Grant and never from a path or a flag, so a
// task that reads a customer's rows says here that it is per tenant and the
// runner hands it one Grant at a time.
func (e *Event) PerTenant() *Event {
	e.perTenant = true
	return e
}

// RunsPerTenant reports whether the event is expanded to one run per tenant.
func (e *Event) RunsPerTenant() bool { return e.perTenant }

// Tenant fixes the tenant this copy of the event runs for.
//
// The runner calls it while expanding a per-tenant event; a schedule that pins
// one tenant by hand calls it too.
func (e *Event) Tenant(tenant string) *Event {
	e.tenant = tenant
	return e
}

// Grant is the authorization this run carries.
//
// It is auth.SystemGrant of the event's action and tenant, which is the one way
// a scheduled task reaches a repository.
func (e *Event) Grant() auth.Grant { return auth.SystemGrant(e.action, e.tenant) }
