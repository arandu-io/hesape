// Package capsule mirrors Illuminate\Queue\Capsule.
//
// The files it answers to, in the clone at
// laravel_illuminate/queue/Capsule:
//
//	Manager.php
//
// Nothing is here, and nothing is coming. The capsule is how Illuminate lends
// the queue to a program that is not a Laravel application: it builds a
// container, registers the connectors into it and hands back a QueueManager.
// Outside an Arandu application the queue is the same queue.QueueManager, built
// the same way in the same three lines, so a second way in would be a second
// way (RULE 9).
//
// Its three public methods -- addConnection, getQueueManager and
// registerConnectors -- are named in queue/doc.go with the rest.
package capsule
