// Package events holds the announcement a gate makes when it has decided.
//
// There is one: [GateEvaluated]. It is the audit trail of the product's own
// thesis -- every authorization decision passes through Gate.Raw, and this is
// the only place that sees all of them with the answer attached.
//
// Deprecated: it announces the decisions of the deprecated access.Gate. See
// that package.
package events
