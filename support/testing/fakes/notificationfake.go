package fakes

import (
	"fmt"
	"sync"
)

// Notifiable is the little of a notifiable entity that a NotificationFake asks
// of what it is sending to: the key it files the record under, which is what
// tells one user from another.
type Notifiable interface {
	// GetKey returns the key that tells this notifiable from another.
	GetKey() string
}

// Notification is what a [NotificationFake] asks of what it records: the
// channels the notification says it goes out on.
//
// A notification that does not satisfy it is recorded on no channel.
type Notification interface {
	// Via returns the channels the notification goes out on for this
	// notifiable.
	Via(notifiable any) []string
}

// NotificationIdentity is satisfied by a notification carrying the id the
// dispatcher stamps on it, so the same notification sent to ten people is one
// notification.
//
// It is optional: a notification that does not satisfy it is recorded without
// an id, and no assertion in this package reads one.
type NotificationIdentity interface {
	// ID returns the id the notification was stamped with.
	ID() string
	// SetID stamps the notification with an id.
	SetID(id string)
}

// ShouldSend is satisfied by a notification that decides, per notifiable and
// per channel, whether it goes out at all.
type ShouldSend interface {
	// ShouldSend reports whether the notification goes out to this notifiable
	// on this channel.
	ShouldSend(notifiable any, channel string) bool
}

// HasLocalePreference is satisfied by a notifiable that states the locale it
// would rather be written to in.
type HasLocalePreference interface {
	// PreferredLocale returns the locale the notifiable prefers.
	PreferredLocale() string
}

// AnonymousNotifiable is the notifiable a notification sent on demand goes to,
// which is nobody in particular.
type AnonymousNotifiable struct {
	// Routes is the address to deliver to, per channel.
	Routes map[string]string
}

// NewAnonymousNotifiable returns an anonymous notifiable with no routes.
func NewAnonymousNotifiable() *AnonymousNotifiable {
	return &AnonymousNotifiable{Routes: map[string]string{}}
}

// Route sets where the channel should deliver, and returns the notifiable. The
// database channel is an error: an on-demand notification has no row to write
// against.
func (a *AnonymousNotifiable) Route(channel string, route string) (*AnonymousNotifiable, error) {
	if channel == "database" {
		return a, fmt.Errorf("the database channel does not support on-demand notifications")
	}
	if a.Routes == nil {
		a.Routes = map[string]string{}
	}
	a.Routes[channel] = route
	return a, nil
}

// GetKey returns the empty string: an anonymous notifiable has no key.
func (a *AnonymousNotifiable) GetKey() string { return "" }

// sentNotification is one recorded notification: what went out, to whom, on
// which channels, and in which locale.
type sentNotification struct {
	notifiableClass string
	notifiableKey   string
	notifiable      any
	notification    any
	channels        []string
	locale          string
}

// describe renders one record for a failure message.
func (s sentNotification) describe() string {
	line := "[" + className(s.notification) + "] to " + s.notifiableClass
	if s.notifiableKey != "" {
		line += "#" + s.notifiableKey
	}
	if len(s.channels) > 0 {
		line += " on " + joinNames(s.channels)
	}
	if s.locale != "" {
		line += " in " + s.locale
	}
	return line
}

// NotificationFake is the notification dispatcher a test installs so that
// nothing is delivered, and every notification can be asserted on
// afterwards.
//
// It is safe to use from a test that calls t.Parallel: every record is written
// and read under a mutex, and a truth test runs on a copy rather than while the
// lock is held.
type NotificationFake struct {
	mu sync.Mutex
	// locale is written into every record that carries none of its own.
	locale              string
	notifications       []sentNotification
	serializeAndRestore bool
}

// NewNotificationFake returns a dispatcher that records everything and
// delivers nothing.
func NewNotificationFake() *NotificationFake {
	return &NotificationFake{}
}

func (f *NotificationFake) isFake() {}

// AssertSentOnDemand fails the test unless a
// notification went out to nobody in particular, addressed by route rather than
// by model.
func (f *NotificationFake) AssertSentOnDemand(t TestingT, notification any, callback any) {
	t.Helper()
	f.AssertSentTo(t, NewAnonymousNotifiable(), notification, callback)
}

// AssertSentTo fails the test unless the
// notifiable was sent a notification of the given type and the truth test
// accepted it.
//
// The notifiable may be one value or a slice of them.
func (f *NotificationFake) AssertSentTo(t TestingT, notifiable any, notification any, callback any) {
	t.Helper()

	if list, ok := notifiableList(notifiable); ok {
		if len(list) == 0 {
			t.Errorf("AssertSentTo: no notifiable given.")
			return
		}
		for _, single := range list {
			f.AssertSentTo(t, single, notification, callback)
		}
		return
	}

	if times, ok := callback.(int); ok {
		f.AssertSentToTimes(t, notifiable, notification, times)
		return
	}

	test, ok := notificationTest(t, "AssertSentTo", callback)
	if !ok {
		return
	}

	if len(f.sentRecords(notifiable, notification, test)) > 0 {
		return
	}

	all := f.snapshot()
	t.Errorf(
		"AssertSentTo: the expected [%s] notification was not sent to %s. %s sent:%s",
		tokenName(notification), describeNotifiable(notifiable),
		countedWere(len(all), "notification"), listOf(describeNotifications(all)),
	)
}

// AssertSentOnDemandTimes fails the test unless a notification of the given
// type was sent on demand exactly that many times.
func (f *NotificationFake) AssertSentOnDemandTimes(t TestingT, notification any, times int) {
	t.Helper()
	f.AssertSentToTimes(t, NewAnonymousNotifiable(), notification, times)
}

// AssertSentToTimes fails the test unless the notifiable was sent exactly that
// many of them. It counts how many times
// this notifiable was sent it.
func (f *NotificationFake) AssertSentToTimes(t TestingT, notifiable any, notification any, times int) {
	t.Helper()

	found := f.sentRecords(notifiable, notification, nil)
	if len(found) == times {
		return
	}

	all := f.snapshot()
	t.Errorf(
		"AssertSentToTimes: expected [%s] to be sent to %s %d %s, but it was sent %d %s. %s sent:%s",
		tokenName(notification), describeNotifiable(notifiable), times, plural("time", times),
		len(found), plural("time", len(found)),
		countedWere(len(all), "notification"), listOf(describeNotifications(all)),
	)
}

// AssertNotSentTo fails the test when the notifiable was sent one. The
// callback slot takes the same forms as [NotificationFake.AssertSentTo], minus
// the count.
func (f *NotificationFake) AssertNotSentTo(t TestingT, notifiable any, notification any, callback any) {
	t.Helper()

	if list, ok := notifiableList(notifiable); ok {
		if len(list) == 0 {
			t.Errorf("AssertNotSentTo: no notifiable given.")
			return
		}
		for _, single := range list {
			f.AssertNotSentTo(t, single, notification, callback)
		}
		return
	}

	test, ok := notificationTest(t, "AssertNotSentTo", callback)
	if !ok {
		return
	}

	found := f.sentRecords(notifiable, notification, test)
	if len(found) == 0 {
		return
	}

	t.Errorf(
		"AssertNotSentTo: the unexpected [%s] notification was sent to %s %d %s:%s",
		tokenName(notification), describeNotifiable(notifiable), len(found), plural("time", len(found)),
		listOf(describeNotifications(found)),
	)
}

// AssertNothingSent fails the test unless nothing was sent at all.
func (f *NotificationFake) AssertNothingSent(t TestingT) {
	t.Helper()

	all := f.snapshot()
	if len(all) == 0 {
		return
	}

	t.Errorf(
		"AssertNothingSent: the following %s sent unexpectedly:%s",
		plural("notification was", len(all)), listOf(describeNotifications(all)),
	)
}

// AssertNothingSentTo fails the test unless nothing at
// all reached this notifiable, of any type.
func (f *NotificationFake) AssertNothingSentTo(t TestingT, notifiable any) {
	t.Helper()

	if list, ok := notifiableList(notifiable); ok {
		if len(list) == 0 {
			t.Errorf("AssertNothingSentTo: no notifiable given.")
			return
		}
		for _, single := range list {
			f.AssertNothingSentTo(t, single)
		}
		return
	}

	var found []sentNotification
	for _, record := range f.snapshot() {
		if record.notifiableClass == className(notifiable) && record.notifiableKey == notifiableKey(notifiable) {
			found = append(found, record)
		}
	}
	if len(found) == 0 {
		return
	}

	t.Errorf(
		"AssertNothingSentTo: %s sent to %s unexpectedly:%s",
		countedWere(len(found), "notification"), describeNotifiable(notifiable), listOf(describeNotifications(found)),
	)
}

// AssertSentTimes fails the test unless the notification went out exactly that
// many times, counting how many times the
// notification went out in total, to everybody.
func (f *NotificationFake) AssertSentTimes(t TestingT, notification any, expectedCount int) {
	t.Helper()

	all := f.snapshot()
	count := 0
	for _, record := range all {
		if sameClass(record.notification, notification) {
			count++
		}
	}
	if count == expectedCount {
		return
	}

	t.Errorf(
		"AssertSentTimes: expected [%s] to be sent %d %s, but it was sent %d %s. %s sent:%s",
		tokenName(notification), expectedCount, plural("time", expectedCount), count, plural("time", count),
		countedWere(len(all), "notification"), listOf(describeNotifications(all)),
	)
}

// AssertCount fails the test unless the total number of
// notifications sent, of every type and to everybody.
func (f *NotificationFake) AssertCount(t TestingT, expectedCount int) {
	t.Helper()

	all := f.snapshot()
	if len(all) == expectedCount {
		return
	}

	t.Errorf(
		"AssertCount: expected %d %s to be sent, but %s sent:%s",
		expectedCount, plural("notification", expectedCount),
		countedWere(len(all), "notification"), listOf(describeNotifications(all)),
	)
}

// Sent returns the notifications of the given type that
// went to this notifiable and that the truth test accepted, in the order they
// were sent.
func (f *NotificationFake) Sent(notifiable any, notification any, callback any) []any {
	test, ok := notificationTest(nil, "Sent", callback)
	if !ok {
		return nil
	}
	records := f.sentRecords(notifiable, notification, test)
	found := make([]any, 0, len(records))
	for _, record := range records {
		found = append(found, record.notification)
	}
	return found
}

// HasSent reports whether the notifiable was sent a notification of that type
// at all.
func (f *NotificationFake) HasSent(notifiable any, notification any) bool {
	return len(f.sentRecords(notifiable, notification, nil)) > 0
}

// Send is [NotificationFake.SendNow]: a fake has no queue to put a
// notification on.
func (f *NotificationFake) Send(notifiables any, notification any) {
	f.SendNow(notifiables, notification, nil)
}

// SendNow writes one record per notifiable, on the channels the notification
// says it goes out on.
//
// The channels given here win over the notification's own Via. A notifiable
// whose channels come back empty is skipped, which is how a notification that
// decided not to go out leaves no record.
func (f *NotificationFake) SendNow(notifiables any, notification any, channels []string) {
	list, ok := notifiableList(notifiables)
	if !ok {
		list = []any{notifiables}
	}

	for _, notifiable := range list {
		if identity, ok := notification.(NotificationIdentity); ok && identity.ID() == "" {
			identity.SetID(uuid())
		}

		notifiableChannels := channels
		if len(notifiableChannels) == 0 {
			if via, ok := notification.(Notification); ok {
				notifiableChannels = via.Via(notifiable)
			}
		}

		if decider, ok := notification.(ShouldSend); ok {
			kept := make([]string, 0, len(notifiableChannels))
			for _, channel := range notifiableChannels {
				if decider.ShouldSend(notifiable, channel) {
					kept = append(kept, channel)
				}
			}
			notifiableChannels = kept
		}

		if len(notifiableChannels) == 0 {
			continue
		}

		f.record(notifiable, notification, notifiableChannels)
	}
}

// record files one notification, and asks the notification and the notifiable
// their questions before it takes the lock: either of them could ask the fake
// something back, and a fake that deadlocks a test is worse than one that
// records a beat later.
func (f *NotificationFake) record(notifiable any, notification any, channels []string) {
	f.mu.Lock()
	serialize := f.serializeAndRestore
	fallback := f.locale
	f.mu.Unlock()

	recorded := notification
	if serialize && shouldQueue(notification) {
		recorded = restore(notification)
	}

	locale := ""
	if identity, ok := notification.(NotificationLocale); ok {
		locale = identity.NotificationLocale()
	}
	if locale == "" {
		locale = fallback
	}
	if locale == "" {
		if preference, ok := notifiable.(HasLocalePreference); ok {
			locale = preference.PreferredLocale()
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.notifications = append(f.notifications, sentNotification{
		notifiableClass: className(notifiable),
		notifiableKey:   notifiableKey(notifiable),
		notifiable:      notifiable,
		notification:    recorded,
		channels:        append([]string(nil), channels...),
		locale:          locale,
	})
}

// NotificationLocale is satisfied by a notification carrying the locale it was
// sent with, which wins over the locale the fake was told to use.
//
// It is optional: a notification without one falls through to the fake's
// locale and then to the notifiable's preference.
type NotificationLocale interface {
	// NotificationLocale returns the locale the notification carries.
	NotificationLocale() string
}

// Channel returns nil: a faked dispatcher has no channel to build.
func (f *NotificationFake) Channel(name string) any {
	return nil
}

// Locale sets the locale every later record is written in, unless the
// notification carries one of its own, and returns the fake.
func (f *NotificationFake) Locale(locale string) *NotificationFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.locale = locale
	return f
}

// SerializeAndRestore says whether a queued notification is put through the
// round trip the queue would put it through before it is recorded, and returns
// the fake.
func (f *NotificationFake) SerializeAndRestore(serializeAndRestore bool) *NotificationFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.serializeAndRestore = serializeAndRestore
	return f
}

// SentNotifications returns every record, keyed by the class of the notifiable
// and then by its key.
func (f *NotificationFake) SentNotifications() map[string]map[string][]any {
	sent := map[string]map[string][]any{}
	for _, record := range f.snapshot() {
		byKey, ok := sent[record.notifiableClass]
		if !ok {
			byKey = map[string][]any{}
			sent[record.notifiableClass] = byKey
		}
		byKey[record.notifiableKey] = append(byKey[record.notifiableKey], record.notification)
	}
	return sent
}

// snapshot copies the ledger under the lock, so a truth test runs outside it.
func (f *NotificationFake) snapshot() []sentNotification {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentNotification(nil), f.notifications...)
}

// sentRecords returns the records filed under this notifiable and this exact
// notification class, and accepted by the truth test.
func (f *NotificationFake) sentRecords(notifiable any, notification any, test func(sentNotification) bool) []sentNotification {
	class := className(notifiable)
	key := notifiableKey(notifiable)

	var found []sentNotification
	for _, record := range f.snapshot() {
		if record.notifiableClass != class || record.notifiableKey != key {
			continue
		}
		if !sameClass(record.notification, notification) {
			continue
		}
		if !callFn(test, record) {
			continue
		}
		found = append(found, record)
	}
	return found
}

// notifiableKey answers `(string) $notifiable->getKey()`: the empty string when
// the notifiable has no key, which is what an AnonymousNotifiable is.
func notifiableKey(notifiable any) string {
	if keyed, ok := notifiable.(Notifiable); ok {
		return keyed.GetKey()
	}
	return ""
}

// notifiableList answers `is_array($notifiable) || $notifiable instanceof
// Collection`: whether the assertion was handed several notifiables rather than
// one.
//
// A string is not a list here even though Go could range over its bytes.
func notifiableList(notifiable any) ([]any, bool) {
	if list, ok := notifiable.([]any); ok {
		return list, true
	}
	return nil, false
}

// describeNotifiable renders a notifiable for a failure message.
func describeNotifiable(notifiable any) string {
	name := className(notifiable)
	if key := notifiableKey(notifiable); key != "" {
		return name + "#" + key
	}
	return name
}

// describeNotifications renders the records a failure message ends with.
func describeNotifications(records []sentNotification) []string {
	lines := make([]string, 0, len(records))
	for _, record := range records {
		lines = append(lines, record.describe())
	}
	return lines
}

// notificationTest normalizes the truth test forms NotificationFake accepts. It
// reports the failure itself when handed a form it does not know, and answers
// false so the caller stops.
func notificationTest(t TestingT, name string, callback any) (func(sentNotification) bool, bool) {
	switch cb := callback.(type) {
	case nil:
		return nil, true
	case func(notification any) bool:
		if cb == nil {
			return nil, true
		}
		return func(record sentNotification) bool { return cb(record.notification) }, true
	case func(notification any, channels []string) bool:
		if cb == nil {
			return nil, true
		}
		return func(record sentNotification) bool { return cb(record.notification, record.channels) }, true
	case func(notification any, channels []string, notifiable any) bool:
		if cb == nil {
			return nil, true
		}
		return func(record sentNotification) bool {
			return cb(record.notification, record.channels, record.notifiable)
		}, true
	default:
		if t != nil {
			t.Helper()
			t.Errorf("%s: the callback must be nil, a func(notification any) bool, a func(notification any, channels []string) bool, a func(notification any, channels []string, notifiable any) bool or an int; got %T.", name, callback)
		}
		return nil, false
	}
}
