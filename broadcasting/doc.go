// Package broadcasting publishes an event to the clients listening on a
// channel, and authorizes the subscriptions that ask to listen.
//
// An event is put on its way by [BroadcastManager.Queue], which pushes it as a
// job unless the event asked to go now. The job resolves a driver by name and
// hands it the channels, the event name and the payload; the drivers live in
// github.com/arandu-io/hesape/broadcasting/broadcasters.
//
// A subscription arrives at [BroadcastController.Authenticate], which asks the
// driver to authorize it. The decision is made by a Policy and comes back as an
// auth.Grant, and the name the client is finally signed onto is built from that
// Grant: every published channel is "<tenant>:<channel>", and the tenant comes
// off the Grant and from nowhere else. See [TenantChannel].
package broadcasting
