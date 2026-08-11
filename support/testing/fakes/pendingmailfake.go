package fakes

// PendingMailFake answers Illuminate\Support\Testing\Fakes\PendingMailFake.
//
// It is what Mail::to('a@example.com') returns while the mailer is faked: the
// same fluent chain the real PendingMail offers, recording the addresses and
// handing them to the MailFake when the chain ends in Send, SendNow or Queue.
//
// The addresses live here rather than on the mailable because that is where
// Laravel puts them -- Mail::to(...)->send($mailable) addresses a mailable that
// carries no address of its own, and a test that asserts on the recipient is
// asserting on this, not on the mailable.
type PendingMailFake struct {
	mailer *MailFake
	to     []string
	cc     []string
	bcc    []string
	locale string
}

// NewPendingMailFake answers PendingMailFake::__construct.
func NewPendingMailFake(mailer *MailFake) *PendingMailFake {
	return &PendingMailFake{mailer: mailer}
}

// To answers PendingMail::to, which PendingMailFake inherits.
//
// Calling it more than once appends, as passing an array does: Laravel merges
// into $this->to rather than replacing it, so two calls address two people.
func (p *PendingMailFake) To(addresses ...string) *PendingMailFake {
	p.to = append(p.to, addresses...)
	return p
}

// CC answers PendingMail::cc. The PHP spells it cc; Go initialisms are upper
// case throughout.
func (p *PendingMailFake) CC(addresses ...string) *PendingMailFake {
	p.cc = append(p.cc, addresses...)
	return p
}

// BCC answers PendingMail::bcc.
func (p *PendingMailFake) BCC(addresses ...string) *PendingMailFake {
	p.bcc = append(p.bcc, addresses...)
	return p
}

// Locale answers PendingMail::locale.
func (p *PendingMailFake) Locale(locale string) *PendingMailFake {
	p.locale = locale
	return p
}

// Send answers PendingMailFake::send.
//
// It records rather than sends, and the mailable is recorded as sent -- not as
// queued -- even when it would have been queued for real. That is the PHP's
// behaviour: PendingMailFake::send calls the fake mailer's send() whatever the
// mailable implements, so AssertSent is the assertion that answers here and
// AssertQueued is not.
func (p *PendingMailFake) Send(mailable any) {
	p.mailer.sendMail(mailable, false, p)
}

// SendNow answers PendingMailFake::sendNow. It is Send: with no queue behind a
// fake, "now" and "when you get to it" are the same moment.
func (p *PendingMailFake) SendNow(mailable any) {
	p.mailer.sendMail(mailable, false, p)
}

// Queue answers PendingMailFake::queue.
//
// This one records as queued, so a test can tell a mailable that was pushed to
// the queue from one that was sent inline -- which is the difference between a
// request that answered in 40ms and one that waited on an SMTP handshake.
func (p *PendingMailFake) Queue(mailable any) {
	p.mailer.sendMail(mailable, true, p)
}

// Later answers PendingMail::later. The delay is recorded and not slept: a fake
// that honoured it would make every test that schedules an e-mail slow.
func (p *PendingMailFake) Later(_ any, mailable any) {
	p.mailer.sendMail(mailable, true, p)
}
