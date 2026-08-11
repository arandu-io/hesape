package transport

import (
	"fmt"

	"github.com/arandu-io/hesape/mail"
)

// Register wires the transports this package carries into a manager, under the
// names a configuration file asks for.
//
// It exists because Illuminate's MailManager::createSymfonyTransport is a
// switch that constructs transport classes directly, and the Go equivalent
// would be a cycle: mail/transport imports mail, so mail cannot import
// mail/transport. Registering inverts it, and has a second effect worth having
// -- a transport somebody else writes arrives through exactly the same door as
// the seven here, so there is one way to add one.
//
//	manager := mail.NewMailManager(config, views, events)
//	transport.Register(manager)
func Register(m *mail.MailManager) *mail.MailManager {
	m.Extend("smtp", func(cfg mail.MailerConfig) (mail.Transport, error) {
		port := cfg.Port
		if port == "" {
			port = "587"
		}
		return SMTP{
			Host:     cfg.Host,
			Port:     port,
			Username: cfg.Username,
			Password: cfg.Password,
			Timeout:  cfg.Timeout,
		}, nil
	})

	m.Extend("log", func(cfg mail.MailerConfig) (mail.Transport, error) {
		return Log{}, nil
	})

	m.Extend("array", func(cfg mail.MailerConfig) (mail.Transport, error) {
		return &Array{}, nil
	})

	m.Extend("resend", func(cfg mail.MailerConfig) (mail.Transport, error) {
		return Resend{Key: cfg.Key, Endpoint: cfg.Endpoint, Timeout: cfg.Timeout}, nil
	})

	m.Extend("sendgrid", func(cfg mail.MailerConfig) (mail.Transport, error) {
		return SendGrid{Key: cfg.Key, Endpoint: cfg.Endpoint, Timeout: cfg.Timeout}, nil
	})

	m.Extend("postmark", func(cfg mail.MailerConfig) (mail.Transport, error) {
		return Postmark{Token: cfg.Key, Endpoint: cfg.Endpoint, Timeout: cfg.Timeout}, nil
	})

	m.Extend("failover", func(cfg mail.MailerConfig) (mail.Transport, error) {
		if len(cfg.Mailers) == 0 {
			return nil, fmt.Errorf("mail: the failover transport has no mailers to fail over to")
		}
		list := make([]mail.Transport, 0, len(cfg.Mailers))
		for _, name := range cfg.Mailers {
			part, ok := m.ConfigFor(name)
			if !ok {
				return nil, fmt.Errorf("mail: mailer [%s] is not defined", name)
			}
			built, err := m.CreateSymfonyTransport(part)
			if err != nil {
				return nil, err
			}
			list = append(list, built)
		}
		return Failover{Transports: list}, nil
	})

	return m
}
