// Package macroable exports nothing.
//
// A method's receiver has to be declared in the package that declares the type,
// and there is no hook for a call that resolves to nothing, so a type cannot
// gain a method from outside the package that declares it.
//
// What to write instead depends on what the added behaviour needs from the
// value it operates on:
//
//   - A free function, when the behaviour only reads its argument. This is the
//     shape for str, number, collections/arr and every other package whose
//     surface is functions over values rather than methods on state.
//
//   - Your own type embedding the framework's, when the behaviour chains.
//     Embedding promotes every method of the embedded type, so the new method
//     sits beside Filter, Map and Sort rather than in a second vocabulary. The
//     cost is one conversion at each boundary where the wrapper meets a
//     signature written against the embedded type.
//
//   - A small interface, declared by the consumer, when more than one type
//     answers the same need. Nothing is grafted onto an existing type; the
//     requirement is written down where it is required.
//
// What this gives up is that a third-party package can no longer add a method
// to every Collection in the process. In exchange every method call names the
// package that declared it, and no import can change the meaning of code it
// does not appear in.
package macroable
