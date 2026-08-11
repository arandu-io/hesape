package mail

// Content is Illuminate\Mail\Mailables\Content: what the body is made of.
//
// # View names, not bodies
//
// View, HTML, Text and Markdown are all *view names*, exactly as they are in
// Illuminate: Content::$html is documented there as "alternative syntax for
// view", and Content::$text is "the Blade view that represents the text version
// of the message". Only HTMLString is a body -- Illuminate's $htmlString, "the
// pre-rendered HTML of the message".
//
// This package used to read HTML as a literal body and carry an invented
// TextView field for the text view name. Both were renamed to Illuminate's
// spelling: a field with the right name and the wrong meaning is worse than a
// missing one, because nobody checks.
//
// The fluent setters Illuminate puts on Content -- view(), html(), text(),
// markdown(), htmlString(), with() -- have no Go twin: PHP lets a property and
// a method share a name and Go does not, and a struct literal is what PHP's
// named arguments are. The same methods exist on [PendingMail], which is where
// a Laravel developer calls them from anyway.
type Content struct {
	// View is the HTML part, by the name the view is registered under.
	View string

	// HTML is Illuminate's alternative spelling of View, and is also a view
	// name. View wins when both are set.
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

	// With is what the views render from, and is Illuminate's $with.
	With any
}
