// Package messages is what a notification looks like on a channel.
//
// It mirrors Illuminate\Notifications\Messages. The files it answers to, in the
// clone at laravel_illuminate/notifications (Laravel 13,
// illuminate/notifications ^13.0):
//
//	Messages/SimpleMessage.php     -> Mail
//	Messages/MailMessage.php       -> Mail
//	Messages/DatabaseMessage.php   -> Database
//	Messages/BroadcastMessage.php  -> Broadcast
//	Action.php                     -> Action
//
// SimpleMessage and MailMessage are one type here. The split exists in PHP so
// that a channel with no view layer can reuse the lines, and there is one mail
// channel in this collection.
//
// A message is data. It does not know how to reach anybody and holds no
// transport, which is what lets a test build one and read it, and what lets the
// same Mail be rendered to HTML by Render and to text by PlainText without
// either of them being the authority.
//
// # Why Mail is structured rather than a body
//
// Laravel builds the HTML from a markdown template and a theme. RULE 13 rules
// the theme out -- it is a second asset pipeline -- and RULE 9 rules out having
// both a markdown path and a view path to draw the same message. So a mail
// notification carries a greeting, some lines, at most one action and a
// salutation: Render draws the HTML from those fields, PlainText renders the
// text part from the same fields, and the two cannot disagree about what the
// message said.
//
// View, Text, Markdown, Template and Theme are here and are carried: they name
// a template for the view layer to render, which is a name and not an asset
// pipeline. A message that names one is not rendered by Render, and Render says
// so rather than quietly dropping it.
//
// A message that needs a layout of its own is not a notification. It is a
// Mailable, and hesape/mail is where those live.
package messages
