package hashing

import (
	"errors"
	"fmt"
	"sync"
)

// The driver names HashManager answers to. They are the strings after "create"
// and before "Driver" in the PHP method names, which is how Manager::createDriver
// turns a configured name into a factory call: "bcrypt" reaches
// createBcryptDriver, "argon" reaches createArgonDriver, "argon2id" reaches
// createArgon2idDriver.
const (
	// DriverBcrypt is the name HashManager::createBcryptDriver answers to, and
	// the default HashManager::getDefaultDriver falls back to.
	DriverBcrypt = "bcrypt"
	// DriverArgon is the name HashManager::createArgonDriver answers to. It is
	// argon2i, which is what PASSWORD_ARGON2I selects.
	DriverArgon = "argon"
	// DriverArgon2id is the name HashManager::createArgon2idDriver answers to.
	DriverArgon2id = "argon2id"
)

// ErrDriverNotSupported answers the InvalidArgumentException
// Manager::createDriver throws for a driver name it has no factory for. The
// message is the PHP's, with the name it was given.
var ErrDriverNotSupported = errors.New("hashing: driver not supported")

// Config is the part of Illuminate\Contracts\Config\Repository that HashManager
// reads: three keys, one method.
//
// It is an interface and not a concrete type so that this package does not
// import the configuration package to read three keys out of it. The signature
// is the one config.Repository.Get already has, so *config.Repository satisfies
// this without knowing it exists -- which is what a Go interface is for, and
// what PHP spells with a contract in a separate package.
//
// PHP reads $this->config from the container, and ADR 0002 rejected the
// container. The manager takes the repository directly instead, which is the
// same dependency written down rather than resolved.
type Config interface {
	// Get answers Illuminate\Contracts\Config\Repository::get: the value at a
	// dotted key, or the single optional default when the key is absent.
	Get(key string, def ...any) any
}

// Hasher answers Illuminate\Contracts\Hashing\Hasher, the interface ArgonHasher
// and BcryptHasher declare and HashManager both implements and returns.
//
// It is the four methods of the PHP contract. Info reports whether the value is
// a hash at all in its second result, where password_get_info returns an array
// whose 'algo' is null; that second result is what IsHashed reads.
type Hasher interface {
	// Info answers Hasher::info.
	Info(hashedValue string) (Params, bool)
	// Make answers Hasher::make.
	Make(value string, options ...Options) (string, error)
	// Check answers Hasher::check.
	Check(value, hashedValue string, options ...Options) (bool, error)
	// NeedsRehash answers Hasher::needsRehash.
	NeedsRehash(hashedValue string, options ...Options) bool
}

// configurationVerifier is the "method_exists($driver, 'verifyConfiguration')"
// test HashManager::verifyConfiguration performs, written as the interface Go
// asks a value about instead. Every hasher in this package satisfies it; the
// PHP check exists because a custom driver registered through Manager::extend
// need not.
type configurationVerifier interface {
	VerifyConfiguration(value string) bool
}

// HashManager answers Illuminate\Hashing\HashManager: it picks a hasher by name
// from configuration and forwards to it.
//
// It reads three keys, and they are the keys the PHP reads: hashing.driver for
// the name, hashing.bcrypt and hashing.argon for the options each hasher is
// built with. Both option keys may hold either the map a PHP config file writes
// -- {"rounds": 12, "verify": true} -- or an [Options] value, which is what a Go
// configuration would put there. They are the same three settings in two
// spellings, not two ways to configure hashing.
//
// It is safe for concurrent use. PHP has no lock because a process serves one
// request; a long-lived Go binary hashes on every request at once, and the
// driver cache is shared between them.
type HashManager struct {
	config Config

	mu      sync.Mutex
	drivers map[string]Hasher
	def     Hasher
}

// NewHashManager answers "new HashManager($app)", with the configuration
// repository in place of the container ADR 0002 rejected. A nil Config is the
// empty one: every key is absent, so the manager is bcrypt with the PHP class
// defaults.
//
// The default driver is resolved here rather than on the first hash, so an
// unsupported hashing.driver stops the process at boot instead of failing the
// first sign-in of the day. PHP has no such moment -- its manager resolves on
// first use, inside a request -- and this is the only place the two differ.
func NewHashManager(config Config) (*HashManager, error) {
	m := &HashManager{config: config, drivers: map[string]Hasher{}}

	def, err := m.Driver(m.GetDefaultDriver())
	if err != nil {
		return nil, err
	}
	m.def = def
	return m, nil
}

// GetDefaultDriver answers HashManager::getDefaultDriver: the value of
// hashing.driver, falling back to "bcrypt".
//
// It reads the configuration on every call, as the PHP does. Changing the key
// after [NewHashManager] has run moves what this reports and not what the
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

// Driver answers Manager::driver($driver = null): the hasher registered under
// the given name, or the default one when no name is given.
//
// At most one name may be given, which is PHP's optional argument. An unknown
// name is [ErrDriverNotSupported], which is the InvalidArgumentException the PHP
// throws. Instances are created once and reused, as Manager caches them in
// $this->drivers.
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

// CreateBcryptDriver answers HashManager::createBcryptDriver: a BcryptHasher
// built from hashing.bcrypt.
func (m *HashManager) CreateBcryptDriver() *BcryptHasher {
	return NewBcryptHasher(m.options("hashing.bcrypt"))
}

// CreateArgonDriver answers HashManager::createArgonDriver: an ArgonHasher --
// argon2i -- built from hashing.argon.
func (m *HashManager) CreateArgonDriver() *ArgonHasher {
	return NewArgonHasher(m.options("hashing.argon"))
}

// CreateArgon2idDriver answers HashManager::createArgon2idDriver: an
// Argon2IdHasher built from hashing.argon, which is the same key
// [HashManager.CreateArgonDriver] reads. The PHP shares it too: the two
// variants take the same three cost factors.
func (m *HashManager) CreateArgon2idDriver() *Argon2IdHasher {
	return NewArgon2IdHasher(m.options("hashing.argon"))
}

// Info answers HashManager::info, forwarding to the default driver.
func (m *HashManager) Info(hashedValue string) (Params, bool) {
	return m.driver().Info(hashedValue)
}

// Make answers HashManager::make, forwarding to the default driver.
func (m *HashManager) Make(value string, options ...Options) (string, error) {
	return m.driver().Make(value, options...)
}

// Check answers HashManager::check, forwarding to the default driver.
func (m *HashManager) Check(value, hashedValue string, options ...Options) (bool, error) {
	return m.driver().Check(value, hashedValue, options...)
}

// NeedsRehash answers HashManager::needsRehash, forwarding to the default
// driver.
func (m *HashManager) NeedsRehash(hashedValue string, options ...Options) bool {
	return m.driver().NeedsRehash(hashedValue, options...)
}

// IsHashed answers HashManager::isHashed, which asks the driver for the value's
// info and reports whether it named an algorithm. A plaintext password on its
// way into a password column answers false here, which is the one question
// worth asking before writing that column.
func (m *HashManager) IsHashed(value string) bool {
	_, ok := m.driver().Info(value)
	return ok
}

// VerifyConfiguration answers HashManager::verifyConfiguration: whether the
// given hash was written by the configured algorithm with cost factors no
// higher than the configured ones.
//
// A driver with no such method is true in PHP, and so it is here -- see
// configurationVerifier for why the test exists at all.
func (m *HashManager) VerifyConfiguration(value string) bool {
	verifier, ok := m.driver().(configurationVerifier)
	if !ok {
		return true
	}
	return verifier.VerifyConfiguration(value)
}

// driver is "$this->driver()" for the forwarding methods above, which in PHP
// can throw and here cannot: [NewHashManager] already resolved the default and
// refused to return a manager without one.
func (m *HashManager) driver() Hasher {
	if m.def != nil {
		return m.def
	}
	// Only reachable on a zero-value HashManager, which no constructor returns.
	// Bcrypt is what getDefaultDriver falls back to.
	return NewBcryptHasher()
}

// options reads a hasher's settings out of the configuration at the given key.
// It answers the "$this->config->get('hashing.bcrypt') ?? []" in each of the
// three create*Driver methods.
func (m *HashManager) options(key string) Options {
	if m.config == nil {
		return Options{}
	}
	return optionsFrom(m.config.Get(key))
}

// optionsFrom reads the $options array a PHP configuration file writes. An
// absent key, a null and a value of the wrong shape are all the empty array,
// which is the "?? []" the PHP ends every one of those reads with -- a hasher
// built from nothing is a hasher on its class defaults, not an error.
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

// configInt reads one cost factor. PHP has one integer type and a config file
// decoded from JSON hands Go a float64, so every whole-number shape answers
// here; anything else is the absent key, which is the hasher's own default.
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
