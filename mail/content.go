package mail

// Content is what the body is made of.
//
// # View names, not bodies
//
// View, HTML, Text and Markdown are all *view names*. Only HTMLString is a
// body. A field with the right name and the wrong meaning is worse than a
// missing one, because nobody checks.
//
// There are no fluent setters here: a Go type cannot carry a method and a field
// of the same name, and a struct literal is how a content is built. The setters
// live on [PendingMail].
type Content struct {
	// View is the HTML part, by the name the view is registered under.
	View string

	// HTML is the alternative spelling of View, and is also a view name. View
	// wins when both are set.
	HTML string

	// Text is the plain-text part, by view name.
	//
	// It matters because a message with no text part is filed as spam more
	// often, and every client that cannot render HTML shows nothing at all.
	Text string

	// Markdown is the markdown view the message is drawn from, rendered by
	// [Markdown] into both parts at once.
	Markdown string

	// HTMLString is the HTML part as a body that is already HTML by the time it
	// reaches here: one a module composed, or one that arrived from somewhere
	// else.
	//
	// It is not an invitation to build a body with Sprintf. A string
	// concatenated from user input is an injection that a mail client renders,
	// and a view escapes by construction; this field exists for HTML that is
	// already safe, and the caller owns that claim.
	HTMLString string

	// With is what the views render from. Nil means the mailable itself.
	With any
}
