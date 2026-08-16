package hashing

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// The hasher defaults. [ArgonHasher] starts at memory 1024, time 2 and threads
// 2; [BcryptHasher] starts at 12 rounds. They are not the parameters the
// package-level [Make] writes: [Make] is deliberately not configurable, while a
// hasher takes its numbers from the caller.
const (
	defaultArgonMemory  = 1024
	defaultArgonTime    = 2
	defaultArgonThreads = 2
	defaultBcryptRounds = 12
)

// ErrValueTooLong is returned by [BcryptHasher.Make] when the value is longer
// than the configured Limit.
var ErrValueTooLong = errors.New("hashing: value is too long to hash")

// ErrWrongAlgorithm is returned by Check when the hasher was built with Verify
// and the stored hash was written by another algorithm.
var ErrWrongAlgorithm = errors.New("hashing: this password does not use the expected algorithm")

// Options are the per-call and constructor settings the hashers take. It is one
// struct shared by every hasher: each reads the fields it knows and ignores the
// rest.
//
// A zero field means the setting was not given, so the hasher's own value is
// used.
type Options struct {
	// Rounds is the bcrypt cost factor.
	Rounds int
	// Memory is the argon2 memory cost in KiB.
	Memory int
	// Time is the number of argon2 passes.
	Time int
	// Threads is the argon2 degree of parallelism.
	Threads int
	// Verify makes Check refuse a hash written by another algorithm instead
	// of verifying it. It is read at construction only.
	Verify bool
	// Limit is the longest value in bytes [BcryptHasher.Make] accepts. Zero
	// is no limit.
	Limit int
}

// firstOption reads the single optional Options. Only the first is read.
func firstOption(options []Options) Options {
	if len(options) == 0 {
		return Options{}
	}
	return options[0]
}

// AbstractHasher holds the two methods every hasher shares, and the concrete
// hashers embed it.
type AbstractHasher struct{}

// Info reports the parameters hashedValue was written with. The second result
// is false for a value that is not a hash at all, so a caller cannot read
// parameters off a value that has none.
func (AbstractHasher) Info(hashedValue string) (Params, bool) {
	return Info(hashedValue)
}

// Check reports whether value hashes to hashedValue. An empty hashed value is
// false rather than an error. This implementation performs no algorithm check,
// so it never refuses on the algorithm alone.
func (AbstractHasher) Check(value, hashedValue string) bool {
	if hashedValue == "" {
		return false
	}
	return Check(value, hashedValue) == nil
}

// ArgonHasher writes argon2i. [Argon2IdHasher] is the same hasher over
// argon2id.
type ArgonHasher struct {
	AbstractHasher

	memory          int
	time            int
	threads         int
	verifyAlgorithm bool
	algorithm       Algorithm
}

// NewArgonHasher returns an argon2i hasher. An absent option keeps the default:
// memory 1024, time 2, threads 2, verify off.
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

// Algorithm reports which argon2 variant this hasher writes: argon2i here, and
// argon2id when the hasher came from [NewArgon2IdHasher].
func (h *ArgonHasher) Algorithm() Algorithm {
	return h.algorithm
}

// Make hashes value with this hasher's parameters, or the ones given. The error
// reports parameters argon2 cannot be run with, which is the only way this
// fails.
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

// Check reports whether value hashes to hashedValue. An empty hashed value is
// false. With Verify set, a hash written by another algorithm is
// [ErrWrongAlgorithm].
//
// A value that cannot be read at all is false with an error naming the corrupt
// column, which a merely wrong password never produces.
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

// NeedsRehash is true when the hash was not written by this algorithm with
// these parameters. A value that cannot be read needs a rehash too.
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

// VerifyConfiguration reports whether the hash uses this algorithm and was
// written with parameters no stronger than the configured ones.
func (h *ArgonHasher) VerifyConfiguration(value string) bool {
	return h.isUsingCorrectAlgorithm(value) && h.isUsingValidOptions(value)
}

// SetMemory sets the argon2 memory cost and returns the hasher, so calls chain.
func (h *ArgonHasher) SetMemory(memory int) *ArgonHasher {
	h.memory = memory
	return h
}

// SetTime sets the number of argon2 passes and returns the hasher.
func (h *ArgonHasher) SetTime(time int) *ArgonHasher {
	h.time = time
	return h
}

// SetThreads sets the argon2 degree of parallelism and returns the hasher.
func (h *ArgonHasher) SetThreads(threads int) *ArgonHasher {
	h.threads = threads
	return h
}

// isUsingCorrectAlgorithm reports whether hashedValue was written by this
// hasher's algorithm.
func (h *ArgonHasher) isUsingCorrectAlgorithm(hashedValue string) bool {
	p, ok := Info(hashedValue)
	return ok && p.Algorithm == h.algorithm
}

// isUsingValidOptions reports whether hashedValue was written with costs no
// higher than this hasher's. A hash whose costs cannot be parsed is refused
// before it reaches [Params], so an unreadable hash is false.
func (h *ArgonHasher) isUsingValidOptions(hashedValue string) bool {
	p, ok := Info(hashedValue)
	if !ok || p.Memory == 0 || p.Time == 0 || p.Threads == 0 {
		return false
	}
	return int(p.Memory) <= h.memory && int(p.Time) <= h.time && int(p.Threads) <= h.threads
}

// memoryOf is the memory cost for this call: the option when given, otherwise
// the hasher's own.
func (h *ArgonHasher) memoryOf(o Options) int {
	if o.Memory > 0 {
		return o.Memory
	}
	return h.memory
}

// timeOf is the number of passes for this call: the option when given,
// otherwise the hasher's own.
func (h *ArgonHasher) timeOf(o Options) int {
	if o.Time > 0 {
		return o.Time
	}
	return h.time
}

// threadsOf is the degree of parallelism for this call: the option when given,
// otherwise the hasher's own.
func (h *ArgonHasher) threadsOf(o Options) int {
	if o.Threads > 0 {
		return o.Threads
	}
	return h.threads
}

// Argon2IdHasher is [ArgonHasher] over argon2id rather than argon2i. The
// algorithm is a field, so the embedded methods already read it.
type Argon2IdHasher struct {
	ArgonHasher
}

// NewArgon2IdHasher returns an argon2id hasher. It takes the same options as
// [NewArgonHasher].
func NewArgon2IdHasher(options ...Options) *Argon2IdHasher {
	h := &Argon2IdHasher{ArgonHasher: *NewArgonHasher(options...)}
	h.algorithm = Argon2id
	return h
}

// SetMemory sets the argon2 memory cost and returns this hasher, so a chain
// stays on the argon2id type.
func (h *Argon2IdHasher) SetMemory(memory int) *Argon2IdHasher {
	h.ArgonHasher.SetMemory(memory)
	return h
}

// SetTime sets the number of passes and returns this hasher.
func (h *Argon2IdHasher) SetTime(time int) *Argon2IdHasher {
	h.ArgonHasher.SetTime(time)
	return h
}

// SetThreads sets the degree of parallelism and returns this hasher.
func (h *Argon2IdHasher) SetThreads(threads int) *Argon2IdHasher {
	h.ArgonHasher.SetThreads(threads)
	return h
}

// BcryptHasher is the hasher a users table imported from an existing
// application was written with, so it exists to read those rows; the
// package-level [Make] writes argon2id.
type BcryptHasher struct {
	AbstractHasher

	rounds          int
	verifyAlgorithm bool
	limit           int
}

// NewBcryptHasher returns a bcrypt hasher. An absent option keeps the default:
// 12 rounds, verify off, no length limit.
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

// Algorithm reports which algorithm this hasher writes, which is always bcrypt.
func (h *BcryptHasher) Algorithm() Algorithm {
	return Bcrypt
}

// Make hashes value. A value longer than the configured Limit is
// [ErrValueTooLong], and the length is counted in bytes. bcrypt itself refuses
// a value over 72 bytes.
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

// Check reports whether value hashes to hashedValue. An empty hashed value is
// false. With Verify set, a hash written by another algorithm is
// [ErrWrongAlgorithm].
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

// NeedsRehash is true when the hash is not bcrypt, or was written at a cost
// other than the effective one.
func (h *BcryptHasher) NeedsRehash(hashedValue string, options ...Options) bool {
	p, ok := Info(hashedValue)
	if !ok {
		return true
	}
	return p.Algorithm != Bcrypt || p.Cost != h.cost(firstOption(options))
}

// VerifyConfiguration reports whether the hash is bcrypt and its cost is no
// higher than the configured rounds.
func (h *BcryptHasher) VerifyConfiguration(value string) bool {
	return h.isUsingCorrectAlgorithm(value) && h.isUsingValidOptions(value)
}

// SetRounds sets the bcrypt cost factor and returns the hasher, so calls chain.
func (h *BcryptHasher) SetRounds(rounds int) *BcryptHasher {
	h.rounds = rounds
	return h
}

// isUsingCorrectAlgorithm reports whether hashedValue was written by bcrypt.
func (h *BcryptHasher) isUsingCorrectAlgorithm(hashedValue string) bool {
	p, ok := Info(hashedValue)
	return ok && p.Algorithm == Bcrypt
}

// isUsingValidOptions reports whether hashedValue was written at a cost no
// higher than this hasher's rounds.
func (h *BcryptHasher) isUsingValidOptions(hashedValue string) bool {
	p, ok := Info(hashedValue)
	if !ok || p.Cost == 0 {
		return false
	}
	return p.Cost <= h.rounds
}

// cost is the bcrypt cost for this call: the option when given, otherwise the
// configured rounds.
func (h *BcryptHasher) cost(o Options) int {
	if o.Rounds > 0 {
		return o.Rounds
	}
	return h.rounds
}
