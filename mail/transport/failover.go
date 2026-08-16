package transport

import (
	"context"
	"errors"
	"fmt"

	"github.com/arandu-io/hesape/mail"
)

// Failover sends through the first transport that accepts the message.
//
//	transport.Failover{Transports: []mail.Transport{
//		transport.Postmark{Token: cfg.PostmarkToken},
//		transport.Resend{Key: cfg.ResendKey},
//	}}
//
// It exists because the failure it covers is the one an application cannot code
// around: a provider having an afternoon, while the thing waiting to be sent is
// the password reset that lets somebody back in.
//
// # Why any failure moves to the next one
//
// A 502 is obviously worth trying elsewhere. A 422 looks like it is not -- the
// message was refused, so refusing it again proves nothing. Except that the
// commonest 422 is "this domain is not verified on this account", which is a
// fact about the provider and not about the message: the second provider, where
// the domain is verified, accepts it. So every failure moves on.
//
// The cost of being wrong in this direction is one extra rejected API call. The
// cost of being wrong in the other is an e-mail nobody sends.
//
// # What the error says
//
// When nothing accepted the message the error names every transport and what
// each one said, because "sending failed" with three providers configured sends
// somebody to three dashboards.
//
// The failures are joined, so the whole thing reads as a mail.ErrRetryable when
// any one of them was: a provider that answered 503 is a provider that may
// accept the same message in a minute, and that is worth a job rescheduling even
// if a different provider rejected the address outright. Only when every
// transport refused permanently is the failure permanent -- which is the case
// where the message itself is the problem and retrying reproduces it.
type Failover struct {
	// Transports are tried in the order they are written. The first is the one
	// that sends in the ordinary case, so it is the one whose deliverability and
	// price the application is choosing; the rest are the insurance.
	Transports []mail.Transport
}

// Name identifies the transport in a log line. The [mail.SentMessage] returned by Send
// names the transport that actually accepted the message, which is the one
// worth knowing.
func (Failover) Name() string { return "failover" }

// Send tries each transport in turn and returns as soon as one accepts.
func (t Failover) Send(ctx context.Context, m mail.Message) (mail.SentMessage, error) {
	if len(t.Transports) == 0 {
		return mail.SentMessage{}, errors.New("mail: the failover transport has nothing to fail over to")
	}

	failures := make([]error, 0, len(t.Transports))

	for _, next := range t.Transports {
		sent, err := next.Send(ctx, m)
		if err == nil {
			return sent, nil
		}

		// Wrapped rather than flattened to text: the join is what carries each
		// transport's mail.ErrRetryable out to the caller, and a chain rebuilt
		// from strings carries nothing.
		failures = append(failures, fmt.Errorf("%s: %w", next.Name(), err))

		// A cancelled request is not a provider that failed, and trying the next
		// one only spends the time the caller already stopped waiting for.
		if ctx.Err() != nil {
			break
		}
	}

	return mail.SentMessage{}, fmt.Errorf("mail: no transport accepted the message: %w", errors.Join(failures...))
}

var _ mail.Transport = Failover{}
