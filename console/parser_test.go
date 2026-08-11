package console_test

import (
	"slices"
	"testing"

	"github.com/arandu-io/hesape/console"
)

func TestParseReadsTheCommandNameFromTheSignature(t *testing.T) {
	name, arguments, options, err := console.Parse("mail:send")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if name != "mail:send" {
		t.Errorf("name = %q, want mail:send", name)
	}
	if len(arguments) != 0 || len(options) != 0 {
		t.Errorf("a signature with no tokens declared %d arguments and %d options", len(arguments), len(options))
	}
}

func TestParseRefusesASignatureWithNoName(t *testing.T) {
	for _, expression := range []string{"", "   ", "{user}"} {
		if _, _, _, err := console.Parse(expression); err == nil {
			t.Errorf("Parse(%q) returned no error, and a command with no name cannot be typed", expression)
		}
	}
}

func TestParseReadsARequiredArgument(t *testing.T) {
	_, arguments, _, err := console.Parse("mail:send {user}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(arguments) != 1 {
		t.Fatalf("got %d arguments, want 1", len(arguments))
	}

	argument := arguments[0]
	if argument.Name != "user" {
		t.Errorf("name = %q, want user", argument.Name)
	}
	if !argument.IsRequired() {
		t.Error("a bare {user} is required")
	}
	if argument.IsArray() {
		t.Error("a bare {user} takes one value")
	}
}

func TestParseReadsAnOptionalArgument(t *testing.T) {
	_, arguments, _, err := console.Parse("mail:send {user?}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if arguments[0].Name != "user" {
		t.Errorf("name = %q, want user", arguments[0].Name)
	}
	if arguments[0].IsRequired() {
		t.Error("{user?} is optional")
	}
}

func TestParseReadsAnArgumentWithADefault(t *testing.T) {
	_, arguments, _, err := console.Parse("mail:send {user=guest}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if arguments[0].Name != "user" {
		t.Errorf("name = %q, want user", arguments[0].Name)
	}
	if arguments[0].IsRequired() {
		t.Error("an argument with a default is optional")
	}
	if got := arguments[0].Default; !slices.Equal(got, []string{"guest"}) {
		t.Errorf("default = %v, want [guest]", got)
	}
}

func TestParseReadsAnArrayArgument(t *testing.T) {
	_, arguments, _, err := console.Parse("mail:send {users*}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if arguments[0].Name != "users" {
		t.Errorf("name = %q, want users", arguments[0].Name)
	}
	if !arguments[0].IsArray() {
		t.Error("{users*} takes every remaining operand")
	}
	if !arguments[0].IsRequired() {
		t.Error("{users*} needs at least one; {users?*} is the one that may be empty")
	}
}

func TestParseReadsAnOptionalArrayArgument(t *testing.T) {
	_, arguments, _, err := console.Parse("mail:send {users?*}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if arguments[0].Name != "users" {
		t.Errorf("name = %q, want users", arguments[0].Name)
	}
	if !arguments[0].IsArray() {
		t.Error("{users?*} takes every remaining operand")
	}
	if arguments[0].IsRequired() {
		t.Error("{users?*} may be empty")
	}
}

func TestParseReadsAnArrayArgumentWithDefaults(t *testing.T) {
	_, arguments, _, err := console.Parse("mail:send {users=*first,second}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if arguments[0].Name != "users" {
		t.Errorf("name = %q, want users", arguments[0].Name)
	}
	if !arguments[0].IsArray() {
		t.Error("{users=*a,b} is an array")
	}
	if got := arguments[0].Default; !slices.Equal(got, []string{"first", "second"}) {
		t.Errorf("default = %v, want [first second]", got)
	}
}

func TestParseReadsABooleanFlag(t *testing.T) {
	_, _, options, err := console.Parse("mail:send {--force}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(options) != 1 {
		t.Fatalf("got %d options, want 1", len(options))
	}
	if options[0].Name != "force" {
		t.Errorf("name = %q, want force", options[0].Name)
	}
	if options[0].AcceptValue() {
		t.Error("{--force} takes no value")
	}
}

func TestParseReadsAnOptionThatTakesAValue(t *testing.T) {
	_, _, options, err := console.Parse("mail:send {--queue=}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if options[0].Name != "queue" {
		t.Errorf("name = %q, want queue", options[0].Name)
	}
	if !options[0].AcceptValue() {
		t.Error("{--queue=} takes a value")
	}
	if options[0].IsArray() {
		t.Error("{--queue=} takes one value")
	}
}

func TestParseReadsAnOptionWithADefault(t *testing.T) {
	_, _, options, err := console.Parse("mail:send {--queue=default}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if options[0].Name != "queue" {
		t.Errorf("name = %q, want queue", options[0].Name)
	}
	if got := options[0].Default; !slices.Equal(got, []string{"default"}) {
		t.Errorf("default = %v, want [default]", got)
	}
}

func TestParseReadsARepeatableOption(t *testing.T) {
	_, _, options, err := console.Parse("mail:send {--queue=*}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if options[0].Name != "queue" {
		t.Errorf("name = %q, want queue", options[0].Name)
	}
	if !options[0].IsArray() {
		t.Error("{--queue=*} may be repeated")
	}
}

func TestParseReadsARepeatableOptionWithDefaults(t *testing.T) {
	_, _, options, err := console.Parse("mail:send {--queue=*high,low}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !options[0].IsArray() {
		t.Error("{--queue=*a,b} may be repeated")
	}
	if got := options[0].Default; !slices.Equal(got, []string{"high", "low"}) {
		t.Errorf("default = %v, want [high low]", got)
	}
}

func TestParseReadsAShortcut(t *testing.T) {
	_, _, options, err := console.Parse("mail:send {--Q|queue=}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if options[0].Name != "queue" {
		t.Errorf("name = %q, want queue", options[0].Name)
	}
	if options[0].Shortcut != "Q" {
		t.Errorf("shortcut = %q, want Q", options[0].Shortcut)
	}
}

func TestParseReadsTheDescriptionAfterASpacedColon(t *testing.T) {
	_, arguments, options, err := console.Parse(
		"mail:send {user : The user ID} {--queue= : The queue to push onto}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if arguments[0].Name != "user" {
		t.Errorf("argument name = %q, want user", arguments[0].Name)
	}
	if arguments[0].Description != "The user ID" {
		t.Errorf("argument description = %q, want The user ID", arguments[0].Description)
	}
	if options[0].Name != "queue" {
		t.Errorf("option name = %q, want queue", options[0].Name)
	}
	if options[0].Description != "The queue to push onto" {
		t.Errorf("option description = %q, want The queue to push onto", options[0].Description)
	}
}

func TestParseKeepsAColonThatIsNotASeparator(t *testing.T) {
	// The separator is a colon with spaces on both sides, so a default that
	// contains one survives.
	_, arguments, _, err := console.Parse("mail:send {at=10:30}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if arguments[0].Name != "at" {
		t.Errorf("name = %q, want at", arguments[0].Name)
	}
	if got := arguments[0].Default; !slices.Equal(got, []string{"10:30"}) {
		t.Errorf("default = %v, want [10:30]", got)
	}
}

func TestParseReadsEveryTokenOfARealSignature(t *testing.T) {
	name, arguments, options, err := console.Parse(
		"mail:send {user : The user ID} {copies?*} {--Q|queue=default : The queue} {--force}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if name != "mail:send" {
		t.Errorf("name = %q, want mail:send", name)
	}
	if len(arguments) != 2 {
		t.Fatalf("got %d arguments, want 2", len(arguments))
	}
	if len(options) != 2 {
		t.Fatalf("got %d options, want 2", len(options))
	}
	if arguments[0].Name != "user" || arguments[1].Name != "copies" {
		t.Errorf("arguments are %q and %q, want user and copies", arguments[0].Name, arguments[1].Name)
	}
	if options[0].Name != "queue" || options[1].Name != "force" {
		t.Errorf("options are %q and %q, want queue and force", options[0].Name, options[1].Name)
	}
}

func TestMustParsePanicsOnASignatureThatDoesNotParse(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustParse returned, and a command nobody can call must not reach the registry")
		}
	}()
	console.MustParse("{user}")
}
