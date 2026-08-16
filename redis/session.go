package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/redis/connections"
	"github.com/arandu-io/hesape/session"
	goredis "github.com/redis/go-redis/v9"
)

// ErrNoTenant is returned when an operation that is scoped to a customer is
// given no tenant, or one that cannot be part of a key.
//
// An error rather than a fallback bucket: "sign subject 1 out everywhere"
// without a tenant would reach the subject 1 of every customer.
var ErrNoTenant = errors.New("redis: the operation needs a tenant, and none was given")

// errNoSubject is the refusal behind a bulk sign-out with no subject to scope
// it by: the empty id names every session nobody has signed in on -- the
// guests. session.ArrayHandler makes the identical refusal, and two handlers
// that disagree about a bulk sign-out diverge only where this one runs, which
// is production.
var errNoSubject = errors.New("redis: signing out every session of a subject needs a subject id")

// CacheBasedSessionHandler is the distributed session handler.
//
// The collection ships session.ArrayHandler, which is correct for one instance
// and wrong for two:
// behind a load balancer, half the requests land on the replica that never saw
// the login. This is the same interface, over RESP, and swapping it in is one
// line in bootstrap.
//
// It is generic over the session payload, exactly as session.Handler is, and it
// stores session.Record whole. That is not a detail: a handler that maps the
// record onto a wire struct of its own drops any field the struct does not name
// -- the write succeeds, the read comes back zero, and it only shows in the
// deployment that uses this handler. The version of this that lived in the kv
// repository had that defect twice over, and session.Store.Confirm still reads
// the password confirmation stamp back because of it. Marshalling the record
// itself is what makes the class of bug unreachable.
type CacheBasedSessionHandler[T any] struct {
	conn *connections.Connection
}

// NewCacheBasedSessionHandler returns the handler over a connection.
//
// The payload type is the one the application keeps in its sessions, and it is
// the same type session.NewStore is built with:
//
//	handler := redis.NewCacheBasedSessionHandler[auth.Subject](conn)
//	store := session.NewStore(appKey, 2*time.Hour, true, handler)
func NewCacheBasedSessionHandler[T any](c *connections.Connection) *CacheBasedSessionHandler[T] {
	return &CacheBasedSessionHandler[T]{conn: c}
}

var _ session.Handler[any] = (*CacheBasedSessionHandler[any])(nil)

// Read returns the record, or session.ErrExpired when this handler does not
// hold the id.
//
// session.ErrExpired specifically, not a not-found of this package's own.
// Callers branch on it to send somebody back to the login page, and a handler
// that returns something else makes swapping the store change how the
// application behaves.
func (h *CacheBasedSessionHandler[T]) Read(ctx context.Context, id string) (session.Record[T], error) {
	var rec session.Record[T]

	raw, err := h.conn.Client().Get(ctx, h.sessionKey(id)).Bytes()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return rec, session.ErrExpired
		}
		return rec, translate(err)
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return session.Record[T]{}, fmt.Errorf("redis: decoding the session record: %w", err)
	}
	return rec, nil
}

// Write stores the record under id for ttl.
//
// The whole record goes down, payload and all, so every field session.Record
// grows arrives here without this file being touched.
func (h *CacheBasedSessionHandler[T]) Write(ctx context.Context, id string, rec session.Record[T], ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("redis: a session needs a lifetime, or it lives until the server is restarted")
	}

	raw, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("redis: encoding the session record: %w", err)
	}
	if err := translate(h.conn.Client().Set(ctx, h.sessionKey(id), raw, ttl).Err()); err != nil {
		return err
	}
	return h.index(ctx, id, rec, ttl)
}

// Destroy removes the session, if present.
//
// This is what the in-memory handler cannot do across replicas: a logout, or a
// password change, invalidates the session everywhere rather than only on the
// instance that handled the request.
func (h *CacheBasedSessionHandler[T]) Destroy(ctx context.Context, id string) error {
	return translate(h.conn.Client().Del(ctx, h.sessionKey(id)).Err())
}

// DestroyIndex removes every session of one subject of one tenant except
// keepID.
//
// The index is allowed to name sessions that are already gone -- a logout
// deletes by id and does not know whose id it was -- so the ids are deleted
// without asking whether they are still there. Deleting a key that expired
// yesterday costs one round trip and is not an error; keeping the index exact
// would cost a read on every logout. What the index may not do is name them
// forever, which is why index scores them by expiry and drops the dead ones on
// every write.
//
// An empty tenant or an empty subject id is refused. Neither names a subject:
// without a tenant the id exists in every customer, and the empty id of one
// customer is every session nobody has signed in on -- the guests.
func (h *CacheBasedSessionHandler[T]) DestroyIndex(ctx context.Context, tenant, subjectID, keepID string) error {
	if tenant == "" {
		return fmt.Errorf("%w: the same subject id exists in every customer, so signing it out without one reaches all of them", ErrNoTenant)
	}
	if subjectID == "" {
		return errNoSubject
	}
	key, err := h.indexKey(tenant, subjectID)
	if err != nil {
		return err
	}

	members, err := h.conn.Client().ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return translate(err)
	}

	sessions := make([]string, 0, len(members))
	stale := make([]any, 0, len(members))
	for _, id := range members {
		if id == keepID {
			continue
		}
		sessions = append(sessions, h.sessionKey(id))
		stale = append(stale, id)
	}
	if len(sessions) == 0 {
		return nil
	}

	if err := translate(h.conn.Client().Del(ctx, sessions...).Err()); err != nil {
		return err
	}
	return translate(h.conn.Client().ZRem(ctx, key, stale...).Err())
}

// index records the session id under its subject, which is what makes
// DestroyIndex possible: a key-value store cannot be asked "every session of
// this account" without something having written that list down.
//
// A record with no tenant or no subject id is not indexed and cannot be signed
// out in bulk. That is the same refusal session.Store.DestroyOthers makes at
// the front door, and it keeps this from failing a Write -- a login must not
// break because the session it just started belongs to a guest.
//
// The index is a sorted set scored by when each session expires, which is the
// one shape that answers both questions it has to answer. A plain set answered
// neither: nothing removed a member when its session ended, so an account that
// signs in every day accumulated one dead id per login forever -- and forever
// is right, because every login also pushed the set's own expiry out. And the
// set's expiry was a read of TTL followed by a write of EXPIRE, so two logins
// landing together, one with the remember-me box ticked and one without, could
// leave the index expiring in an hour with a month-long session inside it. That
// is exactly the failure the expiry was there to prevent: a password reset that
// reports success and signs nobody out.
//
// With a score, the dead ids are dropped by range on every write, and the key's
// own expiry is read off the furthest session in it rather than computed from
// the one being written -- so any later write repairs it instead of
// compounding.
//
// ZADD, ZREMRANGEBYSCORE, ZRANGE and PEXPIREAT, and nothing else: no script and
// no RedisJSON, so Dragonfly, Valkey and KeyDB answer it exactly as Redis
// does.
func (h *CacheBasedSessionHandler[T]) index(ctx context.Context, id string, rec session.Record[T], ttl time.Duration) error {
	if rec.Tenant == "" || rec.SubjectID == "" {
		return nil
	}
	key, err := h.indexKey(rec.Tenant, rec.SubjectID)
	if err != nil {
		return err
	}

	// Milliseconds, not seconds: a score in seconds puts a session shorter than
	// one second in the same tick as the cutoff below, and the prune would drop
	// the id of a session that is still alive. Milliseconds also stay exact in
	// the float64 a score is, which nanoseconds would not.
	now := time.Now()
	if err := translate(h.conn.Client().ZAdd(ctx, key, goredis.Z{
		Score:  float64(now.Add(ttl).UnixMilli()),
		Member: id,
	}).Err()); err != nil {
		return err
	}

	cutoff := strconv.FormatInt(now.UnixMilli(), 10)
	if err := translate(h.conn.Client().ZRemRangeByScore(ctx, key, "-inf", cutoff).Err()); err != nil {
		return err
	}

	// The furthest session left decides when the index goes. An empty answer is
	// a set the prune emptied -- a ttl that had already run out -- and an empty
	// sorted set is a key the store has already deleted.
	last, err := h.conn.Client().ZRangeWithScores(ctx, key, -1, -1).Result()
	if err != nil {
		return translate(err)
	}
	if len(last) == 0 {
		return nil
	}
	return translate(h.conn.Client().PExpireAt(ctx, key, time.UnixMilli(int64(last[0].Score))).Err())
}

// sessionKey names one session.
//
// It is not tenant-prefixed, and that is deliberate: the session id is what
// identifies the tenant, so prefixing by tenant would require knowing the
// answer before asking the question. The id is 256 bits of entropy and the
// cookie carries an HMAC of it, which is what makes an unprefixed key safe here
// and nowhere else.
func (h *CacheBasedSessionHandler[T]) sessionKey(id string) string {
	return h.conn.Key("session:" + id)
}

// indexKey names the sorted set that lists one subject's session ids.
//
// Unlike sessionKey it carries the tenant, because here the tenant is known
// before the question is asked and there is every reason not to leave it out:
// "sign subject 1 out" must not reach the subject 1 of a different customer.
//
// The tenant is checked against auth.ValidTenant rather than being trusted. The
// separator is a colon, so a tenant containing one names another tenant's key:
// tenant "acme:u-1" with subject "x" builds the same string as tenant "acme"
// with subject "u-1:x". No policy is bypassed on the way -- the key is
// assembled after the policy said yes, which is why nothing upstream catches
// it.
func (h *CacheBasedSessionHandler[T]) indexKey(tenant, subjectID string) (string, error) {
	if tenant == "" {
		return "", ErrNoTenant
	}
	if !auth.ValidTenant(tenant) {
		return "", fmt.Errorf("%w: %q cannot be part of a key", ErrNoTenant, tenant)
	}
	// The subject id is the last segment and nothing follows it, so there is
	// nothing for it to impersonate.
	return h.conn.Key("session-index:" + tenant + ":" + subjectID), nil
}
