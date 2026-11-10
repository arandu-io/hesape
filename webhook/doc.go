// Package webhook sends webhooks to the endpoints a tenant subscribes and
// receives the webhooks other systems send, in one package wired by hand.
//
// # Wiring
//
// New builds the Module from a typed Config, the database handle its tables
// live in, and the guard its management routes sit behind:
//
//	webhooks, err := webhook.New(webhook.Config{Key: appKey, Receivers: receivers}, db, middleware.RequireAuth(sessions))
//
// The application then registers it like any module, adds Publisher to its
// events relay, and runs Migrations as a step of its deploy pipeline -- never
// when the process starts. Schedule declares the daily cleans. The module is a
// queue.JobHandlers, so queue.RegisterJobHandlers gives the worker its jobs,
// and a foundation.CSRFExempt, so foundation.CSRFExemptions exempts its
// receivers from the CSRF check: a kernel that collects both over its modules
// leaves the application nothing to write for either. RegisterJobHandlers and
// ReceivePaths remain for an application that wires them itself.
//
// HTTPActions lists every route: one per receiver method, with no middleware,
// because another system calls a receiver with no session and its signature
// is what admits the call; and, when New was given a guard, the management
// routes under Config.Prefix, each behind it. A nil guard mounts no management
// route at all, for an application that manages endpoints from its own screens
// through EndpointService and CallService. A router that carries a renderer
// and a flash registers the actions as they are; Routes registers them on
// routing.Router, answering errors as a JSON API and a callback need.
//
// # Authorization
//
// Every use case asks a Policy for an auth.Grant before it reaches a table,
// and every row is read and written in the tenant that Grant carries. People
// are given the actions Actions returns -- EndpointView, EndpointCreate,
// EndpointSecret, DeliveryRedeliver, CallProcess and the rest -- and the
// policies deny everything else, to anyone, a guest included.
// EndpointImport belongs to a data migration's own subject. CallReceive,
// CallClean, DeliveryDispatch and DeliveryClean are given to nobody: the
// receiver, the cleans, the relay, the worker and the application's own code
// issue their Grant for the tenant themselves. Nothing in a request names a
// tenant: a management route takes it from the session, a static receiver
// from its configuration, and a dynamic receiver from the record its Resolve
// found, after the signature checked against that record's secrets.
//
// # Sending
//
// A tenant stores an Endpoint: an address, the events it subscribes to, a
// dialect, extra headers and, when it needs one, a Policy of its own. Its
// signing secret and its headers are sealed at rest with a key derived from
// Config.Key, bound to the tenant, the endpoint and the purpose. A secret is
// answered in clear once, when it is made; revealing it again, rotating it --
// the previous one still signing until it retires -- and every other
// management act stores its event in the application's outbox, in the
// transaction of the act, for an activity log to read. Those events are never
// delivered to endpoints.
//
// Four paths record a Delivery: Publisher, for every event the outbox relays,
// to the tenant's active endpoints subscribed to it; Module.Send, for a
// message the application built, to one endpoint; Outgoing, for a call
// built in code to any address, signed with a secret of Server.Secrets and
// dispatched outright or under a condition; and Module.Relay, for a request
// the application received and forwards as it arrived (see Relaying a
// received request). A delivery is recorded once per event and endpoint, its
// body frozen, in the transaction of the job that makes its first attempt.
//
// Each attempt is signed when it is made, under the secrets signing at that
// moment, and goes out through a transport that refuses loopback, private,
// link-local and shared (100.64.0.0/10) addresses on every hop, resolving a
// name once and refusing it whole when any of its addresses is refused. What
// the answer means is decided by the delivery's Policy: how many attempts and
// how far apart, frozen when the delivery is recorded; which failures are
// tried again; whether Retry-After is honored; how many redirects are
// followed; the limits on the body sent, the answer read and the time taken.
// Every attempt is kept as a DeliveryAttempt. An endpoint can be turned off
// after a number of deliveries in a row gave up, and its waiting deliveries
// go when it is turned on again. A delivery can be redelivered -- the same id
// and body, a new timestamp and signature -- and an endpoint can be sent a
// test call.
//
// # Dialects
//
// DialectArandu signs with Sign: the hex HMAC-SHA256 of the timestamp, the
// delivery id and the body, as sha256=, in X-Arandu-Signature beside
// X-Arandu-Timestamp and X-Arandu-Delivery-Id. Server.HeaderPrefix renames
// the three. During a rotation the signature header is written once per
// secret still signing, the one that retires first on the first line. Verify
// checks it, accepting any of the lines.
//
// DialectHMAC signs with SignBody, the hex HMAC-SHA256 of the body, in the
// endpoint's signature header, with the Arandu headers beside it unless the
// endpoint turns them off. Its header holds one value, so during a rotation
// it is signed with the previous secret until that secret retires, and a
// receiver moves to the new secret then.
//
// DialectStandardWebhooks follows the Standard Webhooks specification:
// Webhook-Id, Webhook-Timestamp, and Webhook-Signature carrying "v1," and the
// base64 HMAC-SHA256 of id.timestamp.body under the key a whsec_ secret
// encodes -- one signature per secret still signing, so a receiver rotating
// its secret accepts either. SignStandard computes it.
//
// DialectRelay signs nothing and writes no Arandu header; it belongs to
// Module.Relay and is described there.
//
// # Relaying a received request
//
// Some senders accept one or two destinations and no more -- a plate camera
// posts to the address it was given -- and the application becomes that one
// address, then sends what it received on to the other systems the customer
// still runs. An Endpoint of DialectRelay is such a system, and Module.Relay
// records one RelayMessage to it: the method (POST or PUT), the Content-Type,
// the body bytes and a few headers of the original request, to the endpoint's
// address, with the original path and query appended when the endpoint has
// KeepPath. Relay only records the delivery and queues its first attempt, in
// the transaction of the caller when it has one; it never waits for the
// receiving server. The Grant carries DeliveryDispatch, as it does for Send,
// and decides the tenant: the endpoint is read in it, so another tenant's id
// answers ErrNotFound, and the bodies are written under it.
//
// What travels is the request and nothing of ours. The attempt writes the
// method, the Content-Type and the body, then the headers of the original
// request that are on the allowlist -- Content-Encoding, Content-Language,
// Content-MD5, Accept, Accept-Language, User-Agent, SOAPAction and the names
// of Server.RelayHeaders -- then the headers the endpoint was given, which win.
// An Authorization, a Cookie, a Host, a Forwarded or X-Forwarded-* header, a
// CF-* header, the headers the Arandu and Standard Webhooks dialects write
// and every name session.IsSecretField calls secret are never forwarded, and
// Server.RelayHeaders cannot list them. There is no signature and no
// X-Arandu header, so the secret of a relay endpoint signs nothing. An endpoint
// of this dialect subscribes to no event: Publisher, Send and the test call
// leave it alone, and Relay refuses an endpoint of any other dialect with
// ErrNotRelayEndpoint.
//
// A body is not text, so a body up to Server.RelayInlineBytes (64 KiB unless
// set) is kept in the delivery's row as base64, and a larger one is written to
// the disk the application gives as Server.Bodies -- the private disk, R2 in
// production -- under a key the module generates, in the tenant of the Grant.
// The row keeps the key, the size and the SHA-256, and every attempt and every
// redelivery opens the same object again and streams it with its length, not
// chunked. Server.Bodies is written to and read by this package alone. A
// module wired without it keeps small bodies in the row and answers
// ErrNoBodyDisk to a larger one. Policy.MaxRelayBytes, 16 MiB by default and
// at most MaxRelayBytes, is the limit of a relayed body, read from the
// message once and never past the limit; the 1 MiB of Policy.MaxRequestBytes
// stays the limit of every other delivery.
//
// The retry policy, the timeouts, the delivery and attempt history,
// redelivery, the guarded transport and the cap on the answer are those of any
// delivery. A relayed attempt follows a redirect only when it keeps its
// method (307 and 308), so the old system never takes a GET in the place of
// the request. The scheduled clean that removes a settled delivery -- delivered
// or given up -- removes its body from the disk first, and a body that cannot
// be removed keeps its row for the next run. The same clean removes the bodies
// older than a day that no delivery holds: what a relay wrote before the
// transaction of its caller rolled back. To relay one request to several
// endpoints, read it once and call Relay for each with a new reader on the
// bytes.
//
// # Receiving
//
// A Receiver is one system that calls the application, mounted at
// ReceivePrefix and its name, at a Path with parameters, or at an absolute
// Mount. A static receiver names its tenant and secret; a dynamic one has a
// Resolve that finds the record a request is addressed to, before its body is
// read. A call is checked by the receiver's SignatureValidator -- HMAC by
// default, Arandu, StandardWebhooks, HeaderSecret, or the application's own,
// each accepting the previous secrets during a rotation -- then offered to the
// Profile, stored as a Call with the headers the receiver keeps, and answered
// by the Responder. Its Processor runs on the queue, retried up to the
// receiver's Tries; a SyncProcessor runs before the answer instead, and its
// result is the answer. Every refusal answers the status Statuses says, with
// its standard sentence or, under RefusalEmpty, with the status alone.
//
// A sender repeats a delivery under the same id when it did not see the
// answer, and counts a refusal as a failure, so a repeat is answered rather
// than refused, and processed once. The call keeps its delivery id -- the one
// the Arandu or Standard Webhooks signature covered, or the key the
// receiver's DedupeKey reads from the body -- under a unique index for the
// receiver and its tenant, and the index, not a read before the write,
// decides between two copies that arrive together. A body-only HMAC signs no
// id, and without a DedupeKey every copy is processed. The id lasts as long
// as the call, which Validate requires to be at least twice the validator's
// tolerance.
//
// # Manager
//
// Manager, its Store and DatabaseStore are the way this package sent webhooks
// before the Module, and they remain for the applications whose deliveries
// are in the webhook_deliveries table. They are deprecated. Manager.Dispatch
// writes exactly what it always wrote, and its job is still webhook.deliver
// with a {"deliveryId"} payload, retried by the queue five times; its attempt
// is the Module's engine, in the Arandu dialect under a Policy built from
// ManagerOptions, so it refuses the shared address space too and reads an
// answer only as far as it needs. CreateDeliveriesTable stays while any
// application has rows in that table, and Module.Migrations never declares it.
package webhook
