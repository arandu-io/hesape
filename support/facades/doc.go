// Package facades declares nothing, and never will.
//
// There are no global accessors that resolve a service on demand: each package
// exports what it owns, and a caller imports that package and calls it. Where a
// test needs to replace something, the package that owns it says how -- moving
// the clock, for one, is support.Travel, support.FreezeTime and
// support.TravelBack.
package facades
