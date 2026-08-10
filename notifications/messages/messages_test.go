package messages_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/notifications/messages"
)

func TestALineAfterTheActionLandsAfterIt(t *testing.T) {
	m := messages.Mail{Subject: "Reset your password"}.
		Greet("Hello Ada").
		Line("Somebody asked to reset your password.").
		Act("Reset password", "https://example.com/reset/abc").
		Line("If it was not you, ignore this message.").
		Close("Regards")

	if len(m.Intro) != 1 || m.Intro[0] != "Somebody asked to reset your password." {
		t.Fatalf("intro = %v", m.Intro)
	}
	if len(m.Outro) != 1 || m.Outro[0] != "If it was not you, ignore this message." {
		t.Fatalf("outro = %v", m.Outro)
	}
	if m.Action.Text != "Reset password" {
		t.Fatalf("action = %+v", m.Action)
	}
}

func TestTheBuildersDoNotMutateTheMessageTheyWereCalledOn(t *testing.T) {
	base := messages.Mail{Subject: "Hello"}.Line("one")
	a := base.Line("two")
	b := base.Line("three")

	if len(base.Intro) != 1 {
		t.Fatalf("the base message grew to %v", base.Intro)
	}
	if a.Intro[1] != "two" || b.Intro[1] != "three" {
		t.Fatalf("the two branches share a slice: %v and %v", a.Intro, b.Intro)
	}
}

func TestPlainTextRendersEveryPart(t *testing.T) {
	text := messages.Mail{Subject: "Reset your password"}.
		Greet("Hello Ada").
		Line("Somebody asked to reset your password.").
		Act("Reset password", "https://example.com/reset/abc").
		Line("The link expires in an hour.").
		Close("Regards").
		PlainText()

	for _, want := range []string{
		"Hello Ada",
		"Somebody asked to reset your password.",
		"Reset password: https://example.com/reset/abc",
		"The link expires in an hour.",
		"Regards",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the text part is missing %q:\n%s", want, text)
		}
	}
	// The link has to survive: a text part with a button and no URL is a
	// message the recipient cannot act on.
	if strings.Count(text, "https://example.com/reset/abc") != 1 {
		t.Fatalf("the link appears %d times:\n%s", strings.Count(text, "https://example.com/reset/abc"), text)
	}
}

func TestValidateRefusesAMessageNobodyCouldUse(t *testing.T) {
	cases := map[string]messages.Mail{
		"no subject": messages.Mail{}.Line("something happened"),
		"no body":    {Subject: "Something happened"},
		"a relative action link": messages.Mail{Subject: "Reset"}.
			Line("here").Act("Reset", "/reset/abc"),
	}
	for name, m := range cases {
		if err := m.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	ok := messages.Mail{Subject: "Reset"}.Line("here").Act("Reset", "https://example.com/reset")
	if err := ok.Validate(); err != nil {
		t.Fatalf("a complete message was refused: %v", err)
	}
}

func TestToneDefaultsToInfo(t *testing.T) {
	if got := (messages.Mail{}).Tone(); got != messages.LevelInfo {
		t.Fatalf("tone = %q", got)
	}
	if got := (messages.Mail{}).Success().Tone(); got != messages.LevelSuccess {
		t.Fatalf("tone = %q", got)
	}
	if got := (messages.Mail{}).Failure().Tone(); got != messages.LevelError {
		t.Fatalf("tone = %q", got)
	}
}

func TestPayloadsEncode(t *testing.T) {
	raw, err := messages.Database{Data: map[string]string{"invoice": "2026-114"}}.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if string(raw) != `{"invoice":"2026-114"}` {
		t.Fatalf("payload = %s", raw)
	}

	empty, err := messages.Database{}.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if string(empty) != "{}" {
		t.Fatalf("an empty payload is %s, and the data column is NOT NULL", empty)
	}

	if _, err := (messages.Broadcast{Data: make(chan int)}.JSON()); err == nil {
		t.Fatal("a payload that cannot be encoded should be an error")
	}
}
