// Package connectors opens the connection the rest of the adapter speaks over.
//
// One type, Connector, with Connect for a single server and ConnectToCluster
// for a set of them. Both funnel into connections.Connect: there is one driver
// and it speaks RESP, so there is nothing for a second connector to be, and
// RedisManager.SetDriver changes which custom creator RedisManager.Extend
// registered runs rather than how the socket is opened.
package connectors
