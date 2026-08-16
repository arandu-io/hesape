package hashing_test

import (
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/arandu-io/hesape/config"
	"github.com/arandu-io/hesape/hashing"
)

// fakeConfig is the smallest thing that satisfies hashing.Config: a map read by
// dotted key. The manager reads three keys, so this is the whole of the
// contract it uses.
type fakeConfig map[string]any

func (c fakeConfig) Get(key string, def ...any) any {
	if v, ok := c[key]; ok {
		return v
	}
	if len(def) > 0 {
		return def[0]
	}
	return nil
}

// cheapBcrypt is the hashing.bcrypt section with the work factor at the floor,
// so a test that hashes costs a millisecond instead of a second.
func cheapBcrypt() map[string]any {
	return map[string]any{"rounds": bcrypt.MinCost}
}

// TestHashManagerDefaultsToBcrypt is HashManager::getDefaultDriver falling back
// to 'bcrypt' when hashing.driver is absent, and a nil configuration being the
// empty one rather than a crash.
func TestHashManagerDefaultsToBcrypt(t *testing.T) {
	for name, cfg := range map[string]hashing.Config{
		"nil config":   nil,
		"empty config": fakeConfig{},
	} {
		t.Run(name, func(t *testing.T) {
			m, err := hashing.NewHashManager(cfg)
			if err != nil {
				t.Fatalf("NewHashManager: %v", err)
			}
			if got := m.GetDefaultDriver(); got != hashing.DriverBcrypt {
				t.Errorf("GetDefaultDriver = %q, want %q", got, hashing.DriverBcrypt)
			}
			driver, err := m.Driver()
			if err != nil {
				t.Fatalf("Driver: %v", err)
			}
			if _, ok := driver.(*hashing.BcryptHasher); !ok {
				t.Errorf("Driver returned %T, want *hashing.BcryptHasher", driver)
			}
		})
	}
}

// TestHashManagerSelectsTheConfiguredDriver walks the three driver names and
// checks that what comes back writes that algorithm -- a manager that returns
// the right type but hashes with another is the failure worth catching.
func TestHashManagerSelectsTheConfiguredDriver(t *testing.T) {
	cases := []struct {
		driver string
		want   hashing.Algorithm
	}{
		{hashing.DriverBcrypt, hashing.Bcrypt},
		{hashing.DriverArgon, hashing.Argon2i},
		{hashing.DriverArgon2id, hashing.Argon2id},
	}

	for _, c := range cases {
		t.Run(c.driver, func(t *testing.T) {
			m, err := hashing.NewHashManager(fakeConfig{
				"hashing.driver": c.driver,
				"hashing.bcrypt": cheapBcrypt(),
				"hashing.argon":  map[string]any{"memory": 64, "time": 1, "threads": 1},
			})
			if err != nil {
				t.Fatalf("NewHashManager: %v", err)
			}

			hash, err := m.Make(validPassword)
			if err != nil {
				t.Fatalf("Make: %v", err)
			}
			info, ok := m.Info(hash)
			if !ok {
				t.Fatalf("Info did not recognise the hash the manager just wrote")
			}
			if info.Algorithm != c.want {
				t.Errorf("hashed with %q, want %q", info.Algorithm, c.want)
			}
			ok, err = m.Check(validPassword, hash)
			if err != nil || !ok {
				t.Errorf("Check(%q) = %v, %v; want true, nil", c.driver, ok, err)
			}
		})
	}
}

// TestHashManagerRefusesAnUnknownDriver pins where an unknown driver name
// fails. The manager refuses to exist at all, rather than failing on the first
// hash.
func TestHashManagerRefusesAnUnknownDriver(t *testing.T) {
	_, err := hashing.NewHashManager(fakeConfig{"hashing.driver": "scrypt"})
	if !errors.Is(err, hashing.ErrDriverNotSupported) {
		t.Fatalf("NewHashManager with an unknown driver returned %v", err)
	}

	m, err := hashing.NewHashManager(fakeConfig{"hashing.bcrypt": cheapBcrypt()})
	if err != nil {
		t.Fatalf("NewHashManager: %v", err)
	}
	if _, err := m.Driver("md5"); !errors.Is(err, hashing.ErrDriverNotSupported) {
		t.Errorf("Driver(\"md5\") returned %v, want ErrDriverNotSupported", err)
	}
}

// TestHashManagerCachesDrivers pins the driver cache: asking twice gives the
// same instance, so a SetRounds on one is visible through the other.
func TestHashManagerCachesDrivers(t *testing.T) {
	m, err := hashing.NewHashManager(fakeConfig{"hashing.bcrypt": cheapBcrypt()})
	if err != nil {
		t.Fatalf("NewHashManager: %v", err)
	}

	first, err := m.Driver(hashing.DriverArgon2id)
	if err != nil {
		t.Fatalf("Driver: %v", err)
	}
	second, err := m.Driver(hashing.DriverArgon2id)
	if err != nil {
		t.Fatalf("Driver: %v", err)
	}
	if first != second {
		t.Error("Driver built a second instance for the same name")
	}

	// An empty name means the default.
	def, err := m.Driver("")
	if err != nil {
		t.Fatalf("Driver(\"\"): %v", err)
	}
	if _, ok := def.(*hashing.BcryptHasher); !ok {
		t.Errorf("Driver(\"\") returned %T, want the default driver", def)
	}
}

// TestHashManagerReadsTheOptionsSection pins how each options section is read:
// what the section holds reaches the hasher, and what it does not hold leaves
// the hasher's own default in place.
func TestHashManagerReadsTheOptionsSection(t *testing.T) {
	m, err := hashing.NewHashManager(fakeConfig{
		"hashing.bcrypt": map[string]any{"rounds": bcrypt.MinCost, "limit": 8},
	})
	if err != nil {
		t.Fatalf("NewHashManager: %v", err)
	}

	// 'limit' reached the hasher: a longer value is refused.
	if _, err := m.Make("123456789"); !errors.Is(err, hashing.ErrValueTooLong) {
		t.Errorf("Make past the configured limit returned %v, want ErrValueTooLong", err)
	}

	hash, err := m.Make("12345678")
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	info, ok := m.Info(hash)
	if !ok {
		t.Fatal("Info did not recognise the hash")
	}
	if info.Cost != bcrypt.MinCost {
		t.Errorf("cost = %d, want the configured %d", info.Cost, bcrypt.MinCost)
	}

	// A section of the wrong shape reads as absent, not as an error.
	broken, err := hashing.NewHashManager(fakeConfig{"hashing.bcrypt": "twelve"})
	if err != nil {
		t.Fatalf("NewHashManager with an unreadable section: %v", err)
	}
	if got := broken.GetDefaultDriver(); got != hashing.DriverBcrypt {
		t.Errorf("GetDefaultDriver = %q, want %q", got, hashing.DriverBcrypt)
	}
}

// TestHashManagerAcceptsAnOptionsValue is the Go spelling of the same section.
// A configuration written in Go stores an Options rather than a map, and the
// manager reads both -- one setting, two spellings, not two ways to configure.
func TestHashManagerAcceptsAnOptionsValue(t *testing.T) {
	m, err := hashing.NewHashManager(fakeConfig{
		"hashing.bcrypt": hashing.Options{Rounds: bcrypt.MinCost, Limit: 8},
	})
	if err != nil {
		t.Fatalf("NewHashManager: %v", err)
	}
	if _, err := m.Make("123456789"); !errors.Is(err, hashing.ErrValueTooLong) {
		t.Errorf("Make past the configured limit returned %v, want ErrValueTooLong", err)
	}
}

// TestHashManagerIsHashed is HashManager::isHashed, which is info()['algo'] not
// being null. A plaintext password on its way into a password column is the
// case it exists for.
func TestHashManagerIsHashed(t *testing.T) {
	m, err := hashing.NewHashManager(fakeConfig{"hashing.bcrypt": cheapBcrypt()})
	if err != nil {
		t.Fatalf("NewHashManager: %v", err)
	}

	hash, err := m.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !m.IsHashed(hash) {
		t.Error("IsHashed said a hash is not one")
	}
	for _, plain := range []string{"", validPassword, "$2y$", "$argon2id$"} {
		if m.IsHashed(plain) {
			t.Errorf("IsHashed(%q) is true", plain)
		}
	}
}

// TestHashManagerNeedsRehashAndVerifyConfiguration forwards both to the driver.
// A hash written by another driver needs a rehash and fails verification, which
// is how an imported table is walked forward.
func TestHashManagerNeedsRehashAndVerifyConfiguration(t *testing.T) {
	m, err := hashing.NewHashManager(fakeConfig{
		"hashing.driver": hashing.DriverArgon2id,
		"hashing.argon":  map[string]any{"memory": 64, "time": 1, "threads": 1},
	})
	if err != nil {
		t.Fatalf("NewHashManager: %v", err)
	}

	own, err := m.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if m.NeedsRehash(own) {
		t.Error("NeedsRehash said the manager's own hash needs one")
	}
	if !m.VerifyConfiguration(own) {
		t.Error("VerifyConfiguration refused the manager's own hash")
	}

	foreign, err := fastBcrypt().Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !m.NeedsRehash(foreign) {
		t.Error("NeedsRehash accepted a bcrypt hash under an argon2id manager")
	}
	if m.VerifyConfiguration(foreign) {
		t.Error("VerifyConfiguration accepted a bcrypt hash under an argon2id manager")
	}
	// An empty stored hash is false and not an error.
	ok, err := m.Check(validPassword, "")
	if ok || err != nil {
		t.Errorf("Check against an empty hash = %v, %v; want false, nil", ok, err)
	}
}

// TestTheConfigRepositorySatisfiesConfig is the claim the Config comment makes:
// *config.Repository fits without either package knowing about the other. If
// the signature of either side moves, this stops compiling, which is the point.
func TestTheConfigRepositorySatisfiesConfig(t *testing.T) {
	repository := config.NewRepository(map[string]any{
		"hashing": map[string]any{
			"driver": hashing.DriverArgon2id,
			"argon":  map[string]any{"memory": 64, "time": 1, "threads": 1},
		},
	})

	m, err := hashing.NewHashManager(repository)
	if err != nil {
		t.Fatalf("NewHashManager over a config.Repository: %v", err)
	}
	if got := m.GetDefaultDriver(); got != hashing.DriverArgon2id {
		t.Fatalf("GetDefaultDriver = %q, want %q", got, hashing.DriverArgon2id)
	}

	hash, err := m.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	info, ok := m.Info(hash)
	if !ok || info.Algorithm != hashing.Argon2id {
		t.Fatalf("Info = %+v, %v; want argon2id", info, ok)
	}
	if info.Memory != 64 || info.Time != 1 || info.Threads != 1 {
		t.Errorf("the hashing.argon section did not reach the hasher: %+v", info)
	}
}
