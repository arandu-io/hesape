package mail

// TextMessage is the message as the plain-text part sees it.
//
// It exists for one reason: [TextMessage.Embed] and [TextMessage.EmbedData]
// answer the empty string. A markdown mailable renders the same template twice,
// once as HTML and once as text, and the text pass must not emit a cid:
// reference for an image nothing will display -- it would arrive as the literal
// text "cid:9f3c...@arandu" in the middle of a sentence.
//
// Everything else is the embedded [Message], unchanged.
type TextMessage struct {
	*Message
}

// NewTextMessage wraps a message for the plain-text pass.
func NewTextMessage(m *Message) *TextMessage { return &TextMessage{Message: m} }

// Embed answers the empty string: there is nothing to embed in plain text.
func (*TextMessage) Embed(any) string { return "" }

// EmbedData answers the empty string, for the same reason as Embed.
func (*TextMessage) EmbedData([]byte, string, ...string) string { return "" }
