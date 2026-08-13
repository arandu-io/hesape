package concerns

import (
	"reflect"

	"github.com/arandu-io/hesape/broadcasting"
	"github.com/arandu-io/hesape/str"
)

// BroadcastableModelEventOccurred is
// Illuminate\Database\Eloquent\BroadcastableModelEventOccurred: the event
// [BroadcastsEvents] hands the broadcaster.
//
// It lives beside the trait rather than in database/eloquent because it is the
// trait's event and nothing else builds one -- the PHP keeps them apart only
// because a trait file holds a trait.
//
// The PHP implements ShouldBroadcast, which is a marker interface with no
// methods; in Go an interface with no methods is satisfied by everything, so
// there is nothing to declare. What the broadcasters actually ask for --
// BroadcastOn, BroadcastAs, BroadcastWith and ShouldBroadcastNow -- is below,
// and each one satisfies the interface of the same name in
// github.com/arandu-io/hesape/broadcasting.
//
// SerializesModels has no twin: it exists so a PHP queue payload carries a model
// identifier instead of a serialized object graph, and a Go job carries whatever
// its fields hold.
type BroadcastableModelEventOccurred struct {
	// InteractsWithSockets is the PHP's `use InteractsWithSockets`, and it brings
	// the public $socket with it.
	broadcasting.InteractsWithSockets

	// Model is the public $model: the model the event is about.
	Model any

	// Connection is the public $connection: the queue connection the broadcast
	// job is pushed on.
	Connection string

	// Queue is the public $queue.
	Queue string

	// AfterCommit is the public $afterCommit.
	AfterCommit bool

	// SoftDeletes is what shouldBroadcastNow asks with
	// `method_exists($this->model, 'bootSoftDeletes')`. Go cannot ask a value
	// whether it has a method by name, so BroadcastsEvents states it.
	SoftDeletes bool

	// event is the protected $event.
	event Event

	// channels is the protected $channels: what onChannels was given.
	channels []broadcasting.Channel
}

// NewBroadcastableModelEventOccurred is
// BroadcastableModelEventOccurred::__construct.
func NewBroadcastableModelEventOccurred(model any, event Event) *BroadcastableModelEventOccurred {
	return &BroadcastableModelEventOccurred{Model: model, event: event}
}

// BroadcastOn is BroadcastableModelEventOccurred::broadcastOn: the channels the
// event goes out on.
//
// The PHP takes the channels given to onChannels, falls back to
// $this->model->broadcastOn($event), and maps every Model in the result to a
// PrivateChannel. The map is why the default works at all: the trait's
// broadcastOn returns [$this].
//
// A trait struct in Go cannot return the model it was embedded in, so
// [BroadcastsEvents.BroadcastOn] answers nothing and that private channel is
// built here, from the model this event already holds. It is the same channel
// the PHP arrives at by the same rule -- a model on the list means its own
// private channel -- decided where the model is in hand.
func (b *BroadcastableModelEventOccurred) BroadcastOn() []broadcasting.Channel {
	if len(b.channels) > 0 {
		return b.channels
	}

	if on, ok := b.Model.(BroadcastsModelEventOn); ok {
		if channels := on.BroadcastOn(b.event); len(channels) > 0 {
			return channels
		}
	}

	if has, ok := b.Model.(broadcasting.HasBroadcastChannel); ok && has.BroadcastChannel() != "" {
		return []broadcasting.Channel{broadcasting.NewPrivateChannelFor(has)}
	}

	return nil
}

// BroadcastAs is BroadcastableModelEventOccurred::broadcastAs: the name the
// event goes out under, "OrderCreated".
//
// The default is class_basename($this->model).ucfirst($this->event). There is no
// class name in Go, so the model's own name for itself is used when it has one
// -- BroadcastsEvents.Class, read through HasBroadcastChannel's sibling -- and
// reflect.Type.Name otherwise, which is what BroadcastEvent::displayName does
// for the same question.
//
// A model with its own broadcastAs is taken at its word, unless it answers the
// empty string: that is the PHP's `?: $default`, and it is what a model that
// only names some of its events returns for the rest.
func (b *BroadcastableModelEventOccurred) BroadcastAs() string {
	if as, ok := b.Model.(BroadcastsModelEventAs); ok {
		if name := as.BroadcastAs(b.event); name != "" {
			return name
		}
	}

	return b.modelBasename() + str.Ucfirst(string(b.event))
}

// modelBasename is class_basename($this->model).
func (b *BroadcastableModelEventOccurred) modelBasename() string {
	if has, ok := b.Model.(broadcasting.HasBroadcastChannel); ok {
		if route := has.BroadcastChannelRoute(); route != "" {
			// The route is "App.Models.Order.{order}", so the class name is
			// everything before the placeholder and its basename is the last
			// segment of that. Reading it here rather than adding a second field
			// keeps one statement of the model's name (RULE 9).
			if i := lastPlaceholder(route); i >= 0 {
				return lastDotSegment(trimTrailingDot(route[:i]))
			}
		}
	}

	if b.Model == nil {
		return ""
	}

	t := reflect.TypeOf(b.Model)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	return t.Name()
}

// BroadcastWith is BroadcastableModelEventOccurred::broadcastWith: the payload
// the event carries.
//
// A nil answer is the PHP's null, and the broadcaster reads the event's public
// properties instead -- which is what
// BroadcastEvent::getPayloadFromEvent does when method_exists says no.
func (b *BroadcastableModelEventOccurred) BroadcastWith() map[string]any {
	if with, ok := b.Model.(BroadcastsModelEventWith); ok {
		return with.BroadcastWith(b.event)
	}

	return nil
}

// OnChannels is BroadcastableModelEventOccurred::onChannels: name the channels
// by hand, in place of the model's.
//
// The PHP takes Arr::wrap($channels), so an empty list leaves the model's own
// answer standing. A nil or empty slice does the same here.
func (b *BroadcastableModelEventOccurred) OnChannels(channels []broadcasting.Channel) *BroadcastableModelEventOccurred {
	if len(channels) > 0 {
		b.channels = channels
	}

	return b
}

// ShouldBroadcastNow is BroadcastableModelEventOccurred::shouldBroadcastNow.
//
// A delete on a model that does not soft delete goes out synchronously, because
// by the time a worker picks the job up the row is gone and there is nothing
// left to load.
func (b *BroadcastableModelEventOccurred) ShouldBroadcastNow() bool {
	return b.event == Deleted && !b.SoftDeletes
}

// Event is BroadcastableModelEventOccurred::event: the model event this is
// about.
func (b *BroadcastableModelEventOccurred) Event() Event { return b.event }

// lastPlaceholder answers the index of the '{' that opens the trailing
// placeholder of a channel route, or -1.
func lastPlaceholder(route string) int {
	for i := len(route) - 1; i >= 0; i-- {
		if route[i] == '{' {
			return i
		}
	}

	return -1
}

// trimTrailingDot drops the separator a route leaves between the class name and
// its placeholder.
func trimTrailingDot(value string) string {
	if len(value) > 0 && value[len(value)-1] == '.' {
		return value[:len(value)-1]
	}

	return value
}
