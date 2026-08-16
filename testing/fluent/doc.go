// Package fluent asserts about a JSON payload one property at a time.
//
// [AssertableJSON] is the whole of it. A test descends into the payload,
// names each property it cares about, and closes with
// [AssertableJSON.Interacted] -- which fails when the response carried a
// property the test never named.
//
// That accounting is what the package is for. A fragment assertion says "these
// pairs are in there" and passes over everything else; this one notices the
// field somebody added without meaning to publish it.
//
//	json := fluent.FromAssertableJSONString(t, response.DecodeResponseJSON())
//	json.Has("data", 3).Where("data.0.name", "Alice")
//	json.Interacted()
//
// A scope that does not want the accounting says [AssertableJSON.Etc], and
// should still say [AssertableJSON.Missing] about what must not be there.
package fluent
