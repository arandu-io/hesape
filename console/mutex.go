package console

import (
	"context"
	"errors"
	"time"

	"github.com/arandu-io/hesape/cache"
)

// DefaultIsolationTTL is how long an isolation lock lives when the command does
// not say.
//
// It is CacheCommandMutex's CarbonInterval::hour(). It is the deadlock
// protection and nothing else: a process that dies holding the lock releases it
// when this runs out, so it is sized above the longest isolated command rather
// than tight against the usual one.
const DefaultIsolationTTL = time.Hour

// CommandMutex is what stops a command from running twice at once.
//
// It answers Illuminate\Console\CommandMutex. The mechanical differences from
// the PHP: every method takes a context, because a lock is a round trip to a
// store, and every method returns an error alongside the bool, because that
// round trip can fail and a mutex that reports "not held" when it could not ask
// is a mutex that lets both copies run.
type CommandMutex interface {
	// Create takes the mutex, and reports whether it got it.
	Create(ctx context.Context, command Command) (bool, error)

	// Exists reports whether the mutex is held.
	Exists(ctx context.Context, command Command) (bool, error)

	// Forget releases the mutex.
	Forget(ctx context.Context, command Command) (bool, error)
}

// CacheCommandMutex is the CommandMutex over the cache locks.
//
// It answers Illuminate\Console\CacheCommandMutex, and it is the one the
// framework wires: the lock is the same one cache.Locks issues, so an isolated
// command and a scheduled task contend through one mechanism rather than two.
type CacheCommandMutex struct {
	locks *cache.Locks
	ttl   time.Duration

	// held keeps the lock this process took, so Forget releases the one it owns
	// rather than force-releasing whatever is there. A mutex that force-releases
	// is a mutex that lets the process which took the lock second delete the
	// mark of the process which took it first.
	held map[string]*cache.Lock
}

// NewCacheCommandMutex returns the mutex over an issuer.
func NewCacheCommandMutex(locks *cache.Locks) *CacheCommandMutex {
	return &CacheCommandMutex{locks: locks, ttl: DefaultIsolationTTL, held: map[string]*cache.Lock{}}
}

// UseStore points the mutex at another lock issuer.
//
// It answers CacheCommandMutex::useStore. PHP names a store and resolves it
// from the container; the issuer is passed here, for the reason ADR 0001 gives.
func (m *CacheCommandMutex) UseStore(locks *cache.Locks) *CacheCommandMutex {
	m.locks = locks
	return m
}

// ExpiresAfter sets how long a lock this mutex takes lives.
//
// It answers the isolationLockExpiresAt hook, which PHP looks for on the
// command by method_exists.
func (m *CacheCommandMutex) ExpiresAfter(ttl time.Duration) *CacheCommandMutex {
	if ttl > 0 {
		m.ttl = ttl
	}
	return m
}

// Create takes the mutex for the command.
//
// A lock another process holds is false and no error: that is the mutex working.
func (m *CacheCommandMutex) Create(ctx context.Context, command Command) (bool, error) {
	if m.locks == nil {
		return false, errors.New("console: the command mutex has no lock issuer")
	}

	name := CommandMutexName(command)
	lock := m.locks.Lock(name, m.ttl)

	if err := lock.Acquire(ctx); err != nil {
		if errors.Is(err, cache.ErrLocked) {
			return false, nil
		}
		return false, err
	}
	m.held[name] = lock
	return true, nil
}

// Exists reports whether the mutex is held.
//
// It takes the lock to find out and gives it straight back, which is what the
// PHP does with its tap over $lock->get().
func (m *CacheCommandMutex) Exists(ctx context.Context, command Command) (bool, error) {
	if m.locks == nil {
		return false, errors.New("console: the command mutex has no lock issuer")
	}

	name := CommandMutexName(command)
	if _, mine := m.held[name]; mine {
		return true, nil
	}

	lock := m.locks.Lock(name, m.ttl)
	if err := lock.Acquire(ctx); err != nil {
		if errors.Is(err, cache.ErrLocked) {
			return true, nil
		}
		return false, err
	}
	if err := lock.Release(ctx); err != nil {
		return false, err
	}
	return false, nil
}

// Forget releases the mutex this process took.
//
// A mutex this process does not hold is left alone and reported as not
// released: releasing it would be releasing somebody else's.
func (m *CacheCommandMutex) Forget(ctx context.Context, command Command) (bool, error) {
	name := CommandMutexName(command)
	lock, mine := m.held[name]
	if !mine {
		return false, nil
	}
	delete(m.held, name)
	if err := lock.Release(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// CommandMutexName is the key one command's isolation lock is stored under.
//
// It answers CacheCommandMutex::commandMutexName, prefix included. It is keyed
// on Isolated rather than on the name when the command sets one, which is what
// lets two commands that must not overlap each other share a lock -- a nightly
// reconciliation and the manual command that does the same work name the same
// lock, and neither starts while the other holds it.
//
// The key carries no tenant, and that is deliberate: a lock per tenant would let
// N replicas each run the command for a different tenant at the same time, which
// is the problem and not the solution. See RULE 14 and docs/15.
func CommandMutexName(command Command) string {
	name := command.Isolated
	if name == "" {
		name = command.Name
	}
	return "framework/command-" + name
}
