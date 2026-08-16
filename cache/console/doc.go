// Package console holds the cache commands.
//
// Four of them, each a value with a Handle and a Command that puts it in a
// console.Registry. They are here and not in the parent package because a
// command imports the console, the console imports the cache, and a cache that
// imported its own commands would be a cycle.
//
// Every command that touches entries takes a --tenant and holds a system grant
// for it. Flush empties one tenant's slice of one namespace, and a cache:clear
// that emptied the store would clear every other customer on the way past.
package console
