package view_test

import (
	"strings"
	"testing"
)

func TestPasswordRuntimeUsesLiteralSymbolSetsAndUnrewardedLimits(t *testing.T) {
	source := code(readUI(t))
	for _, required := range []string{"rules.symbolCharacters", "Array.from(rules.symbolCharacters)", "key !== 'max'", "line.hidden = !value || ok", "data-password-complete", "satisfied === total && met.max"} {
		if !strings.Contains(source, required) {
			t.Fatalf("missing native password contract: %s", required)
		}
	}
	if strings.Contains(source, "new RegExp(rules.symbolCharacters") {
		t.Fatal("literal symbol set became an evaluated expression")
	}
}
