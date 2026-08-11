package mail

import (
	"fmt"
	"sync"
	"time"
)

// MailerConfig is one entry of Laravel's config/mail.php "mailers" array.
//
// Illuminate reads an untyped array out of the container's config repository
// and every driver picks the keys it knows. A struct is what that array is, and
// it is typed here because a misspelt key in PHP is a transport that silently
// falls back and a misspelt field in Go does not compile.
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

// ManagerConfig is Laravel's config/mail.php.
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

// TransportCreator builds a transport out of its configuration, and is
// Illuminate's create*Transport method turned into a value.
type TransportCreator func(cfg MailerConfig) (Transport, error)

// MailManager is Illuminate\Mail\MailManager: every configured mailer, built on
// demand and kept.
//
// # Why the transports are registered rather than switched on
//
// Illuminate's createSymfonyTransport is a switch over method names, and each
// arm constructs a transport class from the same namespace. Go would need this
// package to import github.com/arandu-io/hesape/mail/transport, which imports
// this one -- a cycle the compiler refuses. So the drivers register themselves:
// transport.Register(manager) adds the seven this collection carries, and
// Extend adds anybody else's under exactly the same mechanism. There is one way
// to add a transport, and the built-in ones do not get a private door.
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

// NewMailManager is Illuminate's MailManager::__construct. It takes the
// configuration and the view factory where PHP takes the application, because
// ADR 0001 has no container to ask.
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

// Driver is Mailer under the name the rest of Illuminate's managers use.
func (m *MailManager) Driver(name string) (*Mailer, error) { return m.Mailer(name) }

func (m *MailManager) resolve(name string) (*Mailer, error) {
	cfg, ok := m.ConfigFor(name)
	if !ok {
		return nil, fmt.Errorf("mail: mailer [%s] is not defined", name)
	}
	return m.Build(cfg)
}

// Build makes a mailer out of a configuration, which is Illuminate's on-demand
// mailer when the configuration was never registered.
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

// CreateSymfonyTransport builds the transport a configuration names.
//
// Symfony in the name is Laravel's mailer library; what comes back is a
// [Transport].
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
