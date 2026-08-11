// Package connectors mirrors Illuminate\Redis\Connectors.
//
// The files it answers to, in the clone at
// laravel_illuminate/redis/Connectors:
//
//	PhpRedisConnector.php -> Connector
//	PredisConnector.php   -> Connector
//
// Two files, one type. That is the whole of what this package has to explain:
// PhpRedisConnector and PredisConnector exist because PHP has two Redis client
// libraries -- phpredis, the C extension, and predis, written in PHP -- and an
// application chooses between them in configuration. Go has one driver, it
// speaks RESP, and there is nothing for a second connector to be.
//
// So RedisManager.SetDriver still takes a driver name, because the name is part
// of the configuration a Laravel developer already writes, and it changes
// nothing about how the socket is opened. What it does change is which custom
// creator RedisManager.Extend registered runs -- that is the extension point
// the two classes were standing in for.
//
// An earlier version of this file said nothing would ever be implemented here
// and pointed at connections.Connect. That was right about there being one way
// to open a connection and wrong about where it lives: Connector.Connect and
// Connector.ConnectToCluster are the two names Laravel calls, both funnel into
// connections.Connect, and a package that only holds a note is a package
// somebody has to be told about.
package connectors
