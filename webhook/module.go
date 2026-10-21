package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/events"
	"github.com/arandu-io/hesape/exception"
	"github.com/arandu-io/hesape/foundation"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/pipeline"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/validation"
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
	_ foundation.Module      = (*Module)(nil)
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
		fixed := mountExemption(rc.Mount)
		if !seen[fixed] {
			seen[fixed] = true
			out = append(out, fixed)
		}
	}
	return out
}

// CSRFExemptPaths is ReceivePaths under the name foundation.CSRFExempt
// declares, so the kernel exempts the receivers from the CSRF check without
// the application naming them.
func (m *Module) CSRFExemptPaths() []string { return m.ReceivePaths() }

var (
	_ foundation.CSRFExempt = (*Module)(nil)
	_ queue.JobHandlers     = (*Module)(nil)
)

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
		widenTimePrecision,
	}
}

// HTTPAction is one route of the Module: the method and path it answers, the
// name it is registered under, the action that answers it, and the middleware
// it sits behind.
//
// The list exists so a router that is not routing.Router -- one that carries
// a renderer and a flash, and answers a rejected form with them -- registers
// the very same actions instead of a second copy of them. Action is a
// controller action, func(*Context) error; an error it returns is answered by
// whoever registers it.
type HTTPAction struct {
	Method     string
	Path       string
	Name       string
	Action     func(*hhttp.Context) error
	Middleware []pipeline.Middleware[http.Handler]
}

// HTTPActions returns every route of the Module, in registration order.
//
// The receivers come first, one route per method, with no middleware:
// another system calls them with no session, and the signature is what
// admits a call. Then, when New was given a guard, the management routes
// under Prefix, each behind it. With no guard there are no management routes
// at all, rather than routes nobody guards.
func (m *Module) HTTPActions() []HTTPAction {
	var out []HTTPAction
	for _, configured := range m.cfg.Receivers {
		rc := m.client.receivers[configured.Name]
		action := func(ctx *hhttp.Context) error { return m.client.receive(ctx.Response, ctx.Request, rc) }
		for i, method := range rc.Methods {
			name := "webhook.receive." + rc.Name
			if i > 0 {
				name += "." + strings.ToLower(method)
			}
			out = append(out, HTTPAction{Method: method, Path: rc.route(m.cfg.ReceivePrefix), Name: name, Action: action})
		}
	}
	if m.guard == nil {
		return out
	}

	p := m.cfg.Prefix
	manage := func(method, path, name string, action func(*hhttp.Context) error) {
		out = append(out, HTTPAction{
			Method: method, Path: p + path, Name: name, Action: action,
			Middleware: []pipeline.Middleware[http.Handler]{m.guard},
		})
	}
	manage(http.MethodGet, "/endpoints", "webhook.endpoints.index", m.endpointIndex)
	manage(http.MethodPost, "/endpoints", "webhook.endpoints.store", m.endpointStore)
	manage(http.MethodGet, "/endpoints/{id}", "webhook.endpoints.show", m.endpointShow)
	manage(http.MethodPut, "/endpoints/{id}", "webhook.endpoints.update", m.endpointUpdate)
	manage(http.MethodDelete, "/endpoints/{id}", "webhook.endpoints.destroy", m.endpointDestroy)
	manage(http.MethodGet, "/endpoints/{id}/secret", "webhook.endpoints.secret", m.endpointSecret)
	manage(http.MethodPost, "/endpoints/{id}/test", "webhook.endpoints.test", m.endpointTest)
	manage(http.MethodGet, "/endpoints/{id}/deliveries", "webhook.endpoints.deliveries", m.endpointDeliveries)
	manage(http.MethodPost, "/endpoints/{id}/rotate", "webhook.endpoints.rotate", m.endpointRotate)
	manage(http.MethodPost, "/endpoints/{id}/enable", "webhook.endpoints.enable", m.endpointEnable)
	manage(http.MethodPost, "/endpoints/{id}/disable", "webhook.endpoints.disable", m.endpointDisable)
	manage(http.MethodGet, "/deliveries/{id}", "webhook.deliveries.show", m.deliveryShow)
	manage(http.MethodGet, "/deliveries/{id}/attempts", "webhook.deliveries.attempts", m.deliveryAttempts)
	manage(http.MethodPost, "/deliveries/{id}/redeliver", "webhook.deliveries.redeliver", m.deliveryRedeliver)
	manage(http.MethodGet, "/calls", "webhook.calls.index", m.callIndex)
	manage(http.MethodGet, "/calls/{id}", "webhook.calls.show", m.callShow)
	manage(http.MethodPost, "/calls/{id}/process", "webhook.calls.process", m.callProcess)
	return out
}

// Routes registers every HTTPAction on r, each answered by answerJSON.
func (m *Module) Routes(r *routing.Router) {
	for _, action := range m.HTTPActions() {
		r.Match([]string{action.Method}, action.Path, answerJSON(action.Action, r.Table()), action.Middleware...).Name(action.Name)
	}
}

// answerJSON turns an action into the handler routing.Router dispatches to,
// and answers the error the action returns.
//
// The routes it serves are an API and a callback, and neither has a form to
// go back to, so a validation.Errors is answered 422 with the messages keyed
// by field. An error that names its status -- a missing record, a policy's
// refusal, a receiver's refusal -- is answered with that status: a problem
// document to a request that wants JSON, the status and its standard sentence
// to any other, never the error's own text. An error nothing claims is one
// the action could not handle, and it goes up as a panic, to whatever answers
// a panic with 500.
func answerJSON(action func(*hhttp.Context) error, urls hhttp.URLGenerator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := hhttp.NewContext(w, r, nil, urls)
		err := action(ctx)
		if err == nil {
			return
		}
		var rejected validation.Errors
		if errors.As(err, &rejected) && rejected.Any() {
			exception.WriteValidationProblem(w, r, rejected)
			return
		}
		status, ok := exception.StatusOf(err)
		if !ok || status < 400 || status > 599 {
			panic(err)
		}
		sentence := http.StatusText(status)
		if sentence == "" {
			sentence = "the request could not be completed"
		}
		if ctx.WantsJSON() {
			exception.WriteProblem(w, r, status, sentence)
			return
		}
		hhttp.Refuse(w, r, status, sentence)
	})
}

// Handlers are thin: read the input and who is asking, ask the service,
// answer. An error is returned as it is, and whoever registered the action
// answers it -- validation.Errors with 422 or the form again, ErrNotFound
// with 404, a policy refusal with 403.

func (m *Module) endpointIndex(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	rows, err := m.endpoints.List(ctx.Ctx(), who, database.Query{Cursor: ctx.Query("cursor"), Limit: m.cfg.PageSize})
	if err != nil {
		return err
	}
	out := page{}
	for _, row := range rows {
		out.items = append(out.items, NewEndpointResource(row).ToArray())
	}
	if len(rows) == m.cfg.PageSize {
		out.cursor = rows[len(rows)-1].ID
	}
	return ctx.JSON(http.StatusOK, out)
}

func (m *Module) endpointStore(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	record, secret, err := m.endpoints.Create(ctx.Ctx(), who, endpointRequest(ctx))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusCreated, EndpointResource{e: record, secret: secret})
}

func (m *Module) endpointShow(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	record, err := m.endpoints.Find(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, NewEndpointResource(record))
}

func (m *Module) endpointUpdate(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	record, err := m.endpoints.Update(ctx.Ctx(), who, ctx.Param("id"), endpointRequest(ctx))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, NewEndpointResource(record))
}

func (m *Module) endpointDestroy(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	if err := m.endpoints.Delete(ctx.Ctx(), who, ctx.Param("id")); err != nil {
		return err
	}
	return ctx.Status(http.StatusNoContent)
}

func (m *Module) endpointSecret(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	secret, err := m.endpoints.Secret(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, secretResource(secret))
}

func (m *Module) endpointTest(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	delivery, err := m.endpoints.Test(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusAccepted, NewDeliveryResource(delivery))
}

func (m *Module) endpointRotate(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	secret, err := m.endpoints.Rotate(ctx.Ctx(), who, ctx.Param("id"), DefaultRotationOverlap)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, secretResource(secret))
}

func (m *Module) endpointEnable(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	record, err := m.endpoints.Enable(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, NewEndpointResource(record))
}

func (m *Module) endpointDisable(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	disable := m.endpoints.Disable
	if reason := strings.TrimSpace(ctx.Input("reason")); reason != "" {
		disable = func(c context.Context, s auth.Subject, id string) (*Endpoint, error) {
			return m.endpoints.DisableWithReason(c, s, id, reason)
		}
	}
	record, err := disable(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, NewEndpointResource(record))
}

func (m *Module) deliveryShow(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	delivery, err := m.endpoints.Delivery(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, NewDeliveryResource(delivery))
}

func (m *Module) deliveryAttempts(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	rows, err := m.endpoints.Attempts(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	out := page{}
	for _, row := range rows {
		out.items = append(out.items, NewDeliveryAttemptResource(row).ToArray())
	}
	return ctx.JSON(http.StatusOK, out)
}

func (m *Module) deliveryRedeliver(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	delivery, err := m.endpoints.Redeliver(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusAccepted, NewDeliveryResource(delivery))
}

func (m *Module) endpointDeliveries(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	rows, err := m.endpoints.Deliveries(ctx.Ctx(), who, ctx.Param("id"), m.cfg.PageSize)
	if err != nil {
		return err
	}
	out := page{}
	for _, row := range rows {
		out.items = append(out.items, NewDeliveryResource(row).ToArray())
	}
	return ctx.JSON(http.StatusOK, out)
}

func (m *Module) callIndex(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	rows, err := m.calls.List(ctx.Ctx(), who, CallFilter{Name: ctx.Query("name"), Status: CallStatus(ctx.Query("status"))},
		database.Query{Cursor: ctx.Query("cursor"), Limit: m.cfg.PageSize})
	if err != nil {
		return err
	}
	out := page{}
	for _, row := range rows {
		out.items = append(out.items, NewCallResource(row).ToArray())
	}
	if len(rows) == m.cfg.PageSize {
		out.cursor = rows[len(rows)-1].ID
	}
	return ctx.JSON(http.StatusOK, out)
}

func (m *Module) callShow(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	record, err := m.calls.Find(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, NewCallResource(record))
}

func (m *Module) callProcess(ctx *hhttp.Context) error {
	who, _ := ctx.User()
	if err := m.calls.Process(ctx.Ctx(), who, ctx.Param("id")); err != nil {
		return err
	}
	return ctx.Status(http.StatusAccepted)
}

// endpointRequest reads an endpoint from a form or a JSON body. events is a
// list, or one comma-separated text.
func endpointRequest(ctx *hhttp.Context) EndpointRequest {
	in := hhttp.NewRequest(ctx.Request)
	var events []string
	switch v := in.Input("events").(type) {
	case []any:
		for _, item := range v {
			events = append(events, fmt.Sprint(item))
		}
	case []string:
		events = v
	case string:
		events = strings.Split(v, ",")
	}
	yes := func(field string) bool {
		value := strings.ToLower(ctx.Input(field))
		return value == "1" || value == "true" || value == "on"
	}
	return EndpointRequest{
		Name: ctx.Input("name"), URL: ctx.Input("url"), Events: events,
		Dialect: ctx.Input("dialect"), Signer: ctx.Input("signer"), SignatureHeader: ctx.Input("signature_header"),
		WithoutAranduHeaders: yes("without_arandu_headers"), Headers: ctx.Input("headers"), Active: yes("active"),
	}
}

// secretResource answers an endpoint's secret.
type secretResource string

func (s secretResource) ToArray() map[string]any { return map[string]any{"secret": string(s)} }
func (s secretResource) With() map[string]any    { return nil }
