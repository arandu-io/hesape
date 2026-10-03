package redis_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/redis"
	"github.com/arandu-io/hesape/redis/connections"
	"github.com/arandu-io/hesape/session"
	goredis "github.com/redis/go-redis/v9"
)

// The registration, and the promise that opening the store through it changes
// no byte on the server.
//
// The first tests need no server: what they check is what importing this
// package links and what cache.Open hands the connection. The rest compare the
// store and the session handler cache.Open builds with the ones built by hand
// over a connection, key for key and byte for byte, because a deployment that
// moves from one wiring to the other must find its cache, its locks and its
// signed-in visitors where it left them.

// TestImportingThePackageLinksTheStore is the whole of what a blank import
// does, and the check a boot makes before it opens anything.
func TestImportingThePackageLinksTheStore(t *testing.T) {
	if !slices.Contains(cache.Registered(), "redis") {
		t.Fatalf("cache.Registered() = %v, want it to include redis once this package is imported", cache.Registered())
	}
	for _, setting := range []string{"CACHE_STORE", "SESSION_DRIVER"} {
		if err := cache.Linked(setting, "redis"); err != nil {
			t.Errorf("cache.Linked(%s, redis) = %v, want nil", setting, err)
		}
	}
}

// TestOpenDialsNothing: a store that is down must not stop the boot -- that is
// the opposite of what a cache is for. Nothing listens on port 1, and Open
// succeeds anyway; Ping is what finds out.
func TestOpenDialsNothing(t *testing.T) {
	store, err := cache.Open("redis", cache.Endpoint{Address: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("Open dialled, or refused an endpoint it had no reason to: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := store.Ping(ctx); err == nil {
		t.Error("Ping reported a server that is not there as reachable")
	}
}

// TestOpenRefusesAnEndpointWithNoAddress: an empty address read from the
// environment means nobody set it, and talking to localhost instead would be a
// replica sharing nothing with the others.
func TestOpenRefusesAnEndpointWithNoAddress(t *testing.T) {
	store, err := cache.Open("redis", cache.Endpoint{Prefix: "app"})
	if err == nil {
		_ = store.Close()
		t.Fatal("Open accepted an endpoint with no address")
	}
	if !strings.Contains(err.Error(), "no address") {
		t.Errorf("the error does not say what is missing: %v", err)
	}
}

// TestOpenHandsTheEndpointToTheConnection: every field of the endpoint is a
// setting somebody wrote down, and one that stops at the struct is a setting
// that silently does nothing -- TLS above all, where the difference is
// invisible from outside.
func TestOpenHandsTheEndpointToTheConnection(t *testing.T) {
	encryption := &tls.Config{MinVersion: tls.VersionTLS13}
	store, err := cache.Open("redis", cache.Endpoint{
		Address:  "127.0.0.1:1",
		Password: "secret",
		Database: 3,
		Prefix:   "app:",
		TLS:      encryption,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	shared, ok := store.(*redis.Shared)
	if !ok {
		t.Fatalf("Open returned a %T, want *redis.Shared", store)
	}
	if got := shared.GetPrefix(); got != "app" {
		t.Errorf("prefix = %q, want the prefix without its separator, as connections.Connect keeps it", got)
	}

	client, ok := shared.Connection().Client().(*goredis.Client)
	if !ok {
		t.Fatalf("the connection holds a %T, want the single-node client", shared.Connection().Client())
	}
	options := client.Options()
	if options.Addr != "127.0.0.1:1" || options.Password != "secret" || options.DB != 3 {
		t.Errorf("the client was handed addr=%q password=%q db=%d", options.Addr, options.Password, options.DB)
	}
	if options.TLSConfig != encryption {
		t.Errorf("the client was handed TLS %v, want the configuration in the endpoint", options.TLSConfig)
	}
}

// TestTheSharedStoreKeepsEveryHalfACallerAssertsFor: the lock handle asks the
// store for its current owner and the recovery command asks it to flush locks,
// both by type assertion -- a wrapper that hid them would make ForceRelease
// answer ErrUnsupported for a store that supports it.
func TestTheSharedStoreKeepsEveryHalfACallerAssertsFor(t *testing.T) {
	store, err := cache.Open("redis", cache.Endpoint{Address: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, ok := store.(session.Keeper); !ok {
		t.Error("the shared store does not offer its sessions")
	}
	if _, ok := store.(cache.CurrentOwner); !ok {
		t.Error("the shared store does not answer who holds a lock")
	}
	if _, ok := store.(cache.CanFlushLocks); !ok {
		t.Error("the shared store does not flush its locks")
	}
}

// endpoint is the server, with a prefix of its own for this test and this run.
func endpoint(t *testing.T) cache.Endpoint {
	t.Helper()
	return cache.Endpoint{
		Address: address(t),
		Prefix:  "test-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + strconv.FormatInt(time.Now().UnixNano(), 36),
	}
}

// opened returns the store cache.Open builds over the endpoint, connected.
func opened(t *testing.T, e cache.Endpoint) *redis.Shared {
	t.Helper()

	store, err := cache.Open("redis", e)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("connecting to %s: %v", e.Address, err)
	}
	return store.(*redis.Shared)
}

// byHand returns the connection the store and the session handler were built
// over before the registry existed.
func byHand(t *testing.T, e cache.Endpoint) *connections.Connection {
	t.Helper()

	conn := connections.Connect(connections.Config{Address: e.Address, Prefix: e.Prefix})
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// stored is one key as the server holds it: the bytes of a string, the members
// of a sorted set in order. The scores of a sorted set are left out, because
// the session index scores each id by when it expires and two runs a moment
// apart expire a moment apart.
func snapshot(t *testing.T, conn *connections.Connection) map[string]string {
	t.Helper()
	ctx := context.Background()
	client := conn.Client()

	out := map[string]string{}
	var cursor uint64
	for {
		keys, next, err := client.Scan(ctx, cursor, conn.Key("*"), 100).Result()
		if err != nil {
			t.Fatalf("SCAN: %v", err)
		}
		for _, key := range keys {
			kind, err := client.Type(ctx, key).Result()
			if err != nil {
				t.Fatalf("TYPE %s: %v", key, err)
			}
			switch kind {
			case "string":
				value, err := client.Get(ctx, key).Result()
				if err != nil {
					t.Fatalf("GET %s: %v", key, err)
				}
				out[key] = "string " + value
			case "zset":
				members, err := client.ZRange(ctx, key, 0, -1).Result()
				if err != nil {
					t.Fatalf("ZRANGE %s: %v", key, err)
				}
				out[key] = "zset " + strings.Join(members, ",")
			default:
				out[key] = kind
			}
		}
		if next == 0 {
			return out
		}
		cursor = next
	}
}

// forgetAll removes what a scenario wrote, so the second run starts from the
// same empty prefix the first did.
func forgetAll(t *testing.T, conn *connections.Connection, keys map[string]string) {
	t.Helper()
	for key := range keys {
		if err := conn.Client().Del(context.Background(), key).Err(); err != nil {
			t.Fatalf("DEL %s: %v", key, err)
		}
	}
}

// confirmedAt is a stamp with nanoseconds, in UTC so it reads back equal.
var confirmedAt = time.Date(2026, 9, 30, 17, 5, 6, 123456789, time.UTC)

func fullRecord(id string) session.Record[auth.Subject] {
	rec := record(id)
	rec.Payload.Verified = true
	rec.Remembered = true
	rec.PasswordConfirmedAt = confirmedAt
	return rec
}

// writeEverything is one of each kind of key the store and the session handler
// write: a tenant's cache entry, a lock, a session and the index beside it. It
// returns the owner the lock was taken with, which is random by design.
func writeEverything(t *testing.T, store interface {
	cache.Store
	cache.Locking
}, sessions session.Handler[auth.Subject]) string {
	t.Helper()
	ctx := context.Background()

	if err := cache.New(store).Namespace("invoice").Put(ctx, grant(tenantA), "i-1", total{Cents: 1250}, time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	lock := cache.NewLocks(store).Lock("outbox-relay", time.Minute)
	if err := lock.Acquire(ctx); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if err := sessions.Write(ctx, "session-1", fullRecord("u-1"), time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return lock.Owner()
}

// TestTheRegisteredStoreWritesTheKeysTheStoreAlwaysHas is the golden test of
// the switch. The same writes go through the store and session handler built
// by hand over a connection, and then through the ones cache.Open builds over
// the same endpoint; the server has to end up holding the same keys with the
// same bytes under them.
//
// A difference here is a deploy that empties every tenant's cache, releases
// every lock the scheduler holds, or signs every visitor out -- and none of
// those is visible in a test that only reads back what it wrote.
func TestTheRegisteredStoreWritesTheKeysTheStoreAlwaysHas(t *testing.T) {
	e := endpoint(t)
	conn := byHand(t, e)

	owner := writeEverything(t, redis.NewRedisStore(conn), redis.NewCacheBasedSessionHandler[auth.Subject](conn))
	before := snapshot(t, conn)
	lockKey := conn.Key("lock:outbox-relay")
	if before[lockKey] != "string "+owner {
		t.Fatalf("the lock was not written where it always was: %v", before)
	}
	before[lockKey] = "string <owner>"
	forgetAll(t, conn, before)

	shared := opened(t, e)
	owner = writeEverything(t, shared, session.Decode[auth.Subject](shared.Sessions()))
	after := snapshot(t, conn)
	if after[lockKey] != "string "+owner {
		t.Fatalf("the registered store wrote its lock somewhere else: %v", after)
	}
	after[lockKey] = "string <owner>"

	if !reflect.DeepEqual(before, after) {
		t.Errorf("the server holds different keys or bytes after the switch\nbefore:\n%s\nafter:\n%s", describe(before), describe(after))
	}

	// And the keys are the ones each part of the collection promises, so the
	// comparison above is not two empty maps agreeing.
	for _, key := range []string{
		conn.Key("session:session-1"),
		conn.Key("session-index:" + tenantA + ":u-1"),
		lockKey,
	} {
		if _, ok := after[key]; !ok {
			t.Errorf("%s is missing from the server:\n%s", key, describe(after))
		}
	}
	if len(after) != 4 {
		t.Errorf("the server holds %d keys under the prefix, want 4 -- an entry, a lock, a session and its index:\n%s", len(after), describe(after))
	}
}

func describe(keys map[string]string) string {
	lines := make([]string, 0, len(keys))
	for key, value := range keys {
		lines = append(lines, "  "+key+" = "+value)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// profile is a payload whose encoding can be written out by hand: tagged
// fields, and text encoding/json escapes alongside text it leaves alone.
type profile struct {
	ID   string `json:"id"`
	Note string `json:"note"`
}

// goldenSession is the record below as the session handler has always stored
// it. It is written out rather than computed, so the test compares the bytes
// on the server with something no code in this module produced.
//
// The note is spelled with \x5c rather than a backslash so the escapes
// encoding/json writes for <, > and & -- a backslash, a u and four hex digits
// -- are in the constant as bytes, the way they are on the server.
const goldenSession = "{\"Payload\":{\"id\":\"u-1\",\"note\":\"\x5cu003cb\x5cu003eR\x5cu0026D\x5cu003c/b\x5cu003e — São Paulo\"}," +
	`"Tenant":"11111111-1111-4111-8111-111111111111","SubjectID":"u-1","Remembered":true,` +
	`"PasswordConfirmedAt":"2026-09-30T14:05:06.123456789-03:00"}`

func goldenRecord() session.Record[profile] {
	return session.Record[profile]{
		Payload:             profile{ID: "u-1", Note: "<b>R&D</b> — São Paulo"},
		Tenant:              tenantA,
		SubjectID:           "u-1",
		Remembered:          true,
		PasswordConfirmedAt: time.Date(2026, 9, 30, 14, 5, 6, 123456789, time.FixedZone("BRT", -3*60*60)),
	}
}

// TestBothHandlersStoreTheGoldenBytes: the typed handler and the one the
// registry hands out write exactly the record the handler has always written,
// escapes and all.
func TestBothHandlersStoreTheGoldenBytes(t *testing.T) {
	e := endpoint(t)
	conn := byHand(t, e)
	shared := opened(t, e)
	ctx := context.Background()

	handlers := map[string]session.Handler[profile]{
		"typed":      redis.NewCacheBasedSessionHandler[profile](conn),
		"registered": session.Decode[profile](shared.Sessions()),
	}
	for name, handler := range handlers {
		id := "golden-" + name
		if err := handler.Write(ctx, id, goldenRecord(), time.Hour); err != nil {
			t.Fatalf("%s Write: %v", name, err)
		}
		raw, err := conn.Client().Get(ctx, conn.Key("session:"+id)).Result()
		if err != nil {
			t.Fatalf("%s: reading the stored record: %v", name, err)
		}
		if raw != goldenSession {
			t.Errorf("the %s handler stored\n%s\nwant\n%s", name, raw, goldenSession)
		}
	}
}

// TestASessionWrittenBeforeTheSwitchReadsBackAfterIt: a visitor signed in on
// the release before keeps their session on the release after.
func TestASessionWrittenBeforeTheSwitchReadsBackAfterIt(t *testing.T) {
	e := endpoint(t)
	conn := byHand(t, e)
	shared := opened(t, e)
	ctx := context.Background()

	want := fullRecord("u-1")
	if err := redis.NewCacheBasedSessionHandler[auth.Subject](conn).Write(ctx, "old", want, time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := session.Decode[auth.Subject](shared.Sessions()).Read(ctx, "old")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("read back\n%+v\nwant\n%+v", got, want)
	}
}

// TestASessionWrittenAfterTheSwitchReadsBackBeforeIt: and a rollback does not
// sign out the people who signed in on the release being rolled back.
func TestASessionWrittenAfterTheSwitchReadsBackBeforeIt(t *testing.T) {
	e := endpoint(t)
	conn := byHand(t, e)
	shared := opened(t, e)
	ctx := context.Background()

	want := fullRecord("u-2")
	if err := session.Decode[auth.Subject](shared.Sessions()).Write(ctx, "new", want, time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := redis.NewCacheBasedSessionHandler[auth.Subject](conn).Read(ctx, "new")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("read back\n%+v\nwant\n%+v", got, want)
	}
}

// TestSigningOutThroughTheRegisteredHandlerReachesOldSessions: the index is
// shared too, so a password reset after the switch signs out the sessions
// opened before it.
func TestSigningOutThroughTheRegisteredHandlerReachesOldSessions(t *testing.T) {
	e := endpoint(t)
	conn := byHand(t, e)
	shared := opened(t, e)
	ctx := context.Background()

	if err := redis.NewCacheBasedSessionHandler[auth.Subject](conn).Write(ctx, "old", fullRecord("u-1"), time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}
	registered := session.Decode[auth.Subject](shared.Sessions())
	if err := registered.DestroyIndex(ctx, tenantA, "u-1", ""); err != nil {
		t.Fatalf("DestroyIndex: %v", err)
	}
	if _, err := registered.Read(ctx, "old"); !errors.Is(err, session.ErrExpired) {
		t.Errorf("after DestroyIndex, Read = %v, want session.ErrExpired", err)
	}
}

// TestTheRawHandlerHoldsThePayloadAsStored: Sessions hands out the JSON the
// server holds, unchanged, which is what lets the application type it later.
func TestTheRawHandlerHoldsThePayloadAsStored(t *testing.T) {
	e := endpoint(t)
	conn := byHand(t, e)
	shared := opened(t, e)
	ctx := context.Background()

	if err := redis.NewCacheBasedSessionHandler[profile](conn).Write(ctx, "raw", goldenRecord(), time.Hour); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := shared.Sessions().Read(ctx, "raw")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	want, _ := json.Marshal(goldenRecord().Payload)
	if string(got.Payload) != string(want) {
		t.Errorf("payload = %s, want %s", got.Payload, want)
	}
}
