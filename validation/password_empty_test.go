package validation_test

import (
	"testing"

	"github.com/arandu-io/hesape/validation"
)

// TestAPasswordPolicyLeavesAnEmptyValueToRequired: the length of a policy does
// not run on a blank value, the way no rule in a chain does unless it is
// implicit, so an empty password is refused by required and not by the policy.
// A form that dropped required beside the policy would accept an empty
// password, and this test is where that division of work is written down.
func TestAPasswordPolicyLeavesAnEmptyValueToRequired(t *testing.T) {
	for _, value := range []string{"", "   "} {
		policy := validation.PasswordMin(8)
		if !policy.Passes("password", value) {
			t.Errorf("PasswordMin(8).Passes(%q) = false (%v), want true: a blank value is required's to refuse", value, policy.Message())
		}

		v := validation.Make(validation.Data{"password": value},
			validation.MustCompile(validation.Rules{"password": "required"}))
		if !v.Fails() {
			t.Errorf("required passed %q, want it refused: it is the rule that refuses an empty password", value)
		}
	}
}

// TestAPasswordCharacterCheckRefusesAnEmptyValue: the character checks read
// the text itself rather than running as a rule in a chain, so they refuse an
// empty value even though the length does not.
func TestAPasswordCharacterCheckRefusesAnEmptyValue(t *testing.T) {
	policy := validation.PasswordMin(8).MixedCase()
	if policy.Passes("password", "") {
		t.Fatal("PasswordMin(8).MixedCase().Passes(\"\") = true, want false: the mixed-case check reads the empty text and finds no letter of either case")
	}
	if got := policy.Message(); len(got) != 1 {
		t.Fatalf("messages = %v, want only the mixed-case one: the length does not run on an empty value", got)
	}
}
