package concerns

import (
	"reflect"

	"github.com/arandu-io/hesape/broadcasting"
	"github.com/arandu-io/hesape/str"
)

// BroadcastableModelEventOccurred is the event [BroadcastsEvents] hands the
// broadcaster.
//
// It lives beside [BroadcastsEvents] because nothing else builds one.
//
// What the broadcasters ask for -- BroadcastOn, BroadcastAs, BroadcastWith and
// ShouldBroadcastNow -- is below, and each one satisfies the interface of the
// same name in github.com/arandu-io/hesape/broadcasting.
type BroadcastableModelEventOccurred struct {
	// InteractsWithSockets is embedded for the Socket field it brings.
	broadcasting.InteractsWithSockets

	// Model is the model the event is about.
	Model any

	// Connection is the queue connection the broadcast job is pushed on.
	Connection string

	// Queue is the queue the broadcast job is pushed on.
	Queue string

	// AfterCommit says whether the broadcast waits for the surrounding
	// transaction to commit.
	AfterCommit bool

	// SoftDeletes says whether the model soft deletes. Go cannot ask a
	// value whether it has a method by name, so BroadcastsEvents states it,
	// and ShouldBroadcastNow reads it.
	SoftDeletes bool

	// event is the model event this is about.
	event Event

	// channels is what OnChannels was given.
	channels []broadcasting.Channel
}

// NewBroadcastableModelEventOccurred returns the event for model and event,
// with no channels named yet.
func NewBroadcastableModelEventOccurred(model any, event Event) *BroadcastableModelEventOccurred {
	return &BroadcastableModelEventOccurred{Model: model, event: event}
}

// BroadcastOn returns the channels the event goes out on: the channels
// given to OnChannels, or the model's own BroadcastOn when it implements
// BroadcastsModelEventOn, or the model's own private channel via
// HasBroadcastChannel, in that order.
//
// [BroadcastsEvents.BroadcastOn] returns nothing on its own -- it cannot
// reach the model it is embedded in -- so the model's private channel is
// built here instead, from the model this event already holds.
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

// BroadcastAs returns the name the event goes out under, "OrderCreated".
//
// The default is the model's base name plus the event name, capitalised.
// There is no class name in Go, so the model's own name for itself is used
// when it has one -- BroadcastsEvents.Class, read through
// HasBroadcastChannel's sibling -- and reflect.Type.Name otherwise.
//
// A model with its own BroadcastAs is taken at its word, unless it returns
// the empty string: that is what a model that only names some of its
// events returns for the rest, and it falls back to the default.
func (b *BroadcastableModelEventOccurred) BroadcastAs() string {
	if as, ok := b.Model.(BroadcastsModelEventAs); ok {
		if name := as.BroadcastAs(b.event); name != "" {
			return name
		}
	}

	return b.modelBasename() + str.Ucfirst(string(b.event))
}

// modelBasename returns the model's base name, for BroadcastAs's default.
func (b *BroadcastableModelEventOccurred) modelBasename() string {
	if has, ok := b.Model.(broadcasting.HasBroadcastChannel); ok {
		if route := has.BroadcastChannelRoute(); route != "" {
			// The route is "App.Models.Order.{order}", so the class name is
			// everything before the placeholder and its basename is the last
			// segment of that. Reading it here rather than adding a second field
			// keeps one statement of the model's name.
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

// BroadcastWith returns the payload the event carries, from the model when
// it implements BroadcastsModelEventWith, or nil otherwise -- and a nil
// result is the broadcaster's cue to read the event's own public fields
// instead.
func (b *BroadcastableModelEventOccurred) BroadcastWith() map[string]any {
	if with, ok := b.Model.(BroadcastsModelEventWith); ok {
		return with.BroadcastWith(b.event)
	}

	return nil
}

// OnChannels names the channels by hand, in place of the model's.
//
// A nil or empty slice leaves the model's own channels standing.
func (b *BroadcastableModelEventOccurred) OnChannels(channels []broadcasting.Channel) *BroadcastableModelEventOccurred {
	if len(channels) > 0 {
		b.channels = channels
	}

	return b
}

// ShouldBroadcastNow reports whether the event should broadcast
// synchronously rather than through the queue.
//
// A delete on a model that does not soft delete goes out synchronously,
// because by the time a worker picks the job up the row is gone and there
// is nothing left to load.
func (b *BroadcastableModelEventOccurred) ShouldBroadcastNow() bool {
	return b.event == Deleted && !b.SoftDeletes
}

// Event returns the model event this is about.
func (b *BroadcastableModelEventOccurred) Event() Event { return b.event }

// lastPlaceholder returns the index of the '{' that opens the trailing
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
