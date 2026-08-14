package events

import "github.com/arandu-io/hesape/auth"

// GateEvaluated is Illuminate\Auth\Access\Events\GateEvaluated: the record that
// an authorization question was asked, and what it was answered.
//
// Laravel fires it after every Gate::raw, through the dispatcher the Gate
// resolves out of the container. There is no container here (ADR 0001), so the
// Gate is given a place to send it -- see Gate.Observe -- and an application
// that gives it none pays nothing.
//
// # Why this one is worth carrying
//
// Most of Illuminate's events exist so a package can be extended without being
// edited, which is a problem a container has and Go does not. This one is
// different: it is the audit trail of the product's own thesis. Every
// authorization decision the application ever makes passes through Gate.Raw,
// and this is the only place that sees all of them with the answer attached.
//
// What it is NOT is a place to change the answer. It is fired after the decision
// is made, receives a copy, and nothing reads what a handler does with it. An
// event that could veto would be a second authorization path, and there is one
// (RULE 17).
type GateEvaluated struct {
	// Subject is who asked. It is a value and not a pointer: a handler holding
	// it cannot alter the subject the decision was made for.
	Subject auth.Subject

	// Ability is the name the caller passed, before any policy lookup.
	Ability string

	// Result is the raw answer, exactly as the callback gave it: nil when no
	// callback matched, a bool, a *Response, or an error. It is deliberately not
	// reduced to a bool -- "nobody answered" and "answered no" are different
	// facts, and an audit trail that conflates them cannot tell a missing policy
	// from a denial.
	Result any

	// Arguments is what the ability was asked about.
	Arguments []any
}

// Name has no Illuminate counterpart: it is the event's name on the wire, for
// the outbox and for anything that routes by string.
func (GateEvaluated) Name() string { return "auth.gate.evaluated" }
