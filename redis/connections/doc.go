// Package connections holds the connection everything else in this adapter
// speaks over.
//
// It is one connection type over one driver and one address. Sharding is the
// deployment's business -- see Config.Address.
//
// The connection is deliberately thin. It opens the socket, carries the
// application prefix and answers Ping; it does not know what a tenant is, what a
// cache entry is or what a session is. Everything that decides WHICH key a value
// belongs under lives one layer up -- in cache.Repository for entries, in the
// session handler for sessions -- where it is written once instead of once per
// caller.
package connections
