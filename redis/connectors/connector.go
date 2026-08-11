package connectors

import (
	"github.com/arandu-io/hesape/redis/connections"
)

// Connector turns a configuration into an open connection.
//
// It answers Illuminate\Contracts\Redis\Connector, and it is ONE type where
// Laravel has two: PhpRedisConnector and PredisConnector exist because PHP has
// two Redis client libraries -- a C extension and a pure-PHP one -- and an
// application picks between them in config. Go has one driver that speaks RESP,
// so there is one connector, and RedisManager.SetDriver changes nothing about
// how the socket is opened.
//
// It holds no state and has no constructor: Connector{} is a Connector.
type Connector struct{}

// Connect creates a connection to a Redis server.
//
// It answers Connector::connect(). It does not talk to the server; the first
// command does, and connections.Connection.Ping is how the health check asks
// on purpose.
func (Connector) Connect(config connections.Config) *connections.Connection {
	return connections.Connect(config)
}

// ConnectToCluster creates a connection to a Redis cluster.
//
// It answers Connector::connectToCluster(). The first configuration carries
// everything but the addresses -- password, database, prefix, timeouts -- and
// the addresses of every node in the cluster are collected from all of them,
// because a cluster is one credential and many endpoints.
//
// An empty list yields the single-node connection with that configuration,
// which is the same thing a one-node "cluster" is.
func (Connector) ConnectToCluster(configs []connections.Config) *connections.Connection {
	if len(configs) == 0 {
		return connections.Connect(connections.Config{})
	}

	config := configs[0]
	addresses := make([]string, 0, len(configs))
	for _, one := range configs {
		if one.Address != "" {
			addresses = append(addresses, one.Address)
		}
		addresses = append(addresses, one.Addresses...)
	}
	config.Addresses = addresses

	return connections.Connect(config)
}
