package passwords

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// PasswordBrokerManager is the brokers an application has, by name, and which
// one is meant when nobody says.
//
// More than one is unusual and real: an application with a customer table and a
// staff table resets them through different providers, different tables of
// tokens and different mail.
//
// It holds brokers rather than building them: the wiring builds each one and
// hands it over already made, so there is nothing here to resolve from.
type PasswordBrokerManager struct {
	// brokers is what the wiring passed in, by name.
	brokers map[string]*PasswordBroker

	// defaultDriver is the broker an empty name means.
	defaultDriver string
}

// NewPasswordBrokerManager returns a manager over brokers the wiring already
// built.
//
// The map is copied, so a caller that keeps the one it passed cannot add a
// broker to a running application by writing to it.
func NewPasswordBrokerManager(defaultDriver string, brokers map[string]*PasswordBroker) *PasswordBrokerManager {
	return &PasswordBrokerManager{brokers: maps.Clone(brokers), defaultDriver: defaultDriver}
}

// Broker is the broker of that name.
//
// An empty name means the default driver. A name nothing was registered under
// is an error carrying the names that were: a misspelled broker name is a typo
// in the wiring, and the list is what makes it one glance to see.
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

// GetDefaultDriver is the broker an empty name means.
func (m *PasswordBrokerManager) GetDefaultDriver() string { return m.defaultDriver }

// SetDefaultDriver sets it, and every later lookup by empty name goes there.
func (m *PasswordBrokerManager) SetDefaultDriver(name string) { m.defaultDriver = name }
