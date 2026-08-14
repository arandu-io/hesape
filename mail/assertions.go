package mail

import (
	"fmt"
	"strings"
	"testing"
)

// The assertions Illuminate puts on Mailable.
//
// They are here rather than on [PendingMail] because every one of them reads
// the message as it will actually go out -- Illuminate calls renderForAssertions
// first, and a message is what that produces. Build one with [Mailer.Build] and
// assert against it:
//
//	msg, err := mailer.Build(ctx, OrderShipped{order})
//	msg.AssertHasTo(t, "you@example.test").
//		AssertSeeInHTML(t, "Your order has shipped")
//
// Each one takes *testing.T, calls t.Helper first so the failure points at the
// test rather than at this file, and says what it found and not only what it
// wanted. "Did not see expected recipient" without the actual list is a message
// that sends somebody back to the debugger.

// AssertFrom asserts the message is from the given address.
//
// AssertFrom is Mailable::assertFrom.
func (m *Message) AssertFrom(t *testing.T, address string, name ...string) *Message {
	t.Helper()
	if !m.HasFrom(address, name...) {
		t.Errorf("Email was not from expected address.\nExpected: [%s]\nActual: [%s]",
			formatExpectedAddress(address, name...), formatAddresses([]Address{m.From}))
	}
	return m
}

// AssertTo asserts the address is a recipient.
//
// AssertTo is Mailable::assertTo.
func (m *Message) AssertTo(t *testing.T, address string, name ...string) *Message {
	t.Helper()
	if !m.HasTo(address, name...) {
		t.Errorf("Did not see expected recipient in email 'to' recipients.\nExpected: [%s]\nActual: [%s]",
			formatExpectedAddress(address, name...), formatAddresses(m.To))
	}
	return m
}

// AssertHasTo asserts the address is a recipient, and is Illuminate's alias of
// AssertTo.
//
// AssertHasTo is Mailable::assertHasTo.
func (m *Message) AssertHasTo(t *testing.T, address string, name ...string) *Message {
	t.Helper()
	return m.AssertTo(t, address, name...)
}

// AssertHasCC asserts the address is a carbon copy recipient. Illuminate spells
// it assertHasCc.
//
// AssertHasCC is Mailable::assertHasCc.
func (m *Message) AssertHasCC(t *testing.T, address string, name ...string) *Message {
	t.Helper()
	if !m.HasCC(address, name...) {
		t.Errorf("Did not see expected recipient in email 'cc' recipients.\nExpected: [%s]\nActual: [%s]",
			formatExpectedAddress(address, name...), formatAddresses(m.CC))
	}
	return m
}

// AssertHasBCC asserts the address is a blind carbon copy recipient.
// Illuminate spells it assertHasBcc.
//
// AssertHasBCC is Mailable::assertHasBcc.
func (m *Message) AssertHasBCC(t *testing.T, address string, name ...string) *Message {
	t.Helper()
	if !m.HasBCC(address, name...) {
		t.Errorf("Did not see expected recipient in email 'bcc' recipients.\nExpected: [%s]\nActual: [%s]",
			formatExpectedAddress(address, name...), formatAddresses(m.BCC))
	}
	return m
}

// AssertHasReplyTo asserts the address is a "reply to" recipient.
//
// AssertHasReplyTo is Mailable::assertHasReplyTo.
func (m *Message) AssertHasReplyTo(t *testing.T, address string, name ...string) *Message {
	t.Helper()
	if !m.HasReplyTo(address, name...) {
		t.Errorf("Did not see expected address as email 'reply to' recipient.\nExpected: [%s]\nActual: [%s]",
			formatExpectedAddress(address, name...), formatAddresses(m.ReplyTo))
	}
	return m
}

// AssertHasSubject asserts the message carries exactly this subject.
//
// AssertHasSubject is Mailable::assertHasSubject.
func (m *Message) AssertHasSubject(t *testing.T, subject string) *Message {
	t.Helper()
	if !m.HasSubject(subject) {
		t.Errorf("Email subject does not match expected value.\nExpected: [%s]\nActual: [%s]",
			subject, m.Subject)
	}
	return m
}

// AssertSeeInHTML asserts the text appears in the HTML part.
//
// Illuminate spells it assertSeeInHtml and escapes the needle unless told not
// to, because the body it is searching is escaped: asserting on "Ada & Co"
// against a body containing "Ada &amp; Co" fails for a reason that has nothing
// to do with the message. The optional escape argument is PHP's $escape = true.
//
// AssertSeeInHTML is Mailable::assertSeeInHtml.
func (m *Message) AssertSeeInHTML(t *testing.T, s string, escape ...bool) *Message {
	t.Helper()
	needle := escaped(s, escape...)
	if !strings.Contains(m.HTML, needle) {
		t.Errorf("Did not see expected text [%s] within email body.\nActual body: %s", needle, m.HTML)
	}
	return m
}

// AssertDontSeeInHTML asserts the text does not appear in the HTML part.
// Illuminate spells it assertDontSeeInHtml.
//
// AssertDontSeeInHTML is Mailable::assertDontSeeInHtml.
func (m *Message) AssertDontSeeInHTML(t *testing.T, s string, escape ...bool) *Message {
	t.Helper()
	needle := escaped(s, escape...)
	if strings.Contains(m.HTML, needle) {
		t.Errorf("Saw unexpected text [%s] within email body.\nActual body: %s", needle, m.HTML)
	}
	return m
}

// AssertSeeInOrderInHTML asserts the texts appear in the HTML part, in this
// order. Illuminate spells it assertSeeInOrderInHtml.
//
// AssertSeeInOrderInHTML is Mailable::assertSeeInOrderInHtml.
func (m *Message) AssertSeeInOrderInHTML(t *testing.T, strs []string, escape ...bool) *Message {
	t.Helper()
	needles := make([]string, len(strs))
	for i, s := range strs {
		needles[i] = escaped(s, escape...)
	}
	if missing, ok := seeInOrder(m.HTML, needles); !ok {
		t.Errorf("Did not see expected text [%s] in order within email body.\nActual body: %s",
			missing, m.HTML)
	}
	return m
}

// AssertSeeInText asserts the text appears in the plain-text part. Nothing is
// escaped: the plain-text part is not HTML.
//
// AssertSeeInText is Mailable::assertSeeInText.
func (m *Message) AssertSeeInText(t *testing.T, s string) *Message {
	t.Helper()
	if !strings.Contains(m.Text, s) {
		t.Errorf("Did not see expected text [%s] within text email body.\nActual body: %s", s, m.Text)
	}
	return m
}

// AssertDontSeeInText asserts the text does not appear in the plain-text part.
//
// AssertDontSeeInText is Mailable::assertDontSeeInText.
func (m *Message) AssertDontSeeInText(t *testing.T, s string) *Message {
	t.Helper()
	if strings.Contains(m.Text, s) {
		t.Errorf("Saw unexpected text [%s] within text email body.\nActual body: %s", s, m.Text)
	}
	return m
}

// AssertSeeInOrderInText asserts the texts appear in the plain-text part, in
// this order.
//
// AssertSeeInOrderInText is Mailable::assertSeeInOrderInText.
func (m *Message) AssertSeeInOrderInText(t *testing.T, strs []string) *Message {
	t.Helper()
	if missing, ok := seeInOrder(m.Text, strs); !ok {
		t.Errorf("Did not see expected text [%s] in order within text email body.\nActual body: %s",
			missing, m.Text)
	}
	return m
}

// AssertHasAttachment asserts the file is attached.
//
// AssertHasAttachment is Mailable::assertHasAttachment.
func (m *Message) AssertHasAttachment(t *testing.T, file any, options ...AttachOptions) *Message {
	t.Helper()
	if !m.HasAttachment(file, options...) {
		t.Errorf("Did not find the expected attachment.\nExpected: [%v]\nActual: [%s]", file, m.attachmentNames())
	}
	return m
}

// AssertHasAttachedData asserts these exact bytes are attached under this name.
//
// AssertHasAttachedData is Mailable::assertHasAttachedData.
func (m *Message) AssertHasAttachedData(t *testing.T, data []byte, name string, options ...AttachOptions) *Message {
	t.Helper()
	if !m.HasAttachedData(data, name, options...) {
		t.Errorf("Did not find the expected attachment.\nExpected: [%s]\nActual: [%s]", name, m.attachmentNames())
	}
	return m
}

// AssertHasAttachmentFromStorage asserts a path on the default disk is
// attached.
//
// AssertHasAttachmentFromStorage is Mailable::assertHasAttachmentFromStorage.
func (m *Message) AssertHasAttachmentFromStorage(t *testing.T, p, name string, options ...AttachOptions) *Message {
	t.Helper()
	if !m.HasAttachmentFromStorage(p, name, options...) {
		t.Errorf("Did not find the expected attachment.\nExpected: [%s]\nActual: [%s]", p, m.attachmentNames())
	}
	return m
}

// AssertHasAttachmentFromStorageDisk asserts a path on the named disk is
// attached.
//
// AssertHasAttachmentFromStorageDisk is
// Mailable::assertHasAttachmentFromStorageDisk.
func (m *Message) AssertHasAttachmentFromStorageDisk(t *testing.T, disk, p, name string, options ...AttachOptions) *Message {
	t.Helper()
	if !m.HasAttachmentFromStorageDisk(disk, p, name, options...) {
		t.Errorf("Did not find the expected attachment.\nExpected: [%s on disk %s]\nActual: [%s]",
			p, disk, m.attachmentNames())
	}
	return m
}

// AssertHasTag asserts the message carries this tag.
//
// AssertHasTag is Mailable::assertHasTag.
func (m *Message) AssertHasTag(t *testing.T, tag string) *Message {
	t.Helper()
	if !m.HasTag(tag) {
		actual := "none"
		if len(m.Tags) > 0 {
			actual = strings.Join(m.Tags, ", ")
		}
		t.Errorf("Did not see expected tag in email tags.\nExpected: [%s]\nActual: [%s]", tag, actual)
	}
	return m
}

// AssertHasMetadata asserts the message carries this key with this value.
//
// AssertHasMetadata is Mailable::assertHasMetadata.
func (m *Message) AssertHasMetadata(t *testing.T, key, value string) *Message {
	t.Helper()
	if !m.HasMetadata(key, value) {
		actual := fmt.Sprintf("key [%s] not found", key)
		if got, ok := m.Metadata[key]; ok {
			actual = fmt.Sprintf("[%s] => [%s]", key, got)
		}
		t.Errorf("Email metadata does not match expected value.\nExpected: [%s] => [%s]\nActual: %s",
			key, value, actual)
	}
	return m
}

func (m *Message) attachmentNames() string {
	names := make([]string, 0, len(m.Attachments)+len(m.RawAttachments)+len(m.DiskAttachments))
	for _, a := range m.Attachments {
		names = append(names, fmt.Sprintf("%v", a.File))
	}
	for _, a := range m.RawAttachments {
		names = append(names, a.Name)
	}
	for _, a := range m.DiskAttachments {
		names = append(names, a.Disk+":"+a.Path)
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func formatExpectedAddress(address string, name ...string) string {
	if len(name) > 0 && name[0] != "" {
		return address + " (" + name[0] + ")"
	}
	return address
}

func formatAddresses(list []Address) string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		if a.Address == "" {
			continue
		}
		if a.Name != "" {
			out = append(out, a.Address+" ("+a.Name+")")
			continue
		}
		out = append(out, a.Address)
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}

// seeInOrder is Illuminate\Testing\Constraints\SeeInOrder: it returns the first
// needle that was not found after the ones before it.
func seeInOrder(haystack string, needles []string) (string, bool) {
	from := 0
	for _, needle := range needles {
		if needle == "" {
			continue
		}
		at := strings.Index(haystack[from:], needle)
		if at < 0 {
			return needle, false
		}
		from += at + len(needle)
	}
	return "", true
}

// escaped is PHP's htmlspecialchars with ENT_QUOTES, which is what
// EncodedHtmlString::convert($string, withQuote: true) does. The ampersand is
// replaced first, or every replacement after it would be escaped again.
func escaped(s string, escape ...bool) string {
	if len(escape) > 0 && !escape[0] {
		return s
	}
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#039;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
