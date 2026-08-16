// Package remote exports nothing, and will not.
//
// There is no SSH connection, no host group and no remote task type here,
// because none of them is a type in Go. Running a command on another machine is
// [github.com/arandu-io/hesape/process] with the ssh binary as the transport;
// running the same command on several machines is
// [github.com/arandu-io/hesape/concurrency.Run] over that; naming a command a
// person types is [github.com/arandu-io/hesape/console.Command]. Moving a
// customer's file is [github.com/arandu-io/hesape/filesystem.Disk], which
// carries the grant and the tenant prefix a shell cannot.
package remote
