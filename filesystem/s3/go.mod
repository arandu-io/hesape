module github.com/arandu-io/hesape/filesystem/s3

go 1.26

// Its own module, and not because the SDK is heavy -- there is no SDK. The root
// module of hesape declares one dependency in total and its CI reproves any
// second one, so anything that would grow the graph of a project that only
// wanted a directory on disk lives here instead.
//
// The protocol is HTTP with a signature. SigV4 is two hundred lines against an
// AWS SDK that brings a hundred modules, its own credential chain, its own retry
// policy and its own context rules -- and the algorithm has not changed since
// 2012, while the SDK's surface changes every quarter.
require github.com/arandu-io/hesape v0.11.0
