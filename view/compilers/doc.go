// Package compilers turns view source into the Go a compiled view runs.
//
// Compiler is the shared machinery every concrete compiler embeds: the cache
// path, the compiled extension and the hash that names a compiled file.
// KyseCompiler is the concrete compiler: it holds the custom directive
// registry, the condition registry, the component aliases and namespaces,
// the echo format and the raw-block store. ComponentTagCompiler is the
// precompile step that expands <x-...> component tags into the directives
// KyseCompiler understands.
package compilers
