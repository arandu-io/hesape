package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/str"
)

// DefaultLimit is how many notifications a read returns when the caller asks
// for none. A bell menu shows a screenful; a query with no limit at all is the
// one that pages in four years of rows on the day somebody opens it.
const DefaultLimit = 50

// MaxLimit caps what a caller may ask for, for the same reason.
const MaxLimit = 500

// scope is the check every store operation starts with: the Grant was issued
// for this action, and it carries a tenant to scope by.
//
// It is a function rather than a copied three lines in each method because the
// method that forgets it is the method that reads every customer's rows.
func scope(g auth.Grant, a auth.Action) (string, error) {
	if err := g.Check(a); err != nil {
		return "", err
	}
	tenant := auth.Tenant(g)
	if !auth.ValidTenant(tenant) {
		return "", fmt.Errorf("%w: the grant for %s carries no usable tenant", auth.ErrForbidden, a)
	}
	return tenant, nil
}

// sane clamps a caller's limit into the range a bell menu can use.
func sane(limit int) int {
	switch {
	case limit <= 0:
		return DefaultLimit
	case limit > MaxLimit:
		return MaxLimit
	default:
		return limit
	}
}

// prepare fills in what a Record needs before it can be written and refuses
// what cannot be.
func prepare(r Record, tenant string, now time.Time) (Record, error) {
	if !r.Key.Valid() {
		return Record{}, fmt.Errorf("notifications: %q is not a key", string(r.Key))
	}
	if r.NotifiableID == "" || r.NotifiableType == "" {
		return Record{}, ErrAnonymous
	}
	if len(r.Data) == 0 {
		r.Data = json.RawMessage("{}")
	}
	if !json.Valid(r.Data) {
		return Record{}, errors.New("notifications: the payload is not JSON")
	}
	if r.ID == "" {
		r.ID = str.UUID7()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	// The tenant is taken from the Grant and never from the record handed in.
	// A caller who sets it is a caller who could set somebody else's.
	r.Tenant = tenant
	return r, nil
}

// MemoryStore keeps notifications in memory.
//
// It is the store a test uses and the store a single-process tool uses, in one
// type: a second "fake" implementation next to a real one is two things to keep
// in step, and the one the tests use is the one that drifts (RULE 9).
//
// It enforces the same Grant and the same tenant scoping as the table, which is
// the point -- a test that passes against a store with no authorization proves
// nothing about the code that runs in production.
type MemoryStore struct {
	mu   sync.Mutex
	rows []Record
	now  func() time.Time
}

// NewMemoryStore returns an empty store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{now: func() time.Time { return time.Now().UTC() }}
}

var _ Store = (*MemoryStore)(nil)

// Save writes one.
func (s *MemoryStore) Save(_ context.Context, g auth.Grant, r Record) (Record, error) {
	tenant, err := scope(g, ActionSend)
	if err != nil {
		return Record{}, err
	}
	row, err := prepare(r, tenant, s.now())
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, row)
	return row, nil
}

// For returns the most recent notifications for a recipient, newest first.
func (s *MemoryStore) For(_ context.Context, g auth.Grant, to Notifiable, limit int) ([]Record, error) {
	return s.list(g, to, limit, false)
}

// Unread is For, restricted to the ones not yet read.
func (s *MemoryStore) Unread(_ context.Context, g auth.Grant, to Notifiable, limit int) ([]Record, error) {
	return s.list(g, to, limit, true)
}

func (s *MemoryStore) list(g auth.Grant, to Notifiable, limit int, unreadOnly bool) ([]Record, error) {
	tenant, err := scope(g, ActionList)
	if err != nil {
		return nil, err
	}
	if to == nil {
		return nil, errors.New("notifications: no recipient")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Record
	for _, r := range s.rows {
		if r.Tenant != tenant || r.NotifiableType != to.NotifiableType() || r.NotifiableID != to.NotifiableID() {
			continue
		}
		if unreadOnly && r.Read() {
			continue
		}
		out = append(out, r)
	}
	slices.SortStableFunc(out, func(a, b Record) int {
		switch {
		case a.CreatedAt.After(b.CreatedAt):
			return -1
		case a.CreatedAt.Before(b.CreatedAt):
			return 1
		default:
			return 0
		}
	})
	if n := sane(limit); len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// MarkAsRead stamps one.
func (s *MemoryStore) MarkAsRead(_ context.Context, g auth.Grant, id string) error {
	tenant, err := scope(g, ActionRead)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.rows {
		if r.ID != id || r.Tenant != tenant {
			continue
		}
		if r.Unread() {
			s.rows[i].ReadAt = s.now()
		}
		return nil
	}
	return fmt.Errorf("%w: notification %s", database.ErrNotFound, id)
}

// MarkAsUnread clears the stamp on one.
func (s *MemoryStore) MarkAsUnread(_ context.Context, g auth.Grant, id string) error {
	tenant, err := scope(g, ActionRead)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.rows {
		if r.ID != id || r.Tenant != tenant {
			continue
		}
		s.rows[i].ReadAt = time.Time{}
		return nil
	}
	return fmt.Errorf("%w: notification %s", database.ErrNotFound, id)
}

// MarkAllAsRead stamps every unread notification a recipient has.
func (s *MemoryStore) MarkAllAsRead(_ context.Context, g auth.Grant, to Notifiable) error {
	tenant, err := scope(g, ActionRead)
	if err != nil {
		return err
	}
	if to == nil {
		return errors.New("notifications: no recipient")
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.rows {
		if r.Tenant != tenant || r.NotifiableType != to.NotifiableType() || r.NotifiableID != to.NotifiableID() {
			continue
		}
		if r.Unread() {
			s.rows[i].ReadAt = now
		}
	}
	return nil
}

// Delete removes one.
func (s *MemoryStore) Delete(_ context.Context, g auth.Grant, id string) error {
	tenant, err := scope(g, ActionDelete)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.rows {
		if r.ID == id && r.Tenant == tenant {
			s.rows = append(s.rows[:i], s.rows[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: notification %s", database.ErrNotFound, id)
}
