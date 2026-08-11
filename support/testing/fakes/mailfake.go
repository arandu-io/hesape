package fakes

import (
	"strings"
	"sync"
	"time"
)

// Mailable is the little of Illuminate\Mail\Mailable that a MailFake asks of
// what it records, and it is optional: a mailable that carries its own
// recipients answers these three, and one addressed through Mail::to() does not
// have to, because the MailFake remembers the addresses the PendingMailFake
// collected.
//
// HasCC and HasBCC answer hasCc and hasBcc; the initialisms are upper case
// because Go spells them that way.
type Mailable interface {
	// HasTo answers Illuminate\Mail\Mailable::hasTo.
	HasTo(address string) bool
	// HasCC answers Illuminate\Mail\Mailable::hasCc.
	HasCC(address string) bool
	// HasBCC answers Illuminate\Mail\Mailable::hasBcc.
	HasBCC(address string) bool
}

// ShouldQueue answers Illuminate\Contracts\Queue\ShouldQueue: a mailable or a
// job that goes on the queue rather than out now.
//
// The PHP is an empty marker interface, which Go cannot express, so this one
// has a method and the answer is read from it: a type that implements
// ShouldQueue and returns false is sent now, exactly as if it did not implement
// it at all.
type ShouldQueue interface {
	ShouldQueue() bool
}

// sentMail is one recorded outgoing message: the mailable itself, the mailer it
// went through, and the addresses the pending mail collected on its way.
//
// PHP keeps only the mailable, because in PHP the addresses were written onto
// it by PendingMail::fill. A Go interface cannot write to the value it was
// handed, so the fake remembers them beside it -- which is also what lets a
// failure message name the addresses a message went to.
type sentMail struct {
	mailable any
	mailer   string
	to       []string
	cc       []string
	bcc      []string
	locale   string
}

// hasTo answers Illuminate\Mail\Mailable::hasTo for a record: the mailable's
// own answer when it has one, and the addresses the pending mail collected
// otherwise.
func (m sentMail) hasTo(address string) bool {
	if mailable, ok := m.mailable.(Mailable); ok && mailable.HasTo(address) {
		return true
	}
	return contains(m.to, address)
}

// describe renders one record for a failure message.
func (m sentMail) describe() string {
	line := "[" + className(m.mailable) + "]"
	if len(m.to) > 0 {
		line += " to " + strings.Join(m.to, ", ")
	}
	if len(m.cc) > 0 {
		line += " cc " + strings.Join(m.cc, ", ")
	}
	if len(m.bcc) > 0 {
		line += " bcc " + strings.Join(m.bcc, ", ")
	}
	if m.mailer != "" {
		line += " through the [" + m.mailer + "] mailer"
	}
	return line
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// MailFake answers Illuminate\Support\Testing\Fakes\MailFake: the mailer a test
// installs so that nothing leaves the process, and every message can be
// asserted on afterwards.
//
// It is safe to use from a test that calls t.Parallel: every record is written
// and read under a mutex, and a truth test runs on a copy rather than while the
// lock is held, so a callback that asks the fake another question does not
// deadlock.
type MailFake struct {
	mu              sync.Mutex
	currentMailer   string
	mailables       []sentMail
	queuedMailables []sentMail
}

// NewMailFake answers MailFake::__construct.
//
// The PHP takes the MailManager it stands in for and forwards unknown calls to
// it through __call. Neither survives here: __call is a PHP language interface,
// and the manager is a container binding (ADR 0001).
func NewMailFake() *MailFake {
	return &MailFake{}
}

func (f *MailFake) isFake() {}

// AssertSent answers MailFake::assertSent: it fails unless a mailable of the
// given type was sent and the truth test accepted it.
//
// The type is named the way Go names one, with reflect.TypeFor[OrderShipped]()
// where the PHP writes OrderShipped::class; a value of the type works too.
//
// The callback slot is whatever the PHP accepts there, and means the same
// thing: nil for no truth test, a func(mailable any) bool for one, an int to
// assert a count, a string for an address the mailable went to, or a []string
// for several. Go has no overloading, so the slot is typed any and a form it
// does not know is reported as a failure naming the forms it does.
//
// A mailable is found by `instanceof`, as the PHP is: a type that embeds
// another is found by an assertion naming either.
func (f *MailFake) AssertSent(t TestingT, mailable any, callback any) {
	t.Helper()

	switch cb := callback.(type) {
	case nil:
		f.assertSent(t, mailable, nil)
	case int:
		f.AssertSentTimes(t, mailable, cb)
	case string:
		f.assertSentTo(t, mailable, cb)
	case []string:
		for _, address := range cb {
			f.assertSentTo(t, mailable, address)
		}
	case func(mailable any) bool:
		f.assertSent(t, mailable, cb)
	default:
		t.Errorf("AssertSent: the callback must be nil, a func(mailable any) bool, an int, a string address or a []string; got %T.", callback)
	}
}

func (f *MailFake) assertSent(t TestingT, mailable any, callback func(mailable any) bool) {
	t.Helper()

	if len(f.Sent(mailable, callback)) > 0 {
		return
	}

	sent, queued := f.snapshot()

	suggestion := ""
	if len(queued) > 0 {
		suggestion = " Did you mean to use AssertQueued() instead?"
	}

	t.Errorf(
		"AssertSent: the expected [%s] mailable was not sent.%s %s were sent:%s",
		tokenName(mailable), suggestion, countedAs(len(sent), "mailable"), listOf(describeAll(sent)),
	)
}

func (f *MailFake) assertSentTo(t TestingT, mailable any, address string) {
	t.Helper()

	sent, queued := f.snapshot()
	for _, record := range filterMail(sent, mailable, nil) {
		if record.hasTo(address) {
			return
		}
	}

	suggestion := ""
	if len(queued) > 0 {
		suggestion = " Did you mean to use AssertQueued() instead?"
	}

	t.Errorf(
		"AssertSent: the expected [%s] mailable was not sent to address [%s].%s %s were sent:%s",
		tokenName(mailable), address, suggestion, countedAs(len(sent), "mailable"), listOf(describeAll(sent)),
	)
}

// AssertSentTimes answers MailFake::assertSentTimes: it fails unless exactly
// that many mailables of the type were sent.
//
// PHP reaches it by passing a number where assertSent takes a callback, and so
// does AssertSent here. It is also a method of its own, and public rather than
// protected, as it already is in the current Laravel.
func (f *MailFake) AssertSentTimes(t TestingT, mailable any, times int) {
	t.Helper()

	found := f.Sent(mailable, nil)
	if len(found) == times {
		return
	}

	sent, _ := f.snapshot()

	t.Errorf(
		"AssertSentTimes: the expected [%s] mailable was sent %d %s instead of %d %s. %s were sent:%s",
		tokenName(mailable), len(found), plural("time", len(found)), times, plural("time", times),
		countedAs(len(sent), "mailable"), listOf(describeAll(sent)),
	)
}

// AssertNotOutgoing answers MailFake::assertNotOutgoing: neither sent nor
// queued.
func (f *MailFake) AssertNotOutgoing(t TestingT, mailable any, callback any) {
	t.Helper()
	f.AssertNotSent(t, mailable, callback)
	f.AssertNotQueued(t, mailable, callback)
}

// AssertNotSent answers MailFake::assertNotSent. The callback slot takes the
// same forms as AssertSent, minus the count: PHP does not accept one here
// either.
func (f *MailFake) AssertNotSent(t TestingT, mailable any, callback any) {
	t.Helper()

	switch cb := callback.(type) {
	case nil:
		f.assertNotSent(t, mailable, nil)
	case string:
		f.assertNotSentTo(t, mailable, cb)
	case []string:
		for _, address := range cb {
			f.assertNotSentTo(t, mailable, address)
		}
	case func(mailable any) bool:
		f.assertNotSent(t, mailable, cb)
	default:
		t.Errorf("AssertNotSent: the callback must be nil, a func(mailable any) bool, a string address or a []string; got %T.", callback)
	}
}

func (f *MailFake) assertNotSent(t TestingT, mailable any, callback func(mailable any) bool) {
	t.Helper()

	found := f.sentRecords(mailable, callback)
	if len(found) == 0 {
		return
	}

	t.Errorf(
		"AssertNotSent: the unexpected [%s] mailable was sent %d %s:%s",
		tokenName(mailable), len(found), plural("time", len(found)), listOf(describeAll(found)),
	)
}

func (f *MailFake) assertNotSentTo(t TestingT, mailable any, address string) {
	t.Helper()

	sent, _ := f.snapshot()
	var found []sentMail
	for _, record := range filterMail(sent, mailable, nil) {
		if record.hasTo(address) {
			found = append(found, record)
		}
	}
	if len(found) == 0 {
		return
	}

	t.Errorf(
		"AssertNotSent: the unexpected [%s] mailable was sent to address [%s]:%s",
		tokenName(mailable), address, listOf(describeAll(found)),
	)
}

// AssertNothingSent answers MailFake::assertNothingSent.
func (f *MailFake) AssertNothingSent(t TestingT) {
	t.Helper()

	sent, _ := f.snapshot()
	if len(sent) == 0 {
		return
	}

	t.Errorf(
		"AssertNothingSent: the following %s were sent unexpectedly:%s",
		plural("mailable", len(sent)), listOf(describeAll(sent)),
	)
}

// AssertQueued answers MailFake::assertQueued. The callback slot takes the same
// forms as AssertSent.
func (f *MailFake) AssertQueued(t TestingT, mailable any, callback any) {
	t.Helper()

	switch cb := callback.(type) {
	case nil:
		f.assertQueued(t, mailable, nil)
	case int:
		f.AssertQueuedTimes(t, mailable, cb)
	case string:
		f.assertQueuedTo(t, mailable, cb)
	case []string:
		for _, address := range cb {
			f.assertQueuedTo(t, mailable, address)
		}
	case func(mailable any) bool:
		f.assertQueued(t, mailable, cb)
	default:
		t.Errorf("AssertQueued: the callback must be nil, a func(mailable any) bool, an int, a string address or a []string; got %T.", callback)
	}
}

func (f *MailFake) assertQueued(t TestingT, mailable any, callback func(mailable any) bool) {
	t.Helper()

	if len(f.Queued(mailable, callback)) > 0 {
		return
	}

	sent, queued := f.snapshot()

	suggestion := ""
	if len(sent) > 0 {
		suggestion = " Did you mean to use AssertSent() instead?"
	}

	t.Errorf(
		"AssertQueued: the expected [%s] mailable was not queued.%s %s were queued:%s",
		tokenName(mailable), suggestion, countedAs(len(queued), "mailable"), listOf(describeAll(queued)),
	)
}

func (f *MailFake) assertQueuedTo(t TestingT, mailable any, address string) {
	t.Helper()

	_, queued := f.snapshot()
	for _, record := range filterMail(queued, mailable, nil) {
		if record.hasTo(address) {
			return
		}
	}

	t.Errorf(
		"AssertQueued: the expected [%s] mailable was not queued to address [%s]. %s were queued:%s",
		tokenName(mailable), address, countedAs(len(queued), "mailable"), listOf(describeAll(queued)),
	)
}

// AssertQueuedTimes answers MailFake::assertQueuedTimes. It is a method of its
// own for the same reason AssertSentTimes is.
func (f *MailFake) AssertQueuedTimes(t TestingT, mailable any, times int) {
	t.Helper()

	found := f.Queued(mailable, nil)
	if len(found) == times {
		return
	}

	_, queued := f.snapshot()

	t.Errorf(
		"AssertQueuedTimes: the expected [%s] mailable was queued %d %s instead of %d %s. %s were queued:%s",
		tokenName(mailable), len(found), plural("time", len(found)), times, plural("time", times),
		countedAs(len(queued), "mailable"), listOf(describeAll(queued)),
	)
}

// AssertNotQueued answers MailFake::assertNotQueued.
func (f *MailFake) AssertNotQueued(t TestingT, mailable any, callback any) {
	t.Helper()

	switch cb := callback.(type) {
	case nil:
		f.assertNotQueued(t, mailable, nil)
	case string:
		f.assertNotQueuedTo(t, mailable, cb)
	case []string:
		for _, address := range cb {
			f.assertNotQueuedTo(t, mailable, address)
		}
	case func(mailable any) bool:
		f.assertNotQueued(t, mailable, cb)
	default:
		t.Errorf("AssertNotQueued: the callback must be nil, a func(mailable any) bool, a string address or a []string; got %T.", callback)
	}
}

func (f *MailFake) assertNotQueued(t TestingT, mailable any, callback func(mailable any) bool) {
	t.Helper()

	found := f.queuedRecords(mailable, callback)
	if len(found) == 0 {
		return
	}

	t.Errorf(
		"AssertNotQueued: the unexpected [%s] mailable was queued %d %s:%s",
		tokenName(mailable), len(found), plural("time", len(found)), listOf(describeAll(found)),
	)
}

func (f *MailFake) assertNotQueuedTo(t TestingT, mailable any, address string) {
	t.Helper()

	_, queued := f.snapshot()
	var found []sentMail
	for _, record := range filterMail(queued, mailable, nil) {
		if record.hasTo(address) {
			found = append(found, record)
		}
	}
	if len(found) == 0 {
		return
	}

	t.Errorf(
		"AssertNotQueued: the unexpected [%s] mailable was queued to address [%s]:%s",
		tokenName(mailable), address, listOf(describeAll(found)),
	)
}

// AssertNothingQueued answers MailFake::assertNothingQueued.
func (f *MailFake) AssertNothingQueued(t TestingT) {
	t.Helper()

	_, queued := f.snapshot()
	if len(queued) == 0 {
		return
	}

	t.Errorf(
		"AssertNothingQueued: the following %s were queued unexpectedly:%s",
		plural("mailable", len(queued)), listOf(describeAll(queued)),
	)
}

// AssertNothingOutgoing answers MailFake::assertNothingOutgoing.
func (f *MailFake) AssertNothingOutgoing(t TestingT) {
	t.Helper()
	f.AssertNothingSent(t)
	f.AssertNothingQueued(t)
}

// AssertSentCount answers MailFake::assertSentCount: the total, of every type.
func (f *MailFake) AssertSentCount(t TestingT, count int) {
	t.Helper()

	sent, _ := f.snapshot()
	if len(sent) == count {
		return
	}

	t.Errorf(
		"AssertSentCount: the total number of mailables sent was %d instead of %d:%s",
		len(sent), count, listOf(describeAll(sent)),
	)
}

// AssertQueuedCount answers MailFake::assertQueuedCount.
func (f *MailFake) AssertQueuedCount(t TestingT, count int) {
	t.Helper()

	_, queued := f.snapshot()
	if len(queued) == count {
		return
	}

	t.Errorf(
		"AssertQueuedCount: the total number of mailables queued was %d instead of %d:%s",
		len(queued), count, listOf(describeAll(queued)),
	)
}

// AssertOutgoingCount answers MailFake::assertOutgoingCount: sent plus queued.
func (f *MailFake) AssertOutgoingCount(t TestingT, count int) {
	t.Helper()

	sent, queued := f.snapshot()
	total := len(sent) + len(queued)
	if total == count {
		return
	}

	t.Errorf(
		"AssertOutgoingCount: the total number of outgoing mailables was %d instead of %d:%s",
		total, count, listOf(append(describeAll(sent), describeAll(queued)...)),
	)
}

// Sent answers MailFake::sent: the mailables of the given type that the truth
// test accepted, in the order they were sent. A nil truth test accepts all of
// them, which is PHP's `$callback ?: fn () => true`.
func (f *MailFake) Sent(mailable any, callback func(mailable any) bool) []any {
	return mailablesIn(f.sentRecords(mailable, callback))
}

// HasSent answers MailFake::hasSent: whether a mailable of the type was sent at
// all, truth test aside.
func (f *MailFake) HasSent(mailable any) bool {
	return len(f.Sent(mailable, nil)) > 0
}

// Queued answers MailFake::queued.
func (f *MailFake) Queued(mailable any, callback func(mailable any) bool) []any {
	return mailablesIn(f.queuedRecords(mailable, callback))
}

// HasQueued answers MailFake::hasQueued.
func (f *MailFake) HasQueued(mailable any) bool {
	return len(f.Queued(mailable, nil)) > 0
}

// Mailer answers MailFake::mailer: it names the mailer the next message goes
// through, and returns the fake so the call can be chained.
func (f *MailFake) Mailer(name string) *MailFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentMailer = name
	return f
}

// ForgetMailers answers MailFake::forgetMailers: it drops the mailer chosen by
// the last Mailer call.
func (f *MailFake) ForgetMailers() *MailFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentMailer = ""
	return f
}

// To answers MailFake::to: it begins a message to the given addresses.
//
// The PHP takes users -- a model, a collection, an address -- and reads the
// address off them. Here it takes addresses, because a fake that guesses which
// field of a struct holds the e-mail is a fake that guesses wrong.
func (f *MailFake) To(addresses ...string) *PendingMailFake {
	return NewPendingMailFake(f).To(addresses...)
}

// CC answers MailFake::cc.
func (f *MailFake) CC(addresses ...string) *PendingMailFake {
	return NewPendingMailFake(f).CC(addresses...)
}

// BCC answers MailFake::bcc.
func (f *MailFake) BCC(addresses ...string) *PendingMailFake {
	return NewPendingMailFake(f).BCC(addresses...)
}

// Raw answers MailFake::raw, and does nothing, as the PHP does: a raw message
// has no mailable to record, so there is nothing an assertion could name.
func (f *MailFake) Raw(text string, callback func(any)) {}

// Send answers MailFake::send: it records the mailable as sent, or as queued
// when the mailable is a ShouldQueue.
func (f *MailFake) Send(mailable any) {
	f.sendMail(mailable, shouldQueue(mailable), nil)
}

// SendNow answers MailFake::sendNow: recorded as sent even when the mailable is
// a ShouldQueue.
func (f *MailFake) SendNow(mailable any) {
	f.sendMail(mailable, false, nil)
}

// Queue answers MailFake::queue: the mailable is recorded as queued.
//
// The queue name is accepted and dropped, as the PHP drops it: the fake records
// which mailer a message went through, and nothing in Illuminate reads back the
// queue a mailable was put on.
func (f *MailFake) Queue(mailable any, queue string) {
	f.sendMail(mailable, true, nil)
}

// Later answers MailFake::later: the same as Queue, delay and all. The delay is
// accepted and dropped for the same reason the queue name is.
func (f *MailFake) Later(delay time.Duration, mailable any, queue string) {
	f.Queue(mailable, queue)
}

// sendMail answers MailFake::sendMail: the one place a record is written.
//
// The PHP writes the current mailer onto the mailable before storing it; here
// it is stored beside the mailable, because an interface cannot write to the
// value it was handed. Either way the current mailer is cleared afterwards, so
// the next message does not inherit it.
func (f *MailFake) sendMail(mailable any, queued bool, pending *PendingMailFake) {
	if mailable == nil {
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	record := sentMail{mailable: mailable, mailer: f.currentMailer}
	if pending != nil {
		record.to = append([]string(nil), pending.to...)
		record.cc = append([]string(nil), pending.cc...)
		record.bcc = append([]string(nil), pending.bcc...)
		record.locale = pending.locale
	}

	f.currentMailer = ""

	if queued {
		f.queuedMailables = append(f.queuedMailables, record)
		return
	}
	f.mailables = append(f.mailables, record)
}

// snapshot copies both ledgers under the lock, so a truth test runs outside it.
func (f *MailFake) snapshot() (sent, queued []sentMail) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentMail(nil), f.mailables...), append([]sentMail(nil), f.queuedMailables...)
}

func (f *MailFake) sentRecords(mailable any, callback func(mailable any) bool) []sentMail {
	sent, _ := f.snapshot()
	return filterMail(sent, mailable, callback)
}

func (f *MailFake) queuedRecords(mailable any, callback func(mailable any) bool) []sentMail {
	_, queued := f.snapshot()
	return filterMail(queued, mailable, callback)
}

// filterMail answers MailFake::mailablesOf plus the truth test: `instanceof`,
// then the callback.
func filterMail(records []sentMail, mailable any, callback func(mailable any) bool) []sentMail {
	var found []sentMail
	for _, record := range records {
		if !instanceOf(record.mailable, mailable) {
			continue
		}
		if !callFn(callback, record.mailable) {
			continue
		}
		found = append(found, record)
	}
	return found
}

func mailablesIn(records []sentMail) []any {
	found := make([]any, 0, len(records))
	for _, record := range records {
		found = append(found, record.mailable)
	}
	return found
}

func describeAll(records []sentMail) []string {
	lines := make([]string, 0, len(records))
	for _, record := range records {
		lines = append(lines, record.describe())
	}
	return lines
}

// shouldQueue answers `$view instanceof ShouldQueue`.
func shouldQueue(v any) bool {
	queueable, ok := v.(ShouldQueue)
	return ok && queueable.ShouldQueue()
}
