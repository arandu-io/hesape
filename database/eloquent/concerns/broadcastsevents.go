package concerns

import (
	"fmt"
	"strings"
	"sync"

	"github.com/arandu-io/hesape/broadcasting"
	"github.com/arandu-io/hesape/str"
)

// BroadcastFactory is what BroadcastsEvents needs of a broadcaster: the one call
// that publishes an event.
//
// It is a field on [BroadcastsEvents] rather than something looked up.
// *broadcasting.BroadcastManager satisfies it, and it is an interface rather
// than that type so a model can be broadcast in a test without a broker
// configured.
type BroadcastFactory interface {
	// Event begins broadcasting event.
	Event(event any) *broadcasting.PendingBroadcast
}

// Transactions is what BroadcastsEvents needs of the connection's transaction
// state, and it is what makes BroadcastAfterCommit mean what it says.
//
// A model event broadcast synchronously never reaches a queue, so the wait is
// held here: with a transaction open, the send is registered as an after-commit
// callback and nothing leaves until the outermost commit. A
// *database/concerns.ManagesTransactions satisfies this.
type Transactions interface {
	// AfterCommit runs callback once the outermost transaction commits.
	AfterCommit(callback func()) error
	// TransactionLevel returns how many transactions are open.
	TransactionLevel() int
}

// The optional methods BroadcastableModelEventOccurred probes the model for.
//
// Go has no way to ask a value whether it has a method by name, so each
// question is a type assertion instead, and each answer is one of these
// interfaces. A model that embeds [BroadcastsEvents] already satisfies
// BroadcastsModelEventOn through the promoted method, and declaring its own
// method of the same name shadows it.
type (
	// BroadcastsModelEventOn is implemented by a model that names the
	// channels its events go out on.
	BroadcastsModelEventOn interface {
		BroadcastOn(event Event) []broadcasting.Channel
	}
	// BroadcastsModelEventAs is implemented by a model that names its
	// events under a name other than the default.
	BroadcastsModelEventAs interface {
		BroadcastAs(event Event) string
	}
	// BroadcastsModelEventWith is implemented by a model that gives its
	// events a payload other than their public fields.
	BroadcastsModelEventWith interface {
		BroadcastWith(event Event) map[string]any
	}
)

// broadcastingMuted is the process-wide switch broadcasting is checked
// against, kept as a depth rather than a bool so that nesting
// WithoutBroadcasting works. It is the same shape the muted counter of
// HasEvents has, for the same reason.
var broadcastingMuted = struct {
	sync.RWMutex
	depth int
}{}

// BroadcastsEvents turns a model's created, updated, deleted, trashed and
// restored events into broadcasts.
//
// It is a struct a model embeds. It cannot reach the model it was embedded in,
// so the three values it needs -- the model's name, its key and whether it soft
// deletes -- are fields the model states.
//
// # The tenant
//
// A channel name is a key with a subscriber on the other end, and every key is
// prefixed by the tenant of the auth.Grant. That prefix is not applied here:
// [broadcasting.TenantChannel] applies it, once, on the way into a driver and
// again on the way out of the authentication endpoint, so the name published and
// the name authorized cannot drift apart. What
// [BroadcastsEvents.BroadcastChannel] returns is the tenant-free half --
// "App.Models.Order.17" -- and what a subscriber in tenant "acme" actually
// reaches is "acme:private-App.Models.Order.17", which no subscriber in another
// tenant can name.
type BroadcastsEvents struct {
	// Broadcast is where a model event is sent to be published. A model
	// with none broadcasts nothing.
	Broadcast BroadcastFactory

	// Transactions is the connection whose commits AfterCommit waits for. A
	// model with none sends immediately, the same as when no transaction is
	// open.
	Transactions Transactions

	// Connection is the queue connection the broadcast job is pushed on.
	// Empty means the default one.
	Connection string

	// Queue is the queue the broadcast job is pushed on. Empty means the
	// default queue.
	Queue string

	// AfterCommit says whether the broadcast waits for the surrounding
	// transaction to commit. See BroadcastAfterCommit.
	AfterCommit bool

	// Class is the model's name as it goes into the channel name, such as
	// "App.Models.Order".
	//
	// reflect gives a bare type name like "models.Order", not the
	// dot-separated form an application may want on the wire, so the model
	// states the name rather than having one derived for it. A model that
	// states none has no channel, and BroadcastChannel returns the empty
	// string.
	Class string

	// Key is the model's primary key, as it goes into the channel name.
	Key any

	// SoftDeletes says whether the model soft deletes. BootBroadcastsEvents
	// registers the Trashed and Restored listeners only when this is true,
	// and NewBroadcastableEvent carries it onto the event.
	SoftDeletes bool
}

// BootBroadcastsEvents registers the listeners that broadcast each model event.
//
// The model calls it where a reader can see it, next to
// InitializeGuardsAttributes and the other initializers. It registers the same
// five events in the same order, and the two soft-delete ones only when the
// model soft deletes.
//
// The registry is the argument because this struct cannot reach the model it was
// embedded in; it is the model's own HasEvents.
func (b *BroadcastsEvents) BootBroadcastsEvents(events *HasEvents) {
	events.Created(func(model any) bool {
		b.send(b.BroadcastCreated(model))
		return true
	})

	events.Updated(func(model any) bool {
		b.send(b.BroadcastUpdated(model))
		return true
	})

	if b.SoftDeletes {
		events.Listen(Trashed, func(model any) bool {
			b.send(b.BroadcastTrashed(model))
			return true
		})

		events.Listen(Restored, func(model any) bool {
			b.send(b.BroadcastRestored(model))
			return true
		})
	}

	events.Deleted(func(model any) bool {
		b.send(b.BroadcastDeleted(model))
		return true
	})
}

// send hands a pending broadcast to the dispatcher, or holds it until the
// transaction commits.
//
// Go has no destructor to send on when a temporary value goes out of
// scope, so the send is explicit -- and it is the one place AfterCommit can
// be honoured for a broadcast that never sees a queue.
//
// With no transactions manager to register against, the send happens now:
// nothing is tracking commits, so there is no commit to wait for, and
// holding the event forever would lose it.
func (b *BroadcastsEvents) send(pending *broadcasting.PendingBroadcast) {
	if pending == nil {
		return
	}

	if b.BroadcastAfterCommit() && b.Transactions != nil && b.Transactions.TransactionLevel() > 0 {
		if err := b.Transactions.AfterCommit(func() { pending.Send() }); err == nil {
			return
		}
	}

	pending.Send()
}

// BroadcastCreated returns the pending broadcast for model's Created event,
// or nil when there is no channel to send it on.
//
// The model is an argument because BroadcastsEvents, embedded by
// composition, cannot reach the model that embeds it. channels is variadic
// because it is optional: given, it names the channels directly; omitted,
// BroadcastOn decides them.
func (b *BroadcastsEvents) BroadcastCreated(model any, channels ...broadcasting.Channel) *broadcasting.PendingBroadcast {
	return b.BroadcastIfBroadcastChannelsExistForEvent(
		b.NewBroadcastableModelEvent(model, Created), Created, channels,
	)
}

// BroadcastUpdated returns the pending broadcast for model's Updated event,
// or nil when there is no channel to send it on.
func (b *BroadcastsEvents) BroadcastUpdated(model any, channels ...broadcasting.Channel) *broadcasting.PendingBroadcast {
	return b.BroadcastIfBroadcastChannelsExistForEvent(
		b.NewBroadcastableModelEvent(model, Updated), Updated, channels,
	)
}

// BroadcastTrashed returns the pending broadcast for model's Trashed event,
// or nil when there is no channel to send it on.
func (b *BroadcastsEvents) BroadcastTrashed(model any, channels ...broadcasting.Channel) *broadcasting.PendingBroadcast {
	return b.BroadcastIfBroadcastChannelsExistForEvent(
		b.NewBroadcastableModelEvent(model, Trashed), Trashed, channels,
	)
}

// BroadcastRestored returns the pending broadcast for model's Restored
// event, or nil when there is no channel to send it on.
func (b *BroadcastsEvents) BroadcastRestored(model any, channels ...broadcasting.Channel) *broadcasting.PendingBroadcast {
	return b.BroadcastIfBroadcastChannelsExistForEvent(
		b.NewBroadcastableModelEvent(model, Restored), Restored, channels,
	)
}

// BroadcastDeleted returns the pending broadcast for model's Deleted event,
// or nil when there is no channel to send it on.
func (b *BroadcastsEvents) BroadcastDeleted(model any, channels ...broadcasting.Channel) *broadcasting.PendingBroadcast {
	return b.BroadcastIfBroadcastChannelsExistForEvent(
		b.NewBroadcastableModelEvent(model, Deleted), Deleted, channels,
	)
}

// BroadcastIfBroadcastChannelsExistForEvent sends the event when there is a
// channel to send it on, and does nothing otherwise.
//
// The channels are given to the instance first and the question is then
// asked of the instance, which returns the given channels when there are
// any and the model's otherwise -- one question rather than two.
//
// Inside WithoutBroadcasting -- see [IsBroadcasting] -- nothing is built and
// nothing is sent.
func (b *BroadcastsEvents) BroadcastIfBroadcastChannelsExistForEvent(instance *BroadcastableModelEventOccurred, event Event, channels []broadcasting.Channel) *broadcasting.PendingBroadcast {
	if !IsBroadcasting() || instance == nil || b.Broadcast == nil {
		return nil
	}

	if len(instance.OnChannels(channels).BroadcastOn()) == 0 {
		return nil
	}

	return b.Broadcast.Event(instance)
}

// NewBroadcastableModelEvent builds the event for one model event.
//
// Each of the three settings is a field, and the method that returns it
// returns the field, so there is one value and no fork between them.
func (b *BroadcastsEvents) NewBroadcastableModelEvent(model any, event Event) *BroadcastableModelEventOccurred {
	instance := b.NewBroadcastableEvent(model, event)

	instance.Connection = b.BroadcastConnection()
	instance.Queue = b.BroadcastQueue()
	instance.AfterCommit = b.BroadcastAfterCommit()

	return instance
}

// NewBroadcastableEvent builds the event for model and event, without the
// connection, queue and after-commit settings NewBroadcastableModelEvent
// adds.
//
// SoftDeletes travels with the event because
// BroadcastableModelEventOccurred needs to know whether the model soft
// deletes to decide when to broadcast, and Go cannot ask a value whether it
// has a method by name.
func (b *BroadcastsEvents) NewBroadcastableEvent(model any, event Event) *BroadcastableModelEventOccurred {
	instance := NewBroadcastableModelEventOccurred(model, event)
	instance.SoftDeletes = b.SoftDeletes

	return instance
}

// BroadcastOn returns the channels a model's events broadcast on: nil by
// default.
//
// BroadcastsEvents, embedded by composition, cannot reach the model that
// embeds it, so this returns nothing and the event itself supplies a
// private channel for the model when nothing else named one -- the default
// is unchanged, it is just decided one level up, where the model is in
// hand.
//
// A model that broadcasts somewhere else declares its own method of this
// name, which shadows this one.
func (b *BroadcastsEvents) BroadcastOn(event Event) []broadcasting.Channel { return nil }

// BroadcastConnection returns the queue connection model events are
// broadcast on. Empty means the default one.
func (b *BroadcastsEvents) BroadcastConnection() string { return b.Connection }

// BroadcastQueue returns the queue model events are broadcast on. Empty
// means the default one.
func (b *BroadcastsEvents) BroadcastQueue() string { return b.Queue }

// BroadcastAfterCommit reports whether the broadcast waits for the
// surrounding database transaction to commit.
//
// It is false by default. A model that sets it true does not publish a row
// that a rollback is about to undo -- see [Transactions].
func (b *BroadcastsEvents) BroadcastAfterCommit() bool { return b.AfterCommit }

// BroadcastChannel returns the channel name associated with this model,
// "App.Models.Order.17".
//
// It, together with [BroadcastsEvents.BroadcastChannelRoute], makes a model a
// broadcasting.HasBroadcastChannel, which is what
// broadcasting.NewPrivateChannelFor takes.
//
// The tenant is not in this name and must not be: [broadcasting.TenantChannel]
// puts it there, once, on the way into a driver and on the way out of the
// authentication endpoint. Two customers holding an order 17 get
// "acme:private-App.Models.Order.17" and
// "globex:private-App.Models.Order.17", and neither can name the other's --
// the tenant comes from the auth.Grant and never from the channel the
// client asked for.
//
// A model that stated no Class has no channel, and this returns the empty
// string rather than ".17".
func (b *BroadcastsEvents) BroadcastChannel() string {
	if b.Class == "" {
		return ""
	}

	return b.Class + "." + fmt.Sprint(b.Key)
}

// BroadcastChannelRoute returns the pattern the model's channel is
// registered and authorized under, "App.Models.Order.{order}".
//
// The pattern carries no tenant for the reason the name does not: the
// broadcasters register and match tenant-free patterns, and
// [broadcasting.TenantChannel] is the single place the tenant is added.
// Authorizing the pattern is still an authorization -- the handler registered
// under it runs a Policy and produces an auth.Grant, and it is that Grant's
// tenant the client is confined to.
func (b *BroadcastsEvents) BroadcastChannelRoute() string {
	if b.Class == "" {
		return ""
	}

	return b.Class + ".{" + str.Camel(lastDotSegment(b.Class)) + "}"
}

// WithoutBroadcasting runs the callback with model event broadcasting
// turned off for every model.
//
// It is a package variable behind a mutex, the same shape muted in
// hasevents.go uses: broadcasting is a process-wide switch, with no
// per-model copy to save. It nests, and the previous state comes back even
// when the callback fails.
func WithoutBroadcasting(callback func() error) error {
	broadcastingMuted.Lock()
	broadcastingMuted.depth++
	broadcastingMuted.Unlock()

	defer func() {
		broadcastingMuted.Lock()
		broadcastingMuted.depth--
		broadcastingMuted.Unlock()
	}()

	return callback()
}

// IsBroadcasting reports false inside [WithoutBroadcasting] and true
// otherwise.
//
// It exists as its own function, rather than a private read inside
// BroadcastIfBroadcastChannelsExistForEvent, so a test can ask the same
// question.
func IsBroadcasting() bool {
	broadcastingMuted.RLock()
	defer broadcastingMuted.RUnlock()

	return broadcastingMuted.depth == 0
}

// lastDotSegment returns the last dot-separated segment of class, which is
// what the Class field holds.
func lastDotSegment(class string) string {
	if i := strings.LastIndex(class, "."); i >= 0 {
		return class[i+1:]
	}

	return class
}
