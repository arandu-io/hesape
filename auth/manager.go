package auth

import (
	"fmt"
	"sync"
)

// ManagerConfig is what the PHP AuthManager reads out of the container: the
// auth.php arrays, and the six services createSessionDriver and
// createTokenDriver pull from `$this->app[...]`.
//
// There is no container here (ADR 0001), so the manager is handed what it needs
// instead of resolving it. That is the one shape change in this file, and it is
// the reason AuthManager::setApplication has no counterpart.
//
// Guards and Providers are maps rather than structs because a custom driver
// registered with [AuthManager.Extend] reads keys this package has never heard
// of, exactly as a Laravel package does.
type ManagerConfig struct {
	// DefaultGuard is auth.defaults.guard.
	DefaultGuard string

	// DefaultProvider is auth.defaults.provider.
	DefaultProvider string

	// Guards is auth.guards: the guard's name to its settings. The settings a
	// built-in driver reads are "driver" ("session" or "token"), "provider",
	// "remember", "input_key", "storage_key" and "hash".
	Guards map[string]map[string]any

	// Providers is auth.providers: the provider's name to its settings, of
	// which only "driver" is read here.
	Providers map[string]map[string]any

	// Session is app['session.store'], which the session driver needs.
	Session Session

	// Cookies is app['cookie'].
	Cookies CookieJar

	// Events is app['events'].
	Events Dispatcher

	// Request is app['request'].
	//
	// The PHP re-injects it on every request with $app->refresh('request', ...),
	// because its manager outlives the request and its guards do not. Here a
	// guard is built per request, or its request is replaced with SetRequest;
	// there is nothing to refresh, and refresh() has no counterpart.
	Request Request

	// Hasher is app['hash'], which SessionGuard needs for LogoutOtherDevices.
	Hasher Hasher

	// RehashOnLogin is hashing.rehash_on_login. The PHP defaults it to true;
	// this is a bool, so say so.
	RehashOnLogin bool

	// TimeboxDuration is auth.timebox_duration, in microseconds. Zero is the
	// PHP's default of 200000.
	TimeboxDuration int

	// HashKey is app.key, which SessionGuard hashes the recaller's password
	// segment with.
	HashKey string
}

// AuthManager is Illuminate\Auth\AuthManager: the factory that turns a guard's
// name into a guard, and Illuminate\Auth\CreatesUserProviders, which is a trait
// in the PHP and is folded in here.
//
// It caches what it resolves, as the PHP does. Read that twice before sharing
// one across requests: a guard carries the user it resolved and the request it
// read cookies from, so a cached SessionGuard is per-request state. Build the
// manager per request, or call [AuthManager.ForgetGuards] between them.
//
// Two of the PHP's methods are not here. AuthManager::setApplication is the
// container (ADR 0001), and __call is PHP's dynamic dispatch, which forwards
// every unknown method to the default guard -- Auth::user() reaching
// SessionGuard::user(). Go has no such hook and would not want one: call
// [AuthManager.Guard] and then the method.
type AuthManager struct {
	// mu guards everything below it. The PHP needs no lock because a process
	// serves one request; a Go server does not have that luxury.
	mu sync.Mutex

	// defaultGuard is auth.defaults.guard, which the PHP writes back into the
	// config repository from setDefaultDriver.
	defaultGuard string

	// defaultProvider is auth.defaults.provider.
	defaultProvider string

	// guardConfig is auth.guards.
	guardConfig map[string]map[string]any

	// providerConfig is auth.providers.
	providerConfig map[string]map[string]any

	// guards is $guards: the resolved drivers.
	guards map[string]Guard

	// customCreators is $customCreators.
	customCreators map[string]func(manager *AuthManager, name string, config map[string]any) (Guard, error)

	// customProviderCreators is the trait's $customProviderCreators.
	customProviderCreators map[string]func(config map[string]any) (UserProvider, error)

	// userResolver is $userResolver.
	userResolver func(guard string) Authenticatable

	// The services the drivers are built from. See [ManagerConfig].
	session         Session
	cookies         CookieJar
	events          Dispatcher
	request         Request
	hasher          Hasher
	rehashOnLogin   bool
	timeboxDuration int
	hashKey         string
}

// NewAuthManager is AuthManager::__construct.
//
// The PHP takes the application and reads everything else out of it; this takes
// what it would have read. The user resolver it sets is the PHP's:
// `fn ($guard = null) => $this->guard($guard)->user()`.
func NewAuthManager(config ManagerConfig) *AuthManager {
	manager := &AuthManager{
		defaultGuard:           config.DefaultGuard,
		defaultProvider:        config.DefaultProvider,
		guardConfig:            config.Guards,
		providerConfig:         config.Providers,
		guards:                 map[string]Guard{},
		customCreators:         map[string]func(manager *AuthManager, name string, config map[string]any) (Guard, error){},
		customProviderCreators: map[string]func(config map[string]any) (UserProvider, error){},
		session:                config.Session,
		cookies:                config.Cookies,
		events:                 config.Events,
		request:                config.Request,
		hasher:                 config.Hasher,
		rehashOnLogin:          config.RehashOnLogin,
		timeboxDuration:        config.TimeboxDuration,
		hashKey:                config.HashKey,
	}
	manager.userResolver = manager.resolveUserThroughGuard

	return manager
}

// Guard is AuthManager::guard: the guard of that name, from the cache or newly
// resolved.
//
// An empty name is the PHP's null: the default guard. The PHP throws
// InvalidArgumentException for a guard that is not configured; this returns the
// error.
func (m *AuthManager) Guard(name string) (Guard, error) {
	if name == "" {
		name = m.GetDefaultDriver()
	}

	m.mu.Lock()
	cached, ok := m.guards[name]
	m.mu.Unlock()

	if ok {
		return cached, nil
	}

	guard, err := m.resolve(name)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Somebody else may have resolved the same name while this one was being
	// built. Theirs wins, so that everyone in the request holds one guard.
	if existing, ok := m.guards[name]; ok {
		return existing, nil
	}
	m.guards[name] = guard

	return guard, nil
}

// resolve is AuthManager::resolve.
//
// The PHP finds the driver method by name, with method_exists('create'.
// ucfirst($driver).'Driver'). Go has no such lookup, so the two built-in
// drivers are a switch: a driver that is neither is registered with
// [AuthManager.Extend].
func (m *AuthManager) resolve(name string) (Guard, error) {
	config, ok := m.guardConfigFor(name)
	if !ok {
		return nil, fmt.Errorf("auth: guard [%s] is not defined", name)
	}

	driver := configString(config, "driver")

	if creator, ok := m.customCreator(driver); ok {
		return m.callCustomCreator(creator, name, config)
	}

	switch driver {
	case "session":
		return m.CreateSessionDriver(name, config)
	case "token":
		return m.CreateTokenDriver(name, config)
	}

	return nil, fmt.Errorf("auth: driver [%s] for guard [%s] is not defined", driver, name)
}

// callCustomCreator is AuthManager::callCustomCreator. The PHP passes the
// application; this passes the manager, which is what the PHP's closure reaches
// through the $this it was bound to.
func (m *AuthManager) callCustomCreator(
	creator func(manager *AuthManager, name string, config map[string]any) (Guard, error),
	name string,
	config map[string]any,
) (Guard, error) {
	return creator(m, name, config)
}

// CreateSessionDriver is AuthManager::createSessionDriver.
func (m *AuthManager) CreateSessionDriver(name string, config map[string]any) (*SessionGuard, error) {
	provider, err := m.CreateUserProvider(configString(config, "provider"))
	if err != nil {
		return nil, err
	}

	guard := NewSessionGuard(
		name,
		provider,
		m.session,
		m.request,
		nil,
		m.rehashOnLogin,
		m.timeboxDuration,
		m.hashKey,
	)

	guard.Hasher = m.hasher

	// The cookie jar is what lets the guard write an encrypted "remember me"
	// cookie; the dispatcher is what lets anything listen to the sign-in.
	guard.SetCookieJar(m.cookies)

	guard.SetDispatcher(m.events)

	if remember, ok := configInt(config, "remember"); ok {
		guard.SetRememberDuration(remember)
	}

	return guard, nil
}

// CreateTokenDriver is AuthManager::createTokenDriver.
func (m *AuthManager) CreateTokenDriver(name string, config map[string]any) (*TokenGuard, error) {
	provider, err := m.CreateUserProvider(configString(config, "provider"))
	if err != nil {
		return nil, err
	}

	// The token guard is the basic API token guard: it takes a token field off
	// the request and matches it against the users wherever they are kept.
	//
	// The PHP ignores $name here, and so does this: the argument stays because
	// the PHP's signature has it.
	_ = name

	return NewTokenGuard(
		provider,
		m.request,
		configString(config, "input_key"),
		configString(config, "storage_key"),
		configBool(config, "hash"),
	), nil
}

// GetDefaultDriver is AuthManager::getDefaultDriver: auth.defaults.guard.
func (m *AuthManager) GetDefaultDriver() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.defaultGuard
}

// ShouldUse is AuthManager::shouldUse: make this the default guard, and point
// the user resolver back at whatever the default is.
func (m *AuthManager) ShouldUse(name string) {
	if name == "" {
		name = m.GetDefaultDriver()
	}

	m.SetDefaultDriver(name)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.userResolver = m.resolveUserThroughGuard
}

// SetDefaultDriver is AuthManager::setDefaultDriver.
func (m *AuthManager) SetDefaultDriver(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.defaultGuard = name
}

// ViaRequest is AuthManager::viaRequest: register a driver that is one
// callback, built as a [RequestGuard].
func (m *AuthManager) ViaRequest(driver string, callback func(request Request, provider UserProvider) Authenticatable) *AuthManager {
	return m.Extend(driver, func(manager *AuthManager, name string, config map[string]any) (Guard, error) {
		_ = name
		_ = config

		provider, err := manager.CreateUserProvider("")
		if err != nil {
			return nil, err
		}

		return NewRequestGuard(callback, manager.request, provider), nil
	})
}

// UserResolver is AuthManager::userResolver: the callback that answers who is
// acting, which the Gate and the request both use.
func (m *AuthManager) UserResolver() func(guard string) Authenticatable {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.userResolver
}

// ResolveUsersUsing is AuthManager::resolveUsersUsing.
func (m *AuthManager) ResolveUsersUsing(userResolver func(guard string) Authenticatable) *AuthManager {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.userResolver = userResolver

	return m
}

// Extend is AuthManager::extend: register a custom guard driver.
//
// The PHP binds the closure to the manager so that it can call $this->...;
// Go has no bound closures, so the manager is the callback's first argument.
func (m *AuthManager) Extend(driver string, callback func(manager *AuthManager, name string, config map[string]any) (Guard, error)) *AuthManager {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.customCreators[driver] = callback

	return m
}

// Provider is AuthManager::provider: register a custom user provider creator.
func (m *AuthManager) Provider(name string, callback func(config map[string]any) (UserProvider, error)) *AuthManager {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.customProviderCreators[name] = callback

	return m
}

// HasResolvedGuards is AuthManager::hasResolvedGuards.
func (m *AuthManager) HasResolvedGuards() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.guards) > 0
}

// ForgetGuards is AuthManager::forgetGuards: drop every resolved guard.
//
// It is what a long-running process calls between requests, because a guard
// remembers the user it resolved.
func (m *AuthManager) ForgetGuards() *AuthManager {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.guards = map[string]Guard{}

	return m
}

// CreateUserProvider is CreatesUserProviders::createUserProvider: the provider
// of that name, or the default one when the name is empty.
//
// The PHP's match has two arms of its own, 'database' and 'eloquent'. Neither
// is here: DatabaseUserProvider and EloquentUserProvider are hesape/auth/users,
// and the root of auth imports nothing outside the standard library (see
// doc.go). Register them with [AuthManager.Provider], which is the same
// registry the PHP's customProviderCreators is.
//
// A provider that is not configured at all is nil and no error, as the PHP's
// early return is: a guard may have no provider.
func (m *AuthManager) CreateUserProvider(provider string) (UserProvider, error) {
	config, ok := m.providerConfiguration(provider)
	if !ok {
		return nil, nil
	}

	driver := configString(config, "driver")

	if creator, ok := m.customProviderCreator(driver); ok {
		return creator(config)
	}

	return nil, fmt.Errorf("auth: user provider [%s] is not defined", driver)
}

// GetDefaultUserProvider is CreatesUserProviders::getDefaultUserProvider:
// auth.defaults.provider.
func (m *AuthManager) GetDefaultUserProvider() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.defaultProvider
}

// providerConfiguration is CreatesUserProviders::getProviderConfiguration.
func (m *AuthManager) providerConfiguration(provider string) (map[string]any, bool) {
	if provider == "" {
		provider = m.GetDefaultUserProvider()
	}
	if provider == "" {
		return nil, false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	config, ok := m.providerConfig[provider]

	return config, ok && config != nil
}

// guardConfigFor is AuthManager::getConfig.
func (m *AuthManager) guardConfigFor(name string) (map[string]any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	config, ok := m.guardConfig[name]

	return config, ok && config != nil
}

// customCreator reads the driver registry [AuthManager.Extend] writes.
func (m *AuthManager) customCreator(driver string) (func(manager *AuthManager, name string, config map[string]any) (Guard, error), bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	creator, ok := m.customCreators[driver]

	return creator, ok
}

// customProviderCreator reads the registry [AuthManager.Provider] writes.
func (m *AuthManager) customProviderCreator(driver string) (func(config map[string]any) (UserProvider, error), bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	creator, ok := m.customProviderCreators[driver]

	return creator, ok
}

// resolveUserThroughGuard is the closure the PHP constructor and shouldUse both
// install: the user of the named guard, or of the default one.
func (m *AuthManager) resolveUserThroughGuard(guard string) Authenticatable {
	resolved, err := m.Guard(guard)
	if err != nil {
		return nil
	}
	return resolved.User()
}

// configString reads a string out of a config array, and answers "" where the
// PHP's ?? null lands.
func configString(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return value
}

// configBool reads a bool out of a config array.
func configBool(config map[string]any, key string) bool {
	value, _ := config[key].(bool)
	return value
}

// configInt reads an int out of a config array, and says whether it was there.
// A config read from JSON or YAML holds numbers as float64, so both are taken.
func configInt(config map[string]any, key string) (int, bool) {
	switch value := config[key].(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	}
	return 0, false
}
