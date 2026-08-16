package mail

import (
	"fmt"
	"sync"
	"time"
)

// MailerConfig is one configured mailer: which driver builds it, what that
// driver needs, and the addresses it sends with.
//
// It is a struct rather than a map so that a misspelt key does not compile,
// where a misspelt key in an untyped configuration is a transport that silently
// falls back.
type MailerConfig struct {
	// Name is the key this mailer is configured under.
	Name string
	// Transport is which driver builds it: smtp, log, array, failover and
	// whatever [MailManager.Extend] added.
	Transport string

	Host     string
	Port     string
	Username string
	Password string
	Scheme   string
	Timeout  time.Duration

	// Key is the API key of a provider transport.
	Key string
	// Endpoint overrides a provider's URL, for a regional host or a test server.
	Endpoint string
	// Channel is which log channel the log transport writes to.
	Channel string

	// Mailers are the mailers a failover or round-robin transport is built out
	// of, in the order they are tried.
	Mailers []string
	// RetryAfter is how long a failed transport is skipped for.
	RetryAfter time.Duration

	// From, ReplyTo, To and ReturnPath are this mailer's global addresses, and
	// override the manager's when set.
	From       Address
	ReplyTo    Address
	To         Address
	ReturnPath string

	// Options is whatever a custom transport creator needs and this struct does
	// not name.
	Options map[string]string
}

// ManagerConfig is everything a [MailManager] needs to build mailers: the set
// of named mailers it can hand out, which one answers when nobody names one,
// and the addresses and markdown settings they all inherit.
//
// An application fills one of these at boot and passes it to [NewMailManager].
// The addresses here are defaults, not overrides -- a [MailerConfig] that names
// its own From, ReplyTo, To or ReturnPath wins for that mailer.
type ManagerConfig struct {
	// Default is the mailer used when nobody names one.
	Default string
	// Mailers is every configured mailer, by name.
	Mailers map[string]MailerConfig

	// From, ReplyTo, To and ReturnPath are the global addresses every mailer
	// inherits.
	From       Address
	ReplyTo    Address
	To         Address
	ReturnPath string

	// Markdown is the theme and component paths of the markdown renderer.
	Markdown MarkdownConfig
}

// TransportCreator turns one mailer's configuration into the [Transport] that
// delivers for it. It is what [MailManager.Extend] registers under a driver
// name, and what [MailManager.CreateSymfonyTransport] calls when a
// configuration asks for that name.
//
// It runs once per mailer, the first time that mailer is resolved, and it may
// call back into the manager: a failover creator reads the configurations of
// the mailers it fails over to and builds each of them this way.
type TransportCreator func(cfg MailerConfig) (Transport, error)

// MailManager is every configured mailer, built on demand and kept.
//
// # Why the transports are registered rather than switched on
//
// Constructing the transports by name here would need this package to import
// github.com/arandu-io/hesape/mail/transport, which imports this one -- a cycle
// the compiler refuses. So the drivers register themselves:
// transport.Register(manager) adds the seven that package carries, and
// [MailManager.Extend] adds anybody else's under exactly the same mechanism.
// There is one way to add a transport, and the built-in ones do not get a
// private door.
type MailManager struct {
	mu sync.Mutex

	config   ManagerConfig
	views    Renderer
	events   Dispatcher
	queue    QueueFactory
	markdown *Markdown

	mailers  map[string]*Mailer
	creators map[string]TransportCreator
}

// NewMailManager builds a manager over a configuration. The transports it can
// hand out are the ones registered on it afterwards, and the dispatcher is
// optional.
func NewMailManager(config ManagerConfig, views Renderer, events Dispatcher) *MailManager {
	return &MailManager{
		config:   config,
		views:    views,
		events:   events,
		mailers:  map[string]*Mailer{},
		creators: map[string]TransportCreator{},
	}
}

// SetQueue wires the queue every mailer this manager builds pushes onto.
func (m *MailManager) SetQueue(q QueueFactory) *MailManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queue = q
	return m
}

// SetMarkdown wires the markdown renderer every mailer this manager builds
// uses.
func (m *MailManager) SetMarkdown(md *Markdown) *MailManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markdown = md
	return m
}

// Mailer is the named mailer, built the first time it is asked for and kept
// after that. An empty name means the default.
//
// The lock is held around the maps and released while a transport is being
// built. A failover transport builds the transports it fails over to, which
// means a creator can call back into this manager: holding the lock across that
// call is a deadlock, and it is the kind that only shows up in the one
// configuration nobody tested.
func (m *MailManager) Mailer(name string) (*Mailer, error) {
	if name == "" {
		name = m.GetDefaultDriver()
	}

	m.mu.Lock()
	existing, ok := m.mailers[name]
	m.mu.Unlock()
	if ok {
		return existing, nil
	}

	built, err := m.resolve(name)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.mailers[name] = built
	m.mu.Unlock()
	return built, nil
}

// Driver is [MailManager.Mailer] under the name the other managers use.
func (m *MailManager) Driver(name string) (*Mailer, error) { return m.Mailer(name) }

func (m *MailManager) resolve(name string) (*Mailer, error) {
	cfg, ok := m.ConfigFor(name)
	if !ok {
		return nil, fmt.Errorf("mail: mailer [%s] is not defined", name)
	}
	return m.Build(cfg)
}

// Build makes a mailer out of a configuration that was never registered, for
// the one send that needs it. The mailer is returned rather than kept.
func (m *MailManager) Build(cfg MailerConfig) (*Mailer, error) {
	transport, err := m.CreateSymfonyTransport(cfg)
	if err != nil {
		return nil, err
	}
	name := cfg.Name
	if name == "" {
		name = "ondemand"
	}

	m.mu.Lock()
	views, events, queue, markdown := m.views, m.events, m.queue, m.markdown
	m.mu.Unlock()

	built := New(name, views, transport, events)
	if queue != nil {
		built.SetQueue(queue)
	}
	if markdown != nil {
		built.SetMarkdown(markdown)
	}
	m.setGlobalAddresses(built, cfg)
	return built, nil
}

// CreateSymfonyTransport builds the [Transport] a configuration names, and
// fails when no driver is registered under that name.
func (m *MailManager) CreateSymfonyTransport(cfg MailerConfig) (Transport, error) {
	if cfg.Transport == "" {
		return nil, fmt.Errorf("mail: unsupported mail transport []")
	}

	m.mu.Lock()
	creator, ok := m.creators[cfg.Transport]
	m.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("mail: unsupported mail transport [%s]", cfg.Transport)
	}
	return creator(cfg)
}

// ConfigFor is the configuration of a named mailer, and is what a composite
// transport creator -- failover, round robin -- asks for to build its parts.
func (m *MailManager) ConfigFor(name string) (MailerConfig, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, ok := m.config.Mailers[name]
	cfg.Name = name
	return cfg, ok
}

func (m *MailManager) setGlobalAddresses(mailer *Mailer, cfg MailerConfig) {
	from := cfg.From
	if from.Address == "" {
		from = m.config.From
	}
	if from.Address != "" {
		mailer.AlwaysFrom(from.Address, from.Name)
	}

	replyTo := cfg.ReplyTo
	if replyTo.Address == "" {
		replyTo = m.config.ReplyTo
	}
	if replyTo.Address != "" {
		mailer.AlwaysReplyTo(replyTo.Address, replyTo.Name)
	}

	to := cfg.To
	if to.Address == "" {
		to = m.config.To
	}
	if to.Address != "" {
		mailer.AlwaysTo(to.Address, to.Name)
	}

	returnPath := firstNonEmpty(cfg.ReturnPath, m.config.ReturnPath)
	if returnPath != "" {
		mailer.AlwaysReturnPath(returnPath)
	}
}

// GetDefaultDriver is which mailer is used when nobody names one.
func (m *MailManager) GetDefaultDriver() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.defaultDriver()
}

func (m *MailManager) defaultDriver() string { return m.config.Default }

// SetDefaultDriver changes which mailer is used when nobody names one.
func (m *MailManager) SetDefaultDriver(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Default = name
}

// Purge forgets one built mailer, so the next call builds it again. An empty
// name means the default.
func (m *MailManager) Purge(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name == "" {
		name = m.defaultDriver()
	}
	delete(m.mailers, name)
}

// Extend registers a transport driver under a name a configuration can ask for.
func (m *MailManager) Extend(driver string, callback TransportCreator) *MailManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creators[driver] = callback
	return m
}

// ForgetMailers forgets every built mailer.
func (m *MailManager) ForgetMailers() *MailManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mailers = map[string]*Mailer{}
	return m
}
