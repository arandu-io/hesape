// Package concerns holds the pieces a model is assembled from: attribute
// handling, guarding, events, global scopes, relationships, timestamps, unique
// identifiers, hidden attributes, recursion guarding and event broadcasting.
//
// # A concern is a struct, and a global switch is a package function
//
// Each concern is a struct a model embeds, which gives it the fields and the
// methods. What could not be a method is the process-wide switch: Unguard,
// Unguarded, WithoutEvents and WithoutTimestamps are package functions, and the
// global they guard is a package variable behind a mutex.
//
// A concern cannot reach the model it was embedded in, so a method that needs
// the model's state takes it: UpdateTimestamps takes the attribute bag. That is
// written on each method that does it.
//
// # What is not here
//
// The cast implementations -- dates, enums, encrypted, hashed, custom casts.
// Those belong in eloquent/casts, one implementation, and a copy here would be
// the second answer to what a column means. What is here is the declaration
// side -- GetCasts, HasCast, MergeCasts and the four encoders FromJSON,
// FromFloat, FromDateTime and FromEncryptedString -- in hasattributescasting.go.
//
// There is no automatic eager loading. Reading an unloaded relation would run a
// query behind the caller, with no auth.Grant on it. GetRelationValue answers
// what is loaded and nothing else.
//
// # The tenant is in every channel, and it is added once
//
// "private-App.Models.Order.17" without a tenant is a single channel shared by
// every customer holding an order 17, and the first subscriber reads the others'
// events. [BroadcastsEvents.BroadcastChannel] and
// [BroadcastsEvents.BroadcastChannelRoute] answer the tenant-free name and
// pattern, and broadcasting.TenantChannel puts the tenant in front -- once, on
// the way into a driver and again on the way out of the authentication
// endpoint, so the name published and the name authorized cannot disagree. The
// tenant comes from the auth.Grant a channel Policy produced and never from the
// channel the client asked for, which is why a client asking for
// "private-orders.17" is authorized for "acme:private-orders.17" and never gets
// to choose the "acme".
package concerns
