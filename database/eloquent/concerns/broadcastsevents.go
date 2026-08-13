package concerns

import (
	"fmt"
	"strings"
	"sync"

	"github.com/arandu-io/hesape/broadcasting"
	"github.com/arandu-io/hesape/str"
)

// BroadcastFactory is the little of Illuminate\Contracts\Broadcasting\Factory
// that BroadcastsEvents uses: the one call the global broadcast() helper makes.
//
// The PHP writes `broadcast($instance)`, which is
// `app(BroadcastingFactory::class)->event($instance)`. There is no container
// (ADR 0001) and no facade (ADR 0002), so the factory is a field on the trait.
// *broadcasting.BroadcastManager satisfies it, and it is an interface rather
// than that type so a model can be broadcast in a test without a broker
// configured.
type BroadcastFactory interface {
	// Event is Factory::event: begin broadcasting an event.
	Event(event any) *broadcasting.PendingBroadcast
}

// Transactions is the little of Illuminate\Database\Concerns\ManagesTransactions
// that BroadcastsEvents uses, and it is what makes BroadcastAfterCommit mean
// what it says.
//
// The PHP puts $afterCommit on the queued job and the queue honours it when it
// pushes. A model event broadcast synchronously never reaches a queue, so the
// wait is held here: with a transaction open, the send is registered as an
// after-commit callback and nothing leaves until the outermost commit. A
// *database/concerns.ManagesTransactions satisfies this.
type Transactions interface {
	// AfterCommit is Connection::afterCommit: run the callback once the
	// outermost transaction commits.
	AfterCommit(callback func()) error
	// TransactionLevel is Connection::transactionLevel: how many transactions
	// are open.
	TransactionLevel() int
}

// The optional methods BroadcastableModelEventOccurred probes the model for
// with method_exists.
//
// Go has no method_exists, so each question is a type assertion and each answer
// is one of these interfaces. A model that embeds [BroadcastsEvents] already
// satisfies BroadcastsModelEventOn through the promoted method, and declaring
// its own method of the same name shadows it -- which is what overriding
// broadcastOn() in the model does in the PHP.
type (
	// BroadcastsModelEventOn is broadcastOn($event): the channels the model's
	// events go out on.
	BroadcastsModelEventOn interface {
		BroadcastOn(event Event) []broadcasting.Channel
	}
	// BroadcastsModelEventAs is broadcastAs($event): the name the event goes out
	// under, in place of the default.
	BroadcastsModelEventAs interface {
		BroadcastAs(event Event) string
	}
	// BroadcastsModelEventWith is broadcastWith($event): the payload, in place of
	// the event's public properties.
	BroadcastsModelEventWith interface {
		BroadcastWith(event Event) map[string]any
	}
)

// broadcastingMuted answers Model::$isBroadcasting, kept as a depth rather than
// a bool so that nesting WithoutBroadcasting works. It is the same shape the
// muted counter of HasEvents has, for the same reason.
var broadcastingMuted = struct {
	sync.RWMutex
	depth int
}{}

// BroadcastsEvents answers Illuminate\Database\Eloquent\BroadcastsEvents: the
// trait that turns a model's created, updated, deleted, trashed and restored
// events into broadcasts.
//
// It is a struct a model embeds, which gives it the fields and the methods the
// way `use` does. What a trait cannot do here is read $this, so the three values
// the PHP reads off the model through late static binding -- its class name, its
// key and whether it soft deletes -- are fields the model states. They are the
// same three values, written down instead of discovered.
//
// # The tenant
//
// A channel name is a key with a subscriber on the other end, and RULE 14 says
// every key is prefixed by the tenant of the auth.Grant. That prefix is not
// applied here: [broadcasting.TenantChannel] applies it, once, on the way into a
// driver and again on the way out of the authentication endpoint, so the name
// published and the name authorized cannot drift apart (RULE 9). What
// [BroadcastsEvents.BroadcastChannel] answers is the tenant-free half --
// "App.Models.Order.17" -- and what a subscriber in tenant "acme" actually
// reaches is "acme:private-App.Models.Order.17", which no subscriber in another
// tenant can name.
type BroadcastsEvents struct {
	// Broadcast is what the PHP's broadcast() helper resolves out of the
	// container. A model with none broadcasts nothing.
	Broadcast BroadcastFactory

	// Transactions is the connection whose commits AfterCommit waits for. A
	// model with none sends immediately, which is what the PHP does outside a
	// transaction.
	Transactions Transactions

	// Connection is the public $broadcastConnection: the queue connection the
	// broadcast job is pushed on. Empty is the PHP's null, the default one.
	Connection string

	// Queue is the public $broadcastQueue. Empty is the default queue.
	Queue string

	// AfterCommit is the public $broadcastAfterCommit.
	AfterCommit bool

	// Class is get_class($this) with the separators already replaced, which is
	// the `str_replace('\\', '.', get_class($this))` of Model::broadcastChannel.
	//
	// A Go type name has no backslashes and reflect answers
	// "models.Order" for a type an application may want on the wire as
	// "App.Models.Order", so the model states the name rather than having one
	// derived for it. A model that states none has no channel, and
	// BroadcastChannel answers the empty string.
	Class string

	// Key is $this->getKey(): the model's primary key, as it goes into the
	// channel name.
	Key any

	// SoftDeletes is the `method_exists(static::class, 'bootSoftDeletes')` of
	// bootBroadcastsEvents and the `! method_exists($this->model,
	// 'bootSoftDeletes')` of BroadcastableModelEventOccurred::shouldBroadcastNow.
	// Go cannot ask a type whether it has a method by name, so the model says so.
	SoftDeletes bool
}

// BootBroadcastsEvents is BroadcastsEvents::bootBroadcastsEvents: it registers
// the observers that broadcast each model event.
//
// The PHP is found and called by name from Model::bootIfNotBooted, which walks
// the class's traits and looks for boot<TraitName>. Go has no such lookup and
// this framework has no magic hook, so the model calls it where a reader can see
// it, next to InitializeGuardsAttributes and the other initializers -- ADR 0044
// motive (1), a PHP language feature. The name is the PHP's, and so is the body:
// the same five events in the same order, and the two soft-delete ones only when
// the model soft deletes.
//
// The registry is the argument because a trait cannot reach the model it was
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
// The PHP sends in PendingBroadcast::__destruct, so the temporary that
// broadcastCreated returns goes out as the statement ends. There is no
// destructor here, so the send is explicit -- and it is the one place
// $broadcastAfterCommit can be honoured for a broadcast that never sees a queue.
//
// With no transactions manager to register against, the send happens now:
// nothing is tracking commits, so there is no commit to wait for, and holding
// the event forever would lose it.
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

// BroadcastCreated is BroadcastsEvents::broadcastCreated.
//
// The model is an argument because a trait cannot reach the model it was
// embedded in. The PHP's $channels parameter accepts a Channel, a
// HasBroadcastChannel, an array or null; Go has no union types, so it is
// variadic and none of them is the PHP's null.
func (b *BroadcastsEvents) BroadcastCreated(model any, channels ...broadcasting.Channel) *broadcasting.PendingBroadcast {
	return b.BroadcastIfBroadcastChannelsExistForEvent(
		b.NewBroadcastableModelEvent(model, Created), Created, channels,
	)
}

// BroadcastUpdated is BroadcastsEvents::broadcastUpdated.
func (b *BroadcastsEvents) BroadcastUpdated(model any, channels ...broadcasting.Channel) *broadcasting.PendingBroadcast {
	return b.BroadcastIfBroadcastChannelsExistForEvent(
		b.NewBroadcastableModelEvent(model, Updated), Updated, channels,
	)
}

// BroadcastTrashed is BroadcastsEvents::broadcastTrashed.
func (b *BroadcastsEvents) BroadcastTrashed(model any, channels ...broadcasting.Channel) *broadcasting.PendingBroadcast {
	return b.BroadcastIfBroadcastChannelsExistForEvent(
		b.NewBroadcastableModelEvent(model, Trashed), Trashed, channels,
	)
}

// BroadcastRestored is BroadcastsEvents::broadcastRestored.
func (b *BroadcastsEvents) BroadcastRestored(model any, channels ...broadcasting.Channel) *broadcasting.PendingBroadcast {
	return b.BroadcastIfBroadcastChannelsExistForEvent(
		b.NewBroadcastableModelEvent(model, Restored), Restored, channels,
	)
}

// BroadcastDeleted is BroadcastsEvents::broadcastDeleted.
func (b *BroadcastsEvents) BroadcastDeleted(model any, channels ...broadcasting.Channel) *broadcasting.PendingBroadcast {
	return b.BroadcastIfBroadcastChannelsExistForEvent(
		b.NewBroadcastableModelEvent(model, Deleted), Deleted, channels,
	)
}

// BroadcastIfBroadcastChannelsExistForEvent is
// BroadcastsEvents::broadcastIfBroadcastChannelsExistForEvent.
//
// The PHP asks `! empty($this->broadcastOn($event)) || ! empty($channels)`, and
// then hands the channels to the instance anyway. Here the channels are given to
// the instance first and the question is asked of the instance, which answers
// the given channels when there are any and the model's otherwise -- the same
// two clauses, asked once (RULE 9).
//
// static::$isBroadcasting is [IsBroadcasting], and the early return is the same:
// inside WithoutBroadcasting, nothing is built and nothing is sent.
func (b *BroadcastsEvents) BroadcastIfBroadcastChannelsExistForEvent(instance *BroadcastableModelEventOccurred, event Event, channels []broadcasting.Channel) *broadcasting.PendingBroadcast {
	if !IsBroadcasting() || instance == nil || b.Broadcast == nil {
		return nil
	}

	if len(instance.OnChannels(channels).BroadcastOn()) == 0 {
		return nil
	}

	return b.Broadcast.Event(instance)
}

// NewBroadcastableModelEvent is BroadcastsEvents::newBroadcastableModelEvent.
//
// The PHP reads each of the three settings as `property_exists($this, 'x') ?
// $this->x : $this->x()`. The property is the field here and the method answers
// the field, so there is one value and no fork between them (RULE 9).
func (b *BroadcastsEvents) NewBroadcastableModelEvent(model any, event Event) *BroadcastableModelEventOccurred {
	instance := b.NewBroadcastableEvent(model, event)

	instance.Connection = b.BroadcastConnection()
	instance.Queue = b.BroadcastQueue()
	instance.AfterCommit = b.BroadcastAfterCommit()

	return instance
}

// NewBroadcastableEvent is BroadcastsEvents::newBroadcastableEvent.
//
// SoftDeletes travels with the event because
// BroadcastableModelEventOccurred::shouldBroadcastNow asks the model whether it
// soft deletes, and Go cannot ask a value whether it has a method by name.
func (b *BroadcastsEvents) NewBroadcastableEvent(model any, event Event) *BroadcastableModelEventOccurred {
	instance := NewBroadcastableModelEventOccurred(model, event)
	instance.SoftDeletes = b.SoftDeletes

	return instance
}

// BroadcastOn is BroadcastsEvents::broadcastOn: the channels a model's events
// broadcast on.
//
// The PHP returns [$this], the model itself, which
// BroadcastableModelEventOccurred::broadcastOn then maps to a PrivateChannel. A
// trait struct cannot reach the model it was embedded in, so this answers
// nothing and the event supplies that same private channel when nothing else
// named one -- the default is unchanged, it is just decided one level up, where
// the model is in hand.
//
// A model that broadcasts somewhere else declares its own method of this name,
// which shadows this one exactly as overriding the trait method does in the PHP.
func (b *BroadcastsEvents) BroadcastOn(event Event) []broadcasting.Channel { return nil }

// BroadcastConnection is BroadcastsEvents::broadcastConnection: the queue
// connection model events are broadcast on. Empty is the PHP's null.
func (b *BroadcastsEvents) BroadcastConnection() string { return b.Connection }

// BroadcastQueue is BroadcastsEvents::broadcastQueue: the queue model events are
// broadcast on. Empty is the PHP's null.
func (b *BroadcastsEvents) BroadcastQueue() string { return b.Queue }

// BroadcastAfterCommit is BroadcastsEvents::broadcastAfterCommit: whether the
// broadcast waits for the surrounding database transaction to commit.
//
// It is false by default, as the PHP's is. A model that sets it true does not
// publish a row that a rollback is about to undo -- see [Transactions].
func (b *BroadcastsEvents) BroadcastAfterCommit() bool { return b.AfterCommit }

// BroadcastChannel is Model::broadcastChannel: the channel name associated with
// this model, "App.Models.Order.17".
//
// It, together with [BroadcastsEvents.BroadcastChannelRoute], makes a model a
// broadcasting.HasBroadcastChannel, which is what
// broadcasting.NewPrivateChannelFor takes.
//
// The tenant is not in this name and must not be: [broadcasting.TenantChannel]
// puts it there, once, on the way into a driver and on the way out of the
// authentication endpoint. Two customers holding an order 17 get
// "acme:private-App.Models.Order.17" and "globex:private-App.Models.Order.17",
// and neither can name the other's -- the tenant comes from the auth.Grant and
// never from the channel the client asked for (RULE 14).
//
// A model that stated no Class has no channel, and this answers the empty
// string rather than ".17".
func (b *BroadcastsEvents) BroadcastChannel() string {
	if b.Class == "" {
		return ""
	}

	return b.Class + "." + fmt.Sprint(b.Key)
}

// BroadcastChannelRoute is Model::broadcastChannelRoute: the pattern the
// model's channel is registered and authorized under,
// "App.Models.Order.{order}".
//
// The pattern carries no tenant for the reason the name does not: the
// broadcasters register and match tenant-free patterns, and
// [broadcasting.TenantChannel] is the single place the tenant is added
// (RULE 9). Authorizing the pattern is still an authorization -- the handler
// registered under it runs a Policy and produces an auth.Grant, and it is that
// Grant's tenant the client is confined to (RULE 17).
func (b *BroadcastsEvents) BroadcastChannelRoute() string {
	if b.Class == "" {
		return ""
	}

	return b.Class + ".{" + str.Camel(lastDotSegment(b.Class)) + "}"
}

// WithoutBroadcasting is Model::withoutBroadcasting: run the callback with model
// event broadcasting turned off for every model.
//
// The PHP saves and restores static::$isBroadcasting. It is a package variable
// behind a mutex here for the reason every other static in this package is:
// there is no late static binding, so there is no per-class copy to save. It
// nests, and the previous state comes back even when the callback fails.
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

// IsBroadcasting answers Model::$isBroadcasting: false inside
// [WithoutBroadcasting] and true otherwise.
//
// The PHP reads the static property directly from
// broadcastIfBroadcastChannelsExistForEvent. A package variable behind a mutex
// cannot be read that way from outside, and a test has to be able to ask.
func IsBroadcasting() bool {
	broadcastingMuted.RLock()
	defer broadcastingMuted.RUnlock()

	return broadcastingMuted.depth == 0
}

// lastDotSegment is class_basename over a class name whose separators have
// already been replaced, which is what the Class field holds.
func lastDotSegment(class string) string {
	if i := strings.LastIndex(class, "."); i >= 0 {
		return class[i+1:]
	}

	return class
}
