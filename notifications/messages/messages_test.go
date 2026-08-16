package messages_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/notifications/messages"
)

func TestALineAfterTheActionLandsAfterIt(t *testing.T) {
	m := messages.NewMail().Subject("Reset your password").
		Greeting("Hello Ada").
		Line("Somebody asked to reset your password.").
		Action("Reset password", "https://example.com/reset/abc").
		Line("If it was not you, ignore this message.").
		Salutation("Regards")

	if len(m.IntroLines) != 1 || m.IntroLines[0] != "Somebody asked to reset your password." {
		t.Fatalf("intro = %v", m.IntroLines)
	}
	if len(m.OutroLines) != 1 || m.OutroLines[0] != "If it was not you, ignore this message." {
		t.Fatalf("outro = %v", m.OutroLines)
	}
	if m.ActionText != "Reset password" {
		t.Fatalf("action = %q -> %q", m.ActionText, m.ActionURL)
	}
}

func TestTheBuildersDoNotMutateTheMessageTheyWereCalledOn(t *testing.T) {
	base := messages.NewMail().Subject("Hello").Line("one")
	a := base.Line("two")
	b := base.Line("three")

	if len(base.IntroLines) != 1 {
		t.Fatalf("the base message grew to %v", base.IntroLines)
	}
	if a.IntroLines[1] != "two" || b.IntroLines[1] != "three" {
		t.Fatalf("the two branches share a slice: %v and %v", a.IntroLines, b.IntroLines)
	}
}

// TestWithCollapsesWhitespace checks that a paragraph written across several
// source lines arrives as one sentence, not as three with the Go indentation in
// the middle of it.
func TestWithCollapsesWhitespace(t *testing.T) {
	m := messages.NewMail().Line(`Somebody asked
		to reset your password.`)
	if m.IntroLines[0] != "Somebody asked to reset your password." {
		t.Fatalf("line = %q", m.IntroLines[0])
	}
}

func TestLinesAndTheirConditionalForms(t *testing.T) {
	m := messages.NewMail().
		Lines("one", "two").
		LineIf(false, "not this one").
		LineIf(true, "three").
		LinesIf(false, "nor these", "at all").
		LinesIf(true, "four")

	if got := strings.Join(m.IntroLines, ","); got != "one,two,three,four" {
		t.Fatalf("lines = %q", got)
	}
}

func TestPlainTextRendersEveryPart(t *testing.T) {
	text := messages.NewMail().Subject("Reset your password").
		Greeting("Hello Ada").
		Line("Somebody asked to reset your password.").
		Action("Reset password", "https://example.com/reset/abc").
		Line("The link expires in an hour.").
		Salutation("Regards").
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

func TestRenderEscapesWhatTheApplicationPutInTheMessage(t *testing.T) {
	html, err := messages.NewMail().Subject("Hello").
		Greeting("Hello <script>alert(1)</script>").
		Line("Your invoice is ready.").
		Action("View", "https://example.com/i/1").
		Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("the greeting was not escaped:\n%s", html)
	}
	if !strings.Contains(html, `href="https://example.com/i/1"`) {
		t.Fatalf("the action link is missing:\n%s", html)
	}
}

func TestRenderRefusesAMessageTheViewLayerOwns(t *testing.T) {
	_, err := messages.NewMail().Subject("Hello").View("mail.invoice", nil).Render()
	if err == nil || !strings.Contains(err.Error(), "mail.invoice") {
		t.Fatalf("err = %v, want it to name the template", err)
	}
}

func TestViewAndMarkdownReplaceEachOther(t *testing.T) {
	m := messages.NewMail().Markdown("mail.md", map[string]any{"a": 1}).View("mail.html", nil)
	if m.MarkdownView != "" || m.ViewName != "mail.html" {
		t.Fatalf("view = %q, markdown = %q", m.ViewName, m.MarkdownView)
	}
	m = m.Markdown("mail.md", nil)
	if m.ViewName != "" || m.MarkdownView != "mail.md" {
		t.Fatalf("view = %q, markdown = %q", m.ViewName, m.MarkdownView)
	}
	if m = m.Template("mail.other").Theme("dark"); m.MarkdownView != "mail.other" || m.ThemeName != "dark" {
		t.Fatalf("template = %q, theme = %q", m.MarkdownView, m.ThemeName)
	}
}

func TestTheEnvelopeFields(t *testing.T) {
	m := messages.NewMail().
		From("billing@example.com", "Billing").
		ReplyTo("support@example.com", "Support").
		CC("ada@example.com", "Ada").
		BCC("audit@example.com", "").
		Mailer("postmark").
		Tag("invoice").
		Metadata("tenant", "acme").
		Priority(1).
		Attach("/tmp/invoice.pdf", messages.Attachment{MIME: "application/pdf"}).
		AttachData([]byte("hello"), "note.txt", messages.Attachment{}).
		AttachMany(messages.Attachment{File: "/tmp/a.png"}, messages.Attachment{File: "/tmp/b.png"})

	if m.FromAddress.Address != "billing@example.com" || m.FromAddress.Name != "Billing" {
		t.Errorf("from = %+v", m.FromAddress)
	}
	if len(m.ReplyToAddresses) != 1 || len(m.CCAddresses) != 1 || len(m.BCCAddresses) != 1 {
		t.Errorf("copies = %+v %+v %+v", m.ReplyToAddresses, m.CCAddresses, m.BCCAddresses)
	}
	if m.MailerName != "postmark" || m.PriorityLevel != 1 {
		t.Errorf("mailer = %q, priority = %d", m.MailerName, m.PriorityLevel)
	}
	if len(m.Tags) != 1 || m.MetadataHeaders["tenant"] != "acme" {
		t.Errorf("tags = %v, metadata = %v", m.Tags, m.MetadataHeaders)
	}
	if len(m.Attachments) != 3 || len(m.RawAttachments) != 1 {
		t.Errorf("attachments = %d files and %d in memory", len(m.Attachments), len(m.RawAttachments))
	}
	if string(m.RawAttachments[0].Data) != "hello" || m.RawAttachments[0].Name != "note.txt" {
		t.Errorf("raw attachment = %+v", m.RawAttachments[0])
	}

	called := 0
	m = m.WithSymfonyMessage(func(map[string]string) { called++ })
	if len(m.Callbacks) != 1 {
		t.Fatalf("%d callbacks registered, want 1", len(m.Callbacks))
	}
	m.Callbacks[0](map[string]string{})
	if called != 1 {
		t.Errorf("the callback ran %d times", called)
	}
}

func TestToArrayAndDataCarryTheMessage(t *testing.T) {
	m := messages.NewMail().Subject("Paid").Greeting("Hi").
		Line("Thanks.").Action("Write to us", "mailto:billing@example.com").
		Salutation("Regards")

	got := m.ToArray()
	if got["subject"] != "Paid" || got["greeting"] != "Hi" || got["salutation"] != "Regards" {
		t.Errorf("array = %v", got)
	}
	if got["action_url"] != "mailto:billing@example.com" || got["displayable_action_url"] != "billing@example.com" {
		t.Errorf("the displayable url is %v", got["displayable_action_url"])
	}

	data := m.View("mail.invoice", map[string]any{"subject": "overridden", "n": 7}).Data()
	if data["subject"] != "overridden" || data["n"] != 7 {
		t.Errorf("the view data did not win: %v", data)
	}
}

func TestValidateRefusesAMessageNobodyCouldUse(t *testing.T) {
	cases := map[string]messages.Mail{
		"no subject": messages.NewMail().Line("something happened"),
		"no body":    messages.NewMail().Subject("Something happened"),
		"a relative action link": messages.NewMail().Subject("Reset").
			Line("here").Action("Reset", "/reset/abc"),
	}
	for name, m := range cases {
		if err := m.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	ok := messages.NewMail().Subject("Reset").Line("here").Action("Reset", "https://example.com/reset")
	if err := ok.Validate(); err != nil {
		t.Fatalf("a complete message was refused: %v", err)
	}
	// A message whose body is a named template has a body.
	if err := (messages.NewMail().Subject("Reset").View("mail.reset", nil)).Validate(); err != nil {
		t.Fatalf("a message rendered from a view was refused: %v", err)
	}
}

func TestToneDefaultsToInfo(t *testing.T) {
	if got := messages.NewMail().Tone(); got != messages.LevelInfo {
		t.Fatalf("tone = %q", got)
	}
	if got := messages.NewMail().Success().Tone(); got != messages.LevelSuccess {
		t.Fatalf("tone = %q", got)
	}
	if got := messages.NewMail().Error().Tone(); got != messages.LevelError {
		t.Fatalf("tone = %q", got)
	}
	if got := messages.NewMail().Level(messages.LevelSuccess).Tone(); got != messages.LevelSuccess {
		t.Fatalf("tone = %q", got)
	}
}

func TestPayloadsEncode(t *testing.T) {
	raw, err := messages.NewDatabase(map[string]string{"invoice": "2026-114"}).JSON()
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

	if _, err := (messages.NewBroadcast(make(chan int)).JSON()); err == nil {
		t.Fatal("a payload that cannot be encoded should be an error")
	}

	b := messages.NewBroadcast(nil).Data(map[string]int{"n": 1}).OnConnection("redis").OnQueue("push")
	if b.Connection != "redis" || b.Queue != "push" {
		t.Errorf("broadcast routing = %+v", b)
	}
	raw, err = b.JSON()
	if err != nil || string(raw) != `{"n":1}` {
		t.Fatalf("payload = %s (%v)", raw, err)
	}
}

func TestActionEmpty(t *testing.T) {
	if !(messages.Action{}).Empty() {
		t.Error("the zero action is not empty")
	}
	if messages.NewAction("Go", "https://example.com").Empty() {
		t.Error("a complete action reads as empty")
	}
	if !messages.NewAction("Go", "").Empty() {
		t.Error("an action with no link reads as complete")
	}
}
