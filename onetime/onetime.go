package onetime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/arandu-io/hesape/cache"
)

// CodeStore is where a one-time code lives between being mailed and being used.
//
// Two methods, because there are only two things that ever happen to such a
// code: it is created, and it is spent. Everything else -- the expiry, the
// attempt limit, the cooldown, the burn -- is a rule the implementation keeps
// on its own, and none of it is a decision the caller gets to make per call.
//
// The guarantees an implementation owes, in the order they matter:
//
//   - The code is stored hashed. A store holding codes in clear is a store
//     whose backup is a list of valid codes.
//   - The comparison is in constant time.
//   - Spending is atomic. Two requests arriving together with the same code is
//     the case that decides whether "once" means once.
//   - Purpose and subject are both bound. A code issued to confirm an address
//     does not delete an account, and one person's does not work for another.
//   - A code expires, and an expired one is refused.
//   - Issuing invalidates whatever was outstanding.
//   - Attempts against one code are limited, and the limit fails closed:
//     storage that cannot be reached refuses the attempt rather than allowing
//     it.
type CodeStore interface {
	// Issue creates a code for purpose and subject and returns it in the clear
	// -- the only time it exists in the clear -- for the caller to send.
	Issue(ctx context.Context, purpose, subject string) (code string, err error)

	// Consume spends code. It returns nil only when the code was the
	// outstanding one for that purpose and subject, had not expired, and had
	// not already been spent. Every other outcome is an error, including every
	// failure of the storage underneath.
	Consume(ctx context.Context, purpose, subject, code string) error
}

// The defaults. A caller that has not thought about these numbers gets numbers
// somebody thought about; a caller that has, sets them.
const (
	// DefaultTTL is how long a code lasts when Config says nothing. Long enough
	// to cover a slow mail server and somebody switching windows, short enough
	// that a code read off a screen over a shoulder is usually already dead.
	DefaultTTL = 10 * time.Minute

	// DefaultCooldown is the shortest gap between two issues when Config says
	// nothing. It is what stops a resend button from being a way to mail
	// somebody a hundred messages.
	DefaultCooldown = time.Minute

	// DefaultMaxAttempts is how many times one code may be presented when Config
	// says nothing. Five guesses against a million codes is one chance in two
	// hundred thousand of hitting a code before it dies, and it is more retries
	// than anybody copying six digits out of another window has ever needed.
	DefaultMaxAttempts = 5
)

// minKeyLen is the shortest application key this package accepts. It is the
// output size of the digest below: a key shorter than the digest it keys is the
// weakest part of the construction, and there is no reason to have one.
const minKeyLen = sha256.Size

var (
	// ErrNoCode means no code is outstanding for this purpose and subject: none
	// was issued, or the one that was has already been spent or replaced. The
	// action is to issue another.
	ErrNoCode = errors.New("onetime: no code is waiting for this purpose and subject")

	// ErrExpired is an outstanding code that ran out of time. Named apart from
	// every other refusal because it is the one somebody can act on -- "ask for
	// another" rather than "check what you typed".
	//
	// It is reachable only while the record outlives the code it describes,
	// which is to say when [Config.Cooldown] is the longer of the two deadlines.
	// In the ordinary configuration the two run out together, and expiry arrives
	// as ErrNoCode instead. Both mean the same thing to whoever is waiting, so a
	// caller that shows them differently has written two sentences for one
	// situation.
	ErrExpired = errors.New("onetime: the code has expired")

	// ErrInvalidCode is a code that does not match the outstanding one.
	ErrInvalidCode = errors.New("onetime: the code does not match")

	// ErrTooManyAttempts means this code has been presented as often as it is
	// allowed to be. The code is finished; another has to be issued.
	ErrTooManyAttempts = errors.New("onetime: this code has been tried too many times")

	// ErrCooldown is an issue that came too soon after the last one. The error
	// returned always carries a [CooldownError], which says how long is left.
	ErrCooldown = errors.New("onetime: a code was issued too recently")

	// ErrUnavailable is storage that could not answer.
	//
	// It is separate from every refusal above because it means something
	// different to the caller: the person did nothing wrong and the application
	// is broken. It is still a refusal -- the attempt does not go through --
	// because the alternative is that an outage becomes a way past the attempt
	// limit.
	ErrUnavailable = errors.New("onetime: the store could not answer, and the attempt is refused")

	// ErrNoPurpose is an empty purpose. A code that is not for anything in
	// particular is a code that works on everything.
	ErrNoPurpose = errors.New("onetime: no purpose was named")

	// ErrNoSubject is an empty subject. A code that belongs to nobody belongs to
	// whoever presents it.
	ErrNoSubject = errors.New("onetime: no subject was named")
)

// CooldownError says how long is left before another code may be issued.
//
// It is a type and not just [ErrCooldown] because the only useful thing to put
// on the screen is the number of seconds, and the caller cannot work it out: it
// knows the cooldown but not when the last code went out.
//
// It unwraps to ErrCooldown, so errors.Is finds it either way.
type CooldownError struct {
	// RetryAfter is how long until Issue would succeed. It is always positive.
	RetryAfter time.Duration
}

func (e *CooldownError) Error() string {
	return fmt.Sprintf("%s: another may be issued in %s", ErrCooldown.Error(), e.RetryAfter.Round(time.Second))
}

// Unwrap makes errors.Is(err, ErrCooldown) true.
func (e *CooldownError) Unwrap() error { return ErrCooldown }

// Config is what a [Codes] may be told. Every field has a working default, and
// a zero Config is the one to start from.
type Config struct {
	// TTL is how long an issued code stays usable. Zero means [DefaultTTL].
	TTL time.Duration

	// Cooldown is the shortest gap between two issues for one purpose and
	// subject. Zero means [DefaultCooldown], and a negative value is refused, so
	// there is no way to turn it off: a resend with no cooldown is a mail flood
	// with a button on it.
	//
	// It is remembered in the outstanding record and nowhere else, which has one
	// consequence worth knowing before setting it: spending a code ends the
	// wait, because the record goes with it. Somebody who has just confirmed an
	// address may ask for the next code at once, which is what they should be
	// able to do -- the cooldown is there to stop a resend button from mailing
	// ten messages, and using a code is not resending it.
	//
	// A Cooldown longer than TTL is honoured. The entry the record lives in is
	// kept for whichever of the two is longer, so the code dies on its own
	// deadline while the memory of when it went out lives as long as the wait
	// does.
	Cooldown time.Duration

	// MaxAttempts is how many times one issued code may be presented before it
	// is finished, right or wrong. Zero means [DefaultMaxAttempts].
	//
	// It is not a knob for tuning throughput. It is the whole defence of a
	// six-digit code, and a large number here is the same as no defence.
	MaxAttempts int

	// Now is the clock. A nil Now means time.Now; a test sets it.
	Now func() time.Time
}

// Codes is the [CodeStore] that keeps its records in a cache.
//
// A cache and not a table, because every record here is temporary by
// construction -- it has a deadline, it is read once, and losing one costs
// somebody a resend rather than costing the application a row. What the store
// underneath has to be is shared: with an in-process store and more than one
// replica, a code issued by one replica is unknown to the other, and roughly
// half of all confirmations fail.
//
// It is safe for concurrent use, and safe against the same code arriving twice
// at the same instant: exactly one of the two Consume calls returns nil.
type Codes struct {
	store cache.Store

	// key is derived from the application key rather than being it, so that a
	// digest computed here can never be mistaken for, or replayed against, a
	// signature computed somewhere else from the same secret.
	key []byte

	ttl         time.Duration
	cooldown    time.Duration
	maxAttempts int

	now func() time.Time

	// random is the source the codes come out of. It is a field so that a test
	// can prove the rejection sampling actually rejects; nothing else has a
	// reason to change it, which is why it is not in Config.
	random io.Reader
}

var _ CodeStore = (*Codes)(nil)

// New returns a Codes over store, keyed by the application key.
//
// The application key is the same secret the session and the signed links use;
// an attacker holding it does not need a fourth. It is not used directly: what
// keys the digests is derived from it, and the bytes handed in are copied, so a
// caller that reuses or zeroes its buffer cannot change what this computes.
func New(store cache.Store, appKey []byte, cfg Config) (*Codes, error) {
	if store == nil {
		return nil, errors.New("onetime: no store was given, and a code that is not written down cannot be checked")
	}
	if len(appKey) < minKeyLen {
		return nil, fmt.Errorf("onetime: the application key is %d bytes and must be at least %d", len(appKey), minKeyLen)
	}
	if cfg.TTL < 0 {
		return nil, fmt.Errorf("onetime: a ttl of %s is not a lifetime", cfg.TTL)
	}
	if cfg.Cooldown < 0 {
		return nil, fmt.Errorf("onetime: a cooldown of %s is not a wait", cfg.Cooldown)
	}
	if cfg.MaxAttempts < 0 {
		return nil, fmt.Errorf("onetime: %d attempts is not a limit", cfg.MaxAttempts)
	}

	c := &Codes{
		store:       store,
		ttl:         cfg.TTL,
		cooldown:    cfg.Cooldown,
		maxAttempts: cfg.MaxAttempts,
		now:         cfg.Now,
		random:      defaultRandom,
	}
	if c.ttl == 0 {
		c.ttl = DefaultTTL
	}
	if c.cooldown == 0 {
		c.cooldown = DefaultCooldown
	}
	if c.maxAttempts == 0 {
		c.maxAttempts = DefaultMaxAttempts
	}
	if c.now == nil {
		c.now = time.Now
	}

	mac := hmac.New(sha256.New, appKey)
	mac.Write([]byte(derivationLabel))
	c.key = mac.Sum(nil)

	return c, nil
}

// derivationLabel separates this package's digests from every other use of the
// application key.
const derivationLabel = "hesape/onetime code key v1"

// record is what one outstanding code looks like in the store.
//
// It holds neither the purpose nor the subject. Both are bound into Digest and
// into the key the record lives under, so the store proves them without holding
// them -- and a cache dump is therefore not a list of the addresses that have a
// code waiting.
type record struct {
	// Nonce distinguishes one issue from the next. It is what makes the attempt
	// counter and the spent-marker belong to a particular code rather than to
	// the pair of purpose and subject, so that replacing a code cannot revive
	// the counters of the one before it.
	Nonce string `json:"n"`

	// Digest is the keyed hash of the code, the purpose, the subject and the
	// nonce together.
	Digest []byte `json:"d"`

	// IssuedAt is what the cooldown is measured from, in Unix milliseconds.
	IssuedAt int64 `json:"i"`

	// ExpiresAt is when the code stops being one, in Unix milliseconds. It is
	// written down rather than left to the store's own expiry because the
	// store's expiry is housekeeping: it decides when the bytes are reclaimed,
	// and this decides when the code stops working.
	ExpiresAt int64 `json:"e"`
}

// Issue creates a code for purpose and subject and returns it in the clear.
//
// The returned string is the only copy of the code that ever exists in the
// clear: what goes into the store is a keyed digest, and there is no way back
// out of it. A caller that loses the return value has to issue another.
//
// Issuing replaces. Whatever was outstanding for this purpose and subject stops
// working the instant this returns, so a caller that mails the new code has not
// left the old one alive behind it.
func (c *Codes) Issue(ctx context.Context, purpose, subject string) (string, error) {
	if purpose == "" {
		return "", ErrNoPurpose
	}
	if subject == "" {
		return "", ErrNoSubject
	}

	scope := c.scope(purpose, subject)
	now := c.now()

	// The cooldown is read off the outstanding record. It needs no key of its
	// own: the record already knows when it was written, and a separate one
	// would be a second thing to keep in step with the first.
	switch stored, err := c.store.Get(ctx, recordKey(scope)); {
	case err == nil:
		var previous record
		if json.Unmarshal(stored, &previous) == nil {
			issued := time.UnixMilli(previous.IssuedAt)
			if wait := c.cooldown - now.Sub(issued); wait > 0 {
				return "", &CooldownError{RetryAfter: wait}
			}
		}
	case errors.Is(err, cache.ErrNotFound):
		// Nothing outstanding, so nothing to wait for.
	default:
		return "", fmt.Errorf("%w: reading the outstanding code: %w", ErrUnavailable, err)
	}

	code, err := c.generate()
	if err != nil {
		return "", err
	}

	raw := make([]byte, 16)
	if _, err := io.ReadFull(c.random, raw); err != nil {
		return "", fmt.Errorf("onetime: reading randomness for the nonce: %w", err)
	}
	nonce := hex.EncodeToString(raw)

	fresh := record{
		Nonce:     nonce,
		Digest:    c.digest(purpose, subject, nonce, code),
		IssuedAt:  now.UnixMilli(),
		ExpiresAt: now.Add(c.ttl).UnixMilli(),
	}
	// A record is a string, a byte slice and two integers, so this cannot fail.
	// It is still checked rather than discarded, because the day somebody adds a
	// field to record is the day it can.
	encoded, err := json.Marshal(fresh)
	if err != nil {
		return "", fmt.Errorf("onetime: encoding the record: %w", err)
	}

	// The entry is kept for whichever of the two deadlines is longer, and the
	// two are different things. The code dies at the ExpiresAt above, which is
	// what Consume reads; the entry is also what remembers when the last code
	// went out, and a cooldown whose memory is reclaimed before it has run is
	// not a cooldown.
	life := c.ttl
	if c.cooldown > life {
		life = c.cooldown
	}

	// Put and not Add: this overwrites what was there, and the overwrite is how
	// the previous code stops being valid. Add would leave the old record in
	// place and hand the caller a code the store has never heard of.
	if err := c.store.Put(ctx, recordKey(scope), encoded, life); err != nil {
		return "", fmt.Errorf("%w: writing the code: %w", ErrUnavailable, err)
	}
	return code, nil
}

// Consume spends code, and returns nil only if it was the right one.
//
// The order inside is the security of the thing, and it is not arrangeable:
// the attempt is counted before the code is compared, so a store that cannot
// count refuses the comparison instead of allowing an unlimited number of them;
// and the code is compared before it is spent, so a wrong guess cannot spend
// somebody else's code.
//
// Every failure is a refusal, including the failures that are the
// application's fault rather than the caller's. Nothing here returns nil
// without a matching code.
func (c *Codes) Consume(ctx context.Context, purpose, subject, code string) error {
	if purpose == "" {
		return ErrNoPurpose
	}
	if subject == "" {
		return ErrNoSubject
	}
	if len(code) == 0 {
		return ErrInvalidCode
	}

	scope := c.scope(purpose, subject)

	stored, err := c.store.Get(ctx, recordKey(scope))
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return ErrNoCode
		}
		return fmt.Errorf("%w: reading the code: %w", ErrUnavailable, err)
	}
	var outstanding record
	if err := json.Unmarshal(stored, &outstanding); err != nil {
		// A record that cannot be read is not a record that can be matched. It
		// is refused rather than replaced, because whatever wrote it is the
		// thing to fix.
		return fmt.Errorf("%w: the stored record is unreadable: %w", ErrUnavailable, err)
	}

	remaining := time.UnixMilli(outstanding.ExpiresAt).Sub(c.now())
	if remaining <= 0 {
		return ErrExpired
	}

	// Counted first, and counted whether the guess turns out right or wrong.
	// Anything that reaches this line has already got past the purpose and the
	// subject, so what is left to do with the code is guess at it, and the
	// count is what makes guessing finite. Increment is the store's atomic
	// counter: a read-then-write here would let attempts arriving together
	// share one increment, which is exactly how a limit of five becomes a limit
	// of five at a time.
	attempts, err := c.store.Increment(ctx, attemptKey(scope, outstanding.Nonce), 1, remaining)
	if err != nil {
		return fmt.Errorf("%w: counting the attempt: %w", ErrUnavailable, err)
	}
	if attempts > int64(c.maxAttempts) {
		return ErrTooManyAttempts
	}

	if !hmac.Equal(outstanding.Digest, c.digest(purpose, subject, outstanding.Nonce, code)) {
		return ErrInvalidCode
	}

	// The burn, and it is one call on purpose. Add writes only if the key is
	// absent and says whether it wrote, so of any number of correct attempts
	// arriving at once, exactly one is told it wrote -- and that one is the
	// consumer. A get-then-put here, or a delete-and-assume, would let two
	// requests carrying the same code both succeed.
	spent, err := c.store.Add(ctx, spentKey(scope, outstanding.Nonce), []byte("1"), remaining)
	if err != nil {
		return fmt.Errorf("%w: spending the code: %w", ErrUnavailable, err)
	}
	if !spent {
		return ErrNoCode
	}

	// Housekeeping, and only housekeeping: the marker above is what makes the
	// code spent, so this failing costs a record its early removal and costs
	// correctness nothing -- a second attempt with the same code reaches the
	// Add above and is told the code is gone.
	_ = c.store.Forget(ctx, recordKey(scope))
	return nil
}

// scope is the opaque name of one purpose-and-subject pair.
//
// It is a keyed digest rather than the two strings joined, for two reasons that
// are both about what a joined string does. It would put the subject -- an
// e-mail address, usually -- into a cache key, where it is visible to anything
// that can list keys. And it would be ambiguous: purpose "reset" with subject
// "a:b" and purpose "reset:a" with subject "b" join to the same string, so one
// person's code would be looked up under the other's name.
func (c *Codes) scope(purpose, subject string) string {
	return hex.EncodeToString(c.mac("scope", purpose, subject)[:16])
}

// digest is the keyed hash of a code, bound to the purpose, the subject and the
// issue it belongs to.
//
// Keyed, and not a bare SHA-256: six digits is a million possibilities, so a
// bare digest of one is a table anybody can build in a second. What makes the
// stored digest worth nothing to whoever reads the cache is that they do not
// have the key, and the key does not live in the cache.
func (c *Codes) digest(purpose, subject, nonce, code string) []byte {
	return c.mac("code", purpose, subject, nonce, code)
}

// mac computes a keyed digest over label and fields.
//
// Every field is preceded by its length, so no set of fields can be rearranged
// into another set with the same digest. Joining them with a separator instead
// would mean the separator can never appear in a field, which is a rule about
// e-mail addresses that nobody would remember to keep.
func (c *Codes) mac(label string, fields ...string) []byte {
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(label))

	var length [8]byte
	for _, field := range fields {
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		mac.Write(length[:])
		mac.Write([]byte(field))
	}
	return mac.Sum(nil)
}

// The three keys one code occupies, and why there are three.
//
// The record is per purpose-and-subject, because the question "what code is
// outstanding for this person" has one answer. The other two are per issue as
// well: an attempt counter shared between successive codes would let a resend
// inherit the guesses made against the code before it, and a spent-marker
// shared between them would let a code spent a minute ago refuse the one issued
// since.
const (
	recordPrefix  = "onetime:code:"
	attemptPrefix = "onetime:attempts:"
	spentPrefix   = "onetime:spent:"
)

func recordKey(scope string) string { return recordPrefix + scope }

func attemptKey(scope, nonce string) string { return attemptPrefix + scope + ":" + nonce }

func spentKey(scope, nonce string) string { return spentPrefix + scope + ":" + nonce }
