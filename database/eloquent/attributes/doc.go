// Package attributes declares nothing, and will not.
//
// Go has struct tags and nothing else of the kind, and reading behaviour out of
// an annotation is the mechanism this framework's thesis rejects -- what decides
// is the type, checked by the compiler.
//
// So what a model configures, it configures in Go, where a reader can see it: a
// struct tag on the entity field, or a call in the constructor -- Observe,
// AddGlobalScope, SetTable, Guard.
package attributes
