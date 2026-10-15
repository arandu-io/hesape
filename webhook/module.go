package webhook

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/events"
	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/pipeline"
	"github.com/arandu-io/hesape/queue"
)

// Module is what the application registers: webhooks both ways, the
// endpoints it sends to and the receivers other systems call.
type Module struct {
	cfg       Config
	guard     pipeline.Middleware[http.Handler]
	sender    *sender
	client    *client
	endpoints *EndpointService
	calls     *CallService
}

// Compile-time proof that the module honors the contracts it claims.
var (
	_ foundation.Migratable  = (*Module)(nil)
	_ foundation.Schedulable = (*Module)(nil)
)

// New returns the module, or the reason it cannot be built.
//
// db holds the endpoints, the received calls, the deliveries and their
// attempts, and the queue the attempts and the processing travel on.
//
// guard is the middleware every management route sits behind: the one that
// signs the request in and puts its subject on the context, which the
// handlers read with Context.User. Nil mounts none of the management routes,
// for an application that manages endpoints from its own screens through the
// services. The receivers sit behind no guard: another system calls them with
// no session, and the signature is what admits a call.
func New(cfg Config, db *database.DB, guard pipeline.Middleware[http.Handler]) (*Module, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, errors.New("webhook: New needs a database handle: the endpoints, the received calls and the deliveries are kept in tables")
	}
	cfg = cfg.withDefaults()
	seal, err := newSealer(cfg.Key)
	if err != nil {
		return nil, err
	}
	audit := newAuditor(db, cfg.Server.Now)
	s, err := newSender(db, cfg, seal, audit)
	if err != nil {
		return nil, err
	}
	c := newClient(db, cfg, audit)
	return &Module{
		cfg: cfg, guard: guard, sender: s, client: c,
		endpoints: newEndpointService(db, s), calls: newCallService(db, c),
	}, nil
}

// Name is the module identifier.
func (m *Module) Name() string { return "webhook" }

// EndpointService is the sending side's use cases, for an application that
// calls them from its own screens. Every method asks the policy.
func (m *Module) EndpointService() *EndpointService { return m.endpoints }

// CallService is the receiving side's record, for an application that reads
// it from its own screens. Every method asks the policy.
func (m *Module) CallService() *CallService { return m.calls }

// Publisher is the events outbox's listener that delivers each event to the
// tenant's active endpoints subscribed to it. The application adds it to its
// relay's publishers. The module's own management events, which travel on
// the same outbox, are not delivered.
func (m *Module) Publisher() events.Publisher { return publisher{s: m.sender} }

// Send records one message to one of the tenant's endpoints, whatever it
// subscribes to, and queues its first attempt in the same transaction: the
// message the application built, its id and its body sent exactly as given
// on every attempt. g carries DeliveryDispatch for the tenant: the
// application decided to send, and that decision is the Grant. A second
// message with the same EventID to the same endpoint answers the first
// delivery and sends nothing.
func (m *Module) Send(ctx context.Context, g auth.Grant, endpointID string, msg Message) (*Delivery, error) {
	return m.sender.send(ctx, g, endpointID, msg)
}

// Outgoing starts a call built in code: an address, an event, a payload and
// a named secret, dispatched under a Grant.
func (m *Module) Outgoing() *Outgoing { return &Outgoing{s: m.sender} }

// ReceivePrefix is the path the receivers answer under, for the
// application's CSRF exemption.
func (m *Module) ReceivePrefix() string { return m.cfg.ReceivePrefix }

// ReceivePaths are the path prefixes the receivers answer under, for the
// application's CSRF exemption: ReceivePrefix, and the fixed part of every
// Mount, up to its first parameter.
//
//	middleware.CSRFExcept(webhookModule.ReceivePaths()...)
func (m *Module) ReceivePaths() []string {
	out := []string{m.cfg.ReceivePrefix + "/"}
	seen := map[string]bool{out[0]: true}
	for _, rc := range m.cfg.Receivers {
		if rc.Mount == "" {
			continue
		}
		fixed := rc.Mount
		if i := strings.Index(fixed, "{"); i >= 0 {
			fixed = fixed[:strings.LastIndex(fixed[:i], "/")+1]
		}
		if !seen[fixed] {
			seen[fixed] = true
			out = append(out, fixed)
		}
	}
	return out
}

// RegisterJobHandlers gives the worker the attempts of the deliveries and
// the processing of received calls. The worker is the application's,
// started by its worker command, on Server.Queue.
func (m *Module) RegisterJobHandlers(worker *queue.Worker) error {
	if worker == nil {
		return errors.New("webhook: RegisterJobHandlers needs a worker")
	}
	worker.HandleFunc(DeliverJob, m.sender.handle)
	worker.HandleFunc(ProcessJob, m.client.handle)
	return nil
}

// Schedule declares the daily cleans: of the received calls older than
// DeleteAfterDays, and of the deliveries settled longer ago than
// Server.Retention, with their attempts. Each is one pass over every tenant
// that has rows in the table, whether or not the application's scheduler
// lists it -- the platform tenant of a receiver, a tenant a resolver named --
// each with a Grant for that tenant alone. CleanSpec "-" declares none.
func (m *Module) Schedule() []foundation.Task {
	if m.cfg.CleanSpec == "-" {
		return nil
	}
	retention, days := m.cfg.Server.Retention, m.cfg.DeleteAfterDays
	return []foundation.Task{{
		ID:        "webhook.deliveries.clean",
		Spec:      m.cfg.CleanSpec,
		Scope:     foundation.Global,
		Timeout:   10 * time.Minute,
		Singleton: true,
		Action:    DeliveryClean,
		Run: func(ctx context.Context, _ auth.Grant) error {
			return eachTenant(ctx, m.sender.db, deliveriesTable, func(tenant string) error {
				//arandu:system-grant the tenant has deliveries in this module's table; the clean removes only its settled deliveries older than Server.Retention.
				return m.sender.clean(ctx, auth.SystemGrant(DeliveryClean, tenant), m.sender.clock().Add(-retention))
			})
		},
	}, {
		ID:        "webhook.calls.clean",
		Spec:      m.cfg.CleanSpec,
		Scope:     foundation.Global,
		Timeout:   10 * time.Minute,
		Singleton: true,
		Action:    CallClean,
		Run: func(ctx context.Context, _ auth.Grant) error {
			return eachTenant(ctx, m.client.db, callsTable, func(tenant string) error {
				//arandu:system-grant the tenant has calls in this module's table; the clean removes only its calls older than DeleteAfterDays.
				_, err := m.calls.clean(ctx, auth.SystemGrant(CallClean, tenant), time.Now().UTC().AddDate(0, 0, -days))
				return err
			})
		},
	}}
}

// eachTenant runs fn once for every valid tenant with a row in one of this
// module's tables. It reads the tenant column and nothing else; every row fn
// reaches, it reaches with a Grant of that tenant.
func eachTenant(ctx context.Context, db *database.DB, table string, fn func(tenant string) error) error {
	rows, err := db.QueryContext(ctx, "SELECT DISTINCT tenant_id FROM "+table)
	if err != nil {
		return err
	}
	var tenants []string
	for rows.Next() {
		var tenant string
		if err := rows.Scan(&tenant); err != nil {
			_ = rows.Close()
			return err
		}
		if auth.ValidTenant(tenant) {
			tenants = append(tenants, tenant)
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, tenant := range tenants {
		if err := fn(tenant); err != nil {
			return err
		}
	}
	return nil
}

// Migrations declares the schema this module owns: the endpoints, the
// received calls, the deliveries and their attempts. The jobs table is the
// queue's, and the outbox the management events are stored in is the events
// package's, both migrated by the application. CreateDeliveriesTable is never
// among them.
func (m *Module) Migrations() []foundation.Migration {
	return []foundation.Migration{
		createEndpoints{}, createCalls{}, extendEndpoints{}, createDeliveries{}, createAttempts{},
		addCallReference{}, addCallRequest{},
	}
}
