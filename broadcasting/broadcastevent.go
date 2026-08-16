package broadcasting

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// Arrayable is the one thing a payload value is checked for before it travels:
// a value that knows how to flatten itself.
type Arrayable interface {
	// ToArray flattens the value into what it travels as.
	ToArray() map[string]any
}

// The optional interfaces [BroadcastEvent.Handle] asks an event about. An event
// implements the ones it wants, and Handle has a fallback for each one it does
// not.
type (
	// BroadcastsAs is the name the event goes out under. The fallback is
	// [BroadcastEvent.DisplayName].
	BroadcastsAs interface {
		BroadcastAs() string
	}
	// BroadcastsOn is the channels the event goes out on. It is the one method
	// an event must have: Handle refuses an event without it.
	BroadcastsOn interface {
		BroadcastOn() []Channel
	}
	// BroadcastsWith is the payload, in place of the event's exported fields.
	BroadcastsWith interface {
		BroadcastWith() map[string]any
	}
	// BroadcastsOnConnections is the connections the event goes out on, which
	// [InteractsWithBroadcasting] provides.
	BroadcastsOnConnections interface {
		BroadcastConnections() []string
	}
	// HasBroadcastMiddleware is the job middleware of the underlying event.
	HasBroadcastMiddleware interface {
		Middleware() []any
	}
	// HandlesBroadcastFailure is what the event does when the job carrying it
	// fails.
	HandlesBroadcastFailure interface {
		Failed(ctx context.Context, cause error) error
	}
)

// Factory is the one method [BroadcastEvent.Handle] needs from the manager.
//
// It is an interface rather than *BroadcastManager so a test can hand Handle a
// driver directly.
type Factory interface {
	// Connection is the driver registered under a name, or the default one when
	// the name is empty.
	Connection(name string) (Broadcaster, error)
}

// BroadcastEvent is the queued job that carries an event to the broadcasters.
//
// The fields are exported because the queue reads them off the job. The
// constructor fills each one from the optional interfaces below, and an
// application that wants a different value sets the field.
type BroadcastEvent struct {
	// Event is what is being broadcast.
	Event any
	// Tries is how many times the job may be attempted.
	Tries int
	// Timeout is how long one attempt may run. It is a Duration rather than a
	// count of seconds, because an API that measures time in bare ints is one
	// that gets milliseconds passed to it.
	Timeout time.Duration
	// Backoff is how long to wait before retrying.
	Backoff time.Duration
	// MaxExceptions is how many uncaught failures the job may accumulate before
	// it is given up on.
	MaxExceptions int
	// DeleteWhenMissingModels tells the queue to drop the job when a model it
	// carries can no longer be found. It starts true.
	DeleteWhenMissingModels bool
}

// The optional interfaces [NewBroadcastEvent] fills the job's fields from. An
// event that declares none gets the zero value of each, and
// DeleteWhenMissingModels true.
type (
	// HasTries fills [BroadcastEvent.Tries].
	HasTries interface {
		Tries() int
	}
	// HasTimeout fills [BroadcastEvent.Timeout].
	HasTimeout interface {
		Timeout() time.Duration
	}
	// HasBackoff fills [BroadcastEvent.Backoff].
	HasBackoff interface {
		Backoff() time.Duration
	}
	// HasMaxExceptions fills [BroadcastEvent.MaxExceptions].
	HasMaxExceptions interface {
		MaxExceptions() int
	}
	// HasDeleteWhenMissingModels fills
	// [BroadcastEvent.DeleteWhenMissingModels].
	HasDeleteWhenMissingModels interface {
		DeleteWhenMissingModels() bool
	}
)

// NewBroadcastEvent builds the job that carries event to the broadcasters.
func NewBroadcastEvent(event any) *BroadcastEvent {
	b := &BroadcastEvent{Event: event, DeleteWhenMissingModels: true}

	if v, ok := event.(HasTries); ok {
		b.Tries = v.Tries()
	}
	if v, ok := event.(HasTimeout); ok {
		b.Timeout = v.Timeout()
	}
	if v, ok := event.(HasBackoff); ok {
		b.Backoff = v.Backoff()
	}
	if v, ok := event.(HasMaxExceptions); ok {
		b.MaxExceptions = v.MaxExceptions()
	}
	if v, ok := event.(HasDeleteWhenMissingModels); ok {
		b.DeleteWhenMissingModels = v.DeleteWhenMissingModels()
	}

	return b
}

// Handle is the queued job's body: it names the event, reads its payload and
// publishes it on every connection the event asked for.
//
// ctx is there because publishing to a broker is I/O.
//
// g is where the tenant comes from. Every channel this job publishes on is
// named "<tenant>:<channel>", the tenant comes from the Grant and from nothing
// else, and a job that could publish without one would publish into a channel
// every customer of the system can subscribe to. The Grant is the job's own --
// in a worker, queue/jobs.GrantFor rebuilds exactly the Grant the push
// authorized.
//
// An event on no channels returns without touching a driver.
func (b *BroadcastEvent) Handle(ctx context.Context, g auth.Grant, manager Factory) error {
	name := b.DisplayName()
	if as, ok := b.Event.(BroadcastsAs); ok {
		name = as.BroadcastAs()
	}

	on, ok := b.Event.(BroadcastsOn)
	if !ok {
		return NewBroadcastError("broadcasting: %T has no BroadcastOn method, so there is no channel to publish it on", b.Event)
	}

	channels := on.BroadcastOn()
	if len(channels) == 0 {
		return nil
	}

	connections := []string{""}
	if via, ok := b.Event.(BroadcastsOnConnections); ok {
		connections = via.BroadcastConnections()
	}

	payload := b.getPayloadFromEvent(b.Event)

	for _, connection := range connections {
		driver, err := manager.Connection(connection)
		if err != nil {
			return err
		}
		if err := driver.Broadcast(ctx, g, channels, name, b.getConnectionPayload(payload, connection)); err != nil {
			return err
		}
	}

	return nil
}

// getPayloadFromEvent builds the document an event is published as.
//
// An event that says what it broadcasts with is taken at its word, and the
// socket id is merged in on top -- which is how ToOthers reaches the broker. An
// event that says nothing has its exported fields read.
func (b *BroadcastEvent) getPayloadFromEvent(event any) map[string]any {
	if with, ok := event.(BroadcastsWith); ok {
		if payload := with.BroadcastWith(); payload != nil {
			merged := make(map[string]any, len(payload)+1)
			for key, value := range payload {
				merged[key] = value
			}
			merged["socket"] = socketOf(event)

			return merged
		}
	}

	payload := exportedFields(event)

	// The queue an event asks for is configuration for the push, not something
	// a subscriber should read.
	delete(payload, "broadcastQueue")

	return payload
}

// getConnectionPayload narrows the payload to one connection: a payload keyed
// by connection name lets one event send different data to each broker, and the
// socket id survives the narrowing.
//
// There is no equivalent for channels: []Channel is a list, so there is nowhere
// to key one by connection name.
func (b *BroadcastEvent) getConnectionPayload(payload map[string]any, connection string) map[string]any {
	nested, ok := payload[connection].(map[string]any)
	if !ok {
		return payload
	}

	narrowed := make(map[string]any, len(nested)+1)
	for key, value := range nested {
		narrowed[key] = value
	}
	if socket, ok := payload["socket"]; ok {
		narrowed["socket"] = socket
	}

	return narrowed
}

// Middleware is the job middleware of the underlying event, or none when it
// declares none.
func (b *BroadcastEvent) Middleware() []any {
	if m, ok := b.Event.(HasBroadcastMiddleware); ok {
		return m.Middleware()
	}

	return nil
}

// Failed hands the failure to the event, when the event wants it.
//
// It returns an error because an event's own failure handling can fail, and
// swallowing that would lose the only report of it.
func (b *BroadcastEvent) Failed(ctx context.Context, cause error) error {
	if f, ok := b.Event.(HandlesBroadcastFailure); ok {
		return f.Failed(ctx, cause)
	}

	return nil
}

// DisplayName names the event being carried, which is what a worker log line
// has to carry for anyone to know which event failed.
//
// It is reflect.Type.String of the event, and the empty string when there is
// none.
func (b *BroadcastEvent) DisplayName() string {
	if b.Event == nil {
		return ""
	}

	return reflect.TypeOf(b.Event).String()
}

// Clone copies the job and the event it carries, so a queued copy cannot be
// mutated by whoever still holds the original.
//
// The copy is shallow: a pointer event is followed one level and the struct
// behind it copied.
func (b *BroadcastEvent) Clone() *BroadcastEvent {
	cloned := *b
	cloned.Event = cloneEvent(b.Event)

	return &cloned
}

// UniqueBroadcastEvent is a [BroadcastEvent] that will not be queued twice
// while one is still in flight.
//
// [BroadcastManager.Queue] is what reads the two fields: it takes a lock under
// UniqueID before pushing, and drops the event when somebody already holds it.
type UniqueBroadcastEvent struct {
	BroadcastEvent

	// UniqueID is the lock identifier.
	UniqueID string
	// UniqueFor is how long the lock is held.
	UniqueFor time.Duration
}

// The two optional interfaces [NewUniqueBroadcastEvent] reads. An event that
// implements HasUniqueID is the one [BroadcastManager.Queue] takes a lock for.
type (
	// HasUniqueID fills [UniqueBroadcastEvent.UniqueID].
	HasUniqueID interface {
		UniqueID() string
	}
	// HasUniqueFor fills [UniqueBroadcastEvent.UniqueFor].
	HasUniqueFor interface {
		UniqueFor() time.Duration
	}
)

// NewUniqueBroadcastEvent builds the job for an event that asked to be unique.
func NewUniqueBroadcastEvent(event any) *UniqueBroadcastEvent {
	u := &UniqueBroadcastEvent{BroadcastEvent: *NewBroadcastEvent(event)}

	if v, ok := event.(HasUniqueID); ok {
		u.UniqueID += v.UniqueID()
	}
	if v, ok := event.(HasUniqueFor); ok {
		u.UniqueFor = v.UniqueFor()
	}

	return u
}

// formatProperty flattens one payload value: an [Arrayable] becomes its map,
// and everything else travels as it is.
func formatProperty(value any) any {
	if a, ok := value.(Arrayable); ok {
		return a.ToArray()
	}

	return value
}

// exportedFields reads the payload of an event out of its exported fields.
//
// A key is the field's json tag when it has one and the field name otherwise,
// because the tag is where a Go struct already says what it is called on the
// wire, and a payload with two spellings of the same field is worse than either
// spelling.
func exportedFields(event any) map[string]any {
	payload := map[string]any{}

	value := reflect.ValueOf(event)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return payload
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return payload
	}

	for _, field := range reflect.VisibleFields(value.Type()) {
		if !field.IsExported() || field.Anonymous {
			continue
		}
		name, ok := fieldName(field)
		if !ok {
			continue
		}
		payload[name] = formatProperty(value.FieldByIndex(field.Index).Interface())
	}

	return payload
}

// fieldName answers the payload key for a struct field, and false for a field
// tagged json:"-", which is a field that said it does not travel.
func fieldName(field reflect.StructField) (string, bool) {
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return field.Name, true
	}
	name, _, _ := strings.Cut(tag, ",")
	switch name {
	case "-":
		return "", false
	case "":
		return field.Name, true
	default:
		return name, true
	}
}

// socketOf is the socket id an event carries when it embeds
// [InteractsWithSockets], and the empty string otherwise.
func socketOf(event any) any {
	value := reflect.ValueOf(event)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return ""
	}

	field := value.FieldByName("Socket")
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}

	return field.String()
}

// cloneEvent is a shallow copy of the event, behind the same kind of reference
// the original was held by.
func cloneEvent(event any) any {
	value := reflect.ValueOf(event)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return event
	}

	cloned := reflect.New(value.Type().Elem())
	cloned.Elem().Set(value.Elem())

	return cloned.Interface()
}
