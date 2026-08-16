package scheduling

import (
	"context"
	"errors"
	"time"

	"github.com/arandu-io/hesape/cache"
)

// EventMutex is what stops one event from overlapping itself.
//
// Every method takes a context, because a lock is a round trip to a store,
// and every method can fail, because that round trip can -- and a mutex
// that reports "not held" when it could not ask is a mutex that lets both
// copies run.
type EventMutex interface {
	// Create takes the mutex for the event, and reports whether it got it.
	Create(ctx context.Context, event *Event) (bool, error)

	// Exists reports whether the mutex is held.
	Exists(ctx context.Context, event *Event) (bool, error)

	// Forget releases the mutex.
	Forget(ctx context.Context, event *Event) error
}

// SchedulingMutex is what makes an event run on exactly one replica per window.
//
// It is a different mutex from EventMutex and for a different reason: this
// one marks a window as claimed, and it is never released -- releasing it
// would let the replica that ticks two hundred milliseconds later claim the
// same window and run it a second time, which is the duplicate the mark
// exists to stop.
type SchedulingMutex interface {
	// Create claims the window, and reports whether it got it.
	Create(ctx context.Context, event *Event, at time.Time) (bool, error)

	// Exists reports whether the window is claimed.
	Exists(ctx context.Context, event *Event, at time.Time) (bool, error)
}

// CacheAware is a mutex that can be pointed at another store.
//
// The issuer is passed explicitly rather than resolved from a container.
type CacheAware interface {
	// UseStore points the mutex at another lock issuer.
	UseStore(locks *cache.Locks)
}

// CacheEventMutex is the EventMutex over the cache locks.
type CacheEventMutex struct {
	locks *cache.Locks
	held  map[string]*cache.Lock
}

// NewCacheEventMutex returns the mutex over an issuer.
func NewCacheEventMutex(locks *cache.Locks) *CacheEventMutex {
	return &CacheEventMutex{locks: locks, held: map[string]*cache.Lock{}}
}

// UseStore points the mutex at another lock issuer.
func (m *CacheEventMutex) UseStore(locks *cache.Locks) { m.locks = locks }

// Create takes the mutex for the event.
//
// A lock another process holds is false and no error: that is the mutex working.
func (m *CacheEventMutex) Create(ctx context.Context, event *Event) (bool, error) {
	if m.locks == nil {
		return false, errors.New("scheduling: the event mutex has no lock issuer")
	}

	name := event.MutexName()
	lock := m.locks.Lock(name, time.Duration(event.ExpiresAt())*time.Minute)

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
// It takes the lock to find out and gives it straight back.
func (m *CacheEventMutex) Exists(ctx context.Context, event *Event) (bool, error) {
	if m.locks == nil {
		return false, errors.New("scheduling: the event mutex has no lock issuer")
	}

	name := event.MutexName()
	if _, mine := m.held[name]; mine {
		return true, nil
	}

	lock := m.locks.Lock(name, time.Duration(event.ExpiresAt())*time.Minute)
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
// A lock this process does not hold is left alone: force-releasing it would
// let the replica that took the lock second delete the mark of the replica
// that took it first.
func (m *CacheEventMutex) Forget(ctx context.Context, event *Event) error {
	name := event.MutexName()
	lock, mine := m.held[name]
	if !mine {
		return nil
	}
	delete(m.held, name)
	return lock.Release(ctx)
}

// schedulingMutexTTL is how long a claimed window is remembered: long enough
// that no replica ticking the same minute can miss the mark, short enough
// that it is gone before the same expression comes round the next day.
const schedulingMutexTTL = time.Hour

// CacheSchedulingMutex is the SchedulingMutex over the cache locks.
type CacheSchedulingMutex struct {
	locks *cache.Locks
}

// NewCacheSchedulingMutex returns the mutex over an issuer.
func NewCacheSchedulingMutex(locks *cache.Locks) *CacheSchedulingMutex {
	return &CacheSchedulingMutex{locks: locks}
}

// UseStore points the mutex at another lock issuer.
func (m *CacheSchedulingMutex) UseStore(locks *cache.Locks) { m.locks = locks }

// Create claims the window for this replica.
//
// The lock is taken and never released: it marks the window as claimed rather
// than guarding the work as a mutex would.
func (m *CacheSchedulingMutex) Create(ctx context.Context, event *Event, at time.Time) (bool, error) {
	if m.locks == nil {
		return false, errors.New("scheduling: the scheduling mutex has no lock issuer")
	}

	if err := m.locks.Lock(schedulingMutexName(event, at), schedulingMutexTTL).Acquire(ctx); err != nil {
		if errors.Is(err, cache.ErrLocked) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Exists reports whether the window is already claimed.
func (m *CacheSchedulingMutex) Exists(ctx context.Context, event *Event, at time.Time) (bool, error) {
	if m.locks == nil {
		return false, errors.New("scheduling: the scheduling mutex has no lock issuer")
	}

	lock := m.locks.Lock(schedulingMutexName(event, at), schedulingMutexTTL)
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

// schedulingMutexName is the event's mutex name with the window appended.
//
// The window is the hour and the minute, so two replicas ticking the same
// minute contend and the next minute is a different key.
func schedulingMutexName(event *Event, at time.Time) string {
	return event.MutexName() + at.Format("1504")
}
