package fluent

import (
	"encoding/json"
	"fmt"
)

// The debugging helpers on [AssertableJSON].

// Dump logs the scope, or one property of it.
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
