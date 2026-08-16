// Package html builds escaped HTML fragments as values.
//
// It answers to Illuminate\Html -- [HtmlBuilder] and [FormBuilder], method for
// method, measured against laravel_illuminate/html, which is the archived
// illuminate/html at v5.0.0, the last tag before Laravel deleted the component
// in 5.1.
//
//	builder := html.NewHtmlBuilder(urls)
//	form := html.NewFormBuilder(builder, urls, token)
//
//	open, err := form.Open(html.OpenOptions{Route: []string{"invoices.store"}})
//	// <form accept-charset="UTF-8" action="/invoices" method="POST"><input name="_token" ...>
//
// Import it aliased if the file also imports the standard library's, which the
// directory name collides with by design (ADR 0047):
//
//	import hhtml "github.com/arandu-io/hesape/html"
//
// # This is not a template layer, and here is the line
//
// kyse is the view layer, and RULE 9 says there is one of those. Nothing here
// competes with it: every function returns template.HTML, a value, and none of
// them parses a template, resolves a layout or writes a response. They exist for
// the controls kyse has no component for, and for the string work -- escaping,
// obfuscating, building an attribute list -- that a kyse component does inside
// itself.
//
// # Use the kyse component, not the helper
//
// Where the two overlap, the component wins and this documents the fact rather
// than the taste. A component carries the label, the error message, the ARIA
// wiring and the stylesheet hooks; the helper carries one element.
//
//	FormBuilder.Text, Email, URL, Password, Hidden  →  components.Field
//	FormBuilder.Textarea                            →  components.Textarea
//	FormBuilder.Submit, Button, Reset               →  components.Button
//	FormBuilder.Label                               →  components.Field, which draws its own
//	HtmlBuilder.Image (avatar)                      →  components.Avatar
//
// components.Field is the clearest of them: it reads the message and the typed
// value off the page by field name, so a rejected form comes back with the
// sentence attached to the right input and aria-describedby pointing at it.
// [FormBuilder.Text] draws an input. Reach for the helper only where a component
// would be in the way.
//
// # What has no component, and is why this package exists
//
//	Open, Close, Model, Token          the form element and its hidden fields
//	Select, SelectRange, SelectYear,   a select with option groups, old input
//	SelectMonth, GetSelectOption       and multiple selection
//	Checkbox, Radio, File, Image       the controls Field does not cover
//	the seventeen on HtmlBuilder       links, lists, entities, obfuscation
//
// [FormBuilder.Open] and [FormBuilder.Token] are the pair worth knowing:
// together they are the method spoofing and the CSRF field that every non-GET
// form needs and that nothing else in the framework writes for you.
//
// # Escaping: what this package fixes, and what it does not
//
// PHP concatenates unescaped values into markup in five places, and none of
// them is reproduced here. Each symbol's own comment names its own hole; the
// list is HtmlBuilder::image (the src), HtmlBuilder::link (the href),
// HtmlBuilder::nestedListing (the heading of a nested list),
// FormBuilder::label (the for attribute) and FormBuilder::button (the label).
// [HtmlBuilder.Obfuscate] has a sixth, which is also a data-corrupting bug, and
// its comment has both.
//
// Escaping closes the syntax hole: a value cannot end an attribute or open a
// tag. It does not close the scheme hole. `Link("javascript:alert(1)", ...)`
// produces a link that runs script when clicked, in PHP and here, because the
// URL is well-formed and the danger is in what it means rather than in how it
// is written. 20-components/DOC-hesape-reorganization.md records that this is open
// across the whole view layer, kyse included, and that it is a decision to take
// knowingly rather than by omission. Until it is taken, do not pass a URL that
// came from a person to [HtmlBuilder.Link], [HtmlBuilder.Image] or
// [FormBuilder.Open] without checking its scheme.
//
// # The return types say what is safe
//
// Everything that produces markup returns template.HTML, which tells the view
// layer the value is escaped and must not be escaped again.
// [HtmlBuilder.Decode] returns string, and it is the only one: what it produces
// is markup that has been un-escaped, and calling that safe would invert the
// point of the package.
//
// # What is not here
//
// Four global helpers -- link_to, link_to_asset, link_to_route and
// link_to_action -- are one line each and every one of them is
// `app('html')->...`. They are the facade ADR 0002 removed; the method is on the
// builder, and the builder is a value somebody constructed.
// HtmlServiceProvider::register and ::provides are the container ADR 0001
// removed.
//
// # Where the surface differs from PHP, in one list
//
//   - [Attrs] is a map, so attributes come out sorted rather than in the order
//     they were written. Its own comment has the three PHP array shapes and how
//     each is spelled here.
//   - [HtmlBuilder.Ol], [HtmlBuilder.Ul] take []ListItem and
//     [FormBuilder.Select] takes []Option, because PHP encodes the structure in
//     array keys and Go has no ordered map.
//   - [HtmlBuilder.LinkRoute], [HtmlBuilder.LinkAction], [FormBuilder.Open],
//     [FormBuilder.Model] and [FormBuilder.Token] return (T, error) where PHP
//     throws or fatals.
//   - [FormBuilder.OpenOptions] is a struct where PHP has one array holding
//     five reserved keys and the attributes together.
//   - [Store], [Model] and [UrlGenerator] are declared here rather than
//     imported. hesape has no UrlGenerator, and reaching a model attribute by a
//     name held in a string is reflection, which this framework rejects.
//   - [FormBuilder.SelectMonth] writes English month names. strftime reads the
//     process locale; Go's time package has none, and x/text is refused by
//     ADR 0004.
package html
