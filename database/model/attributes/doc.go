// Package attributes declares nothing, and will not.
//
// Go has struct tags and nothing else of the kind, and reading behaviour out of
// an annotation is the mechanism this framework's thesis rejects -- what decides
// is the type, checked by the compiler.
//
// So what a model configures, it configures in Go, where a reader can see it: a
// struct tag on the entity field, or a field of the model.TableSpec handed to
// model.NewTable -- Name for the table, Scopes for the global scopes, Events
// for the callbacks a model event runs, Hidden and Visible for what a row
// serialises.
package attributes
