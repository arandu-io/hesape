package broadcasting

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// Arrayable is Illuminate\Contracts\Support\Arrayable, and it is the one thing
// BroadcastEvent::formatProperty looks for on a property before putting it in a
// payload.
type Arrayable interface {
	// ToArray is toArray().
	ToArray() map[string]any
}

// The optional methods BroadcastEvent::handle probes for with method_exists.
// Go has no method_exists, so each question is a type assertion and each answer
// is one of these interfaces. An event implements the ones it wants; the
// fallbacks in handle() are what the PHP does when method_exists says no.
type (
	// BroadcastsAs is broadcastAs(): the name the event goes out under.
	BroadcastsAs interface {
		BroadcastAs() string
	}
	// BroadcastsOn is broadcastOn(): the channels the event goes out on. It is
	// the one method an event must have -- handle() calls it unguarded.
	BroadcastsOn interface {
		BroadcastOn() []Channel
	}
	// BroadcastsWith is broadcastWith(): the payload, in place of the event's
	// public properties.
	BroadcastsWith interface {
		BroadcastWith() map[string]any
	}
	// BroadcastsOnConnections is broadcastConnections(), which
	// [InteractsWithBroadcasting] provides.
	BroadcastsOnConnections interface {
		BroadcastConnections() []string
	}
	// HasBroadcastMiddleware is middleware(): the job middleware of the
	// underlying event.
	HasBroadcastMiddleware interface {
		Middleware() []any
	}
	// HandlesBroadcastFailure is failed(): what the event does when the job
	// carrying it fails.
	HandlesBroadcastFailure interface {
		Failed(ctx context.Context, cause error) error
	}
)

// Factory is Illuminate\Contracts\Broadcasting\Factory: the one method
// BroadcastEvent::handle needs from the manager.
//
// It is an interface rather than *BroadcastManager so a test can hand handle()
// a driver directly, which is what the PHP does by type-hinting the contract.
type Factory interface {
	// Connection is Factory::connection: the driver registered under a name, or
	// the default one when the name is empty.
	Connection(name string) (Broadcaster, error)
}

// BroadcastEvent is Illuminate\Broadcasting\BroadcastEvent: the queued job that
// carries an event to the broadcasters.
//
// The PHP implements ShouldQueue and uses Queueable, so the queue reads $tries,
// $timeout, $backoff and the rest off it. They are exported fields here for the
// same reason they are public properties there.
//
// The PHP constructor fills each one from a PHP attribute on the event
// (#[Tries], #[Timeout], ...) falling back to a property of the same name. Go
// has no attributes and no property_exists, so the constructor reads the
// optional interfaces below and an application that wants a different value
// sets the field.
type BroadcastEvent struct {
	// Event is the public $event: what is being broadcast.
	Event any
	// Tries is the public $tries.
	Tries int
	// Timeout is the public $timeout. It is a Duration rather than the PHP's
	// seconds, because a Go API that measures time in bare ints is one that gets
	// milliseconds passed to it.
	Timeout time.Duration
	// Backoff is the public $backoff.
	Backoff time.Duration
	// MaxExceptions is the public $maxExceptions.
	MaxExceptions int
	// DeleteWhenMissingModels is the public $deleteWhenMissingModels, which the
	// PHP declares true and then overwrites from the attribute.
	DeleteWhenMissingModels bool
}

// The optional interfaces NewBroadcastEvent reads in place of the PHP's
// attributes and property_exists probes.
type (
	// HasTries is #[Tries] / public $tries.
	HasTries interface {
		Tries() int
	}
	// HasTimeout is #[Timeout] / public $timeout.
	HasTimeout interface {
		Timeout() time.Duration
	}
	// HasBackoff is #[Backoff] / public $backoff.
	HasBackoff interface {
		Backoff() time.Duration
	}
	// HasMaxExceptions is #[MaxExceptions] / public $maxExceptions.
	HasMaxExceptions interface {
		MaxExceptions() int
	}
	// HasDeleteWhenMissingModels is #[DeleteWhenMissingModels] / public
	// $deleteWhenMissingModels.
	HasDeleteWhenMissingModels interface {
		DeleteWhenMissingModels() bool
	}
)

// NewBroadcastEvent is BroadcastEvent::__construct.
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

// Handle is BroadcastEvent::handle: the queued job's body.
//
// The PHP takes only the factory. Two arguments are added, and each one is a
// rule of this framework rather than a preference:
//
// ctx is the first mechanical change ADR 0044 allows for a method that does
// I/O, and publishing to a broker is I/O.
//
// g is RULE 14. Every channel this job publishes on is named
// "<tenant>:<channel>", the tenant comes from the Grant and from nothing else,
// and a job that could publish without one would publish into a channel every
// customer of the system can subscribe to. The Grant is the job's own -- in a
// worker, queue/jobs.GrantFor rebuilds exactly the Grant the push authorized.
//
// An event on no channels returns without touching a driver, which is the PHP's
// early return.
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

// getPayloadFromEvent is BroadcastEvent::getPayloadFromEvent.
//
// An event that says what it broadcasts with is taken at its word, and the
// socket id is merged in on top -- which is how toOthers reaches the broker.
// An event that says nothing has its public properties read, exactly as the PHP
// reads them through ReflectionProperty::IS_PUBLIC.
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

	// unset($payload['broadcastQueue']): the queue an event asks for is
	// configuration for the push, not something a subscriber should read.
	delete(payload, "broadcastQueue")

	return payload
}

// getConnectionPayload is BroadcastEvent::getConnectionPayload: a payload keyed
// by connection name lets one event send different data to each broker, and the
// socket id survives the narrowing.
//
// BroadcastEvent::getConnectionChannels does the same for channels and has no
// twin here: a PHP array is a list and a dictionary at once, so $channels can
// be either, and a Go []Channel can only be the list. The connection-keyed
// channel list is a PHP language feature, which is reason (1) of ADR 0056.
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

// Middleware is BroadcastEvent::middleware: the job middleware of the
// underlying event, or none when it declares none.
func (b *BroadcastEvent) Middleware() []any {
	if m, ok := b.Event.(HasBroadcastMiddleware); ok {
		return m.Middleware()
	}

	return nil
}

// Failed is BroadcastEvent::failed: it hands the failure to the event, when the
// event wants it.
//
// The PHP returns void and this returns an error, which is the second
// mechanical change of ADR 0044: an event's own failure handling can fail, and
// swallowing that would lose the only report of it.
func (b *BroadcastEvent) Failed(ctx context.Context, cause error) error {
	if f, ok := b.Event.(HandlesBroadcastFailure); ok {
		return f.Failed(ctx, cause)
	}

	return nil
}

// DisplayName is BroadcastEvent::displayName: get_class($this->event).
//
// reflect.Type.String is the nearest thing Go has to a class name, and it is
// what a worker log line has to carry for anyone to know which event failed.
func (b *BroadcastEvent) DisplayName() string {
	if b.Event == nil {
		return ""
	}

	return reflect.TypeOf(b.Event).String()
}

// Clone is BroadcastEvent::__clone: the PHP clones the event so a queued copy
// cannot be mutated by whoever still holds the original.
//
// PHP's clone is shallow and so is this: a pointer event is followed one level
// and the struct behind it copied, which is what `clone $this->event` does.
func (b *BroadcastEvent) Clone() *BroadcastEvent {
	cloned := *b
	cloned.Event = cloneEvent(b.Event)

	return &cloned
}

// UniqueBroadcastEvent is Illuminate\Broadcasting\UniqueBroadcastEvent: a
// BroadcastEvent that will not be queued twice while one is still in flight.
//
// The PHP extends BroadcastEvent and implements ShouldBeUnique; embedding is
// the extends, and [BroadcastManager.Queue] is what reads the two fields, as
// the PHP's mustBeUniqueAndCannotAcquireLock does.
type UniqueBroadcastEvent struct {
	BroadcastEvent

	// UniqueID is the public $uniqueId: the lock identifier.
	UniqueID string
	// UniqueFor is the public $uniqueFor: how long the lock is held.
	UniqueFor time.Duration
}

// The two optional methods UniqueBroadcastEvent::__construct probes for.
type (
	// HasUniqueID is uniqueId() / public $uniqueId.
	HasUniqueID interface {
		UniqueID() string
	}
	// HasUniqueFor is uniqueFor() / public $uniqueFor.
	HasUniqueFor interface {
		UniqueFor() time.Duration
	}
)

// NewUniqueBroadcastEvent is UniqueBroadcastEvent::__construct.
//
// The PHP appends the event's uniqueId onto its own null property, which is a
// concatenation with the empty string; that is the assignment below.
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

// formatProperty is BroadcastEvent::formatProperty.
func formatProperty(value any) any {
	if a, ok := value.(Arrayable); ok {
		return a.ToArray()
	}

	return value
}

// exportedFields is the ReflectionClass::getProperties(IS_PUBLIC) loop of
// getPayloadFromEvent.
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

// socketOf is data_get($event, 'socket'): the socket id an event carries when
// it embeds [InteractsWithSockets], and the empty string otherwise.
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

// cloneEvent is the `clone $this->event` of __clone: a shallow copy behind the
// same kind of reference the original was held by.
func cloneEvent(event any) any {
	value := reflect.ValueOf(event)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return event
	}

	cloned := reflect.New(value.Type().Elem())
	cloned.Elem().Set(value.Elem())

	return cloned.Interface()
}
