// Package messages is what a notification looks like on a channel: [Mail] for
// an e-mail, [Database] for a stored row, [Broadcast] for a live push, and
// [Action] for the one button a mail message may carry.
//
// A message is data. It does not know how to reach anybody and holds no
// transport, which is what lets a test build one and read it, and what lets the
// same Mail be rendered to HTML by Render and to text by PlainText without
// either of them being the authority.
//
// # Why Mail is structured rather than a body
//
// A mail notification carries a greeting, some lines, at most one action and a
// salutation rather than a body. Render draws the HTML from those fields,
// PlainText renders the text part from the same fields, and the two cannot
// disagree about what the message said. A theme to render a body under would be
// a second asset pipeline, and a markdown path beside a view path would be a
// second way to draw the same message.
//
// View, Text, Markdown, Template and Theme are here and are carried: they name
// a template for the view layer to render, which is a name and not an asset
// pipeline. A message that names one is not rendered by Render, and Render says
// so rather than quietly dropping it.
//
// A message that needs a layout of its own is not a notification. It is a
// Mailable, and hesape/mail is where those live.
package messages
