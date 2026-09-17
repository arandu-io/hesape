package validation

import (
	"strings"
	"testing"
)

func TestPasswordSymbolsFromUsesLiteralConfiguredCharacters(t *testing.T) {
	policy := PasswordMin(12).Max(128).Letters().MixedCase().Numbers().SymbolsFrom("@$!%*#?&.-")
	for _, sample := range []struct {
		name, value string
		valid       bool
	}{
		{"complete", "Example-2026!", true}, {"upper_missing", "example-2026!", false},
		{"lower_missing", "EXAMPLE-2026!", false}, {"number_missing", "Example-pass!", false},
		{"symbol_missing", "Example2026xx", false}, {"only_space", "Example2026  ", false},
		{"too_short", "Ex1!", false}, {"unicode_letters", "Árvore-linda1!", true},
		{"long_phrase", strings.Repeat("a", 100) + "Z1!", true}, {"over_cap", strings.Repeat("a", 128) + "Z1!", false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			if policy.Passes("password", sample.value) != sample.valid {
				t.Fatal("configured rule result differs")
			}
		})
	}
	for _, character := range "@$!%*#?&.-" {
		if !policy.Passes("password", "Example2026x"+string(character)) {
			t.Fatal("configured literal was rejected")
		}
	}
	if PasswordMin(1).SymbolsFrom("").Passes("password", "!") {
		t.Fatal("empty configured set passed")
	}
	literal := PasswordMin(1).SymbolsFrom("]\\😀")
	for _, value := range []string{"]", "\\", "😀"} {
		if !literal.Passes("password", value) {
			t.Fatal("literal was treated as an expression")
		}
	}
	if literal.Passes("password", "a") {
		t.Fatal("unlisted character was accepted")
	}
}

func TestExplicitSymbolSetDoesNotChangeExistingPasswordDefaults(t *testing.T) {
	if _, exists := PasswordMin(8).Symbols().AppliedRules()["symbolCharacters"]; exists {
		t.Fatal("default policy was widened")
	}
	if !PasswordMin(1).Symbols().Passes("password", " ") {
		t.Fatal("legacy symbol categories changed")
	}
	if !PasswordMin(1).Symbols().Passes("password", "©") {
		t.Fatal("legacy symbol categories changed")
	}
	policy := PasswordMin(12).SymbolsFrom("@$!")
	rules := policy.AppliedRules()
	if rules["symbols"] != true || rules["symbolCharacters"] != "@$!" {
		t.Fatal("the client did not receive the exact policy")
	}
	rules["symbolCharacters"] = "x"
	if policy.AppliedRules()["symbolCharacters"] != "@$!" {
		t.Fatal("returned map changed the policy")
	}
	if PasswordDefault().AppliedRules()["min"] != 8 {
		t.Fatal("global minimum changed")
	}
}
