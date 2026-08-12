package passwords

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// PasswordBrokerManager answers
// Illuminate\Auth\Passwords\PasswordBrokerManager: the brokers an application
// has, by name, and which one is meant when nobody says.
//
// More than one is unusual and real: an application with a customer table and a
// staff table resets them through different providers, different tables of
// tokens and different mail.
//
// # What is left of the PHP class
//
// Most of it was the container. resolve() reads auth.passwords.{name} out of the
// config, createTokenRepository() picks a driver and news up the repository,
// getConfig() reads the config again, and getDefaultDriver() reads
// auth.defaults.passwords. All four are the container reaching for services by
// string key, which ADR 0001 and ADR 0002 removed: there is nothing to resolve
// from, because the brokers are built by the wiring and handed over already
// made.
//
// So this holds them instead of building them, which is reason (2) of ADR 0044.
// The three methods that are not the container are here, spelled as they are
// there.
type PasswordBrokerManager struct {
	// brokers answers $brokers, which in the PHP is the cache of what resolve()
	// built and here is what the wiring passed in.
	brokers map[string]*PasswordBroker

	// defaultDriver answers what getDefaultDriver reads out of
	// auth.defaults.passwords.
	defaultDriver string
}

// NewPasswordBrokerManager answers PasswordBrokerManager::__construct.
//
// The PHP takes the application, which is the container it resolves brokers
// from. This takes the brokers, because they are built by the wiring; see the
// type's doc.
//
// The map is copied, so a caller that keeps the one it passed cannot add a
// broker to a running application by writing to it.
func NewPasswordBrokerManager(defaultDriver string, brokers map[string]*PasswordBroker) *PasswordBrokerManager {
	return &PasswordBrokerManager{brokers: maps.Clone(brokers), defaultDriver: defaultDriver}
}

// Broker answers PasswordBrokerManager::broker.
//
// An empty name is the PHP's null argument: the default driver. A name nothing
// was registered under is an error carrying the names that were, which is the
// InvalidArgumentException the PHP throws with the message written out -- a
// misspelled broker name is a typo in the wiring, and the list is what makes it
// one glance to see.
func (m *PasswordBrokerManager) Broker(name string) (*PasswordBroker, error) {
	if name == "" {
		name = m.GetDefaultDriver()
	}
	if broker, ok := m.brokers[name]; ok {
		return broker, nil
	}
	if len(m.brokers) == 0 {
		return nil, fmt.Errorf("passwords: password resetter [%s] is not defined, and no broker is", name)
	}
	return nil, fmt.Errorf("passwords: password resetter [%s] is not defined (defined: %s)",
		name, strings.Join(slices.Sorted(maps.Keys(m.brokers)), ", "))
}

// GetDefaultDriver answers PasswordBrokerManager::getDefaultDriver.
func (m *PasswordBrokerManager) GetDefaultDriver() string { return m.defaultDriver }

// SetDefaultDriver answers PasswordBrokerManager::setDefaultDriver.
//
// The PHP writes it back into the config, which every later read goes through;
// here it is the field, which is the same thing without a config to mutate.
func (m *PasswordBrokerManager) SetDefaultDriver(name string) { m.defaultDriver = name }
