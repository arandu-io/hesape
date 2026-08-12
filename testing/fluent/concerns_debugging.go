package fluent

import (
	"encoding/json"
	"fmt"
)

// This file answers to Illuminate\Testing\Fluent\Concerns\Debugging.
//
// The PHP is a trait mixed into AssertableJson. Go has no traits, so the
// methods are on AssertableJSON itself and the file keeps the trait's name.

// Dump answers to Debugging::dump: print the scope, or one property of it.
//
// The PHP writes to the output; this writes to the test's log, which go test
// shows beside the test that produced it and hides when the test passes.
//
// The variadic prop stands for the PHP's `?string $prop = null`.
func (a *AssertableJSON) Dump(prop ...string) *AssertableJSON {
	a.t.Helper()

	value := a.prop(prop...)

	encoded, err := json.MarshalIndent(value, "", "    ")
	if err != nil {
		a.t.Logf("%#v", value)
		return a
	}

	path := a.dotPath(prop...)
	if path == "" {
		a.t.Logf("%s", encoded)
		return a
	}
	a.t.Logf("%s", fmt.Sprintf("[%s]\n%s", path, encoded))
	return a
}
