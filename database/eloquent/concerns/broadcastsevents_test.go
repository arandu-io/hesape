package concerns_test

import (
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
	"github.com/arandu-io/hesape/database/eloquent/concerns"
)

// join is the action a channel authorization is issued for. It is spelled here
// rather than imported from broadcasting/broadcasters so that these tests do
// not pull a driver in to name a constant.
const join auth.Action = "broadcasting.join"

// recordingDispatcher is the events dispatcher a PendingBroadcast sends
// through, and it keeps what it was given.
type recordingDispatcher struct{ sent []any }

func (r *recordingDispatcher) Dispatch(event any, payload ...any) []any {
	r.sent = append(r.sent, event)

	return nil
}

// occurred answers the model events this dispatcher was handed, in order.
func (r *recordingDispatcher) occurred() []*concerns.BroadcastableModelEventOccurred {
	events := make([]*concerns.BroadcastableModelEventOccurred, 0, len(r.sent))
	for _, event := range r.sent {
		if model, ok := event.(*concerns.BroadcastableModelEventOccurred); ok {
			events = append(events, model)
		}
	}

	return events
}

// factory stands in for the BroadcastManager: it is the broadcast() helper.
type factory struct{ dispatcher *recordingDispatcher }

func (f *factory) Event(event any) *broadcasting.PendingBroadcast {
	return broadcasting.NewPendingBroadcast(f.dispatcher, event)
}

// openTransaction is a connection with one transaction open, and it holds the
// after-commit callbacks until commit is called.
type openTransaction struct {
	level     int
	callbacks []func()
}

func (t *openTransaction) AfterCommit(callback func()) error {
	t.callbacks = append(t.callbacks, callback)

	return nil
}

func (t *openTransaction) TransactionLevel() int { return t.level }

func (t *openTransaction) commit() {
	callbacks := t.callbacks
	t.callbacks = nil

	for _, callback := range callbacks {
		callback()
	}
}

// order is a model assembled from the two traits, the way a model is.
type order struct {
	concerns.BroadcastsEvents
	concerns.HasEvents
}

// newOrder builds a booted order and the dispatcher its broadcasts land in.
func newOrder(t *testing.T) (*order, *recordingDispatcher) {
	t.Helper()

	dispatcher := &recordingDispatcher{}

	o := &order{}
	o.Class = "App.Models.Order"
	o.Key = 17
	o.Broadcast = &factory{dispatcher: dispatcher}
	o.BootBroadcastsEvents(&o.HasEvents)

	return o, dispatcher
}

func TestEachModelEventBroadcastsOnTheModelsPrivateChannel(t *testing.T) {
	o, dispatcher := newOrder(t)

	o.FireModelEvent(concerns.Created, o, false)
	o.FireModelEvent(concerns.Updated, o, false)
	o.FireModelEvent(concerns.Deleted, o, false)

	events := dispatcher.occurred()
	if len(events) != 3 {
		t.Fatalf("%d events were broadcast, want one for each of created, updated and deleted", len(events))
	}

	for i, want := range []concerns.Event{concerns.Created, concerns.Updated, concerns.Deleted} {
		if events[i].Event() != want {
			t.Errorf("event %d is %q, want %q", i, events[i].Event(), want)
		}

		channels := events[i].BroadcastOn()
		if len(channels) != 1 {
			t.Fatalf("event %d went out on %d channels, want one", i, len(channels))
		}
		if channels[0].Name != "private-App.Models.Order.17" {
			t.Errorf("event %d went out on %q, want the model's own private channel", i, channels[0].Name)
		}
	}
}

// A model that does not soft delete never raises trashed or restored, and
// bootBroadcastsEvents does not register them -- which is the method_exists
// check in the PHP body.
func TestTrashedAndRestoredAreOnlyRegisteredByAModelThatSoftDeletes(t *testing.T) {
	o, dispatcher := newOrder(t)

	o.FireModelEvent(concerns.Trashed, o, false)
	o.FireModelEvent(concerns.Restored, o, false)

	if len(dispatcher.sent) != 0 {
		t.Fatalf("%d events were broadcast by a model that does not soft delete", len(dispatcher.sent))
	}

	soft := &order{}
	soft.Class = "App.Models.Order"
	soft.Key = 17
	soft.SoftDeletes = true
	soft.Broadcast = &factory{dispatcher: dispatcher}
	soft.BootBroadcastsEvents(&soft.HasEvents)

	soft.FireModelEvent(concerns.Trashed, soft, false)
	soft.FireModelEvent(concerns.Restored, soft, false)

	if len(dispatcher.occurred()) != 2 {
		t.Fatalf("%d events were broadcast, want trashed and restored", len(dispatcher.occurred()))
	}
}

func TestBroadcastAsIsTheModelNameAndTheEventName(t *testing.T) {
	o, _ := newOrder(t)

	for event, want := range map[concerns.Event]string{
		concerns.Created:  "OrderCreated",
		concerns.Updated:  "OrderUpdated",
		concerns.Deleted:  "OrderDeleted",
		concerns.Trashed:  "OrderTrashed",
		concerns.Restored: "OrderRestored",
	} {
		if got := o.NewBroadcastableModelEvent(o, event).BroadcastAs(); got != want {
			t.Errorf("%q broadcasts as %q, want %q", event, got, want)
		}
	}
}

// A delete on a model that does not soft delete cannot wait for a worker: by
// the time one picks the job up the row is gone.
func TestOnlyAHardDeleteBroadcastsNow(t *testing.T) {
	o, _ := newOrder(t)

	if !o.NewBroadcastableModelEvent(o, concerns.Deleted).ShouldBroadcastNow() {
		t.Error("a hard delete did not ask to be broadcast now")
	}
	if o.NewBroadcastableModelEvent(o, concerns.Created).ShouldBroadcastNow() {
		t.Error("a create asked to be broadcast now")
	}

	o.SoftDeletes = true
	if o.NewBroadcastableModelEvent(o, concerns.Deleted).ShouldBroadcastNow() {
		t.Error("a delete on a model that soft deletes asked to be broadcast now, and the row is still there")
	}
}

// RULE 14. The channel a model names carries no tenant, and the name it is
// published under does -- so two customers holding an order 17 are on two
// channels, and neither can name the other's, because the tenant comes from the
// Grant and never from what the client asked for.
func TestTheSameModelIsADifferentChannelInEachTenant(t *testing.T) {
	o, _ := newOrder(t)

	channel := broadcasting.NewPrivateChannelFor(o)

	acme, err := broadcasting.TenantChannel(auth.SystemGrant(join, "acme"), channel)
	if err != nil {
		t.Fatalf("acme could not name its own channel: %v", err)
	}
	globex, err := broadcasting.TenantChannel(auth.SystemGrant(join, "globex"), channel)
	if err != nil {
		t.Fatalf("globex could not name its own channel: %v", err)
	}

	if acme == globex {
		t.Fatalf("both tenants publish order 17 on %q, so the first subscriber reads the other's events", acme)
	}
	if acme != "acme:private-App.Models.Order.17" {
		t.Errorf("acme publishes on %q, want the tenant in front of the channel", acme)
	}
	if globex != "globex:private-App.Models.Order.17" {
		t.Errorf("globex publishes on %q, want the tenant in front of the channel", globex)
	}

	// A caller who authorized nothing has no tenant, and therefore no channel.
	if _, err := broadcasting.TenantChannel(auth.Grant{}, channel); !errors.Is(err, broadcasting.ErrNoTenant) {
		t.Errorf("the zero Grant named a channel, error was %v", err)
	}
}

// The route the channel is authorized under is the same shape for every tenant,
// because the tenant is added once, on the way in and on the way out.
func TestTheChannelRouteIsThePatternTheModelIsAuthorizedUnder(t *testing.T) {
	o, _ := newOrder(t)

	if got := o.BroadcastChannelRoute(); got != "App.Models.Order.{order}" {
		t.Errorf("the channel route is %q, want the class and its camel-cased basename", got)
	}

	// A model that stated no class has no channel, rather than ".17".
	var unnamed concerns.BroadcastsEvents
	if got := unnamed.BroadcastChannel(); got != "" {
		t.Errorf("a model with no class name answered the channel %q", got)
	}
	if got := unnamed.BroadcastChannelRoute(); got != "" {
		t.Errorf("a model with no class name answered the route %q", got)
	}
}

// broadcastAfterCommit is the difference between telling every subscriber about
// a row and telling them about a row a rollback is about to remove.
func TestNothingIsBroadcastBeforeTheTransactionCommits(t *testing.T) {
	o, dispatcher := newOrder(t)
	transaction := &openTransaction{level: 1}
	o.AfterCommit = true
	o.Transactions = transaction

	o.FireModelEvent(concerns.Created, o, false)

	if len(dispatcher.sent) != 0 {
		t.Fatalf("%d events left while the transaction was still open", len(dispatcher.sent))
	}

	transaction.commit()

	if len(dispatcher.occurred()) != 1 {
		t.Fatalf("%d events left after the commit, want the one that was held", len(dispatcher.occurred()))
	}
	if !dispatcher.occurred()[0].AfterCommit {
		t.Error("the event does not carry afterCommit, so a queue would not honour it either")
	}
}

func TestWithoutAfterCommitTheEventLeavesImmediately(t *testing.T) {
	o, dispatcher := newOrder(t)
	o.Transactions = &openTransaction{level: 1}

	o.FireModelEvent(concerns.Created, o, false)

	if len(dispatcher.sent) != 1 {
		t.Fatalf("%d events left, want the one that did not ask to wait", len(dispatcher.sent))
	}
}

// A model that asks to wait and has nothing tracking commits sends now: there
// is no commit to wait for, and holding the event forever would lose it.
func TestAModelThatAsksToWaitWithNoTransactionSendsNow(t *testing.T) {
	o, dispatcher := newOrder(t)
	o.AfterCommit = true

	o.FireModelEvent(concerns.Created, o, false)

	if len(dispatcher.sent) != 1 {
		t.Fatalf("%d events left outside a transaction, want one", len(dispatcher.sent))
	}
}

func TestWithoutBroadcastingSuspendsEveryModelEventBroadcast(t *testing.T) {
	o, dispatcher := newOrder(t)

	err := concerns.WithoutBroadcasting(func() error {
		if concerns.IsBroadcasting() {
			t.Error("broadcasting is still on inside WithoutBroadcasting")
		}

		o.FireModelEvent(concerns.Created, o, false)

		return nil
	})
	if err != nil {
		t.Fatalf("WithoutBroadcasting returned %v", err)
	}

	if len(dispatcher.sent) != 0 {
		t.Fatalf("%d events were broadcast with broadcasting off", len(dispatcher.sent))
	}
	if !concerns.IsBroadcasting() {
		t.Fatal("broadcasting did not come back on")
	}

	o.FireModelEvent(concerns.Created, o, false)
	if len(dispatcher.sent) != 1 {
		t.Fatalf("%d events were broadcast once broadcasting was back on, want one", len(dispatcher.sent))
	}
}

// The channels handed to broadcastCreated replace the model's own, which is
// the $channels argument of the PHP.
func TestNamedChannelsReplaceTheModelsOwn(t *testing.T) {
	o, dispatcher := newOrder(t)

	pending := o.BroadcastCreated(o, broadcasting.NewPresenceChannel("orders"))
	if pending == nil {
		t.Fatal("broadcastCreated answered nothing")
	}
	pending.Send()

	events := dispatcher.occurred()
	if len(events) != 1 {
		t.Fatalf("%d events were broadcast, want one", len(events))
	}

	channels := events[0].BroadcastOn()
	if len(channels) != 1 || channels[0].Name != "presence-orders" {
		t.Errorf("the event went out on %v, want the channel it was given", channels)
	}
}

// A model with no factory behind it broadcasts nothing rather than panicking,
// which is what a model built for a unit test looks like.
func TestAModelWithNoBroadcastFactoryBroadcastsNothing(t *testing.T) {
	o := &order{}
	o.Class = "App.Models.Order"
	o.Key = 17
	o.BootBroadcastsEvents(&o.HasEvents)

	if pending := o.BroadcastCreated(o); pending != nil {
		t.Error("a model with no broadcast factory answered a pending broadcast")
	}

	o.FireModelEvent(concerns.Created, o, false)
}

// The connection and the queue travel on the event, which is where
// BroadcastManager reads them off when it pushes.
func TestTheEventCarriesTheConnectionAndTheQueueTheModelAskedFor(t *testing.T) {
	o, _ := newOrder(t)
	o.Connection = "redis"
	o.Queue = "broadcasts"

	event := o.NewBroadcastableModelEvent(o, concerns.Created)

	if event.Connection != "redis" {
		t.Errorf("the event goes out on connection %q, want the one the model asked for", event.Connection)
	}
	if event.Queue != "broadcasts" {
		t.Errorf("the event goes out on queue %q, want the one the model asked for", event.Queue)
	}
	if o.BroadcastConnection() != "redis" || o.BroadcastQueue() != "broadcasts" {
		t.Error("the accessors do not answer the fields they read")
	}
}
