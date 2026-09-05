// Package enums holds two enum types exactly as `aru make:enum` writes them, so
// that what depends on the enum contract is tested against the file a project
// actually gets rather than against a hand-written stand-in.
//
// Nothing here is edited. Each type is copied in as generated, which is what
// makes a failure here mean the generator and the contract have drifted apart --
// the only failure worth having in this package.
package enums
