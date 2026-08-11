package console_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/console"
)

// bind parses a signature and binds a command line to it, which is the pair
// every test below needs.
func bind(t *testing.T, signature string, argv ...string) *console.Input {
	t.Helper()

	_, arguments, options, err := console.Parse(signature)
	if err != nil {
		t.Fatalf("Parse(%q): %v", signature, err)
	}
	in := console.NewInput(arguments, options)
	if err := in.Parse(argv); err != nil {
		t.Fatalf("Parse(%v): %v", argv, err)
	}
	return in
}

func TestInputBindsARequiredArgument(t *testing.T) {
	in := bind(t, "mail:send {user}", "42")

	if !in.HasArgument("user") {
		t.Fatal("user is declared and HasArgument said no")
	}
	if got := in.Argument("user").String(); got != "42" {
		t.Errorf("user = %q, want 42", got)
	}
}

func TestInputRefusesAMissingRequiredArgument(t *testing.T) {
	_, arguments, options, err := console.Parse("mail:send {user}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := console.NewInput(arguments, options).Parse(nil); err == nil {
		t.Error("a missing required argument was accepted, and the command would read an empty string")
	}
}

func TestInputFallsBackToTheDefaultOfAnOptionalArgument(t *testing.T) {
	in := bind(t, "mail:send {user=guest}")

	if got := in.Argument("user").String(); got != "guest" {
		t.Errorf("user = %q, want guest", got)
	}
	if in.Argument("user").Present() {
		t.Error("a default is not the same as having been given")
	}
}

func TestInputCollectsAnArrayArgument(t *testing.T) {
	in := bind(t, "mail:send {users*}", "a", "b", "c")

	if got := in.Argument("users").Slice(); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("users = %v, want [a b c]", got)
	}
}

func TestInputRefusesAnEmptyRequiredArrayArgument(t *testing.T) {
	_, arguments, options, err := console.Parse("mail:send {users*}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := console.NewInput(arguments, options).Parse(nil); err == nil {
		t.Error("{users*} needs at least one operand and none was refused")
	}
}

func TestInputAcceptsAnEmptyOptionalArrayArgument(t *testing.T) {
	in := bind(t, "mail:send {users?*}")

	if got := in.Argument("users").Slice(); len(got) != 0 {
		t.Errorf("users = %v, want empty", got)
	}
}

func TestInputRefusesMoreOperandsThanTheSignatureDeclared(t *testing.T) {
	_, arguments, options, err := console.Parse("mail:send {user}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := console.NewInput(arguments, options).Parse([]string{"a", "b"}); err == nil {
		t.Error("an undeclared operand was accepted, and it would be silently dropped")
	}
}

func TestInputBindsABooleanFlag(t *testing.T) {
	given := bind(t, "mail:send {--force}", "--force")
	if !given.Option("force").Bool() {
		t.Error("--force was given and read as false")
	}

	omitted := bind(t, "mail:send {--force}")
	if omitted.Option("force").Bool() {
		t.Error("--force was not given and read as true")
	}
}

func TestInputBindsAnOptionWrittenWithAnEqualsSign(t *testing.T) {
	in := bind(t, "mail:send {--queue=}", "--queue=high")

	if got := in.Option("queue").String(); got != "high" {
		t.Errorf("queue = %q, want high", got)
	}
}

func TestInputBindsAnOptionWrittenAsTwoTokens(t *testing.T) {
	in := bind(t, "mail:send {--queue=}", "--queue", "high")

	if got := in.Option("queue").String(); got != "high" {
		t.Errorf("queue = %q, want high", got)
	}
}

func TestInputDoesNotSwallowTheNextFlagAsAValue(t *testing.T) {
	in := bind(t, "mail:send {--queue=} {--force}", "--queue", "--force")

	if got := in.Option("queue").String(); got != "" {
		t.Errorf("queue = %q, want empty: --force is a flag, not the queue name", got)
	}
	if !in.Option("force").Bool() {
		t.Error("--force was consumed as a value instead of read as a flag")
	}
}

func TestInputFallsBackToTheDefaultOfAnOption(t *testing.T) {
	in := bind(t, "mail:send {--queue=default}")

	if got := in.Option("queue").String(); got != "default" {
		t.Errorf("queue = %q, want default", got)
	}
}

func TestInputCollectsARepeatedOption(t *testing.T) {
	in := bind(t, "mail:send {--queue=*}", "--queue=high", "--queue=low")

	if got := in.Option("queue").Slice(); !slices.Equal(got, []string{"high", "low"}) {
		t.Errorf("queue = %v, want [high low]", got)
	}
}

func TestInputReplacesTheDefaultsOfARepeatedOptionRatherThanAddingToThem(t *testing.T) {
	in := bind(t, "mail:send {--queue=*high,low}", "--queue=only")

	if got := in.Option("queue").Slice(); !slices.Equal(got, []string{"only"}) {
		t.Errorf("queue = %v, want [only]: a value given replaces the defaults", got)
	}
}

func TestInputBindsAShortcut(t *testing.T) {
	joined := bind(t, "mail:send {--Q|queue=}", "-Qhigh")
	if got := joined.Option("queue").String(); got != "high" {
		t.Errorf("-Qhigh gave queue = %q, want high", got)
	}

	split := bind(t, "mail:send {--Q|queue=}", "-Q", "high")
	if got := split.Option("queue").String(); got != "high" {
		t.Errorf("-Q high gave queue = %q, want high", got)
	}
}

func TestInputUnbundlesShortFlags(t *testing.T) {
	in := bind(t, "mail:send {--f|force} {--d|dry}", "-fd")

	if !in.Option("force").Bool() || !in.Option("dry").Bool() {
		t.Error("-fd is two flags, and one of them was not set")
	}
}

func TestInputTreatsEverythingAfterADoubleDashAsAnOperand(t *testing.T) {
	in := bind(t, "mail:send {user}", "--", "--force")

	if got := in.Argument("user").String(); got != "--force" {
		t.Errorf("user = %q, want --force", got)
	}
}

func TestInputRefusesAnUndeclaredOption(t *testing.T) {
	_, arguments, options, err := console.Parse("mail:send")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := console.NewInput(arguments, options).Parse([]string{"--nope"}); err == nil {
		t.Error("an undeclared option was accepted, and a typo would be silently ignored")
	}
}

func TestValueBoolReadsTheSpellingsOfNo(t *testing.T) {
	for _, given := range []string{"0", "false", "no", "off", ""} {
		in := bind(t, "mail:send {--flag=}", "--flag="+given)
		if in.Option("flag").Bool() {
			t.Errorf("--flag=%q read as true", given)
		}
	}
	for _, given := range []string{"1", "true", "yes", "anything"} {
		in := bind(t, "mail:send {--flag=}", "--flag="+given)
		if !in.Option("flag").Bool() {
			t.Errorf("--flag=%q read as false", given)
		}
	}
}

func TestValueIntReadsANumberAndRefusesTheRest(t *testing.T) {
	in := bind(t, "mail:send {count}", "7")

	got, err := in.Argument("count").Int()
	if err != nil {
		t.Fatalf("Int: %v", err)
	}
	if got != 7 {
		t.Errorf("count = %d, want 7", got)
	}

	bad := bind(t, "mail:send {count}", "seven")
	if _, err := bad.Argument("count").Int(); err == nil {
		t.Error("\"seven\" was read as a number")
	}
}

func TestAnApplicationBindsTheSignatureBeforeTheCommandRuns(t *testing.T) {
	var out, errOut bytes.Buffer

	var user, queue string
	var force bool

	app := console.NewApplication(&out, &errOut, strings.NewReader("")).Add(console.Command{
		Signature:   "mail:send {user : The user ID} {--queue=default} {--force}",
		Description: "send the queued mail",
		Run: func(_ context.Context, o *console.IO) error {
			user = o.Argument("user").String()
			queue = o.Option("queue").String()
			force = o.Option("force").Bool()
			return nil
		},
	})

	if err := app.Handle(t.Context(), []string{"mail:send", "42", "--force"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if user != "42" {
		t.Errorf("user = %q, want 42", user)
	}
	if queue != "default" {
		t.Errorf("queue = %q, want default", queue)
	}
	if !force {
		t.Error("--force was given and the command read false")
	}
}

func TestAnApplicationTakesTheCommandNameFromTheSignature(t *testing.T) {
	var out, errOut bytes.Buffer

	app := console.NewApplication(&out, &errOut, strings.NewReader("")).Add(console.Command{
		Signature: "mail:send {user}",
		Run:       func(context.Context, *console.IO) error { return nil },
	})

	if !app.Has("mail:send") {
		t.Errorf("the command was registered under %v, and mail:send is what the signature says", app.Names())
	}
}

func TestAnApplicationRefusesACommandLineTheSignatureDoesNotAccept(t *testing.T) {
	var out, errOut bytes.Buffer

	ran := false
	app := console.NewApplication(&out, &errOut, strings.NewReader("")).Add(console.Command{
		Signature: "mail:send {user}",
		Run:       func(context.Context, *console.IO) error { ran = true; return nil },
	})

	if err := app.Handle(t.Context(), []string{"mail:send"}); err == nil {
		t.Error("a missing required argument was accepted")
	}
	if ran {
		t.Error("the command ran with an argument it says is required")
	}
}
