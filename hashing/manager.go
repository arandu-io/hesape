package hashing

import (
	"errors"
	"fmt"
	"sync"
)

// The driver names [HashManager] answers to. A configured name selects the
// factory that builds the hasher.
const (
	// DriverBcrypt selects [HashManager.CreateBcryptDriver], and is what
	// [HashManager.GetDefaultDriver] falls back to.
	DriverBcrypt = "bcrypt"
	// DriverArgon selects [HashManager.CreateArgonDriver], which is argon2i.
	DriverArgon = "argon"
	// DriverArgon2id selects [HashManager.CreateArgon2idDriver].
	DriverArgon2id = "argon2id"
)

// ErrDriverNotSupported is returned for a driver name this package has no
// factory for. It is wrapped with the name it was given.
var ErrDriverNotSupported = errors.New("hashing: driver not supported")

// Config is the part of a configuration repository that [HashManager] reads:
// three keys, one method.
//
// It is an interface and not a concrete type so that this package does not
// import the configuration package to read three keys out of it. The signature
// is the one config.Repository.Get already has, so *config.Repository satisfies
// this without knowing it exists.
//
// The manager takes the repository directly rather than resolving it, which is
// the same dependency written down instead of looked up.
type Config interface {
	// Get is the value at a dotted key, or the single optional default when
	// the key is absent.
	Get(key string, def ...any) any
}

// Hasher is the interface [ArgonHasher] and [BcryptHasher] declare, and that
// [HashManager] both implements and returns.
//
// Info reports whether the value is a hash at all in its second result, and
// that second result is what [HashManager.IsHashed] reads.
type Hasher interface {
	// Info reports the parameters hashedValue was written with, and whether
	// it is a hash at all.
	Info(hashedValue string) (Params, bool)
	// Make hashes value.
	Make(value string, options ...Options) (string, error)
	// Check reports whether value hashes to hashedValue.
	Check(value, hashedValue string, options ...Options) (bool, error)
	// NeedsRehash reports whether hashedValue was written with parameters
	// other than the ones in force now.
	NeedsRehash(hashedValue string, options ...Options) bool
}

// configurationVerifier is what [HashManager.VerifyConfiguration] asks a driver
// about. Every hasher in this package satisfies it; a driver registered from
// outside need not.
type configurationVerifier interface {
	VerifyConfiguration(value string) bool
}

// HashManager picks a hasher by name from configuration and forwards to it.
//
// It reads three keys: hashing.driver for the name, hashing.bcrypt and
// hashing.argon for the options each hasher is built with. Both option keys may
// hold either a map -- {"rounds": 12, "verify": true} -- or an [Options] value.
// They are the same three settings in two spellings, not two ways to configure
// hashing.
//
// It is safe for concurrent use: a long-lived binary hashes on every request at
// once, and the driver cache is shared between them.
type HashManager struct {
	config Config

	mu      sync.Mutex
	drivers map[string]Hasher
	def     Hasher
}

// NewHashManager returns a manager reading from config. A nil Config is the
// empty one: every key is absent, so the manager is bcrypt with the default
// parameters.
//
// The default driver is resolved here rather than on the first hash, so an
// unsupported hashing.driver stops the process at boot instead of failing the
// first sign-in of the day.
func NewHashManager(config Config) (*HashManager, error) {
	m := &HashManager{config: config, drivers: map[string]Hasher{}}

	def, err := m.Driver(m.GetDefaultDriver())
	if err != nil {
		return nil, err
	}
	m.def = def
	return m, nil
}

// GetDefaultDriver is the value of hashing.driver, falling back to "bcrypt".
//
// It reads the configuration on every call. Changing the key after
// [NewHashManager] has run moves what this reports and not what the
// manager hashes with: the driver behind [HashManager.Driver] with no argument
// is the one resolved at boot, because a hasher swapped underneath a running
// binary is a password column written by two algorithms nobody chose.
func (m *HashManager) GetDefaultDriver() string {
	if m.config == nil {
		return DriverBcrypt
	}
	name, ok := m.config.Get("hashing.driver", DriverBcrypt).(string)
	if !ok || name == "" {
		return DriverBcrypt
	}
	return name
}

// Driver is the hasher registered under the given name, or the default one
// when no name is given.
//
// At most one name may be given. An unknown name is [ErrDriverNotSupported].
// Instances are created once and reused.
func (m *HashManager) Driver(driver ...string) (Hasher, error) {
	name := DriverBcrypt
	switch {
	case len(driver) > 0 && driver[0] != "":
		name = driver[0]
	case m.def != nil:
		// The default resolved at boot. Reading it here rather than resolving
		// the name again is what keeps GetDefaultDriver a report and not a
		// switch.
		return m.def, nil
	default:
		name = m.GetDefaultDriver()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if hasher, ok := m.drivers[name]; ok {
		return hasher, nil
	}

	var hasher Hasher
	switch name {
	case DriverBcrypt:
		hasher = m.CreateBcryptDriver()
	case DriverArgon:
		hasher = m.CreateArgonDriver()
	case DriverArgon2id:
		hasher = m.CreateArgon2idDriver()
	default:
		return nil, fmt.Errorf("%w: [%s]", ErrDriverNotSupported, name)
	}

	m.drivers[name] = hasher
	return hasher, nil
}

// CreateBcryptDriver is a [BcryptHasher] built from hashing.bcrypt.
func (m *HashManager) CreateBcryptDriver() *BcryptHasher {
	return NewBcryptHasher(m.options("hashing.bcrypt"))
}

// CreateArgonDriver is an [ArgonHasher] -- argon2i -- built from hashing.argon.
func (m *HashManager) CreateArgonDriver() *ArgonHasher {
	return NewArgonHasher(m.options("hashing.argon"))
}

// CreateArgon2idDriver is an [Argon2IdHasher] built from hashing.argon, which
// is the same key [HashManager.CreateArgonDriver] reads: the two variants take
// the same three cost factors.
func (m *HashManager) CreateArgon2idDriver() *Argon2IdHasher {
	return NewArgon2IdHasher(m.options("hashing.argon"))
}

// Info forwards to the default driver.
func (m *HashManager) Info(hashedValue string) (Params, bool) {
	return m.driver().Info(hashedValue)
}

// Make forwards to the default driver.
func (m *HashManager) Make(value string, options ...Options) (string, error) {
	return m.driver().Make(value, options...)
}

// Check forwards to the default driver.
func (m *HashManager) Check(value, hashedValue string, options ...Options) (bool, error) {
	return m.driver().Check(value, hashedValue, options...)
}

// NeedsRehash forwards to the default driver.
func (m *HashManager) NeedsRehash(hashedValue string, options ...Options) bool {
	return m.driver().NeedsRehash(hashedValue, options...)
}

// IsHashed asks the driver for the value's info and reports whether it named an
// algorithm. A plaintext password on its way into a password column answers
// false here, which is the one question worth asking before writing that
// column.
func (m *HashManager) IsHashed(value string) bool {
	_, ok := m.driver().Info(value)
	return ok
}

// VerifyConfiguration reports whether the given hash was written by the
// configured algorithm with cost factors no higher than the configured ones.
//
// A driver that does not implement the check is true.
func (m *HashManager) VerifyConfiguration(value string) bool {
	verifier, ok := m.driver().(configurationVerifier)
	if !ok {
		return true
	}
	return verifier.VerifyConfiguration(value)
}

// driver is the default hasher the forwarding methods above use. It cannot
// fail: [NewHashManager] already resolved the default and refused to return a
// manager without one.
func (m *HashManager) driver() Hasher {
	if m.def != nil {
		return m.def
	}
	// Only reachable on a zero-value HashManager, which no constructor returns.
	// Bcrypt is what getDefaultDriver falls back to.
	return NewBcryptHasher()
}

// options reads a hasher's settings out of the configuration at the given key.
// An absent key is the empty [Options], which leaves the hasher on its own
// defaults.
func (m *HashManager) options(key string) Options {
	if m.config == nil {
		return Options{}
	}
	return optionsFrom(m.config.Get(key))
}

// optionsFrom reads a hasher's settings out of whatever the configuration held.
// An absent key, a nil and a value of the wrong shape are all the empty
// [Options]: a hasher built from nothing is a hasher on its own defaults, not
// an error.
func optionsFrom(v any) Options {
	switch t := v.(type) {
	case Options:
		return t
	case *Options:
		if t == nil {
			return Options{}
		}
		return *t
	case map[string]any:
		return Options{
			Rounds:  configInt(t["rounds"]),
			Memory:  configInt(t["memory"]),
			Time:    configInt(t["time"]),
			Threads: configInt(t["threads"]),
			Verify:  configBool(t["verify"]),
			Limit:   configInt(t["limit"]),
		}
	default:
		return Options{}
	}
}

// configInt reads one cost factor. Configuration decoded from JSON hands back a
// float64, so every whole-number shape is accepted here; anything else is read
// as the absent key, which leaves the hasher on its own default.
func configInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	case uint:
		return int(n)
	case uint8:
		return int(n)
	case uint16:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// configBool reads the 'verify' flag. It is is_bool and not a cast: "true" in a
// configuration file is a string somebody meant as a comment, and turning it on
// would make Check refuse the hashes an imported table is full of.
func configBool(v any) bool {
	b, _ := v.(bool)
	return b
}
