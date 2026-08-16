package fakes

// PendingMailFake is a message being addressed on a [MailFake], collecting its
// recipients before it is recorded.
type PendingMailFake struct {
	mailer *MailFake
	to     []string
	cc     []string
	bcc    []string
	locale string
}

// NewPendingMailFake builds a pending message bound to the mailer that will
// record it.
func NewPendingMailFake(mailer *MailFake) *PendingMailFake {
	return &PendingMailFake{mailer: mailer}
}

// To adds recipients and returns the pending message.
func (p *PendingMailFake) To(addresses ...string) *PendingMailFake {
	p.to = append(p.to, addresses...)
	return p
}

// CC adds carbon-copy recipients and returns the pending message.
func (p *PendingMailFake) CC(addresses ...string) *PendingMailFake {
	p.cc = append(p.cc, addresses...)
	return p
}

// BCC adds blind carbon-copy recipients and returns the pending message.
func (p *PendingMailFake) BCC(addresses ...string) *PendingMailFake {
	p.bcc = append(p.bcc, addresses...)
	return p
}

// Locale sets the locale the message would be rendered in, and returns the
// pending message.
func (p *PendingMailFake) Locale(locale string) *PendingMailFake {
	p.locale = locale
	return p
}

// Send records the mailable rather than sending it, filed as sent and not as
// queued even when it would have been queued for real.
func (p *PendingMailFake) Send(mailable any) {
	p.mailer.sendMail(mailable, false, p)
}

// SendNow is [PendingMailFake.Send]: with no queue behind a fake, "now" and
// "when you get to it" are the same moment.
func (p *PendingMailFake) SendNow(mailable any) {
	p.mailer.sendMail(mailable, false, p)
}

// Queue records the mailable as queued, so a test can tell a mailable that was
// pushed to the queue from one that was sent inline -- which is the difference
// between a request that answered in 40ms and one that waited on an SMTP
// handshake.
func (p *PendingMailFake) Queue(mailable any) {
	p.mailer.sendMail(mailable, true, p)
}

// Later records the mailable as queued. The delay is ignored rather than
// slept: a fake that honoured it would make every test that schedules an
// e-mail slow.
func (p *PendingMailFake) Later(_ any, mailable any) {
	p.mailer.sendMail(mailable, true, p)
}
