// Package events mirrors Illuminate\Auth\Access\Events.
//
// The files it answers to, in the clone at
// laravel_illuminate/auth/Access/Events:
//
//	GateEvaluated.php
//
// GateEvaluated is the only one Illuminate declares here, and it is written.
//
// It is worth carrying where most Illuminate events are not: they exist so a
// package can be extended without being edited, which is a container's problem
// and not Go's. This one is the audit trail of the product's own thesis --
// every authorization decision passes through Gate.Raw, and this is the only
// place that sees all of them with the answer attached.
package events
