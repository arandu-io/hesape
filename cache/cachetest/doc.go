// Package cachetest is the contract suite every Store passes.
//
// It is what makes one behaviour test serve every backend: a Store built over
// memory, over the filesystem, over a table or over RESP is handed to the same
// suite, and a store that passes it can be dropped into a Repository without
// the Repository knowing which one it got.
package cachetest
