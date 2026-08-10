package hashing

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// The hasher defaults, taken from the PHP class properties. ArgonHasher starts
// at memory 1024, time 2 and threads 2; BcryptHasher starts at 12 rounds. They
// are the defaults of Illuminate\Hashing, not the parameters the package-level
// Make writes: Make is the framework's own path and is deliberately not
// configurable, while a hasher is the mirror of the PHP class and takes its
// numbers from the caller.
const (
	defaultArgonMemory  = 1024
	defaultArgonTime    = 2
	defaultArgonThreads = 2
	defaultBcryptRounds = 12
)

// ErrValueTooLong answers the InvalidArgumentException BcryptHasher::make
// throws when the value is longer than the configured 'limit'.
var ErrValueTooLong = errors.New("hashing: value is too long to hash")

// ErrWrongAlgorithm answers the RuntimeException ArgonHasher::check,
// Argon2IdHasher::check and BcryptHasher::check throw when the hasher was built
// with 'verify' and the stored hash was written by another algorithm.
var ErrWrongAlgorithm = errors.New("hashing: this password does not use the expected algorithm")

// Options answers the $options array that Illuminate\Hashing passes to the
// hasher constructors and to make, check and needsRehash. PHP has one array
// shape shared by every hasher, so this is one struct: each hasher reads the
// keys it knows and ignores the rest.
//
// A zero field means the key is absent from the PHP array, so the hasher's own
// value is used — the Go spelling of $options['memory'] ?? $this->memory.
type Options struct {
	// Rounds answers 'rounds', the bcrypt cost factor.
	Rounds int
	// Memory answers 'memory', the argon2 memory cost in KiB.
	Memory int
	// Time answers 'time', the number of argon2 passes.
	Time int
	// Threads answers 'threads', the argon2 degree of parallelism.
	Threads int
	// Verify answers 'verify', which makes Check refuse a hash written by
	// another algorithm instead of verifying it. Constructor only, as in PHP.
	Verify bool
	// Limit answers 'limit', the longest value in bytes BcryptHasher::make
	// accepts. Zero is PHP's null: no limit.
	Limit int
}

// firstOption reads the single optional $options array. PHP declares it as
// "array $options = []"; Go spells the same thing as a variadic parameter, and
// only the first is read.
func firstOption(options []Options) Options {
	if len(options) == 0 {
		return Options{}
	}
	return options[0]
}

// AbstractHasher answers Illuminate\Hashing\AbstractHasher. It holds the two
// methods every hasher shares, and the concrete hashers embed it.
type AbstractHasher struct{}

// Info answers AbstractHasher::info, which is password_get_info. PHP returns an
// array whose 'algoName' is "unknown" for a value that is not a hash; here the
// second result reports that instead, so a caller cannot read parameters off a
// value that has none.
func (AbstractHasher) Info(hashedValue string) (Params, bool) {
	return Info(hashedValue)
}

// Check answers AbstractHasher::check, which is password_verify. An empty
// hashed value is false rather than an error, exactly as the PHP guard on
// is_null and strlen. The base implementation performs no algorithm check, so
// it never fails: the $options argument PHP declares here is unused in its
// body and has no counterpart.
func (AbstractHasher) Check(value, hashedValue string) bool {
	if hashedValue == "" {
		return false
	}
	return Check(value, hashedValue) == nil
}

// ArgonHasher answers Illuminate\Hashing\ArgonHasher. It writes argon2i, which
// is what PASSWORD_ARGON2I selects; Argon2IdHasher is the same hasher over
// argon2id.
type ArgonHasher struct {
	AbstractHasher

	memory          int
	time            int
	threads         int
	verifyAlgorithm bool
	algorithm       Algorithm
}

// NewArgonHasher answers ArgonHasher::__construct. An absent option keeps the
// PHP default: memory 1024, time 2, threads 2, verify off.
func NewArgonHasher(options ...Options) *ArgonHasher {
	o := firstOption(options)
	h := &ArgonHasher{
		memory:          defaultArgonMemory,
		time:            defaultArgonTime,
		threads:         defaultArgonThreads,
		verifyAlgorithm: o.Verify,
		algorithm:       Argon2i,
	}
	if o.Time > 0 {
		h.time = o.Time
	}
	if o.Memory > 0 {
		h.memory = o.Memory
	}
	if o.Threads > 0 {
		h.threads = o.Threads
	}
	return h
}

// Algorithm reports which argon2 variant this hasher writes. It answers the
// protected ArgonHasher::algorithm, which returns PASSWORD_ARGON2I here and
// PASSWORD_ARGON2ID in Argon2IdHasher.
func (h *ArgonHasher) Algorithm() Algorithm {
	return h.algorithm
}

// Make answers ArgonHasher::make. The PHP throws a RuntimeException when the
// build cannot hash with argon2; the error here reports parameters argon2
// cannot be run with, which is the only way this can fail in Go.
func (h *ArgonHasher) Make(value string, options ...Options) (string, error) {
	o := firstOption(options)
	memory, time, threads := h.memoryOf(o), h.timeOf(o), h.threadsOf(o)

	if time < 1 || threads < 1 || threads > 255 || memory < 8*threads {
		return "", fmt.Errorf("hashing: argon2 parameters out of range: m=%d,t=%d,p=%d", memory, time, threads)
	}

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	var key []byte
	if h.algorithm == Argon2id {
		key = argon2.IDKey([]byte(value), salt, uint32(time), uint32(memory), uint8(threads), argonKeyLen)
	} else {
		key = argon2.Key([]byte(value), salt, uint32(time), uint32(memory), uint8(threads), argonKeyLen)
	}

	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		h.algorithm, argon2.Version,
		memory, time, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Check answers ArgonHasher::check, and Argon2IdHasher::check when the hasher
// was built by NewArgon2IdHasher. An empty hashed value is false. With 'verify'
// set, a hash written by another algorithm is ErrWrongAlgorithm, which is the
// RuntimeException the PHP throws.
//
// password_verify returns false for a value it cannot read at all; the bool is
// false here too, and the error names the corrupt column, which a wrong
// password never produces.
func (h *ArgonHasher) Check(value, hashedValue string, options ...Options) (bool, error) {
	if hashedValue == "" {
		return false, nil
	}
	if h.verifyAlgorithm && !h.isUsingCorrectAlgorithm(hashedValue) {
		return false, fmt.Errorf("%w: %s", ErrWrongAlgorithm, h.algorithm)
	}
	switch err := Check(value, hashedValue); {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrInvalidPassword):
		return false, nil
	default:
		return false, err
	}
}

// NeedsRehash answers ArgonHasher::needsRehash, which is password_needs_rehash:
// true when the hash was not written by this algorithm with these parameters. A
// value it cannot read needs a rehash too, as in PHP.
func (h *ArgonHasher) NeedsRehash(hashedValue string, options ...Options) bool {
	p, ok := Info(hashedValue)
	if !ok {
		return true
	}
	o := firstOption(options)
	return p.Algorithm != h.algorithm ||
		int(p.Memory) != h.memoryOf(o) ||
		int(p.Time) != h.timeOf(o) ||
		int(p.Threads) != h.threadsOf(o)
}

// VerifyConfiguration answers ArgonHasher::verifyConfiguration: the hash uses
// this algorithm and was written with parameters no stronger than the ones
// configured.
func (h *ArgonHasher) VerifyConfiguration(value string) bool {
	return h.isUsingCorrectAlgorithm(value) && h.isUsingValidOptions(value)
}

// SetMemory answers ArgonHasher::setMemory and returns the hasher, which is the
// Go reading of PHP's "return $this".
func (h *ArgonHasher) SetMemory(memory int) *ArgonHasher {
	h.memory = memory
	return h
}

// SetTime answers ArgonHasher::setTime and returns the hasher.
func (h *ArgonHasher) SetTime(time int) *ArgonHasher {
	h.time = time
	return h
}

// SetThreads answers ArgonHasher::setThreads and returns the hasher.
func (h *ArgonHasher) SetThreads(threads int) *ArgonHasher {
	h.threads = threads
	return h
}

// isUsingCorrectAlgorithm answers the protected
// ArgonHasher::isUsingCorrectAlgorithm.
func (h *ArgonHasher) isUsingCorrectAlgorithm(hashedValue string) bool {
	p, ok := Info(hashedValue)
	return ok && p.Algorithm == h.algorithm
}

// isUsingValidOptions answers the protected ArgonHasher::isUsingValidOptions.
// PHP first refuses an info array whose costs are not integers; the parser here
// refuses the same values before they reach Params, so an unreadable hash is
// false.
func (h *ArgonHasher) isUsingValidOptions(hashedValue string) bool {
	p, ok := Info(hashedValue)
	if !ok || p.Memory == 0 || p.Time == 0 || p.Threads == 0 {
		return false
	}
	return int(p.Memory) <= h.memory && int(p.Time) <= h.time && int(p.Threads) <= h.threads
}

// memoryOf answers the protected ArgonHasher::memory.
func (h *ArgonHasher) memoryOf(o Options) int {
	if o.Memory > 0 {
		return o.Memory
	}
	return h.memory
}

// timeOf answers the protected ArgonHasher::time.
func (h *ArgonHasher) timeOf(o Options) int {
	if o.Time > 0 {
		return o.Time
	}
	return h.time
}

// threadsOf answers the protected ArgonHasher::threads. PHP forces one thread
// when the argon2 provider is libsodium; Go has one provider, so there is
// nothing to force.
func (h *ArgonHasher) threadsOf(o Options) int {
	if o.Threads > 0 {
		return o.Threads
	}
	return h.threads
}

// Argon2IdHasher answers Illuminate\Hashing\Argon2IdHasher: the argon2 hasher
// over argon2id rather than argon2i. In PHP it overrides only the algorithm and
// the message of the failed algorithm check; here the algorithm is a field, so
// the inherited methods already read it.
type Argon2IdHasher struct {
	ArgonHasher
}

// NewArgon2IdHasher answers Argon2IdHasher::__construct, which is the inherited
// ArgonHasher::__construct over PASSWORD_ARGON2ID.
func NewArgon2IdHasher(options ...Options) *Argon2IdHasher {
	h := &Argon2IdHasher{ArgonHasher: *NewArgonHasher(options...)}
	h.algorithm = Argon2id
	return h
}

// SetMemory answers ArgonHasher::setMemory, returning this hasher so a chain
// stays on the argon2id type as PHP's "return $this" does.
func (h *Argon2IdHasher) SetMemory(memory int) *Argon2IdHasher {
	h.ArgonHasher.SetMemory(memory)
	return h
}

// SetTime answers ArgonHasher::setTime, returning this hasher.
func (h *Argon2IdHasher) SetTime(time int) *Argon2IdHasher {
	h.ArgonHasher.SetTime(time)
	return h
}

// SetThreads answers ArgonHasher::setThreads, returning this hasher.
func (h *Argon2IdHasher) SetThreads(threads int) *Argon2IdHasher {
	h.ArgonHasher.SetThreads(threads)
	return h
}

// BcryptHasher answers Illuminate\Hashing\BcryptHasher. It is the hasher a
// users table imported from a PHP application was written with, so it exists to
// read those rows; the framework's own Make writes argon2id.
type BcryptHasher struct {
	AbstractHasher

	rounds          int
	verifyAlgorithm bool
	limit           int
}

// NewBcryptHasher answers BcryptHasher::__construct. An absent option keeps the
// PHP default: 12 rounds, verify off, no length limit.
func NewBcryptHasher(options ...Options) *BcryptHasher {
	o := firstOption(options)
	h := &BcryptHasher{
		rounds:          defaultBcryptRounds,
		verifyAlgorithm: o.Verify,
		limit:           o.Limit,
	}
	if o.Rounds > 0 {
		h.rounds = o.Rounds
	}
	return h
}

// Algorithm reports which algorithm this hasher writes. BcryptHasher has no
// protected algorithm() in PHP because it names PASSWORD_BCRYPT inline; this
// reports the same constant.
func (h *BcryptHasher) Algorithm() Algorithm {
	return Bcrypt
}

// Make answers BcryptHasher::make. A value longer than the configured 'limit'
// is ErrValueTooLong, which is the InvalidArgumentException the PHP throws, and
// the length is counted in bytes as strlen counts it. bcrypt itself refuses a
// value over 72 bytes, which is the RuntimeException case.
func (h *BcryptHasher) Make(value string, options ...Options) (string, error) {
	if h.limit > 0 && len(value) > h.limit {
		return "", fmt.Errorf("%w: value must be less than %d bytes", ErrValueTooLong, h.limit)
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(value), h.cost(firstOption(options)))
	if err != nil {
		return "", fmt.Errorf("hashing: bcrypt hashing not supported: %w", err)
	}
	return string(hashed), nil
}

// Check answers BcryptHasher::check. An empty hashed value is false. With
// 'verify' set, a hash written by another algorithm is ErrWrongAlgorithm, which
// is the RuntimeException the PHP throws.
func (h *BcryptHasher) Check(value, hashedValue string, options ...Options) (bool, error) {
	if hashedValue == "" {
		return false, nil
	}
	if h.verifyAlgorithm && !h.isUsingCorrectAlgorithm(hashedValue) {
		return false, fmt.Errorf("%w: %s", ErrWrongAlgorithm, Bcrypt)
	}
	switch err := Check(value, hashedValue); {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrInvalidPassword):
		return false, nil
	default:
		return false, err
	}
}

// NeedsRehash answers BcryptHasher::needsRehash, which is password_needs_rehash
// against PASSWORD_BCRYPT and the effective cost.
func (h *BcryptHasher) NeedsRehash(hashedValue string, options ...Options) bool {
	p, ok := Info(hashedValue)
	if !ok {
		return true
	}
	return p.Algorithm != Bcrypt || p.Cost != h.cost(firstOption(options))
}

// VerifyConfiguration answers BcryptHasher::verifyConfiguration: the hash is
// bcrypt and its cost is no higher than the configured rounds.
func (h *BcryptHasher) VerifyConfiguration(value string) bool {
	return h.isUsingCorrectAlgorithm(value) && h.isUsingValidOptions(value)
}

// SetRounds answers BcryptHasher::setRounds and returns the hasher, which is
// the Go reading of PHP's "return $this".
func (h *BcryptHasher) SetRounds(rounds int) *BcryptHasher {
	h.rounds = rounds
	return h
}

// isUsingCorrectAlgorithm answers the protected
// BcryptHasher::isUsingCorrectAlgorithm.
func (h *BcryptHasher) isUsingCorrectAlgorithm(hashedValue string) bool {
	p, ok := Info(hashedValue)
	return ok && p.Algorithm == Bcrypt
}

// isUsingValidOptions answers the protected BcryptHasher::isUsingValidOptions.
func (h *BcryptHasher) isUsingValidOptions(hashedValue string) bool {
	p, ok := Info(hashedValue)
	if !ok || p.Cost == 0 {
		return false
	}
	return p.Cost <= h.rounds
}

// cost answers the protected BcryptHasher::cost, which reads 'rounds' from the
// options array and falls back to the configured rounds.
func (h *BcryptHasher) cost(o Options) int {
	if o.Rounds > 0 {
		return o.Rounds
	}
	return h.rounds
}
